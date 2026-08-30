//! Real `llama.cpp` backend, embedded in-process via the `llama-cpp-2` crate.
//! Metal on Apple Silicon, CPU elsewhere; CUDA/Vulkan/OpenCL come in later milestones.
//!
//! Prompt and completion text never leave this module except through `on_token`.

use std::num::NonZeroU32;
use std::path::Path;
use std::sync::{Arc, OnceLock};
use std::time::Instant;

use async_trait::async_trait;
use llama_cpp_2::context::params::LlamaContextParams;
use llama_cpp_2::llama_backend::LlamaBackend;
use llama_cpp_2::llama_batch::LlamaBatch;
use llama_cpp_2::model::params::LlamaModelParams;
use llama_cpp_2::model::{AddBos, LlamaChatMessage, LlamaModel};
use llama_cpp_2::sampling::LlamaSampler;
use tokio_util::sync::CancellationToken;

use crate::{Completion, InferenceBackend, InferenceError, InferenceRequest};
use sc_protocol::Usage;

fn backend() -> Result<&'static LlamaBackend, InferenceError> {
    static BACKEND: OnceLock<LlamaBackend> = OnceLock::new();
    if let Some(b) = BACKEND.get() {
        return Ok(b);
    }
    let b = LlamaBackend::init().map_err(|e| InferenceError::Backend(e.to_string()))?;
    Ok(BACKEND.get_or_init(|| b))
}

pub struct LlamaCppBackend {
    model_id: String,
    model: Arc<LlamaModel>,
    n_ctx: u32,
}

impl LlamaCppBackend {
    /// Load a GGUF model. Offloads all layers to the GPU where one is available.
    pub fn load(model_id: &str, path: &Path) -> Result<Self, InferenceError> {
        if !path.is_file() {
            return Err(InferenceError::ModelMissing(path.display().to_string()));
        }
        let be = backend()?;
        let params = LlamaModelParams::default().with_n_gpu_layers(u32::MAX);
        let model = LlamaModel::load_from_file(be, path, &params)
            .map_err(|e| InferenceError::Backend(format!("load {}: {e}", path.display())))?;
        Ok(Self {
            model_id: model_id.to_string(),
            model: Arc::new(model),
            n_ctx: 8192,
        })
    }

    fn render_prompt(&self, req: &InferenceRequest) -> String {
        let chat: Result<Vec<LlamaChatMessage>, _> = req
            .messages
            .iter()
            .map(|m| LlamaChatMessage::new(m.role.clone(), m.content.clone()))
            .collect();
        if let Ok(chat) = chat {
            if let Ok(tmpl) = self.model.chat_template(None) {
                if let Ok(s) = self.model.apply_chat_template(&tmpl, &chat, true) {
                    return s;
                }
            }
        }
        // Fallback: a plain role-tagged transcript.
        let mut s = String::new();
        for m in &req.messages {
            s.push_str(&format!("<|{}|>\n{}\n", m.role, m.content));
        }
        s.push_str("<|assistant|>\n");
        s
    }
}

struct Summary {
    prompt_tokens: u32,
    completion_tokens: u32,
    finish_reason: &'static str,
    prefill_ms: f64,
    decode_ms: f64,
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
        req: &InferenceRequest,
        on_token: &mut (dyn FnMut(String) + Send),
        cancel: CancellationToken,
    ) -> Result<Completion, InferenceError> {
        let prompt = self.render_prompt(req);
        let max_tokens = req.params.max_tokens.max(1);
        let temperature = req.params.temperature.unwrap_or(0.8) as f32;
        let top_p = req.params.top_p.unwrap_or(0.95) as f32;
        let top_k = req.params.top_k.unwrap_or(40) as i32;
        let seed = req.params.seed.map(|s| s as u32).unwrap_or(0xDEAD_BEEF);
        let n_ctx = self.n_ctx;
        let model = Arc::clone(&self.model);

        let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel::<String>();

        // Inference is blocking CPU/GPU work: run it off the async worker so token
        // deltas stream to the caller as they are produced.
        let worker = tokio::task::spawn_blocking(move || {
            run_inference(
                backend()?,
                model.as_ref(),
                &prompt,
                max_tokens,
                temperature,
                top_p,
                top_k,
                seed,
                n_ctx,
                &cancel,
                &tx,
            )
        });

        while let Some(piece) = rx.recv().await {
            on_token(piece);
        }

        let s = worker
            .await
            .map_err(|e| InferenceError::Backend(format!("worker join: {e}")))??;
        let total = s.prompt_tokens + s.completion_tokens;
        Ok(Completion {
            usage: Usage {
                prompt_tokens: s.prompt_tokens,
                completion_tokens: s.completion_tokens,
                total_tokens: total,
            },
            finish_reason: s.finish_reason.to_string(),
            prefill_tps: if s.prefill_ms > 0.0 {
                s.prompt_tokens as f64 / (s.prefill_ms / 1000.0)
            } else {
                0.0
            },
            decode_tps: if s.decode_ms > 0.0 {
                s.completion_tokens as f64 / (s.decode_ms / 1000.0)
            } else {
                0.0
            },
        })
    }
}

