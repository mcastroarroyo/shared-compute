//! Local model store. M1 only resolves an explicit on-disk path and can verify a file's
//! SHA-256. M3 adds streaming download from the signed registry manifest with resume.

use sha2::{Digest, Sha256};
use std::fs::File;
use std::io::{BufReader, Read};
use std::path::{Path, PathBuf};

#[derive(Debug, thiserror::Error)]
pub enum ModelError {
    #[error("model file not found: {0}")]
    NotFound(PathBuf),
    #[error("hash mismatch: expected {expected}, got {got}")]
    HashMismatch { expected: String, got: String },
    #[error(transparent)]
    Io(#[from] std::io::Error),
}

/// A directory of local GGUF files, one per model id (`<root>/<model>.gguf`).
pub struct ModelStore {
    root: PathBuf,
}

impl ModelStore {
    pub fn new(root: impl Into<PathBuf>) -> Self {
        Self { root: root.into() }
    }

    pub fn path_for(&self, model: &str) -> PathBuf {
        self.root.join(format!("{model}.gguf"))
    }

    pub fn has(&self, model: &str) -> bool {
        self.path_for(model).is_file()
    }

    /// Resolve an explicit path (M1) or the store path for `model`, erroring if absent.
    pub fn resolve(&self, model: &str, explicit: Option<&Path>) -> Result<PathBuf, ModelError> {
        let p = match explicit {
            Some(p) => p.to_path_buf(),
            None => self.path_for(model),
        };
        if p.is_file() {
            Ok(p)
        } else {
            Err(ModelError::NotFound(p))
        }
    }
}

/// Compute the lowercase hex SHA-256 of a file.
pub fn sha256_file(path: &Path) -> Result<String, ModelError> {
    let f = File::open(path).map_err(|_| ModelError::NotFound(path.to_path_buf()))?;
    let mut reader = BufReader::new(f);
    let mut hasher = Sha256::new();
    let mut buf = [0u8; 64 * 1024];
    loop {
        let n = reader.read(&mut buf)?;
        if n == 0 {
            break;
        }
        hasher.update(&buf[..n]);
    }
    Ok(hex(&hasher.finalize()))
}

/// Verify a file matches an expected lowercase-hex SHA-256.
pub fn verify_sha256(path: &Path, expected_hex: &str) -> Result<(), ModelError> {
    let got = sha256_file(path)?;
    if got.eq_ignore_ascii_case(expected_hex) {
        Ok(())
    } else {
        Err(ModelError::HashMismatch {
            expected: expected_hex.to_string(),
            got,
        })
    }
}

fn hex(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    #[test]
    fn hashes_and_verifies() {
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path().join("m.gguf");
        File::create(&p).unwrap().write_all(b"hello").unwrap();

        // sha256("hello")
        let want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824";
        assert_eq!(sha256_file(&p).unwrap(), want);
        verify_sha256(&p, want).unwrap();
        assert!(verify_sha256(&p, &"0".repeat(64)).is_err());
    }

    #[test]
    fn resolve_missing_errs() {
        let store = ModelStore::new("/nonexistent-root");
        assert!(matches!(
            store.resolve("x", None),
            Err(ModelError::NotFound(_))
        ));
    }
}
