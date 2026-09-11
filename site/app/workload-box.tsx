"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { quotePreview, type QuotePreview } from "./lib/api";

const MODELS = [
  { id: "qwen2.5-0.5b-instruct-q4_k_m", label: "Qwen2.5 0.5B Instruct (Q4) · live" },
];

const PRESETS = [
  { label: "Summarize 2,000 documents", count: 2000, pin: 600, pout: 160 },
  { label: "Classify 10,000 support tickets", count: 5000, pin: 250, pout: 20 },
  { label: "Draft 500 product descriptions", count: 500, pin: 180, pout: 220 },
];

function humanizeETA(sec: number): string {
  if (sec < 90) return `${Math.max(1, Math.round(sec))} sec`;
  const min = sec / 60;
  if (min < 90) return `${Math.round(min)} min`;
  const hr = min / 60;
  if (hr < 36) return `${hr.toFixed(1)} hr`;
  return `${Math.round(hr / 24)} days`;
}

function usd(n: number): string {
  if (n >= 1) return `$${n.toFixed(2)}`;
  if (n >= 0.01) return `$${n.toFixed(3)}`;
  return `$${n.toFixed(4)}`;
}

export function WorkloadBox() {
  const [model, setModel] = useState(MODELS[0].id);
  const [count, setCount] = useState(2000);
  const [pin, setPin] = useState(600);
  const [pout, setPout] = useState(160);
  const [redundancy, setRedundancy] = useState(1);
  const [spot, setSpot] = useState(false);
  const [quote, setQuote] = useState<QuotePreview | null>(null);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const run = useCallback(() => {
    setLoading(true);
    setErr("");
    quotePreview({
      model,
      estimate: { count, avg_prompt_tokens: pin, avg_completion_tokens: pout },
      redundancy,
      spot,
    })
      .then((q) => setQuote(q))
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : "Could not reach the coordinator."))
      .finally(() => setLoading(false));
  }, [model, count, pin, pout, redundancy, spot]);

  useEffect(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(run, 350);
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, [run]);

  const bd = quote?.price.breakdown_usd;
  const total = quote?.price.total_usd ?? 0;
  const tokens = (quote?.estimate.prompt_tokens ?? 0) + (quote?.estimate.completion_tokens ?? 0);
  const perMillionTokens = tokens > 0 ? (total / tokens) * 1_000_000 : 0;
  const parts = bd
    ? [
        { k: "Devices", v: bd.compute_acquisition, c: "var(--teal)" },
        { k: "Coordination", v: bd.coordination, c: "#4a6bd8" },
        { k: "Failure buffer", v: bd.expected_failure, c: "#9a7bd0" },
        { k: "Payments", v: bd.payment_processing, c: "#c98a3c" },
        { k: "Ayni", v: bd.ayni_margin, c: "var(--navy)" },
      ]
    : [];

  return (
    <div className="wbox">
      <div className="wbox-form">
        <label>
          Model
          <select value={model} onChange={(e) => setModel(e.target.value)}>
            {MODELS.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label}
              </option>
            ))}
          </select>
        </label>

        <div className="wbox-presets">
          {PRESETS.map((p) => (
            <button
              key={p.label}
              type="button"
              className="chip"
              onClick={() => {
                setCount(p.count);
                setPin(p.pin);
                setPout(p.pout);
              }}
            >
              {p.label}
            </button>
          ))}
        </div>

        <div className="wbox-grid">
          <label>
            Items
            <input
              type="number"
              min={1}
              max={5000}
              value={count}
              onChange={(e) => setCount(clampNum(e.target.value, 1, 5000))}
            />
          </label>
          <label>
            Redundancy
            <select value={redundancy} onChange={(e) => setRedundancy(Number(e.target.value))}>
              <option value={1}>1× (cheapest)</option>
              <option value={2}>2× (cross-check)</option>
              <option value={3}>3× (majority vote)</option>
            </select>
          </label>
          <label>
            Avg input tokens
            <input
              type="number"
              min={1}
              max={100000}
              value={pin}
              onChange={(e) => setPin(clampNum(e.target.value, 1, 100000))}
            />
          </label>
          <label>
            Avg output tokens
            <input
              type="number"
              min={1}
              max={100000}
              value={pout}
              onChange={(e) => setPout(clampNum(e.target.value, 1, 100000))}
            />
          </label>
        </div>

        <label className="wbox-spot">
          <input type="checkbox" checked={spot} onChange={(e) => setSpot(e.target.checked)} />
          <span>
            <b>Spot</b> — cheaper, best-effort. Runs on leftover capacity and may be
            interrupted and requeued.
          </span>
        </label>
      </div>

      <div className="wbox-out" aria-live="polite">
        {err ? (
          <p className="err">{err}</p>
        ) : (
          <>
            <div className="wbox-headline">
              <div>
                <span className="wbox-k">{spot ? "Spot price" : "Quoted price"}</span>
                <span className="wbox-price">{usd(total)}</span>
                {perMillionTokens > 0 ? (
                  <span className="wbox-k">≈ {usd(perMillionTokens)} per 1M tokens</span>
                ) : null}
              </div>
              <div>
                <span className="wbox-k">Est. completion</span>
                <span className="wbox-eta">
                  {!quote
                    ? "—"
                    : quote.estimate.eta_seconds_max
                      ? `${humanizeETA(quote.estimate.eta_seconds)}–${humanizeETA(
                          quote.estimate.eta_seconds_max
                        )}`
                      : humanizeETA(quote.estimate.eta_seconds)}
                </span>
              </div>
            </div>

            <div className="wbox-bar">
              {parts.map((p) =>
                total > 0 ? (
                  <span
                    key={p.k}
                    style={{ width: `${(p.v / total) * 100}%`, background: p.c }}
                    title={`${p.k}: ${usd(p.v)}`}
                  />
                ) : null
              )}
            </div>
            <ul className="wbox-legend">
              {parts.map((p) => (
                <li key={p.k}>
                  <span className="dot" style={{ background: p.c }} /> {p.k}{" "}
                  <b>{usd(p.v)}</b>
                </li>
              ))}
            </ul>

            <p className="wbox-note">
              {quote?.supply_online
                ? `${quote.estimate.eligible_nodes} device${
                    quote.estimate.eligible_nodes === 1 ? "" : "s"
                  } online for this model · ~${quote.estimate.aggregate_tps} tok/s combined`
                : "No matching devices online right now — estimate assumes one typical phone picks it up."}
              {loading ? " · updating…" : ""}
            </p>
            <p className="wbox-note">
              ~{fmt(quote?.estimate.prompt_tokens)} input + ~
              {fmt(quote?.estimate.completion_tokens)} output tokens.
              {spot
                ? " Spot: best-effort completion, may be interrupted and requeued."
                : ""}{" "}
              Price is the real coordinator quote; accepting it requires an API key.
            </p>
          </>
        )}
      </div>
    </div>
  );
}

function clampNum(v: string, lo: number, hi: number): number {
  const n = Math.round(Number(v) || 0);
  return Math.min(hi, Math.max(lo, n));
}

function fmt(n?: number): string {
  if (!n) return "0";
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}k`;
  return `${n}`;
}
