"use client";

import { useEffect, useState } from "react";
import {
  summarize,
  lastJob,
  CALENDAR_URL,
  type DemoResponse,
  type LastJob,
} from "./lib/api";

const SAMPLE = `Ayni is a compute marketplace built on devices that would otherwise sit idle — \
laptops, desktops, and phones. A customer describes a workload; Ayni returns one \
aggregated price and a completion estimate, then runs the work across whichever \
devices are best suited to it and pays their owners. Every request is sealed to the \
device that serves it with a one-time key, so the coordinator relays ciphertext and \
never sees the prompt or the output. There is no token and no blockchain: settlement \
is ordinary prepaid credit, and governance is a fixed council of models that reviews \
each workload's structure — never its content — before it runs.`;

function humanETA(s: number) {
  if (s < 90) return `${Math.max(1, Math.round(s))} sec`;
  if (s < 5400) return `${Math.round(s / 60)} min`;
  return `${(s / 3600).toFixed(1)} hr`;
}

function deviceLine(d: Record<string, unknown> | undefined) {
  if (!d) return "a device on the network";
  const label = (d.label as string) || `${d.platform ?? "?"} ${d.arch ?? ""}`.trim();
  const cls = d.hardware_class ? ` · class ${d.hardware_class}` : "";
  return `${label}${cls}`;
}

