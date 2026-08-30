//! Per-job handler: open the sealed request, run the backend, seal each token chunk back
//! to the coordinator's per-job ephemeral key, then report completion.
//!
//! Prompt and completion text are never logged here.

use std::sync::{
    atomic::{AtomicU32, Ordering},
    Arc,
};
use std::time::Instant;

use sc_crypto::{open as box_open, parse_public, seal as box_seal, Keypair};
use sc_inference::{InferenceBackend, InferenceError, InferenceRequest};
use sc_net::WsMessage;
use sc_protocol as proto;
use tokio::sync::mpsc::UnboundedSender;
use tokio_util::sync::CancellationToken;
use tracing::{info, warn};

/// Decrements the active-job gauge on drop.
struct ActiveGuard(Arc<AtomicU32>);
impl Drop for ActiveGuard {
    fn drop(&mut self) {
        self.0.fetch_sub(1, Ordering::Relaxed);
    }
}

fn send<T: serde::Serialize>(tx: &UnboundedSender<WsMessage>, payload: &T) {
    match proto::to_frame(payload, None) {
        Ok(bytes) => match String::from_utf8(bytes) {
            Ok(s) => {
                let _ = tx.send(WsMessage::Text(s));
            }
            Err(_) => warn!("frame was not valid utf8"),
        },
        Err(e) => warn!(error = %e, "failed to encode frame"),
    }
}

/// What a finished job produced, surfaced to the [`crate::EventSink`] for UI/metering.
pub struct JobOutcome {
    pub ok: bool,
    pub completion_tokens: u32,
    pub decode_tps: f32,
}

impl JobOutcome {
    fn failed() -> Self {
        Self {
            ok: false,
            completion_tokens: 0,
            decode_tps: 0.0,
        }
    }
}

fn job_error(tx: &UnboundedSender<WsMessage>, job_id: &str, code: &str) {
    send(
        tx,
        &proto::JobError {
            kind: proto::msg_type::JOB_ERROR.into(),
            job_id: job_id.to_string(),
            code: code.to_string(),
            message: None,
        },
    );
}

pub async fn handle_job(
    jr: proto::JobRequest,
    identity: Arc<Keypair>,
    backend: Arc<dyn InferenceBackend>,
    tx: UnboundedSender<WsMessage>,
    cancel: CancellationToken,
    active: Arc<AtomicU32>,
) -> JobOutcome {
    active.fetch_add(1, Ordering::Relaxed);
    let _guard = ActiveGuard(active);
    let job_id = jr.job_id.clone();

    let epk = match parse_public(&jr.sealed.epk) {
        Ok(k) => k,
        Err(_) => {
            job_error(&tx, &job_id, "decrypt");
            return JobOutcome::failed();
        }
    };
    let plaintext = match box_open(&jr.sealed, &epk, &identity.secret) {
        Ok(p) => p,
        Err(_) => {
            warn!(job_id = %job_id, "sealed request failed to open");
            job_error(&tx, &job_id, "decrypt");
            return JobOutcome::failed();
        }
    };
    let pt: proto::JobRequestPlaintext = match serde_json::from_slice(&plaintext) {
        Ok(p) => p,
        Err(_) => {
            job_error(&tx, &job_id, "internal");
            return JobOutcome::failed();
        }
    };

    if !backend.has_model(&pt.model) {
        job_error(&tx, &job_id, "model-missing");
        return JobOutcome::failed();
    }

    let req = InferenceRequest {
        model: pt.model.clone(),
        messages: pt.messages,
        params: pt.params,
    };

    info!(job_id = %job_id, model = %pt.model, backend = backend.name(), "job started");

    let mut seq: u32 = 0;
    let tx_tokens = tx.clone();
    let id_tokens = identity.clone();
    let job_for_tokens = job_id.clone();
    let mut on_token = move |tok: String| {
        let cp = proto::JobChunkPlaintext {
            job_id: job_for_tokens.clone(),
            seq,
            delta: tok,
            done: false,
        };
        let raw = match serde_json::to_vec(&cp) {
            Ok(r) => r,
            Err(_) => return,
        };
        let sealed = box_seal(&raw, &epk, &id_tokens.secret, &epk);
        send(
            &tx_tokens,
            &proto::JobChunk {
                kind: proto::msg_type::JOB_CHUNK.into(),
                job_id: cp.job_id.clone(),
                sealed,
            },
        );
        seq = seq.saturating_add(1);
    };

    let started = Instant::now();
    let result = backend.generate(&req, &mut on_token, cancel).await;
    let duration_ms = started.elapsed().as_millis() as u64;

    match result {
        Ok(c) => {
            info!(job_id = %job_id, completion_tokens = c.usage.completion_tokens, duration_ms, "job done");
            let completion_tokens = c.usage.completion_tokens;
            let decode_tps = c.decode_tps as f32;
            send(
                &tx,
                &proto::JobDone {
                    kind: proto::msg_type::JOB_DONE.into(),
                    job_id: job_id.clone(),
                    usage: c.usage,
                    finish_reason: c.finish_reason,
                    duration_ms,
                    prefill_tps: Some(c.prefill_tps),
                    decode_tps: Some(c.decode_tps),
                },
            );
            JobOutcome {
                ok: true,
                completion_tokens,
                decode_tps,
            }
        }
        Err(e) => {
            let code = match e {
                InferenceError::Cancelled => "cancelled",
                InferenceError::Oom => "oom",
                InferenceError::ModelMissing(_) => "model-missing",
                InferenceError::Backend(_) => "backend",
            };
            warn!(job_id = %job_id, code, "job failed");
            job_error(&tx, &job_id, code);
            JobOutcome::failed()
        }
    }
}
