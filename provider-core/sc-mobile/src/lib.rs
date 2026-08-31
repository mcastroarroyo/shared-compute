//! `sc-mobile` — a uniffi wrapper over `provider-lib` for the Android app (and later iOS).
//!
//! Kotlin gets a `Provider` object with `start(config, listener)` / `stop()`, and a
//! `ProviderListener` callback for lifecycle events.

use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};

use provider_lib::{EventSink, ProviderConfig, ProviderEvent};
use tokio::runtime::Runtime;
use tokio_util::sync::CancellationToken;

uniffi::setup_scaffolding!();

/// Lifecycle events surfaced to the app. Mirrors `provider_lib::ProviderEvent`.
#[derive(uniffi::Enum, Debug, Clone)]
pub enum SpEvent {
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
        completion_tokens: u32,
        decode_tps: f32,
    },
    Disconnected {
        reason: String,
    },
    Error {
        message: String,
    },
}

fn map_event(ev: ProviderEvent) -> SpEvent {
    match ev {
        ProviderEvent::Connecting { url } => SpEvent::Connecting { url },
        ProviderEvent::Registered {
            provider_id,
            trust_tier,
        } => SpEvent::Registered {
            provider_id,
            trust_tier,
        },
        ProviderEvent::JobStarted { job_id } => SpEvent::JobStarted { job_id },
        ProviderEvent::JobFinished {
            job_id,
            ok,
            completion_tokens,
            decode_tps,
        } => SpEvent::JobFinished {
            job_id,
            ok,
            completion_tokens,
            decode_tps,
        },
        ProviderEvent::Disconnected { reason } => SpEvent::Disconnected { reason },
        ProviderEvent::Error { message } => SpEvent::Error { message },
    }
}

/// Implemented in Kotlin; receives provider lifecycle events on a background thread.
#[uniffi::export(with_foreign)]
pub trait ProviderListener: Send + Sync {
    fn on_event(&self, event: SpEvent);
}

/// Forwards events to the app, but drops events from a superseded `start()` so a late
/// error/disconnect from an old run can't clobber the current run's status.
struct Bridge {
    listener: Arc<dyn ProviderListener>,
    generation: u64,
    current: Arc<AtomicU64>,
}
impl EventSink for Bridge {
    fn on_event(&self, ev: ProviderEvent) {
        if self.generation == self.current.load(Ordering::Acquire) {
            self.listener.on_event(map_event(ev));
        }
    }
}

/// Android Key Attestation evidence, produced in Kotlin from the Keystore.
#[derive(uniffi::Record, Clone)]
pub struct AndroidAttestation {
    /// X.509 certs, leaf first, each base64(DER).
    pub cert_chain_der_b64: Vec<String>,
    /// base64 ECDSA-P256/SHA-256 over `"ayni-bind-v1" || static_pk || nonce || be64(issued_at)`.
    pub binding_sig_b64: String,
    pub nonce_b64: String,
    pub issued_at: i64,
}

/// Implemented in Kotlin; signs the provider's X25519 static key with the
/// hardware-backed device key and returns the attestation chain.
#[uniffi::export(with_foreign)]
pub trait AttestationSigner: Send + Sync {
    fn sign(&self, static_pk: Vec<u8>) -> Option<AndroidAttestation>;
}

struct AttestBridge {
    signer: Arc<dyn AttestationSigner>,
}
impl provider_lib::AttestHook for AttestBridge {
    fn evidence(&self, static_pk: [u8; 32]) -> Option<provider_lib::proto::Attestation> {
        let a = self.signer.sign(static_pk.to_vec())?;
        Some(provider_lib::proto::Attestation {
            kind: "android_key".to_string(),
            evidence: Some(serde_json::json!({
                "cert_chain_der_b64": a.cert_chain_der_b64,
                "binding_sig_b64": a.binding_sig_b64,
                "nonce_b64": a.nonce_b64,
                "issued_at": a.issued_at,
            })),
        })
    }
}

/// Provider configuration from the app. `data_dir` is the app's private storage; the
/// identity key and model cache live under it.
#[derive(uniffi::Record, Clone)]
pub struct MobileConfig {
    pub coordinator_url: String,
    pub registration_token: String,
    pub model: String,
    /// App private dir, e.g. `context.filesDir` — identity + model cache go here.
    pub data_dir: String,
    /// "mock" or "llama"
    pub backend: String,
    pub manifest_url: Option<String>,
    pub registry_pubkey: Option<String>,
    pub max_context: u32,
    /// Coordinator Workload Manifest v1 signing key(s), comma-separated,
    /// each "signer-id:<base64>" or bare "<base64>".
    #[uniffi(default = None)]
    pub manifest_verify_key: Option<String>,
    /// Reject any job that arrives without a valid signed manifest.
    #[uniffi(default = false)]
    pub require_manifest: bool,
}