export default function DemoPage() {
  const [doc, setDoc] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [res, setRes] = useState<DemoResponse | null>(null);
  const [seedJob, setSeedJob] = useState<LastJob | null>(null);

  useEffect(() => {
    lastJob().then(setSeedJob).catch(() => {});
  }, []);

  async function run() {
    setErr("");
    setRes(null);
    setBusy(true);
    try {
      setRes(await summarize(doc.trim()));
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : "something went wrong");
    } finally {
      setBusy(false);
    }
  }

  const q = res?.quote;
  const c = res?.council;
  const r = res?.result;
  const fb = res?.fallback ?? null;

  return (
    <>
      <div className="hero">
        <h1>See the network do one real job.</h1>
        <p className="lede">
          Paste a document. Ayni quotes it, a council of models reviews it, and a
          stranger&rsquo;s device returns the summary &mdash; encrypted end to end.
          No sign-up. This one&rsquo;s on us.
        </p>
      </div>

      <div style={{ marginTop: 24 }}>
        <label htmlFor="doc">
          Document{" "}
          <button
            type="button"
            className="sample-btn"
            onClick={() => setDoc(SAMPLE)}
          >
            use a sample
          </button>
        </label>
        <textarea
          id="doc"
          rows={8}
          value={doc}
          maxLength={6000}
          placeholder="Paste a few paragraphs…"
          onChange={(e) => setDoc(e.target.value)}
        />
        <div className="kv" style={{ border: 0, color: "var(--muted)", fontSize: ".8rem" }}>
          <span>{doc.trim().length} / 6000 characters</span>
          {seedJob && !res && (
            <span>
              last real run {new Date(seedJob.recorded_at).toLocaleString()} ·{" "}
              {deviceLine(seedJob.device)}
            </span>
          )}
        </div>
        <div style={{ marginTop: 12 }}>
          <button
            className="btn btn-primary"
            disabled={doc.trim().length < 40 || busy}
            onClick={run}
          >
            {busy ? "Running across the network…" : "Run the demo"}
          </button>
        </div>
        {err && <p className="err" style={{ marginTop: 12 }}>{err}</p>}
      </div>

      {res && (
        <div className="steps">
          {/* 1 — quote */}
          <section className="step">
            <h3>One price, built from the parts</h3>
            <div className="card">
              <div className="kv">
                <span>Quoted price</span>
                <span className="big">${q!.total_usd.toFixed(2)}</span>
              </div>
              <div className="kv">
                <span>Estimated completion</span>
                <span>{humanETA(q!.eta_seconds)}</span>
              </div>
              <div className="kv">
                <span>Devices eligible</span>
                <span>
                  <span className={`dot ${q!.supply_online ? "" : "off"}`} />
                  {q!.eligible_nodes} {q!.supply_online ? "online" : "(reference estimate)"}
                </span>
              </div>
              <div style={{ marginTop: 10 }}>
                {Object.entries(q!.breakdown_usd).map(([k, v]) => (
                  <div className="kv" key={k} style={{ fontSize: ".82rem", color: "var(--muted)" }}>
                    <span>{k.replace(/_/g, " ")}</span>
                    <span>${v.toFixed(4)}</span>
                  </div>
                ))}
              </div>
              <p className="callout">
                Not a bid plus a markup &mdash; assembled from compute, coordination,
                expected failure, payment cost, and Ayni&rsquo;s margin, and shown to you.
              </p>
            </div>
          </section>

          {/* 2 — council */}
          <section className="step">
            <h3>Reviewed before it runs &mdash; on structure, never content</h3>
            <div className="card">
              <div className="kv">
                <span>Council decision</span>
                <span className={`pill ${c!.decision === "APPROVE" ? "" : "warn"}`}>
                  {c!.decision} · {c!.risk_class}
                </span>
              </div>
              <ul className="muted" style={{ margin: "10px 0 12px 1.1em", fontSize: ".88rem" }}>
                {c!.reviews.map((rv, i) => (
                  <li key={i}>
                    {rv.role || rv.seat}: {rv.decision} ({rv.severity})
                  </li>
                ))}
              </ul>
              <div className="facts">{JSON.stringify(c!.facts, null, 2)}</div>
              <p className="callout">
                That is the entire record the Council keeps. There is no field for your
                document&rsquo;s text &mdash; only its shape: token counts, model class,
                redundancy, price. Audit {c!.audit_hash.slice(0, 20)}…
              </p>
            </div>
          </section>

          {/* 3 — result */}
          <section className="step">
            <h3>{r ? "A device you don't control returned this" : "No node was free this second"}</h3>
            <div className="card">
              {r ? (
                <>
                  <div className="result">{r.summary}</div>
                  <div style={{ marginTop: 12 }}>
                    <div className="kv">
                      <span>Served by</span>
                      <span>{deviceLine(r.device)}</span>
                    </div>
                    <div className="kv">
                      <span>Trust tier</span>
                      <span className="pill grey">
                        {(r.device.trust_tier as string) || "community"}
                      </span>
                    </div>
                    <div className="kv">
                      <span>Wall time</span>
                      <span>{(r.wall_ms / 1000).toFixed(1)} s</span>
                    </div>
                    <div className="kv">
                      <span>Tokens (in / out)</span>
                      <span>
                        {r.prompt_tokens} / {r.completion_tokens}
                      </span>
                    </div>
                  </div>
                  <p className="callout">
                    You weren&rsquo;t charged. On the open network this workload would
                    have cost <strong>${r.would_cost_usd.toFixed(2)}</strong>, paid to
                    the device that served it.
                  </p>
                </>
              ) : (
                <>
                  <p className="muted">
                    {res.error === "no_provider_online"
                      ? "Every demo node is busy or offline right now."
                      : `The node errored (${res.error}).`}{" "}
                    Here is the most recent genuine run.
                  </p>
                  {fb ? (
                    <div style={{ marginTop: 10 }}>
                      <div className="kv">
                        <span>Recorded</span>
                        <span>{new Date(fb.recorded_at).toLocaleString()}</span>
                      </div>
                      <div className="kv">
                        <span>Served by</span>
                        <span>{deviceLine(fb.device)}</span>
                      </div>
                      <div className="kv">
                        <span>Wall time</span>
                        <span>{(fb.wall_ms / 1000).toFixed(1)} s</span>
                      </div>
                      <div className="kv">
                        <span>Would cost</span>
                        <span>${fb.would_cost_usd.toFixed(2)}</span>
                      </div>
                    </div>
                  ) : (
                    <p className="muted" style={{ marginTop: 8 }}>
                      No run has completed yet today. Try again in a moment.
                    </p>
                  )}
                </>
              )}
            </div>
          </section>

          {/* 4 — proof */}
          {(res.record || fb) && (
            <section className="step">
              <h3>What the network kept</h3>
              <div className="card">
                <div className="facts">
                  {JSON.stringify(res.record ?? fb, null, 2)}
                </div>
                <p className="callout">
                  Search this for any sentence from your document. It isn&rsquo;t here.
                  The prompt was sealed to the device with a one-time key &mdash; the
                  coordinator only relayed ciphertext, so there is nothing to log.
                </p>
              </div>
            </section>
          )}
        </div>
      )}

      <div className="contact">
        <h2>Ayni is raising.</h2>
        <p className="muted" style={{ marginBottom: 14 }}>
          A privacy-preserving compute marketplace with no token, real device
          attestation, and model-based governance. Happy to walk you through the
          architecture and the numbers.
        </p>
        {CALENDAR_URL ? (
          <a className="btn btn-teal" href={CALENDAR_URL} target="_blank" rel="noreferrer">
            Book a call
          </a>
        ) : (
          <a className="btn btn-teal" href="mailto:hello@ayni-ai.com">
            hello@ayni-ai.com
          </a>
        )}
      </div>
    </>
  );
}
