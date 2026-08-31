//! Workload Manifest v1 — the per-job authorization the coordinator signs and the
//! node verifies before running anything.
//!
//! This is the Rust counterpart of `coordinator/internal/manifest` and
//! `security/ayni_security/manifest.py`. Canonical bytes are the compact,
//! field-order JSON of [`WorkloadManifest`] (serde emits struct fields in
//! declaration order, with no whitespace and no HTML escaping) so Go's
//! `encoding/json` (escaping off) produces identical bytes.

use base64::{engine::general_purpose::STANDARD as B64, Engine};
use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use serde::{Deserialize, Serialize};

pub const WORKLOAD_MANIFEST_VERSION: u32 = 1;
pub const OPERATION_INFERENCE: &str = "inference";
pub const NETWORK_POLICY_NONE: &str = "NONE";

#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum WorkloadManifestError {
    #[error("unknown signer")]
    UnknownSigner,
    #[error("bad signature encoding")]
    BadSignatureEncoding,
    #[error("signature does not verify")]
    BadSignature,
    #[error("bound to a different device")]
    WrongDevice,
    #[error("manifest expired")]
    Expired,
    #[error("manifest created in the future")]
    CreatedInFuture,
    #[error("lease longer than the node ceiling")]
    LeaseTooLong,
    #[error("resource limit exceeds an immutable node ceiling")]
    OverCeiling,
    #[error("manifest structurally invalid: {0}")]
    Structure(&'static str),
    #[error("json: {0}")]
    Json(String),
}

/// Per-job resource envelope. Field order must match the Go struct.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ResourceLimits {
    pub max_runtime_ms: u64,
    pub max_memory_mb: u64,
    pub max_storage_mb: u64,
    pub max_input_bytes: u64,
    pub max_output_tokens: u64,
    pub max_cpu_duty_cycle_pct: u64,
}

/// The signed body. **Field order here is the canonical byte order** — keep it
/// identical to `coordinator/internal/manifest.WorkloadManifest`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct WorkloadManifest {
    pub manifest_version: u32,
    pub job_id: String,
    pub lease_id: String,
    pub workload_id: String,
    pub customer_id: String,
    pub device_pk: String,
    pub runtime_id: String,
    pub runtime_version: String,
    pub runtime_hash: String,
    pub model_id: String,
    pub model_hash: String,
    pub operation: String,
    pub input_hash: String,
    pub resource_limits: ResourceLimits,
    pub network_policy: String,
    pub created_at: i64,
    pub expires_at: i64,
    pub nonce: String,
}

/// What travels on the wire inside a `JobRequest`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SignedManifest {
    pub manifest: WorkloadManifest,
    pub signer_id: String,
    pub signature: String,
}

/// Immutable local ceilings. The coordinator cannot raise these; the node ships
/// them compiled in. Mirrors the Python `NodeSafetyPolicy` defaults.
#[derive(Debug, Clone, Copy)]
pub struct NodeSafetyPolicy {
    pub max_runtime_ms: u64,
    pub max_memory_mb: u64,
    pub max_storage_mb: u64,
    pub max_input_bytes: u64,
    pub max_output_tokens: u64,
    pub max_cpu_duty_cycle_pct: u64,
    pub max_lease_seconds: i64,
    pub max_clock_skew_seconds: i64,
}

impl Default for NodeSafetyPolicy {
    fn default() -> Self {
        Self {
            max_runtime_ms: 120_000,
            max_memory_mb: 8_192,
            max_storage_mb: 16_384,
            max_input_bytes: 1_048_576,
            max_output_tokens: 4_096,
            max_cpu_duty_cycle_pct: 80,
            max_lease_seconds: 300,
            max_clock_skew_seconds: 30,
        }
    }
}

impl WorkloadManifest {
    /// Exact bytes that were signed / must be verified.
    pub fn canonical_bytes(&self) -> Result<Vec<u8>, WorkloadManifestError> {
        serde_json::to_vec(self).map_err(|e| WorkloadManifestError::Json(e.to_string()))
    }

    fn validate_structure(&self) -> Result<(), WorkloadManifestError> {
        use WorkloadManifestError::Structure;
        if self.manifest_version != WORKLOAD_MANIFEST_VERSION {
            return Err(Structure("unsupported manifest_version"));
        }
        if self.operation != OPERATION_INFERENCE {
            return Err(Structure("operation not allowed"));
        }
        if self.network_policy != NETWORK_POLICY_NONE {
            return Err(Structure("network_policy not allowed"));
        }
        if self.job_id.is_empty() || self.lease_id.is_empty() || self.workload_id.is_empty() {
            return Err(Structure("missing job/lease/workload id"));
        }
        if self.model_id.is_empty() || self.runtime_id.is_empty() {
            return Err(Structure("missing runtime_id or model_id"));
        }
        if !is_b64_len(&self.device_pk, 32) {
            return Err(Structure("device_pk must be 32 base64 bytes"));
        }
        if !is_b64_len(&self.nonce, 24) {
            return Err(Structure("nonce must be 24 base64 bytes"));
        }
        for h in [&self.runtime_hash, &self.model_hash, &self.input_hash] {
            if !is_sha256_hex(h) {
                return Err(Structure(
                    "runtime/model/input hash must be lowercase sha-256",
                ));
            }
        }
        let rl = &self.resource_limits;
        for v in [
            rl.max_runtime_ms,
            rl.max_memory_mb,
            rl.max_storage_mb,
            rl.max_input_bytes,
            rl.max_output_tokens,
            rl.max_cpu_duty_cycle_pct,
        ] {
            if v == 0 {
                return Err(Structure("resource_limits values must be positive"));
            }
        }
        if rl.max_cpu_duty_cycle_pct > 100 {
            return Err(Structure("max_cpu_duty_cycle_pct over 100"));
        }
        Ok(())
    }
}

