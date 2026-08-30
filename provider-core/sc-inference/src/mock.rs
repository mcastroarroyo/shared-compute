//! Deterministic mock backend. Lets the full encrypted pipeline (coordinator → sealed job
//! → provider → sealed chunks → consumer SSE) be tested end-to-end without building
//! `llama.cpp` or downloading weights.

use async_trait::async_trait;
use sc_protocol::Usage;
use std::time::Instant;
use tokio::time::{sleep, Duration};
use tokio_util::sync::CancellationToken;

use crate::{Completion, InferenceBackend, InferenceError, InferenceRequest};

/// Emits a canned reply derived from the last user message, one whitespace-delimited
/// token at a time, honouring `max_tokens` and cancellation.
pub struct MockBackend {
    inter_token: Duration,
}

impl Default for MockBackend {
    fn default() -> Self {
        Self {
            inter_token: Duration::from_millis(15),
        }
    }
}

impl MockBackend {
    pub fn new(inter_token_ms: u64) -> Self {
        Self {
            inter_token: Duration::from_millis(inter_token_ms),
        }
    }
}

#[async_trait]
impl InferenceBackend for MockBackend {
    fn name(&self) -> &str {
        "mock"
    }

    fn has_model(&self, _model: &str) -> bool {
        true
    }

    async fn generate(
        &self,
        req: &InferenceRequest,
        on_token: &mut (dyn FnMut(String) + Send),
        cancel: CancellationToken,
    ) -> Result<Completion, InferenceError> {
        let last_user = req
            .messages
            .iter()
            .rev()
            .find(|m| m.role == "user")
            .map(|m| m.content.clone())
            .unwrap_or_default();

        // Keep the echo short and deterministic.
        let echo: String = last_user
            .split_whitespace()
            .take(24)
            .collect::<Vec<_>>()
            .join(" ");
        let reply = format!("mock reply to: {echo}");
        let tokens: Vec<String> = reply.split_inclusive(' ').map(str::to_string).collect();

        let prompt_tokens: u32 = req
            .messages
            .iter()
            .map(|m| m.content.split_whitespace().count() as u32)
            .sum();

        let max = req.params.max_tokens.max(1);
        let start = Instant::now();
        let mut completion_tokens = 0u32;

        for tok in &tokens {
            if cancel.is_cancelled() {
                return Err(InferenceError::Cancelled);
            }
            if completion_tokens >= max {
                break;
            }
            on_token(tok.clone());
            completion_tokens += 1;

            tokio::select! {
                _ = sleep(self.inter_token) => {}
                _ = cancel.cancelled() => return Err(InferenceError::Cancelled),
            }
        }

        let secs = start.elapsed().as_secs_f64().max(1e-6);
        let finish_reason = if completion_tokens >= max {
            "length"
        } else {
            "stop"
        };
        Ok(Completion {
            usage: Usage {
                prompt_tokens,
                completion_tokens,
                total_tokens: prompt_tokens + completion_tokens,
            },
            finish_reason: finish_reason.to_string(),
            prefill_tps: prompt_tokens as f64 / secs,
            decode_tps: completion_tokens as f64 / secs,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use sc_protocol::{ChatMessage, SamplingParams};

    fn req(text: &str, max: u32) -> InferenceRequest {
        InferenceRequest {
            model: "mock".into(),
            messages: vec![ChatMessage {
                role: "user".into(),
                content: text.into(),
            }],
            params: SamplingParams {
                max_tokens: max,
                temperature: None,
                top_p: None,
                top_k: None,
                repeat_penalty: None,
                stop: None,
                seed: None,
            },
        }
    }

    #[tokio::test]
    async fn streams_tokens_and_reports_usage() {
        let be = MockBackend::new(0);
        let mut got = String::new();
        let c = be
            .generate(
                &req("hello world", 100),
                &mut |t| got.push_str(&t),
                CancellationToken::new(),
            )
            .await
            .unwrap();
        assert!(got.contains("hello world"));
        assert!(c.usage.completion_tokens > 0);
        assert_eq!(c.finish_reason, "stop");
    }

    #[tokio::test]
    async fn honors_max_tokens() {
        let be = MockBackend::new(0);
        let mut n = 0;
        let c = be
            .generate(
                &req("a b c d e f g", 3),
                &mut |_| n += 1,
                CancellationToken::new(),
            )
            .await
            .unwrap();
        assert_eq!(n, 3);
        assert_eq!(c.finish_reason, "length");
    }

    #[tokio::test]
    async fn stops_on_cancel() {
        let be = MockBackend::new(50);
        let tok = CancellationToken::new();
        tok.cancel();
        let r = be.generate(&req("x y z", 100), &mut |_| {}, tok).await;
        assert!(matches!(r, Err(InferenceError::Cancelled)));
    }
}
