//! Local model store + verified download from the signed registry manifest.
//!
//! Registry layout (served from `models.<domain>/`):
//!   manifest.json
//!   manifest.json.sig        (base64 Ed25519 over canonical manifest.json)
//!   <model_id>/<file.name>   (the GGUF file(s))

use std::path::{Path, PathBuf};

use sc_manifest::{parse_verifying_key, Manifest, ManifestEntry};
use sha2::{Digest, Sha256};
use tokio::fs;
use tokio::io::AsyncWriteExt;
use tracing::{info, warn};

pub use sc_manifest;

#[derive(Debug, thiserror::Error)]
pub enum ModelError {
    #[error("model file not found: {0}")]
    NotFound(PathBuf),
    #[error("hash mismatch for {file}: expected {expected}, got {got}")]
    HashMismatch {
        file: String,
        expected: String,
        got: String,
    },
    #[error("manifest: {0}")]
    Manifest(#[from] sc_manifest::ManifestError),
    #[error("model {0} is not in the registry manifest")]
    UnknownModel(String),
    #[error("http {0}")]
    Http(String),
    #[error("not enough free disk space: need {need} bytes, have {have}")]
    DiskSpace { need: u64, have: u64 },
    #[error(transparent)]
    Io(#[from] std::io::Error),
}

/// A directory of local model files, organised as `<root>/<model_id>/<file>`.
pub struct ModelStore {
    root: PathBuf,
}

/// Result of resolving/pulling a model: the file to hand to the inference backend.
pub struct ResolvedModel {
    pub model_id: String,
    pub primary_path: PathBuf,
    pub entry: ManifestEntry,
}

impl ModelStore {
    pub fn new(root: impl Into<PathBuf>) -> Self {
        Self { root: root.into() }
    }

    pub fn model_dir(&self, model_id: &str) -> PathBuf {
        self.root.join(model_id)
    }

    /// True if every file for `entry` exists locally with the right size (cheap check).
    pub fn has(&self, entry: &ManifestEntry) -> bool {
        entry.files.iter().all(|f| {
            let p = self.model_dir(&entry.model_id).join(&f.name);
            std::fs::metadata(&p)
                .map(|m| m.len() == f.bytes)
                .unwrap_or(false)
        })
    }

    /// Fetch + verify the signed manifest from `base_url`.
    pub async fn fetch_manifest(
        base_url: &str,
        verifying_key_b64: &str,
    ) -> Result<Manifest, ModelError> {
        let base = base_url.trim_end_matches('/');
        let client = reqwest::Client::builder()
            .user_agent("sc-models")
            .build()
            .map_err(|e| ModelError::Http(e.to_string()))?;

        let m_bytes = get_bytes(&client, &format!("{base}/manifest.json")).await?;
        let sig =
            String::from_utf8(get_bytes(&client, &format!("{base}/manifest.json.sig")).await?)
                .map_err(|_| ModelError::Http("signature not utf8".into()))?;

        let manifest: Manifest =
            serde_json::from_slice(&m_bytes).map_err(sc_manifest::ManifestError::from)?;
        let vk = parse_verifying_key(verifying_key_b64)?;
        manifest.verify(sig.trim(), &vk)?;
        Ok(manifest)
    }

    /// Ensure `model_id` is present and verified locally, downloading files as needed.
    pub async fn ensure(
        &self,
        base_url: &str,
        model_id: &str,
        manifest: &Manifest,
    ) -> Result<ResolvedModel, ModelError> {
        let entry = manifest
            .find(model_id)
            .ok_or_else(|| ModelError::UnknownModel(model_id.to_string()))?
            .clone();

        let dir = self.model_dir(model_id);
        fs::create_dir_all(&dir).await?;

        // Disk-space guard for what's still missing.
        let missing: u64 = entry
            .files
            .iter()
            .filter(|f| {
                let p = dir.join(&f.name);
                std::fs::metadata(&p)
                    .map(|m| m.len() != f.bytes)
                    .unwrap_or(true)
            })
            .map(|f| f.bytes)
            .sum();
        if missing > 0 {
            let free = free_space(&dir).unwrap_or(u64::MAX);
            if free < missing + (64 << 20) {
                return Err(ModelError::DiskSpace {
                    need: missing,
                    have: free,
                });
            }
        }

        let base = base_url.trim_end_matches('/');
        let client = reqwest::Client::builder()
            .user_agent("sc-models")
            .build()
            .map_err(|e| ModelError::Http(e.to_string()))?;

        for f in &entry.files {
            let dest = dir.join(&f.name);
            if fs::metadata(&dest)
                .await
                .map(|m| m.len() == f.bytes)
                .unwrap_or(false)
            {
                match sha256_file(&dest).await {
                    Ok(h) if h.eq_ignore_ascii_case(&f.sha256) => {
                        info!(model = model_id, file = %f.name, "cached, verified");
                        continue;
                    }
                    _ => {
                        warn!(model = model_id, file = %f.name, "cached file failed hash, re-downloading");
                        let _ = fs::remove_file(&dest).await;
                    }
                }
            }
            let url = format!("{base}/{model_id}/{}", f.name);
            download_verified(&client, &url, &dest, &f.sha256, f.bytes).await?;
            info!(model = model_id, file = %f.name, bytes = f.bytes, "downloaded, verified");
        }

        let primary = entry
            .files
            .iter()
            .find(|f| f.name.ends_with(".gguf"))
            .or_else(|| entry.files.first())
            .map(|f| dir.join(&f.name))
            .ok_or_else(|| ModelError::UnknownModel(model_id.to_string()))?;

        Ok(ResolvedModel {
            model_id: model_id.to_string(),
            primary_path: primary,
            entry,
        })
    }

    /// Resolve a model that must already be on disk (offline / --model-path path).
    pub fn resolve_local(
        &self,
        model_id: &str,
        explicit: Option<&Path>,
    ) -> Result<PathBuf, ModelError> {
        if let Some(p) = explicit {
            return if p.is_file() {
                Ok(p.to_path_buf())
            } else {
                Err(ModelError::NotFound(p.to_path_buf()))
            };
        }
        let dir = self.model_dir(model_id);
        let guess = dir.join(format!("{model_id}.gguf"));
        if guess.is_file() {
            return Ok(guess);
        }
        // first .gguf in the model dir
        if let Ok(rd) = std::fs::read_dir(&dir) {
            for e in rd.flatten() {
                let p = e.path();
                if p.extension().map(|x| x == "gguf").unwrap_or(false) {
                    return Ok(p);
                }
            }
        }
        Err(ModelError::NotFound(guess))
    }
}

async fn get_bytes(client: &reqwest::Client, url: &str) -> Result<Vec<u8>, ModelError> {
    let resp = client
        .get(url)
        .send()
        .await
        .map_err(|e| ModelError::Http(e.to_string()))?;
    if !resp.status().is_success() {
        return Err(ModelError::Http(format!("{} for {url}", resp.status())));
    }
    Ok(resp
        .bytes()
        .await
        .map_err(|e| ModelError::Http(e.to_string()))?
        .to_vec())
}

/// Stream `url` to `dest` (via a .part file), hashing as we go, then verify + rename.
async fn download_verified(
    client: &reqwest::Client,
    url: &str,
    dest: &Path,
    expected_sha256: &str,
    expected_bytes: u64,
) -> Result<(), ModelError> {
    use futures_util::StreamExt;

    let part = dest.with_extension("part");
    let _ = fs::remove_file(&part).await;

    let resp = client
        .get(url)
        .send()
        .await
        .map_err(|e| ModelError::Http(e.to_string()))?;
    if !resp.status().is_success() {
        return Err(ModelError::Http(format!("{} for {url}", resp.status())));
    }

    let mut file = fs::File::create(&part).await?;
    let mut hasher = Sha256::new();
    let mut written: u64 = 0;
    let mut stream = resp.bytes_stream();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e| ModelError::Http(e.to_string()))?;
        hasher.update(&chunk);
        file.write_all(&chunk).await?;
        written += chunk.len() as u64;
    }
    file.flush().await?;
    drop(file);