/// A set of coordinator signing keys the node will accept, keyed by signer id.
/// Lets callers avoid depending on `ed25519-dalek` directly.
#[derive(Debug, Clone, Default)]
pub struct TrustedSigners(Vec<(String, VerifyingKey)>);

impl TrustedSigners {
    /// Parse `["signer-v1:<b64>", "<b64>", ...]`. An empty input is a valid,
    /// empty set (verification of any manifest then fails `UnknownSigner`).
    pub fn parse(specs: &[String]) -> Result<Self, WorkloadManifestError> {
        let mut v = Vec::with_capacity(specs.len());
        for s in specs {
            if s.trim().is_empty() {
                continue;
            }
            v.push(parse_trusted(s)?);
        }
        Ok(Self(v))
    }

    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}

/// Verify a [`SignedManifest`] against the trusted coordinator key(s), the node's
/// immutable policy, the device it is bound to, and `now` (unix seconds). Nonce
/// single-use is the caller's responsibility.
pub fn verify(
    sm: &SignedManifest,
    trusted: &TrustedSigners,
    pol: &NodeSafetyPolicy,
    expected_device_pk: &str,
    now: i64,
) -> Result<(), WorkloadManifestError> {
    let vk = trusted
        .0
        .iter()
        .find(|(id, _)| id == &sm.signer_id)
        .map(|(_, k)| k)
        .ok_or(WorkloadManifestError::UnknownSigner)?;

    sm.manifest.validate_structure()?;

    let raw = B64
        .decode(sm.signature.trim())
        .map_err(|_| WorkloadManifestError::BadSignatureEncoding)?;
    let arr: [u8; 64] = raw
        .as_slice()
        .try_into()
        .map_err(|_| WorkloadManifestError::BadSignatureEncoding)?;
    let sig = Signature::from_bytes(&arr);

    let cb = sm.manifest.canonical_bytes()?;
    vk.verify(&cb, &sig)
        .map_err(|_| WorkloadManifestError::BadSignature)?;

    let m = &sm.manifest;
    if !expected_device_pk.is_empty() && m.device_pk != expected_device_pk {
        return Err(WorkloadManifestError::WrongDevice);
    }
    if now + pol.max_clock_skew_seconds < m.created_at {
        return Err(WorkloadManifestError::CreatedInFuture);
    }
    if now >= m.expires_at {
        return Err(WorkloadManifestError::Expired);
    }
    if m.expires_at - m.created_at > pol.max_lease_seconds + pol.max_clock_skew_seconds {
        return Err(WorkloadManifestError::LeaseTooLong);
    }
    let rl = &m.resource_limits;
    let over = rl.max_runtime_ms > pol.max_runtime_ms
        || rl.max_memory_mb > pol.max_memory_mb
        || rl.max_storage_mb > pol.max_storage_mb
        || rl.max_input_bytes > pol.max_input_bytes
        || rl.max_output_tokens > pol.max_output_tokens
        || rl.max_cpu_duty_cycle_pct > pol.max_cpu_duty_cycle_pct;
    if over {
        return Err(WorkloadManifestError::OverCeiling);
    }
    Ok(())
}

/// Parse `"<signer_id>:<base64 32-byte key>"` (or just the base64 key, defaulting
/// the id to `signer-v1`) into a trusted-signer entry.
pub fn parse_trusted(spec: &str) -> Result<(String, VerifyingKey), WorkloadManifestError> {
    let (id, key_b64) = match spec.split_once(':') {
        Some((id, k)) => (id.to_string(), k),
        None => ("signer-v1".to_string(), spec),
    };
    let raw = B64
        .decode(key_b64.trim())
        .map_err(|_| WorkloadManifestError::BadSignatureEncoding)?;
    let arr: [u8; 32] = raw
        .as_slice()
        .try_into()
        .map_err(|_| WorkloadManifestError::Structure("verify key must be 32 bytes"))?;
    let vk = VerifyingKey::from_bytes(&arr).map_err(|_| WorkloadManifestError::BadSignature)?;
    Ok((id, vk))
}

fn is_b64_len(s: &str, n: usize) -> bool {
    B64.decode(s.trim()).map(|b| b.len() == n).unwrap_or(false)
}

