//! provider-daemon — connects to a coordinator, registers this device as an inference
//! provider, and services sealed jobs with an in-process backend.

mod identity;
mod job;

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::{
    atomic::{AtomicU32, Ordering},
    Arc,
};

use anyhow::{bail, Context, Result};
use clap::Parser;
use futures_util::StreamExt;
use sc_attest::{AttestationProvider, NullAttestation};
use sc_inference::{InferenceBackend, MockBackend};
use sc_net::WsMessage;
use sc_protocol as proto;
use tokio::sync::{mpsc, Mutex};
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};
use tracing_subscriber::EnvFilter;

const AGENT: &str = concat!("provider-daemon/", env!("CARGO_PKG_VERSION"));

#[derive(Parser, Debug)]
#[command(version, about = "shared-compute inference provider")]
struct Args {
    /// Coordinator provider WebSocket URL.
    #[arg(
        long,
        env = "SC_COORDINATOR_URL",
        default_value = "ws://127.0.0.1:8080/ws/provider"
    )]
    coordinator_url: String,

    /// Provider registration token (issued out-of-band by the coordinator operator).
    #[arg(
        long,
        env = "SC_REGISTRATION_TOKEN",
        default_value = "dev-provider-token"
    )]
    registration_token: String,

    /// Model id to advertise.
    #[arg(long, env = "SC_MODEL", default_value = "mock-echo")]
    model: String,

    /// Explicit GGUF path (llama backend). Ignored by the mock backend.
    #[arg(long, env = "SC_MODEL_PATH")]
    model_path: Option<PathBuf>,

    /// Backend: "mock" or "llama" (llama requires the `llama` build feature).
    #[arg(long, env = "SC_BACKEND", default_value = "mock")]
    backend: String,

    /// Identity key file. Default: $HOME/.shared-compute/identity.key
    #[arg(long, env = "SC_IDENTITY_PATH")]
    identity_path: Option<PathBuf>,

    /// Max context length to advertise.
    #[arg(long, default_value_t = 8192)]
    max_context: u32,
}

fn hardware_class(ram_mb: u64) -> &'static str {
    match ram_mb {
        0..=6143 => "MICRO",
        6144..=16383 => "SMALL",
        16384..=32767 => "MEDIUM",
        32768..=65535 => "LARGE",
        _ => "XL",
    }
}

