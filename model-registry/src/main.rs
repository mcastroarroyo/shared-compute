//! sc-modelctl — build and sign the model manifest served from the registry (Cloudflare
//! R2 at `models.<domain>/manifest.json`).
//!
//! M0: scaffold + the manifest data model. M3 fills in `build` (hash every GGUF, tokenizer,
//! chat template), `sign` (Ed25519), and `verify`.

use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(version, about = "shared-compute model manifest tool")]
struct Cli {
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// Compute hashes for a model directory and emit a manifest entry (M3).
    Build {
        /// Path to a directory of GGUF files.
        path: String,
    },
    /// Ed25519-sign a manifest.json (M3).
    Sign {
        manifest: String,
        #[arg(long)]
        key: String,
    },
    /// Verify a manifest's signature and per-file hashes (M3).
    Verify { manifest: String },
}

/// One model in the registry manifest.
#[derive(serde::Serialize, serde::Deserialize)]
pub struct ManifestEntry {
    pub model_id: String,
    pub architecture: String,
    pub quantization: String,
    pub hardware_class: String,
    pub context_length: u32,
    /// lowercase-hex SHA-256 of the concatenated file hashes
    pub aggregate_sha256: String,
    pub files: Vec<ManifestFile>,
    pub tokenizer_sha256: String,
    pub chat_template_sha256: String,
    pub bytes: u64,
}

#[derive(serde::Serialize, serde::Deserialize)]
pub struct ManifestFile {
    pub name: String,
    pub sha256: String,
    pub bytes: u64,
}

fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();
    match cli.cmd {
        Cmd::Build { .. } | Cmd::Sign { .. } | Cmd::Verify { .. } => {
            anyhow::bail!("sc-modelctl is a scaffold; implemented in milestone M3")
        }
    }
}
