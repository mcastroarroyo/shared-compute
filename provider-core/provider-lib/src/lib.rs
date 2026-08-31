//! Reusable provider core: connect to a coordinator, register this device, and service
//! sealed inference jobs. Used by `provider-daemon` (CLI) and `sc-mobile` (Android/iOS).
//!
//! Prompt and completion text are never logged.

pub mod benchmark;
mod identity;
mod job;

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::{
    atomic::{AtomicU32, Ordering},
    Arc,
};

use anyhow::{bail, Context, Result};
use sc_attest::{AttestationProvider, NullAttestation};
use sc_inference::{InferenceBackend, MockBackend};
use sc_net::WsMessage;
pub use sc_protocol as proto;
use tokio::sync::{mpsc, Mutex};
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};

const AGENT: &str = concat!("provider-lib/", env!("CARGO_PKG_VERSION"));

/// Everything the provider needs to run. Field names match the daemon's env vars.
#[derive(Debug, Clone)]
pub struct ProviderConfig {
    pub coordinator_url: String,
    pub registration_token: String,
    pub model: String,
    pub model_path: Option<PathBuf>,
    /// "mock" or "llama"
    pub backend: String,
    pub manifest_url: Option<String>,
    pub registry_pubkey: Option<String>,
    pub model_dir: Option<PathBuf>,
    pub identity_path: Option<PathBuf>,
    pub max_context: u32,
    /// Coordinator Workload Manifest v1 signing keys the node will accept,
    /// each `"signer-id:<base64>"` (or bare `<base64>`). Empty = don't verify.
    pub manifest_verify_keys: Vec<String>,
    /// Reject any job that arrives without a valid signed manifest.
    pub require_manifest: bool,
}

impl Default for ProviderConfig {
    fn default() -> Self {
        Self {
            coordinator_url: "ws://127.0.0.1:8080/ws/provider".into(),
            registration_token: "dev-provider-token".into(),
            model: "mock-echo".into(),
            model_path: None,
            backend: "mock".into(),
            manifest_url: None,
            registry_pubkey: None,
            model_dir: None,
            identity_path: None,
            max_context: 8192,
            manifest_verify_keys: Vec::new(),
            require_manifest: false,
        }
    }
}

/// Lifecycle events emitted by [`run`]. Consumers render these (a UI, stderr, logs).
#[derive(Debug, Clone)]
pub enum ProviderEvent {
    Connecting {
        url: String,
    },
    Registered {
        provider_id: String,
        trust_tier: String,
    },
    JobStarted {
        job_id: String,
    },
    JobFinished {
        job_id: String,
        ok: bool,
        /// Completion tokens this node produced (0 on failure).
        completion_tokens: u32,
        /// Decode throughput for this job, tokens/sec (0 on failure).
        decode_tps: f32,
    },
    Disconnected {
        reason: String,
    },
    Error {
        message: String,
    },
}

/// Sink for [`ProviderEvent`]s. `Send + Sync` so it can cross task boundaries.
pub trait EventSink: Send + Sync {
    fn on_event(&self, ev: ProviderEvent);
}

/// Produces the `attestation` block for `register`, binding a platform hardware key
/// to the provider's X25519 static key. Implemented off-core (e.g. the Android app
/// via Keystore) because the hardware key is not reachable from this crate.
pub trait AttestHook: Send + Sync {
    /// `static_pk` is the 32-byte X25519 public key the evidence must bind to.
    /// Return `None` to stay on Tier 0.
    fn evidence(&self, static_pk: [u8; 32]) -> Option<proto::Attestation>;
}

/// An [`EventSink`] that does nothing.
pub struct NullSink;
impl EventSink for NullSink {
    fn on_event(&self, _ev: ProviderEvent) {}
}

pub fn hardware_class(ram_mb: u64) -> &'static str {
    match ram_mb {
        0..=6143 => "MICRO",
        6144..=16383 => "SMALL",
        16384..=32767 => "MEDIUM",
        32768..=65535 => "LARGE",
        _ => "XL",
    }
}

fn model_dir(cfg: &ProviderConfig) -> PathBuf {
    cfg.model_dir.clone().unwrap_or_else(|| {
        identity::default_path()
            .parent()
            .map(|p| p.join("models"))
            .unwrap_or_else(|| PathBuf::from(".shared-compute/models"))
    })
}

/// Resolve the on-disk GGUF path: explicit path, else verified registry download, else a
/// file already in the cache dir.
#[cfg_attr(not(feature = "llama"), allow(dead_code))]
async fn resolve_model_path(cfg: &ProviderConfig) -> Result<PathBuf> {
    use sc_models::ModelStore;

    if let Some(p) = &cfg.model_path {
        return Ok(p.clone());
    }
    let store = ModelStore::new(model_dir(cfg));

    if let Some(url) = &cfg.manifest_url {
        let pk = cfg
            .registry_pubkey
            .as_deref()
            .context("registry_pubkey is required when manifest_url is set")?;
        info!(url, model = %cfg.model, "fetching signed manifest");
        let manifest = ModelStore::fetch_manifest(url, pk)
            .await
            .context("fetch/verify manifest")?;
        let resolved = store
            .ensure(url, &cfg.model, &manifest)
            .await
            .context("download/verify model")?;
        return Ok(resolved.primary_path);
    }

    store
        .resolve_local(&cfg.model, None)
        .context("model not found locally; set model_path or manifest_url")
}

