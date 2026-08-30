//! sc-modelctl — build and Ed25519-sign the model registry manifest that providers
//! verify before serving a model.
//!
//!   sc-modelctl keygen  --out registry-signing
//!   sc-modelctl add     --manifest manifest.json --model-id qwen2.5-0.5b-instruct-q4_k_m \
//!                       --arch llama --quant Q4_K_M --hw-class MICRO --ctx 32768 model.gguf
//!   sc-modelctl sign    --manifest manifest.json --key registry-signing.key
//!   sc-modelctl verify  --manifest manifest.json --pub registry-signing.pub
//!
//! Upload manifest.json, manifest.json.sig, and <model_id>/<file> to the registry bucket.

use std::fs;
use std::io::Read;
use std::path::{Path, PathBuf};

use anyhow::{bail, Context, Result};
use clap::{Parser, Subcommand};
use ed25519_dalek::SigningKey;
use rand::rngs::OsRng;
use sc_manifest::{
    aggregate_sha256, encode_key, hex, Manifest, ManifestEntry, ManifestFile, MANIFEST_VERSION,
};
use sha2::{Digest, Sha256};

#[derive(Parser)]
#[command(version, about = "shared-compute model manifest tool")]
struct Cli {
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// Generate an Ed25519 signing keypair (<out>.key seed + <out>.pub base64).
    Keygen {
        #[arg(long)]
        out: PathBuf,
    },
    /// Add or replace a model entry in manifest.json (created if absent).
    Add {
        #[arg(long)]
        manifest: PathBuf,
        #[arg(long)]
        model_id: String,
        #[arg(long, default_value = "llama")]
        arch: String,
        #[arg(long)]
        quant: String,
        #[arg(long)]
        hw_class: String,
        #[arg(long)]
        ctx: u32,
        /// GGUF file(s), in order.
        files: Vec<PathBuf>,
    },
    /// Write manifest.json.sig (base64 Ed25519 over the canonical manifest).
    Sign {
        #[arg(long)]
        manifest: PathBuf,
        #[arg(long)]
        key: PathBuf,
    },
    /// Verify manifest.json.sig against a public key (base64 string or a .pub file).
    Verify {
        #[arg(long)]
        manifest: PathBuf,
        #[arg(long = "pub", value_name = "PUBKEY")]
        pub_key: String,
    },
}

fn main() -> Result<()> {
    match Cli::parse().cmd {
        Cmd::Keygen { out } => keygen(&out),
        Cmd::Add {
            manifest,
            model_id,
            arch,
            quant,
            hw_class,
            ctx,
            files,
        } => add(&manifest, &model_id, &arch, &quant, &hw_class, ctx, &files),
        Cmd::Sign { manifest, key } => sign(&manifest, &key),
        Cmd::Verify { manifest, pub_key } => verify(&manifest, &pub_key),
    }
}

fn keygen(out: &Path) -> Result<()> {
    let sk = SigningKey::generate(&mut OsRng);
    let key_path = out.with_extension("key");
    let pub_path = out.with_extension("pub");
    fs::write(&key_path, sk.to_bytes()).with_context(|| format!("write {}", key_path.display()))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = fs::set_permissions(&key_path, fs::Permissions::from_mode(0o600));
    }
    let vk_b64 = encode_key(sk.verifying_key().as_bytes());
    fs::write(&pub_path, format!("{vk_b64}\n"))?;
    println!("wrote {}  (KEEP SECRET)", key_path.display());
    println!("wrote {}", pub_path.display());
    println!("public key: {vk_b64}");
    println!("\nPin this in the provider + coordinator as SC_REGISTRY_PUBKEY.");
    Ok(())
}

fn sha256_file(path: &Path) -> Result<(String, u64)> {
    let mut f = fs::File::open(path).with_context(|| format!("open {}", path.display()))?;
    let mut h = Sha256::new();
    let mut buf = [0u8; 1 << 20];
    let mut total = 0u64;
    loop {
        let n = f.read(&mut buf)?;
        if n == 0 {
            break;
        }
        h.update(&buf[..n]);
        total += n as u64;
    }
    Ok((hex(&h.finalize()), total))
}

fn load_manifest(path: &Path) -> Result<Manifest> {
    if path.exists() {
        let bytes = fs::read(path)?;
        Ok(serde_json::from_slice(&bytes).context("parse existing manifest")?)
    } else {
        Ok(Manifest {
            version: MANIFEST_VERSION,
            generated_at: now_rfc3339(),
            models: vec![],
        })
    }
}

