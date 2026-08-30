//! Device self-benchmark: measures what this node can actually sustain, not what the
//! spec sheet claims. The coordinator turns the report into an Ayni Compute Unit (ACU)
//! score and prices work against it.
//!
//! Steps (all local, no prompt data involved):
//!   1. memory read bandwidth (GB/s)
//!   2. a short llama.cpp run for prefill / decode tok/s
//!   3. a longer run, sampling tok/s at the start vs the end, so thermal decay shows up
//!
//! Results are cached to `<data_dir>/benchmark.json` and only re-run when stale
//! (>7 days), the model changed, or the file is missing.

use std::sync::Arc;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

use anyhow::Result;
use sc_inference::{InferenceBackend, InferenceRequest};
use sc_protocol as proto;
use serde::{Deserialize, Serialize};
use tokio_util::sync::CancellationToken;
use tracing::{info, warn};

const CACHE_TTL_SECS: u64 = 7 * 24 * 3600;
const SUSTAINED_TOKENS: u32 = 400;

#[derive(Serialize, Deserialize)]
struct Cache {
    ts: u64,
    model: String,
    report: proto::BenchmarkReport,
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

/// Return a cached report if it's fresh and for the same model.
pub fn cached(data_dir: &std::path::Path, model: &str) -> Option<proto::BenchmarkReport> {
    let raw = std::fs::read(data_dir.join("benchmark.json")).ok()?;
    let c: Cache = serde_json::from_slice(&raw).ok()?;
    if c.model == model && now().saturating_sub(c.ts) < CACHE_TTL_SECS {
        Some(c.report)
    } else {
        None
    }
}

fn store(data_dir: &std::path::Path, model: &str, report: &proto::BenchmarkReport) {
    let c = Cache {
        ts: now(),
        model: model.to_string(),
        report: report.clone(),
    };
    if let Ok(bytes) = serde_json::to_vec(&c) {
        let _ = std::fs::write(data_dir.join("benchmark.json"), bytes);
    }
}

/// Rough memory read bandwidth: sum a 64 MiB buffer many times.
fn mem_bandwidth_gbps() -> f64 {
    const WORDS: usize = 8 * 1024 * 1024; // 64 MiB of u64
    const PASSES: usize = 24;
    let buf: Vec<u64> = (0..WORDS as u64).collect();
    let start = Instant::now();
    let mut acc = 0u64;
    for _ in 0..PASSES {
        let mut i = 0;
        while i < WORDS {
            acc = acc.wrapping_add(buf[i]);
            i += 1;
        }
    }
    std::hint::black_box(acc);
    let secs = start.elapsed().as_secs_f64();
    let bytes = (WORDS * 8 * PASSES) as f64;
    if secs > 0.0 {
        bytes / secs / 1e9
    } else {
        0.0
    }
}

fn prefill_prompt() -> Vec<proto::ChatMessage> {
    // ~300 words of neutral filler so prefill has real work to do. No user data.
    let filler = "the network connects idle devices into shared compute so value flows \
        back to the people and machines that make it possible "
        .repeat(20);
    vec![proto::ChatMessage {
        role: "user".into(),
        content: format!("Summarize the following in one sentence:\n{filler}"),
    }]
}

fn sustained_prompt() -> Vec<proto::ChatMessage> {
    // Forces a long, steady stream of tokens so the first vs last window reveals
    // thermal decay. Deterministic at temperature 0.
    vec![proto::ChatMessage {
        role: "user".into(),
        content: "List the integers from 1 to 400, one per line, with no other text.".into(),
    }]
}

fn params(max_tokens: u32) -> proto::SamplingParams {
    proto::SamplingParams {
        max_tokens,
        temperature: Some(0.0),
        top_p: None,
        top_k: None,
        repeat_penalty: None,
        stop: None,
        seed: Some(1),
    }
}

/// Run the full benchmark. `backend` must already have `model` loaded.
pub async fn run(
    backend: &Arc<dyn InferenceBackend>,
    model: &str,
    data_dir: &std::path::Path,
) -> Result<proto::BenchmarkReport> {
    info!(model, "benchmark: starting");
    let mem = tokio::task::spawn_blocking(mem_bandwidth_gbps)
        .await
        .unwrap_or(0.0);

    // 1. Short run for prefill tok/s.
    let short = InferenceRequest {
        model: model.to_string(),
        messages: prefill_prompt(),
        params: params(16),
    };
    let mut sink = |_: String| {};
    let c1 = backend
        .generate(&short, &mut sink, CancellationToken::new())
        .await
        .map_err(|e| anyhow::anyhow!("benchmark short run: {e}"))?;

    // 2. Sustained run: timestamp every token, compare first vs last window.
    let sustained = InferenceRequest {
        model: model.to_string(),
        messages: sustained_prompt(),
        params: params(SUSTAINED_TOKENS),
    };
    let mut stamps: Vec<Instant> = Vec::with_capacity(SUSTAINED_TOKENS as usize);
    let started = Instant::now();
    let mut on_tok = |_: String| stamps.push(Instant::now());
    let _ = backend
        .generate(&sustained, &mut on_tok, CancellationToken::new())
        .await
        .map_err(|e| anyhow::anyhow!("benchmark sustained run: {e}"))?;
    let total_secs = started.elapsed().as_secs_f64();

    let window_tps = |slice: &[Instant]| -> Option<f64> {
        if slice.len() < 2 {
            return None;
        }
        let span = slice[slice.len() - 1]
            .duration_since(slice[0])
            .as_secs_f64();
        (span > 0.0).then(|| (slice.len() - 1) as f64 / span)
    };
    // Split the token stream in half: first half vs second half exposes thermal
    // decay. Needs enough tokens for each half to be meaningful.
    let (start_tps, end_tps) = if stamps.len() >= 16 {
        let mid = stamps.len() / 2;
        (window_tps(&stamps[..mid]), window_tps(&stamps[mid..]))
    } else {
        (window_tps(&stamps), None)
    };

    let tel = sc_telemetry::sample_telemetry(0, 0);
    // Some platforms (macOS) report 0 available; fall back to total RAM as the
    // capacity signal so the coordinator always has a non-zero number.
    let ram_mb = if tel.mem_available_mb > 0 {
        tel.mem_available_mb
    } else {
        sc_telemetry::host_info().ram_mb
    };
    let cores = std::thread::available_parallelism()
        .map(|n| n.get() as u32)
        .ok();

    // A ~16-token prefill on a tiny prompt can report an implausible tok/s from
    // timer granularity; cap it so it can't skew anything downstream. ACU does
    // not use prefill, but the number is still surfaced.
    let prefill_tps = c1.prefill_tps.min(20_000.0);

    let report = proto::BenchmarkReport {
        kind: proto::msg_type::BENCHMARK_REPORT.into(),
        model: model.to_string(),
        backend: backend.name().to_string(),
        prefill_tps,
        decode_tps: c1.decode_tps,
        context_tested: Some(c1.usage.prompt_tokens),
        sample_ms: Some((total_secs * 1000.0) as u32),
        sustained_seconds: Some(total_secs as u32),
        sustained_start_tps: start_tps,
        sustained_end_tps: end_tps.or(start_tps),
        mem_bandwidth_gbps: Some(mem),
        available_ram_mb: Some(ram_mb),
        available_storage_mb: None,
        cpu_cores: cores,
        thermal_state: Some(tel.thermal_state.clone()),
    };

    info!(
        model,
        prefill_tps = report.prefill_tps,
        decode_tps = report.decode_tps,
        sustained_start = ?report.sustained_start_tps,
        sustained_end = ?report.sustained_end_tps,
        mem_gbps = mem,
        "benchmark: done"
    );
    store(data_dir, model, &report);
    Ok(report)
}

/// Best-effort: run (or reuse a cached) benchmark and hand the report to `send`.
pub async fn run_or_cached<F>(
    backend: &Arc<dyn InferenceBackend>,
    model: &str,
    data_dir: &std::path::Path,
    send: F,
) where
    F: FnOnce(proto::BenchmarkReport),
{
    if let Some(r) = cached(data_dir, model) {
        info!(model, "benchmark: using cached report");
        send(r);
        return;
    }
    match run(backend, model, data_dir).await {
        Ok(r) => send(r),
        Err(e) => warn!(error = %e, "benchmark: skipped"),
    }
}
