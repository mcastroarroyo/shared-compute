//! provider-daemon — CLI wrapper around `provider-lib`. Parses flags/env, runs the
//! provider until Ctrl-C or the connection drops.

use std::path::PathBuf;
use std::sync::Arc;

use anyhow::Result;
use clap::Parser;
use provider_lib::{EventSink, ProviderConfig, ProviderEvent};
use tokio_util::sync::CancellationToken;
use tracing::info;
use tracing_subscriber::EnvFilter;

#[derive(Parser, Debug)]
#[command(version, about = "shared-compute inference provider")]
struct Args {
    #[arg(
        long,
        env = "SC_COORDINATOR_URL",
        default_value = "ws://127.0.0.1:8080/ws/provider"
    )]
    coordinator_url: String,
    #[arg(
        long,
        env = "SC_REGISTRATION_TOKEN",
        default_value = "dev-provider-token"
    )]
    registration_token: String,
    #[arg(long, env = "SC_MODEL", default_value = "mock-echo")]
    model: String,
    #[arg(long, env = "SC_MODEL_PATH")]
    model_path: Option<PathBuf>,
    /// "mock" or "llama" (llama requires the `llama` build feature)
    #[arg(long, env = "SC_BACKEND", default_value = "mock")]
    backend: String,
    #[arg(long, env = "SC_MANIFEST_URL")]
    manifest_url: Option<String>,
    #[arg(long, env = "SC_REGISTRY_PUBKEY")]
    registry_pubkey: Option<String>,
    #[arg(long, env = "SC_MODEL_DIR")]
    model_dir: Option<PathBuf>,
    #[arg(long, env = "SC_IDENTITY_PATH")]
    identity_path: Option<PathBuf>,
    #[arg(long, default_value_t = 8192)]
    max_context: u32,
    /// Coordinator Workload Manifest v1 signing key(s), comma-separated,
    /// each "signer-id:<base64>" or bare "<base64>". Empty = don't verify.
    #[arg(long, env = "SC_MANIFEST_VERIFY_KEY", default_value = "")]
    manifest_verify_key: String,
    /// Reject any job that arrives without a valid signed manifest ("1" to enable).
    #[arg(long, env = "SC_REQUIRE_MANIFEST", default_value = "")]
    require_manifest: String,
}

struct LogSink;
impl EventSink for LogSink {
    fn on_event(&self, ev: ProviderEvent) {
        match ev {
            ProviderEvent::Connecting { url } => info!(%url, "connecting"),
            ProviderEvent::Registered {
                provider_id,
                trust_tier,
            } => {
                info!(%provider_id, %trust_tier, "registered")
            }
            ProviderEvent::JobStarted { job_id } => info!(%job_id, "job started"),
            ProviderEvent::JobFinished {
                job_id,
                ok,
                completion_tokens,
                decode_tps,
            } => {
                info!(%job_id, ok, completion_tokens, decode_tps, "job finished")
            }
            ProviderEvent::Disconnected { reason } => info!(%reason, "disconnected"),
            ProviderEvent::Error { message } => tracing::error!(%message, "provider error"),
        }
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_env("SC_LOG").unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .init();
    rustls::crypto::ring::default_provider()
        .install_default()
        .ok();

    let args = Args::parse();
    let cfg = ProviderConfig {
        coordinator_url: args.coordinator_url,
        registration_token: args.registration_token,
        model: args.model,
        model_path: args.model_path,
        backend: args.backend,
        manifest_url: args.manifest_url,
        registry_pubkey: args.registry_pubkey,
        model_dir: args.model_dir,
        identity_path: args.identity_path,
        max_context: args.max_context,
        manifest_verify_keys: args
            .manifest_verify_key
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect(),
        require_manifest: matches!(args.require_manifest.as_str(), "1" | "true" | "yes"),
    };

    let shutdown = CancellationToken::new();
    {
        let shutdown = shutdown.clone();
        tokio::spawn(async move {
            let _ = tokio::signal::ctrl_c().await;
            info!("shutdown requested");
            shutdown.cancel();
        });
    }

    provider_lib::run(cfg, Arc::new(LogSink), None, shutdown).await
}
