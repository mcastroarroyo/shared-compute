//! Real `llama.cpp` backend. Skeleton only — wired up in the milestone after M1
//! (embed llama.cpp's C API via `bindgen`, Metal on macOS / CPU elsewhere, then
//! feature-gated CUDA / Vulkan / OpenCL).

use async_trait::async_trait;
use std::path::Path;
use tokio_util::sync::CancellationToken;

use crate::{Completion, InferenceBackend, InferenceError, InferenceRequest};

pub struct LlamaCppBackend {
    model_id: String,
}

impl LlamaCppBackend {
    /// Load a GGUF model from `path`. Not implemented yet.
    pub fn load(model_id: &str, _path: &Path) -> Result<Self, InferenceError> {
        Err(InferenceError::Backend(format!(
            "llama.cpp backend not implemented yet (model {model_id})"
        )))
    }
}

#[async_trait]
impl InferenceBackend for LlamaCppBackend {
    fn name(&self) -> &str {
        "llama-cpp"
    }

    fn has_model(&self, model: &str) -> bool {
        model == self.model_id
    }

    async fn generate(
        &self,
        _req: &InferenceRequest,
        _on_token: &mut (dyn FnMut(String) + Send),
        _cancel: CancellationToken,
    ) -> Result<Completion, InferenceError> {
        Err(InferenceError::Backend(
            "llama.cpp backend not implemented yet".into(),
        ))
    }
}