    let got = hex(&hasher.finalize());
    if !got.eq_ignore_ascii_case(expected_sha256) {
        let _ = fs::remove_file(&part).await;
        return Err(ModelError::HashMismatch {
            file: dest
                .file_name()
                .unwrap_or_default()
                .to_string_lossy()
                .into_owned(),
            expected: expected_sha256.to_string(),
            got,
        });
    }
    if expected_bytes != 0 && written != expected_bytes {
        let _ = fs::remove_file(&part).await;
        return Err(ModelError::Http(format!(
            "size mismatch: got {written}, want {expected_bytes}"
        )));
    }
    fs::rename(&part, dest).await?;
    Ok(())
}

/// SHA-256 of a file, computed in a blocking task.
pub async fn sha256_file(path: &Path) -> Result<String, ModelError> {
    let path = path.to_path_buf();
    tokio::task::spawn_blocking(move || {
        use std::io::Read;
        let f = std::fs::File::open(&path).map_err(|_| ModelError::NotFound(path.clone()))?;
        let mut r = std::io::BufReader::new(f);
        let mut h = Sha256::new();
        let mut buf = [0u8; 64 * 1024];
        loop {
            let n = r.read(&mut buf)?;
            if n == 0 {
                break;
            }
            h.update(&buf[..n]);
        }
        Ok(hex(&h.finalize()))
    })
    .await
    .map_err(|e| ModelError::Http(format!("hash task: {e}")))?
}

fn hex(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

#[cfg(unix)]
fn free_space(path: &Path) -> Option<u64> {
    use std::ffi::CString;
    use std::os::unix::ffi::OsStrExt;
    let c = CString::new(path.as_os_str().as_bytes()).ok()?;
    // SAFETY: statvfs with a valid path pointer and a zeroed output struct.
    unsafe {
        let mut s: libc::statvfs = std::mem::zeroed();
        if libc::statvfs(c.as_ptr(), &mut s) == 0 {
            Some(s.f_bavail as u64 * s.f_frsize as u64)
        } else {
            None
        }
    }
}

#[cfg(not(unix))]
fn free_space(_path: &Path) -> Option<u64> {
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    #[tokio::test]
    async fn hashes_file() {
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path().join("m.gguf");
        std::fs::File::create(&p)
            .unwrap()
            .write_all(b"hello")
            .unwrap();
        assert_eq!(
            sha256_file(&p).await.unwrap(),
            "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
        );
    }

    #[test]
    fn resolve_local_missing_errs() {
        let store = ModelStore::new("/no/such/root");
        assert!(matches!(
            store.resolve_local("x", None),
            Err(ModelError::NotFound(_))
        ));
    }
}