fn build_backend(args: &Args) -> Result<Arc<dyn InferenceBackend>> {
    match args.backend.as_str() {
        "mock" => Ok(Arc::new(MockBackend::default())),
        "llama" => {
            #[cfg(feature = "llama")]
            {
                let path = args
                    .model_path
                    .clone()
                    .context("--model-path is required for the llama backend")?;
                Ok(Arc::new(sc_inference::LlamaCppBackend::load(
                    &args.model,
                    &path,
                )?))
            }
            #[cfg(not(feature = "llama"))]
            {
                let _ = args;
                bail!("this build has no llama backend; rebuild with --features llama")
            }
        }
        other => bail!("unknown backend {other:?}"),
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_env("SC_LOG").unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .init();

    // rustls 0.23 needs a process-wide crypto provider selected explicitly.
    rustls::crypto::ring::default_provider()
        .install_default()
        .ok();

    let args = Args::parse();

    let id_path = args
        .identity_path
        .clone()
        .unwrap_or_else(identity::default_path);
    let keypair = Arc::new(identity::load_or_create(&id_path).context("identity")?);
    let static_pk_b64 = sc_crypto::encode_public(&keypair.public);

    let host = sc_telemetry::host_info();
    let backend = build_backend(&args)?;
    let attest = NullAttestation;

    let caps = proto::Capabilities {
        platform: host.platform.clone(),
        arch: host.arch.clone(),
        cpu: Some(host.cpu.clone()),
        gpu: None,
        ram_mb: host.ram_mb,
        vram_mb: None,
        backend: backend.name().to_string(),
        hardware_class: hardware_class(host.ram_mb).to_string(),
        max_context: args.max_context,
        models: vec![args.model.clone()],
        security_capabilities: attest.security_capabilities(),
    };

    info!(
        url = %args.coordinator_url, platform = %caps.platform, arch = %caps.arch,
        backend = %caps.backend, hw_class = %caps.hardware_class, model = %args.model,
        "connecting"
    );

    let ws = sc_net::connect(&args.coordinator_url).await?;
    let (sink, mut stream) = ws.split();

    // Single writer task: everything sends through this channel.
    let (tx, mut rx) = mpsc::unbounded_channel::<WsMessage>();
    let writer = tokio::spawn(async move {
        let mut sink = sink;
        use futures_util::SinkExt;
        while let Some(msg) = rx.recv().await {
            if sink.send(msg).await.is_err() {
                break;
            }
        }
    });

    // Register.
    let register = proto::Register {
        kind: proto::msg_type::REGISTER.into(),
        agent: AGENT.into(),
        registration_token: args.registration_token.clone(),
        static_pk: static_pk_b64,
        capabilities: caps,
        attestation: Some(attest.attestation()),
        resume_provider_id: None,
    };
    tx.send(WsMessage::Text(String::from_utf8(proto::to_frame(
        &register, None,
    )?)?))
    .ok();

    // Await register_ack.
    let ack_bytes = sc_net::next_text(&mut stream)
        .await?
        .context("connection closed before register_ack")?;
    let env = proto::parse_envelope(&ack_bytes).context("register_ack envelope")?;
    if env.kind != proto::msg_type::REGISTER_ACK {
        bail!("expected register_ack, got {}", env.kind);
    }
    let ack: proto::RegisterAck = proto::from_frame(&ack_bytes)?;
    if !ack.ok {
        bail!("registration rejected: {}", ack.error.unwrap_or_default());
    }
    let provider_id = ack.provider_id.clone().unwrap_or_default();
    let hb_secs = ack.heartbeat_seconds.unwrap_or(15).max(5) as u64;
    info!(provider_id = %provider_id, tier = ?ack.trust_tier, heartbeat_s = hb_secs, "registered");

    let active = Arc::new(AtomicU32::new(0));
    let jobs: Arc<Mutex<HashMap<String, CancellationToken>>> = Arc::new(Mutex::new(HashMap::new()));

    // Heartbeat task.
    {
        let tx = tx.clone();
        let active = active.clone();
        tokio::spawn(async move {
            let mut tick = tokio::time::interval(std::time::Duration::from_secs(hb_secs));
            loop {
                tick.tick().await;
                let telem = sc_telemetry::sample_telemetry(active.load(Ordering::Relaxed), 0);
                let hb = proto::Heartbeat {
                    kind: proto::msg_type::HEARTBEAT.into(),
                    telemetry: telem,
                };
                match proto::to_frame(&hb, None) {
                    Ok(b) => {
                        if String::from_utf8(b)
                            .map(|s| tx.send(WsMessage::Text(s)).is_err())
                            .unwrap_or(true)
                        {
                            break;
                        }
                    }
                    Err(_) => break,
                }
            }
        });
    }

    // Main receive loop.
    let shutdown = CancellationToken::new();
    let sd = shutdown.clone();
    tokio::spawn(async move {
        let _ = tokio::signal::ctrl_c().await;
        info!("shutdown requested");
        sd.cancel();
    });

    loop {
        tokio::select! {
            _ = shutdown.cancelled() => break,
            frame = sc_net::next_text(&mut stream) => {
                let bytes = match frame {
                    Ok(Some(b)) => b,
                    Ok(None) => { warn!("coordinator closed the connection"); break; }
                    Err(e) => { error!(error = %e, "read error"); break; }
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
                        jobs.lock().await.insert(jr.job_id.clone(), token.clone());
                        let (identity, backend, tx2, active2, jobs2) =
                            (keypair.clone(), backend.clone(), tx.clone(), active.clone(), jobs.clone());
                        let job_id = jr.job_id.clone();
                        tokio::spawn(async move {
                            job::handle_job(jr, identity, backend, tx2, token, active2).await;
                            jobs2.lock().await.remove(&job_id);
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
                        // M3: download + verify against the signed manifest.
                        warn!("model_pull received but model management is not implemented yet");
                    }
                    other => warn!(kind = other, "ignoring frame"),
                }
            }
        }
    }

    // Cancel in-flight jobs and drain.
    for (_, tok) in jobs.lock().await.drain() {
        tok.cancel();
    }
    drop(tx);
    let _ = writer.await;
    info!("stopped");
    Ok(())
}