fn is_sha256_hex(s: &str) -> bool {
    s.len() == 64
        && s.bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::{Signer, SigningKey};
    use rand::rngs::OsRng;

    fn sample(device_pk: &str, now: i64) -> WorkloadManifest {
        WorkloadManifest {
            manifest_version: WORKLOAD_MANIFEST_VERSION,
            job_id: "job-1".into(),
            lease_id: "job-1".into(),
            workload_id: "job-1".into(),
            customer_id: String::new(),
            device_pk: device_pk.into(),
            runtime_id: "llama.cpp".into(),
            runtime_version: "v0".into(),
            runtime_hash: "a".repeat(64),
            model_id: "qwen2.5-0.5b-instruct-q4_k_m".into(),
            model_hash: "b".repeat(64),
            operation: OPERATION_INFERENCE.into(),
            input_hash: "c".repeat(64),
            resource_limits: ResourceLimits {
                max_runtime_ms: 60_000,
                max_memory_mb: 4_096,
                max_storage_mb: 8_192,
                max_input_bytes: 262_144,
                max_output_tokens: 256,
                max_cpu_duty_cycle_pct: 70,
            },
            network_policy: NETWORK_POLICY_NONE.into(),
            created_at: now,
            expires_at: now + 120,
            nonce: B64.encode([7u8; 24]),
        }
    }

    fn sign(sk: &SigningKey, m: &WorkloadManifest) -> SignedManifest {
        let sig = sk.sign(&m.canonical_bytes().unwrap());
        SignedManifest {
            manifest: m.clone(),
            signer_id: "signer-v1".into(),
            signature: B64.encode(sig.to_bytes()),
        }
    }

    #[test]
    fn roundtrip_and_tamper() {
        let sk = SigningKey::generate(&mut OsRng);
        let trusted = TrustedSigners::parse(&[format!(
            "signer-v1:{}",
            B64.encode(sk.verifying_key().to_bytes())
        )])
        .unwrap();
        let dev = B64.encode([1u8; 32]);
        let sm = sign(&sk, &sample(&dev, 1000));
        verify(&sm, &trusted, &NodeSafetyPolicy::default(), &dev, 1005).unwrap();

        let mut tampered = sm.clone();
        tampered.manifest.model_id = "evil.gguf".into();
        assert_eq!(
            verify(
                &tampered,
                &trusted,
                &NodeSafetyPolicy::default(),
                &dev,
                1005
            ),
            Err(WorkloadManifestError::BadSignature)
        );
    }

    #[test]
    fn rejects_wrong_device_expiry_and_ceiling() {
        let sk = SigningKey::generate(&mut OsRng);
        let trusted = TrustedSigners::parse(&[format!(
            "signer-v1:{}",
            B64.encode(sk.verifying_key().to_bytes())
        )])
        .unwrap();
        let dev = B64.encode([1u8; 32]);
        let sm = sign(&sk, &sample(&dev, 1000));
        let pol = NodeSafetyPolicy::default();

        let other = B64.encode([2u8; 32]);
        assert_eq!(
            verify(&sm, &trusted, &pol, &other, 1005),
            Err(WorkloadManifestError::WrongDevice)
        );
        assert_eq!(
            verify(&sm, &trusted, &pol, &dev, 5000),
            Err(WorkloadManifestError::Expired)
        );

        let mut big = sample(&dev, 1000);
        big.resource_limits.max_memory_mb = 999_999;
        let sm_big = sign(&sk, &big);
        assert_eq!(
            verify(&sm_big, &trusted, &pol, &dev, 1005),
            Err(WorkloadManifestError::OverCeiling)
        );
    }

    #[test]
    fn unknown_signer_is_rejected() {
        let sk = SigningKey::generate(&mut OsRng);
        let dev = B64.encode([1u8; 32]);
        let sm = sign(&sk, &sample(&dev, 1000));
        assert_eq!(
            verify(
                &sm,
                &TrustedSigners::default(),
                &NodeSafetyPolicy::default(),
                &dev,
                1005
            ),
            Err(WorkloadManifestError::UnknownSigner)
        );
    }

    /// The Go coordinator signs; this Rust node must verify byte-for-byte. The
    /// fixture is regenerated by the Go test
    /// `manifest.TestWriteCrossLanguageFixture`.
    #[test]
    fn cross_language_go_fixture() {
        let path = concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../coordinator/internal/manifest/testdata/cross_language_fixture.json"
        );
        let raw = std::fs::read_to_string(path).expect("run the Go fixture test first");
        let v: serde_json::Value = serde_json::from_str(&raw).unwrap();
        let sm: SignedManifest =
            serde_json::from_value(v["signed_manifest"].clone()).unwrap();
        let pub_b64 = v["signer_public_b64"].as_str().unwrap();
        let dev = v["expected_device_pk"].as_str().unwrap();
        let now = v["now"].as_i64().unwrap();
        let trusted =
            TrustedSigners::parse(&[format!("signer-v1:{pub_b64}")]).unwrap();
        verify(&sm, &trusted, &NodeSafetyPolicy::default(), dev, now)
            .expect("Go-signed manifest failed Rust verification — canonical bytes diverged");
    }
}