#[allow(clippy::too_many_arguments)]
fn run_inference(
    be: &LlamaBackend,
    model: &LlamaModel,
    prompt: &str,
    max_tokens: u32,
    temperature: f32,
    top_p: f32,
    top_k: i32,
    seed: u32,
    n_ctx: u32,
    cancel: &CancellationToken,
    tx: &tokio::sync::mpsc::UnboundedSender<String>,
) -> Result<Summary, InferenceError> {
    let ctx_params = LlamaContextParams::default().with_n_ctx(NonZeroU32::new(n_ctx.max(512)));
    let mut ctx = model
        .new_context(be, ctx_params)
        .map_err(|e| InferenceError::Backend(e.to_string()))?;

    let tokens = model
        .str_to_token(prompt, AddBos::Always)
        .map_err(|e| InferenceError::Backend(format!("tokenize: {e}")))?;
    let prompt_tokens = tokens.len() as u32;
    if prompt_tokens >= n_ctx {
        return Err(InferenceError::Backend(
            "prompt exceeds context window".into(),
        ));
    }

    let batch_cap = tokens.len().max(512);
    let mut batch = LlamaBatch::new(batch_cap, 1);
    let last = tokens.len() - 1;
    for (i, tok) in tokens.iter().enumerate() {
        batch
            .add(*tok, i as i32, &[0], i == last)
            .map_err(|e| InferenceError::Backend(e.to_string()))?;
    }

    let prefill_start = Instant::now();
    ctx.decode(&mut batch)
        .map_err(|e| InferenceError::Backend(e.to_string()))?;
    let prefill_ms = prefill_start.elapsed().as_secs_f64() * 1000.0;

    let mut sampler = if temperature <= 0.0 {
        LlamaSampler::chain_simple([LlamaSampler::greedy()])
    } else {
        LlamaSampler::chain_simple([
            LlamaSampler::top_k(top_k),
            LlamaSampler::top_p(top_p, 1),
            LlamaSampler::temp(temperature),
            LlamaSampler::dist(seed),
        ])
    };

    let mut n_cur = batch.n_tokens();
    let mut completion_tokens = 0u32;
    let mut finish_reason = "stop";
    let mut decoder = encoding_rs::UTF_8.new_decoder();
    let decode_start = Instant::now();

    while completion_tokens < max_tokens {
        if cancel.is_cancelled() {
            finish_reason = "cancel";
            break;
        }

        let token = sampler.sample(&ctx, batch.n_tokens() - 1);
        sampler.accept(token);

        if model.is_eog_token(token) {
            finish_reason = "stop";
            break;
        }

        let piece = model
            .token_to_piece(token, &mut decoder, false, None)
            .unwrap_or_default();
        if !piece.is_empty() && tx.send(piece).is_err() {
            finish_reason = "cancel";
            break;
        }
        completion_tokens += 1;

        batch.clear();
        batch
            .add(token, n_cur, &[0], true)
            .map_err(|e| InferenceError::Backend(e.to_string()))?;
        n_cur += 1;

        ctx.decode(&mut batch)
            .map_err(|e| InferenceError::Backend(e.to_string()))?;
    }

    if completion_tokens >= max_tokens {
        finish_reason = "length";
    }
    let decode_ms = decode_start.elapsed().as_secs_f64() * 1000.0;

    Ok(Summary {
        prompt_tokens,
        completion_tokens,
        finish_reason,
        prefill_ms,
        decode_ms,
    })
}