async fn build_backend(cfg: &ProviderConfig) -> Result<Arc<dyn InferenceBackend>> {
    match cfg.backend.as_str() {
        "mock" => Ok(Arc::new(MockBackend::default())),
        "llama" => {
            #[cfg(feature = "llama")]
            {
                let path = resolve_model_path(cfg).await?;
                Ok(Arc::new(sc_inference::LlamaCppBackend::load(
                    &cfg.model, &path,
                )?))
            }
            #[cfg(not(feature = "llama"))]
            {
                let _ = cfg;
                bail!("this build has no llama backend; rebuild with --features llama")
            }
        }
        other => bail!("unknown backend {other:?}"),
    }
}

/// Run the provider until `shutdown` is cancelled or the connection drops. Returns after a
/// clean stop; returns `Err` for a fatal setup error (bad config, model load failure).
pub async fn run(
    cfg: ProviderConfig,
    sink: Arc<dyn EventSink>,
    attest_hook: Option<Arc<dyn AttestHook>>,
    shutdown: CancellationToken,
) -> Result<()> {
    let id_path = cfg
        .identity_path
        .clone()
        .unwrap_or_else(identity::default_path);
    let keypair = Arc::new(identity::load_or_create(&id_path).context("identity")?);
    let static_pk_b64 = sc_crypto::encode_public(&keypair.public);

    let host = sc_telemetry::host_info();
    let backend = build_backend(&cfg).await?;
    let attest = NullAttestation;

    let manifest_gate = Arc::new(job::ManifestGate::new(
        &cfg.manifest_verify_keys,
        cfg.require_manifest,
    ));
    if !cfg.manifest_verify_keys.is_empty() || cfg.require_manifest {
        info!(
            keys = cfg.manifest_verify_keys.len(),
            require = cfg.require_manifest,
            "workload manifest verification enabled"
        );
    }

    // Hardware attestation (Tier 1) if the caller supplied a hook; else Tier 0.
    let hw_attestation: Option<proto::Attestation> = attest_hook
        .as_ref()
        .and_then(|h| h.evidence(*keypair.public.as_bytes()));
    let mut sec_caps = attest.security_capabilities();
    if hw_attestation.is_some() {
        sec_caps.hardware_key = true;
    }

    let caps = proto::Capabilities {
        platform: host.platform.clone(),
        arch: host.arch.clone(),
        cpu: Some(host.cpu.clone()),
        gpu: None,
        ram_mb: host.ram_mb,
        vram_mb: None,
        backend: backend.name().to_string(),
        hardware_class: hardware_class(host.ram_mb).to_string(),
        max_context: cfg.max_context,
        models: vec![cfg.model.clone()],
        security_capabilities: sec_caps,
    };

    info!(url = %cfg.coordinator_url, platform = %caps.platform, backend = %caps.backend,
        hw_class = %caps.hardware_class, model = %cfg.model, "connecting");
    sink.on_event(ProviderEvent::Connecting {
        url: cfg.coordinator_url.clone(),
    });

    let ws = match sc_net::connect(&cfg.coordinator_url).await {
        Ok(ws) => ws,
        Err(e) => {
            sink.on_event(ProviderEvent::Error {
                message: format!("connect: {e}"),
            });
            return Err(e);
        }
    };
    let (mut ws_sink, mut stream) = {
        use futures_util::StreamExt as _;
        ws.split()
    };

    let (tx, mut rx) = mpsc::unbounded_channel::<WsMessage>();
    let writer = tokio::spawn(async move {
        use futures_util::SinkExt;
        while let Some(msg) = rx.recv().await {
            if ws_sink.send(msg).await.is_err() {
                break;
            }
        }
    });

    let register = proto::Register {
        kind: proto::msg_type::REGISTER.into(),
        agent: AGENT.into(),
        registration_token: cfg.registration_token.clone(),
        static_pk: static_pk_b64,
        capabilities: caps,
        attestation: Some(hw_attestation.unwrap_or_else(|| attest.attestation())),
        resume_provider_id: None,
    };
    tx.send(WsMessage::Text(String::from_utf8(proto::to_frame(
        &register, None,
    )?)?))
    .ok();

    let ack_bytes = sc_net::next_text(&mut stream)
        .await?
        .context("connection closed before register_ack")?;
    let env = proto::parse_envelope(&ack_bytes).context("register_ack envelope")?;
    if env.kind != proto::msg_type::REGISTER_ACK {
        bail!("expected register_ack, got {}", env.kind);
    }
    let ack: proto::RegisterAck = proto::from_frame(&ack_bytes)?;
    if !ack.ok {
        let reason = ack.error.unwrap_or_default();
        sink.on_event(ProviderEvent::Error {
            message: format!("registration rejected: {reason}"),
        });
        bail!("registration rejected: {reason}");
    }
    let provider_id = ack.provider_id.clone().unwrap_or_default();
    let tier = ack.trust_tier.clone().unwrap_or_else(|| "community".into());
    let hb_secs = ack.heartbeat_seconds.unwrap_or(15).max(5) as u64;
    info!(provider_id = %provider_id, tier = %tier, heartbeat_s = hb_secs, "registered");
    sink.on_event(ProviderEvent::Registered {
        provider_id: provider_id.clone(),
        trust_tier: tier,
    });

    let active = Arc::new(AtomicU32::new(0));
    let jobs: Arc<Mutex<HashMap<String, CancellationToken>>> = Arc::new(Mutex::new(HashMap::new()));

    // Self-benchmark → benchmark_report. Runs off the hot path; results are cached
    // on disk so this is a no-op on most reconnects.
    {
        let tx = tx.clone();
        let backend = backend.clone();
        let model = cfg.model.clone();
        let data_dir = id_path
            .parent()
            .map(|p| p.to_path_buf())
            .unwrap_or_else(|| std::path::PathBuf::from("."));
        tokio::spawn(async move {
            benchmark::run_or_cached(&backend, &model, &data_dir, |report| {
                if let Ok(bytes) = proto::to_frame(&report, None) {
                    if let Ok(s) = String::from_utf8(bytes) {
                        let _ = tx.send(WsMessage::Text(s));
                    }
                }
            })
            .await;
        });
    }

    // Heartbeat.
    {
        let tx = tx.clone();
        let active = active.clone();
        let shutdown = shutdown.clone();
        tokio::spawn(async move {
            let mut tick = tokio::time::interval(std::time::Duration::from_secs(hb_secs));
            loop {
                tokio::select! {
                    _ = shutdown.cancelled() => break,
                    _ = tick.tick() => {}
                }
                let telem = sc_telemetry::sample_telemetry(active.load(Ordering::Relaxed), 0);
                let hb = proto::Heartbeat {
                    kind: proto::msg_type::HEARTBEAT.into(),
                    telemetry: telem,
                };
                let sent = proto::to_frame(&hb, None)
                    .ok()
                    .and_then(|b| String::from_utf8(b).ok())
                    .map(|s| tx.send(WsMessage::Text(s)).is_ok())
                    .unwrap_or(false);
                if !sent {
                    break;
                }
            }
        });
    }

    let mut disconnect_reason = "shutdown".to_string();
    loop {
        tokio::select! {
            _ = shutdown.cancelled() => break,
            frame = sc_net::next_text(&mut stream) => {
                let bytes = match frame {
                    Ok(Some(b)) => b,
                    Ok(None) => { disconnect_reason = "coordinator closed the connection".into(); break; }
                    Err(e) => { error!(error = %e, "read error"); disconnect_reason = format!("read error: {e}"); break; }
                };
                let env = match proto::parse_envelope(&bytes) {
                    Ok(e) => e,
                    Err(e) => { warn!(error = %e, "bad frame"); continue; }
                };
                match env.kind.as_str() {
                    proto::msg_type::HEARTBEAT_ACK => {}
                    proto::msg_type::JOB_REQUEST => {
                        let jr: proto::JobRequest = match proto::from_frame(&bytes) {
                            Ok(j) => j,
                            Err(e) => { warn!(error = %e, "bad job_request"); continue; }
                        };
                        let token = CancellationToken::new();
                        let job_id = jr.job_id.clone();
                        jobs.lock().await.insert(job_id.clone(), token.clone());
                        sink.on_event(ProviderEvent::JobStarted { job_id: job_id.clone() });
                        let (identity, backend, tx2, active2, jobs2, sink2, gate2) = (
                            keypair.clone(), backend.clone(), tx.clone(),
                            active.clone(), jobs.clone(), sink.clone(), manifest_gate.clone(),
                        );
                        tokio::spawn(async move {
                            let out = job::handle_job(jr, identity, backend, tx2, token, active2, gate2).await;
                            jobs2.lock().await.remove(&job_id);
                            sink2.on_event(ProviderEvent::JobFinished {
                                job_id,
                                ok: out.ok,
                                completion_tokens: out.completion_tokens,
                                decode_tps: out.decode_tps,
                            });
                        });
                    }
                    proto::msg_type::CANCEL => {
                        if let Ok(c) = proto::from_frame::<proto::Cancel>(&bytes) {
                            if let Some(tok) = jobs.lock().await.get(&c.job_id) {
                                tok.cancel();
                            }
                        }
                    }
                    proto::msg_type::MODEL_PULL => {
                        warn!("model_pull received but coordinator-driven model management is not implemented yet");
                    }
                    other => warn!(kind = other, "ignoring frame"),
                }
            }
        }
    }

    for (_, tok) in jobs.lock().await.drain() {
        tok.cancel();
    }
    drop(tx);
    let _ = writer.await;
    info!(reason = %disconnect_reason, "stopped");
    sink.on_event(ProviderEvent::Disconnected {
        reason: disconnect_reason,
    });
    Ok(())
}