impl From<MobileConfig> for ProviderConfig {
    fn from(c: MobileConfig) -> Self {
        let data = PathBuf::from(&c.data_dir);
        ProviderConfig {
            coordinator_url: c.coordinator_url,
            registration_token: c.registration_token,
            model: c.model,
            model_path: None,
            backend: c.backend,
            manifest_url: c.manifest_url,
            registry_pubkey: c.registry_pubkey,
            model_dir: Some(data.join("models")),
            identity_path: Some(data.join("identity.key")),
            max_context: c.max_context,
            manifest_verify_keys: c
                .manifest_verify_key
                .unwrap_or_default()
                .split(',')
                .map(|s| s.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect(),
            require_manifest: c.require_manifest,
        }
    }
}

#[derive(uniffi::Error, Debug, thiserror::Error)]
pub enum SpError {
    #[error("already running")]
    AlreadyRunning,
    #[error("{0}")]
    Start(String),
}

/// The provider handle. Create once, `start` / `stop` as needed.
#[derive(uniffi::Object)]
pub struct Provider {
    rt: Runtime,
    inner: Mutex<Option<Running>>,
    generation: Arc<AtomicU64>,
}

struct Running {
    cancel: CancellationToken,
    handle: tokio::task::JoinHandle<()>,
}

#[uniffi::export]
impl Provider {
    #[uniffi::constructor]
    pub fn new() -> std::sync::Arc<Self> {
        init_logging();
        let _ = rustls::crypto::ring::default_provider().install_default();
        let rt = tokio::runtime::Builder::new_multi_thread()
            .worker_threads(2)
            .enable_all()
            .build()
            .expect("tokio runtime");
        Arc::new(Self {
            rt,
            inner: Mutex::new(None),
            generation: Arc::new(AtomicU64::new(0)),
        })
    }

    pub fn is_running(&self) -> bool {
        self.inner
            .lock()
            .unwrap()
            .as_ref()
            .is_some_and(|r| !r.handle.is_finished())
    }

    /// Start the provider. Events (including terminal `Disconnected`/`Error`) go to
    /// `listener`. Returns immediately; the provider runs until `stop` or disconnect.
    pub fn start(
        &self,
        config: MobileConfig,
        listener: Arc<dyn ProviderListener>,
        attestation_signer: Option<Arc<dyn AttestationSigner>>,
    ) -> Result<(), SpError> {
        let mut guard = self.inner.lock().unwrap();
        if guard.as_ref().is_some_and(|r| !r.handle.is_finished()) {
            return Err(SpError::AlreadyRunning);
        }
        // New generation: any trailing events from a previous run are now ignored.
        let generation = self.generation.fetch_add(1, Ordering::AcqRel) + 1;
        let sink = Arc::new(Bridge {
            listener,
            generation,
            current: self.generation.clone(),
        });
        let attest_hook: Option<Arc<dyn provider_lib::AttestHook>> = attestation_signer
            .map(|s| Arc::new(AttestBridge { signer: s }) as Arc<dyn provider_lib::AttestHook>);
        let cfg: ProviderConfig = config.into();
        let cancel = CancellationToken::new();
        let c2 = cancel.clone();
        let handle = self.rt.spawn(async move {
            if let Err(e) = provider_lib::run(cfg, sink.clone(), attest_hook, c2).await {
                sink.on_event(ProviderEvent::Error {
                    message: e.to_string(),
                });
            }
        });
        *guard = Some(Running { cancel, handle });
        Ok(())
    }

    /// Stop the provider. Trailing events from the stopped run are ignored.
    pub fn stop(&self) {
        self.generation.fetch_add(1, Ordering::AcqRel);
        if let Some(r) = self.inner.lock().unwrap().take() {
            r.cancel.cancel();
            r.handle.abort();
        }
    }
}

fn init_logging() {
    use std::sync::Once;
    static ONCE: Once = Once::new();
    ONCE.call_once(|| {
        let filter = tracing_subscriber::EnvFilter::try_from_env("SC_LOG").unwrap_or_else(|_| {
            tracing_subscriber::EnvFilter::new("info,sc_mobile=debug,provider_lib=debug")
        });
        #[cfg(target_os = "android")]
        {
            use tracing_subscriber::layer::SubscriberExt;
            use tracing_subscriber::util::SubscriberInitExt;
            if let Ok(logcat) = tracing_android::layer("ayni-provider") {
                let _ = tracing_subscriber::registry()
                    .with(filter)
                    .with(logcat)
                    .try_init();
                return;
            }
        }
        let _ = tracing_subscriber::fmt()
            .with_env_filter(filter)
            .with_ansi(false)
            .try_init();
    });
}
