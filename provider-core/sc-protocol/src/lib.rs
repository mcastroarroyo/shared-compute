//! Rust mirror of `/protocol/schemas` (contract v0).
//!
//! Any change here must be mirrored in `coordinator/internal/protocol` and the JSON
//! Schemas, with a bump to `/protocol/VERSION`, in the same change.

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Wire protocol version, sent as the `v` field on every message.
pub const VERSION: &str = "0";

/// The only accepted sealing algorithm in v0.
pub const SEAL_ALG: &str = "crypto_box-x25519-xsalsa20poly1305";

pub mod msg_type {
    pub const REGISTER: &str = "register";
    pub const REGISTER_ACK: &str = "register_ack";
    pub const HEARTBEAT: &str = "heartbeat";
    pub const HEARTBEAT_ACK: &str = "heartbeat_ack";
    pub const MODEL_PULL: &str = "model_pull";
    pub const MODEL_READY: &str = "model_ready";
    pub const JOB_REQUEST: &str = "job_request";
    pub const JOB_CHUNK: &str = "job_chunk";
    pub const JOB_DONE: &str = "job_done";
    pub const JOB_ERROR: &str = "job_error";
    pub const CANCEL: &str = "cancel";
    pub const BENCHMARK_REPORT: &str = "benchmark_report";
}

/// Common header present on every WebSocket frame.
#[derive(Debug, Clone, Deserialize)]
pub struct Envelope {
    pub v: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub id: String,
    #[serde(default)]
    pub re: Option<String>,
    pub ts: i64,
}

/// Parse a raw inbound frame into its envelope. The caller then matches on `kind` and
/// calls [`from_frame`] to get the type-specific struct.
pub fn parse_envelope(raw: &[u8]) -> Result<Envelope, serde_json::Error> {
    let env: Envelope = serde_json::from_slice(raw)?;
    Ok(env)
}

/// Deserialize the full frame bytes into a type-specific payload struct `T`.
pub fn from_frame<T: for<'de> Deserialize<'de>>(raw: &[u8]) -> Result<T, serde_json::Error> {
    serde_json::from_slice(raw)
}

/// Serialize a typed payload struct into a full wire frame, injecting envelope fields
/// (`v`, `id`, `ts`) and (optionally) `re`. Mirrors the Go `protocol.Marshal`.
pub fn to_frame<T: Serialize>(payload: &T, re: Option<&str>) -> Result<Vec<u8>, serde_json::Error> {
    let mut v = serde_json::to_value(payload)?;
    let obj = v
        .as_object_mut()
        .expect("protocol payloads serialize to a JSON object");
    obj.insert("v".into(), Value::String(VERSION.into()));
    obj.insert("id".into(), Value::String(uuid::Uuid::new_v4().to_string()));
    obj.insert("ts".into(), Value::Number(chrono_now_secs().into()));
    if let Some(re) = re {
        obj.insert("re".into(), Value::String(re.into()));
    }
    serde_json::to_vec(&v)
}

fn chrono_now_secs() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

