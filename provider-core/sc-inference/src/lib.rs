//! Inference backend abstraction. M1 ships [`MockBackend`]; the real `llama.cpp` backend
//! lands next behind the `llama` feature.

use async_trait::async_trait;
use sc_protocol::{ChatMessage, SamplingParams, Usage};
use tokio_util::sync::CancellationToken;

#[derive(Debug, thiserror::Error)]
pub enum InferenceError {
    #[error("model not loaded: {0}")]
    ModelMissing(String),
    #[error("out of memory")]
    Oom,
    #[error("cancelled")]
    Cancelled,
    #[error("backend failure: {0}")]
    Backend(String),
}

pub struct InferenceRequest {
    pub model: String,
    pub messages: Vec<ChatMessage>,
    pub params: SamplingParams,
}

/// Result of a completed generation.
pub struct Completion {
    pub usage: Usage,
    pub finish_reason: String,
    pub prefill_tps: f64,
    pub decode_tps: f64,
}

/// A backend runs a model in-process. Implementations MUST NOT log prompt or completion
/// text.
#[async_trait]
pub trait InferenceBackend: Send + Sync {
    /// Short identifier, e.g. "mock", "llama-cpp-metal".
    fn name(&self) -> &str;

    /// Whether this backend currently has `model` loaded / available.
    fn has_model(&self, model: &str) -> bool;

    /// Generate a completion, invoking `on_token` for each decoded token's text.
    /// The token is passed owned so implementations need not keep a borrow alive across
    /// `.await` points. Must return promptly with [`InferenceError::Cancelled`] when
    /// `cancel` fires.
    async fn generate(
        &self,
        req: &InferenceRequest,
        on_token: &mut (dyn FnMut(String) + Send),
        cancel: CancellationToken,
    ) -> Result<Completion, InferenceError>;
}

mod mock;
pub use mock::MockBackend;

#[cfg(feature = "llama")]
mod llama;
#[cfg(feature = "llama")]
pub use llama::LlamaCppBackend;
