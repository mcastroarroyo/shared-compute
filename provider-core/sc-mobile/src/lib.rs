//! `sc-mobile` — a uniffi wrapper over `provider-lib` for the Android app (and later iOS).
//!
//! Kotlin gets a `Provider` object with `start(config, listener)` / `stop()`, and a
//! `ProviderListener` callback for lifecycle events.

use std::path::PathBuf;
use std::sync::Mutex;

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
        ProviderEvent::JobFinished { job_id, ok } => SpEvent::JobFinished { job_id, ok },
        ProviderEvent::Disconnected { reason } => SpEvent::Disconnected { reason },
        ProviderEvent::Error { message } => SpEvent::Error { message },
    }
}

/// Implemented in Kotlin; receives provider lifecycle events on a background thread.
#[uniffi::export(with_foreign)]
pub trait ProviderListener: Send + Sync {
    fn on_event(&self, event: SpEvent);
}

struct Bridge(std::sync::Arc<dyn ProviderListener>);
impl EventSink for Bridge {
    fn on_event(&self, ev: ProviderEvent) {
        self.0.on_event(map_event(ev));
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
        std::sync::Arc::new(Self {
            rt,
            inner: Mutex::new(None),
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
        listener: std::sync::Arc<dyn ProviderListener>,
    ) -> Result<(), SpError> {
        let mut guard = self.inner.lock().unwrap();
        if guard.as_ref().is_some_and(|r| !r.handle.is_finished()) {
            return Err(SpError::AlreadyRunning);
        }
        let cfg: ProviderConfig = config.into();
        let sink = std::sync::Arc::new(Bridge(listener));
        let cancel = CancellationToken::new();
        let c2 = cancel.clone();
        let handle = self.rt.spawn(async move {
            if let Err(e) = provider_lib::run(cfg, sink.clone(), c2).await {
                sink.on_event(ProviderEvent::Error {
                    message: e.to_string(),
                });
            }
        });
        *guard = Some(Running { cancel, handle });
        Ok(())
    }

    /// Stop the provider and wait briefly for it to wind down.
    pub fn stop(&self) {
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
        let _ = tracing_subscriber::fmt()
            .with_env_filter(
                tracing_subscriber::EnvFilter::try_from_env("SC_LOG")
                    .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info,sc_mobile=debug")),
            )
            .with_ansi(false)
            .try_init();
    });
}
