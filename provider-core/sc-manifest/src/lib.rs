//! The signed model registry manifest — shared by `sc-modelctl` (writes + signs) and
//! `sc-models` (fetches + verifies).
//!
//! `manifest.json` is canonicalized (sorted keys, no whitespace) before signing. The
//! detached signature lives at `manifest.json.sig` as base64 Ed25519.

use base64::{engine::general_purpose::STANDARD as B64, Engine};
use ed25519_dalek::{Signature, Signer, SigningKey, Verifier, VerifyingKey};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

pub const MANIFEST_VERSION: u32 = 1;

#[derive(Debug, thiserror::Error)]
pub enum ManifestError {
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),
    #[error("base64: {0}")]
    B64(String),
    #[error("bad key length (want {want}, got {got})")]
    KeyLen { want: usize, got: usize },
    #[error("signature verification failed")]
    BadSignature,
    #[error("model not found in manifest: {0}")]
    UnknownModel(String),
}

/// One file that makes up a model (usually a single `.gguf`, sometimes split shards).
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ManifestFile {
    pub name: String,
    /// lowercase hex SHA-256
    pub sha256: String,
    pub bytes: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ManifestEntry {
    pub model_id: String,
    pub architecture: String,
    pub quantization: String,
    pub hardware_class: String,
    pub context_length: u32,
    /// SHA-256 over the concatenation of each file's lowercase-hex sha256, in list order.
    pub aggregate_sha256: String,
    pub files: Vec<ManifestFile>,
    #[serde(default)]
    pub tokenizer_sha256: String,
    #[serde(default)]
    pub chat_template_sha256: String,
    pub total_bytes: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Manifest {
    pub version: u32,
    pub generated_at: String,
    pub models: Vec<ManifestEntry>,
}

impl Manifest {
    pub fn find(&self, model_id: &str) -> Option<&ManifestEntry> {
        self.models.iter().find(|m| m.model_id == model_id)
    }

    /// Canonical bytes used for signing/verification: JSON with sorted keys, no spaces.
    pub fn canonical_bytes(&self) -> Result<Vec<u8>, ManifestError> {
        let value = serde_json::to_value(self)?;
        let canon = canonicalize(&value);
        Ok(serde_json::to_vec(&canon)?)
    }

    /// Sign the canonical bytes, returning a base64 detached signature.
    pub fn sign(&self, key: &SigningKey) -> Result<String, ManifestError> {
        let sig = key.sign(&self.canonical_bytes()?);
        Ok(B64.encode(sig.to_bytes()))
    }

    /// Verify a base64 detached signature against a verifying key.
    pub fn verify(&self, sig_b64: &str, vk: &VerifyingKey) -> Result<(), ManifestError> {
        let raw = B64
            .decode(sig_b64.trim())
            .map_err(|e| ManifestError::B64(e.to_string()))?;
        let arr: [u8; 64] = raw
            .as_slice()
            .try_into()
            .map_err(|_| ManifestError::KeyLen {
                want: 64,
                got: raw.len(),
            })?;
        let sig = Signature::from_bytes(&arr);
        vk.verify(&self.canonical_bytes()?, &sig)
            .map_err(|_| ManifestError::BadSignature)
    }
}

/// Recursively sort object keys so serialization is deterministic.
fn canonicalize(v: &serde_json::Value) -> serde_json::Value {
    use serde_json::Value;
    match v {
        Value::Object(map) => {
            let mut sorted = serde_json::Map::new();
            let mut keys: Vec<&String> = map.keys().collect();
            keys.sort();
            for k in keys {
                sorted.insert(k.clone(), canonicalize(&map[k]));
            }
            Value::Object(sorted)
        }
        Value::Array(arr) => Value::Array(arr.iter().map(canonicalize).collect()),
        other => other.clone(),
    }
}

/// Compute the aggregate hash for an ordered list of file hashes.
pub fn aggregate_sha256(file_hashes: &[String]) -> String {
    let mut h = Sha256::new();
    for fh in file_hashes {
        h.update(fh.as_bytes());
    }
    hex(&h.finalize())
}

pub fn hex(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

/// Parse a base64 Ed25519 public key (32 bytes).
pub fn parse_verifying_key(b64: &str) -> Result<VerifyingKey, ManifestError> {
    let raw = B64
        .decode(b64.trim())
        .map_err(|e| ManifestError::B64(e.to_string()))?;
    let arr: [u8; 32] = raw
        .as_slice()
        .try_into()
        .map_err(|_| ManifestError::KeyLen {
            want: 32,
            got: raw.len(),
        })?;
    VerifyingKey::from_bytes(&arr).map_err(|_| ManifestError::BadSignature)
}

pub fn encode_key(bytes: &[u8]) -> String {
    B64.encode(bytes)
}

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::SigningKey;
    use rand::rngs::OsRng;

    fn sample() -> Manifest {
        Manifest {
            version: MANIFEST_VERSION,
            generated_at: "2026-08-30T00:00:00Z".into(),
            models: vec![ManifestEntry {
                model_id: "m-1b-q4".into(),
                architecture: "llama".into(),
                quantization: "Q4_K_M".into(),
                hardware_class: "MICRO".into(),
                context_length: 8192,
                aggregate_sha256: aggregate_sha256(&["aa".into(), "bb".into()]),
                files: vec![
                    ManifestFile {
                        name: "m.gguf".into(),
                        sha256: "aa".into(),
                        bytes: 10,
                    },
                    ManifestFile {
                        name: "m.1.gguf".into(),
                        sha256: "bb".into(),
                        bytes: 20,
                    },
                ],
                tokenizer_sha256: String::new(),
                chat_template_sha256: String::new(),
                total_bytes: 30,
            }],
        }
    }

    #[test]
    fn sign_and_verify_roundtrip() {
        let sk = SigningKey::generate(&mut OsRng);
        let vk = sk.verifying_key();
        let m = sample();
        let sig = m.sign(&sk).unwrap();
        m.verify(&sig, &vk).unwrap();

        // Tamper: different model id must fail against the old signature.
        let mut m2 = m.clone();
        m2.models[0].model_id = "changed".into();
        assert!(matches!(
            m2.verify(&sig, &vk),
            Err(ManifestError::BadSignature)
        ));

        // Wrong key must fail.
        let other = SigningKey::generate(&mut OsRng).verifying_key();
        assert!(matches!(
            m.verify(&sig, &other),
            Err(ManifestError::BadSignature)
        ));
    }

    #[test]
    fn canonical_is_order_independent() {
        let m = sample();
        let a = m.canonical_bytes().unwrap();
        let reparsed: Manifest = serde_json::from_slice(&serde_json::to_vec(&m).unwrap()).unwrap();
        let b = reparsed.canonical_bytes().unwrap();
        assert_eq!(a, b);
    }
}