// --------------------------------------------------------------------------------------
// Shared structures
// --------------------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SealedPayload {
    pub alg: String,
    /// base64, coordinator ephemeral public key for this job (32 bytes decoded)
    pub epk: String,
    /// base64, 24 random bytes
    pub nonce: String,
    /// base64
    pub ciphertext: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct SecurityCapabilities {
    pub hardware_key: bool,
    pub secure_boot: bool,
    pub measured_boot: bool,
    pub runtime_attested: bool,
    pub confidential_memory: bool,
    pub confidential_gpu: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Capabilities {
    pub platform: String,
    pub arch: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub cpu: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub gpu: Option<String>,
    pub ram_mb: u64,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub vram_mb: Option<u64>,
    pub backend: String,
    pub hardware_class: String,
    pub max_context: u32,
    pub models: Vec<String>,
    pub security_capabilities: SecurityCapabilities,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Telemetry {
    pub active_jobs: u32,
    pub queue_depth: u32,
    pub cpu_load: f64,
    pub mem_available_mb: u64,
    pub thermal_state: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub battery_pct: Option<u8>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub charging: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub network: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Usage {
    pub prompt_tokens: u32,
    pub completion_tokens: u32,
    pub total_tokens: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Attestation {
    pub kind: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub evidence: Option<Value>,
}

impl Default for Attestation {
    fn default() -> Self {
        Self {
            kind: "none".into(),
            evidence: None,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatMessage {
    pub role: String,
    pub content: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SamplingParams {
    pub max_tokens: u32,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub temperature: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub top_p: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub top_k: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub repeat_penalty: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub stop: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub seed: Option<i64>,
}

// --------------------------------------------------------------------------------------
// provider -> coordinator
// --------------------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize)]
pub struct Register {
    #[serde(rename = "type")]
    pub kind: String,
    pub agent: String,
    pub registration_token: String,
    pub static_pk: String,
    pub capabilities: Capabilities,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub attestation: Option<Attestation>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub resume_provider_id: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct Heartbeat {
    #[serde(rename = "type")]
    pub kind: String,
    pub telemetry: Telemetry,
}

#[derive(Debug, Clone, Serialize)]
pub struct ModelReady {
    #[serde(rename = "type")]
    pub kind: String,
    pub model: String,
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sha256: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub bytes: Option<u64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct JobChunk {
    #[serde(rename = "type")]
    pub kind: String,
    pub job_id: String,
    pub sealed: SealedPayload,
}

#[derive(Debug, Clone, Serialize)]
pub struct JobDone {
    #[serde(rename = "type")]
    pub kind: String,
    pub job_id: String,
    pub usage: Usage,
    pub finish_reason: String,
    pub duration_ms: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub prefill_tps: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub decode_tps: Option<f64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct JobError {
    #[serde(rename = "type")]
    pub kind: String,
    pub job_id: String,
    pub code: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub message: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BenchmarkReport {
    #[serde(rename = "type")]
    pub kind: String,
    pub model: String,
    pub backend: String,
    pub prefill_tps: f64,
    pub decode_tps: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub context_tested: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sample_ms: Option<u32>,

    // --- M10 capability fingerprint (all optional; older providers omit them) ---
    /// Length of the sustained-throughput run.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sustained_seconds: Option<u32>,
    /// Decode tok/s in the first window of the sustained run.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sustained_start_tps: Option<f64>,
    /// Decode tok/s in the last window — the throughput the scheduler should price on.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sustained_end_tps: Option<f64>,
    /// Measured memory read bandwidth, GB/s.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mem_bandwidth_gbps: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub available_ram_mb: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub available_storage_mb: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cpu_cores: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub thermal_state: Option<String>,
}

// --------------------------------------------------------------------------------------
// coordinator -> provider
// --------------------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize)]
pub struct RegisterAck {
    pub ok: bool,
    #[serde(default)]
    pub error: Option<String>,
    #[serde(default)]
    pub provider_id: Option<String>,
    #[serde(default)]
    pub trust_tier: Option<String>,
    #[serde(default)]
    pub accepted_models: Option<Vec<String>>,
    #[serde(default)]
    pub heartbeat_seconds: Option<u32>,
    #[serde(default)]
    pub coordinator_pk: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct HeartbeatAck {
    pub server_ts: i64,
    #[serde(default)]
    pub directive: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ModelPull {
    pub model: String,
    pub manifest_url: String,
    #[serde(default)]
    pub priority: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct JobRequest {
    pub job_id: String,
    pub model: String,
    #[serde(default)]
    pub deadline_ms: Option<u64>,
    pub sealed: SealedPayload,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Cancel {
    pub job_id: String,
    pub reason: String,
}

// --------------------------------------------------------------------------------------
// sealed plaintexts
// --------------------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize)]
pub struct JobRequestPlaintext {
    pub job_id: String,
    pub model: String,
    pub messages: Vec<ChatMessage>,
    pub params: SamplingParams,
    pub stream: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct JobChunkPlaintext {
    pub job_id: String,
    pub seq: u32,
    pub delta: String,
    pub done: bool,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn frame_roundtrip_injects_envelope() {
        let hb = Heartbeat {
            kind: msg_type::HEARTBEAT.into(),
            telemetry: Telemetry {
                thermal_state: "nominal".into(),
                ..Default::default()
            },
        };
        let raw = to_frame(&hb, None).unwrap();
        let env = parse_envelope(&raw).unwrap();
        assert_eq!(env.v, VERSION);
        assert_eq!(env.kind, "heartbeat");
        assert!(!env.id.is_empty());
    }

    #[test]
    fn parses_job_request_frame() {
        let raw = br#"{"v":"0","type":"job_request","id":"11111111-1111-1111-1111-111111111111","ts":1,
            "job_id":"22222222-2222-2222-2222-222222222222","model":"m",
            "sealed":{"alg":"crypto_box-x25519-xsalsa20poly1305","epk":"AA","nonce":"BB","ciphertext":"CC"}}"#;
        let env = parse_envelope(raw).unwrap();
        assert_eq!(env.kind, msg_type::JOB_REQUEST);
        let jr: JobRequest = from_frame(raw).unwrap();
        assert_eq!(jr.model, "m");
        assert_eq!(jr.sealed.alg, SEAL_ALG);
    }
}