fn now_rfc3339() -> String {
    time::OffsetDateTime::now_utc()
        .format(&time::format_description::well_known::Rfc3339)
        .unwrap_or_else(|_| "1970-01-01T00:00:00Z".into())
}

#[allow(clippy::too_many_arguments)]
fn add(
    manifest_path: &Path,
    model_id: &str,
    arch: &str,
    quant: &str,
    hw_class: &str,
    ctx: u32,
    files: &[PathBuf],
) -> Result<()> {
    if files.is_empty() {
        bail!("provide at least one GGUF file");
    }
    const CLASSES: [&str; 6] = [
        "MICRO",
        "SMALL",
        "MEDIUM",
        "LARGE",
        "XL",
        "CONFIDENTIAL_GPU",
    ];
    if !CLASSES.contains(&hw_class) {
        bail!("hw-class must be one of {CLASSES:?}");
    }

    let mut mfiles = Vec::new();
    let mut hashes = Vec::new();
    let mut total = 0u64;
    for f in files {
        let (sha, bytes) = sha256_file(f)?;
        let name = f
            .file_name()
            .context("file has no name")?
            .to_string_lossy()
            .into_owned();
        println!("  {name}  {sha}  {bytes} bytes");
        hashes.push(sha.clone());
        total += bytes;
        mfiles.push(ManifestFile {
            name,
            sha256: sha,
            bytes,
        });
    }

    let entry = ManifestEntry {
        model_id: model_id.to_string(),
        architecture: arch.to_string(),
        quantization: quant.to_string(),
        hardware_class: hw_class.to_string(),
        context_length: ctx,
        aggregate_sha256: aggregate_sha256(&hashes),
        files: mfiles,
        tokenizer_sha256: String::new(),
        chat_template_sha256: String::new(),
        total_bytes: total,
    };

    let mut manifest = load_manifest(manifest_path)?;
    manifest.models.retain(|m| m.model_id != model_id);
    manifest.models.push(entry);
    manifest.models.sort_by(|a, b| a.model_id.cmp(&b.model_id));
    manifest.generated_at = now_rfc3339();

    fs::write(manifest_path, serde_json::to_vec_pretty(&manifest)?)?;
    println!(
        "updated {} ({} model{})",
        manifest_path.display(),
        manifest.models.len(),
        if manifest.models.len() == 1 { "" } else { "s" }
    );
    println!(
        "next: sc-modelctl sign --manifest {} --key <key>",
        manifest_path.display()
    );
    Ok(())
}

fn sign(manifest_path: &Path, key_path: &Path) -> Result<()> {
    let manifest = load_manifest(manifest_path)?;
    let seed = fs::read(key_path).with_context(|| format!("read {}", key_path.display()))?;
    let seed: [u8; 32] = seed
        .as_slice()
        .try_into()
        .context("signing key must be 32 bytes")?;
    let sk = SigningKey::from_bytes(&seed);

    let sig = manifest.sign(&sk)?;
    let sig_path = manifest_path.with_file_name(format!(
        "{}.sig",
        manifest_path.file_name().unwrap().to_string_lossy()
    ));
    fs::write(&sig_path, format!("{sig}\n"))?;
    println!("wrote {}", sig_path.display());
    println!("public key: {}", encode_key(sk.verifying_key().as_bytes()));
    Ok(())
}

fn verify(manifest_path: &Path, pub_key: &str) -> Result<()> {
    let manifest = load_manifest(manifest_path)?;
    let sig_path = manifest_path.with_file_name(format!(
        "{}.sig",
        manifest_path.file_name().unwrap().to_string_lossy()
    ));
    let sig =
        fs::read_to_string(&sig_path).with_context(|| format!("read {}", sig_path.display()))?;

    let vk_b64 = if Path::new(pub_key).is_file() {
        fs::read_to_string(pub_key)?.trim().to_string()
    } else {
        pub_key.trim().to_string()
    };
    let vk = sc_manifest::parse_verifying_key(&vk_b64)?;
    manifest.verify(sig.trim(), &vk)?;
    println!("OK: signature valid, {} model(s)", manifest.models.len());
    for m in &manifest.models {
        println!(
            "  {} [{}] {} files, {} bytes",
            m.model_id,
            m.hardware_class,
            m.files.len(),
            m.total_bytes
        );
    }
    Ok(())
}
