"use client";
import { useCallback, useEffect, useState } from "react";
import { api, fmt, hasAdmin } from "@/lib/api";

// Operator controls. Every action here is written to the audit trail (Events →
// AUDIT) with the caller's address.

export default function Controls() {
  const [intake, setIntake] = useState<{ paused: boolean; message: string; since: string } | null>(null);
  const [msgText, setMsgText] = useState("");
  const [cfg, setCfg] = useState<any>(null);
  const [keys, setKeys] = useState<any[]>([]);
  const [keyId, setKeyId] = useState("");
  const [amount, setAmount] = useState("5");
  const [note, setNote] = useState("");
  const [metrics, setMetrics] = useState("");
  const [status, setStatus] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState("");

  const load = useCallback(async () => {
    if (!hasAdmin()) return;
    setErr("");
    try {
      const [i, c, k] = await Promise.all([api.intake(), api.config(), api.keys()]);
      setIntake(i);
      setMsgText((m) => m || i.message || "");
      setCfg(c);
      setKeys(k.keys || []);
    } catch (e: any) {
      setErr(e.message);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 10_000);
    return () => clearInterval(t);
  }, [load]);

  async function run(name: string, fn: () => Promise<any>, ok: (r: any) => string) {
    setBusy(name);
    setStatus("");
    setErr("");
    try {
      const r = await fn();
      setStatus(ok(r));
      await load();
    } catch (e: any) {
      setErr(`${name}: ${e.message}`);
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      <h1>Controls</h1>
      {err && <div className="err">{err}</div>}
      {status && <div className="banner">{status}</div>}

      <div className="two-col">
        <div>
          <h2>Intake</h2>
          <div className="panel">
            <div className="row" style={{ marginBottom: 8 }}>
              <span className={`pill ${intake?.paused ? "bad" : "good"}`}>{intake ? (intake.paused ? "PAUSED" : "accepting work") : "…"}</span>
              {intake?.paused && <span className="muted small">since {fmt.time(intake.since)}</span>}
            </div>
            <p className="muted small">
              Pausing returns 503 with your message to every new chat, batch, workload and demo request. Providers stay
              connected; in-flight jobs finish.
            </p>
            <input placeholder="message shown to callers" value={msgText} onChange={(e) => setMsgText(e.target.value)} style={{ width: "100%", marginBottom: 8 }} />
            <div className="row">
              {intake?.paused ? (
                <button disabled={busy !== ""} onClick={() => run("resume", () => api.setIntake(false, msgText), () => "intake resumed")}>
                  Resume intake
                </button>
              ) : (
                <button className="danger" disabled={busy !== ""} onClick={() => {
                  if (!window.confirm("Pause all new inference work? Callers will get 503 until you resume.")) return;
                  run("pause", () => api.setIntake(true, msgText), () => "intake paused");
                }}>
                  Pause intake
                </button>
              )}
            </div>
          </div>

          <h2>Fleet</h2>
          <div className="panel">
            <p className="muted small">
              Drain closes every provider connection on this instance. Devices reconnect within ~10 s and land on the
              revision currently receiving traffic. Use it after a deploy or to force phones to re-register.
            </p>
            <button className="danger" disabled={busy !== ""} onClick={() => {
              if (!window.confirm("Disconnect every provider now? They will reconnect automatically.")) return;
              run("drain", () => api.drain(), (r) => `drained ${r.drained} provider(s)`);
            }}>
              Drain all providers
            </button>
            <p className="muted small" style={{ marginTop: 8 }}>
              To disconnect a single device, use the Disconnect button on its row in Devices.
            </p>
          </div>

          <h2>Credit adjustment</h2>
          <div className="panel">
            <p className="muted small">
              Adds (or with a negative amount removes) credit on a consumer key, for goodwill, refunds or testing.
              Limit ±$1,000 per action; every adjustment is audited.
            </p>
            <div className="row" style={{ gap: 8, flexDirection: "column", alignItems: "stretch" }}>
              <select value={keyId} onChange={(e) => setKeyId(e.target.value)}>
                <option value="">pick a key…</option>
                {keys.map((k) => (
                  <option key={k.id} value={k.id}>
                    {k.id} — {k.label || "no label"}{k.disabled ? " (disabled)" : ""}
                  </option>
                ))}
              </select>
              <input placeholder="or type a key id (key_…)" value={keyId} onChange={(e) => setKeyId(e.target.value)} />
              <div className="row">
                <input type="number" step="0.5" value={amount} onChange={(e) => setAmount(e.target.value)} style={{ minWidth: 120, width: 120 }} />
                <span className="muted">USD</span>
                <input placeholder="note (why)" value={note} onChange={(e) => setNote(e.target.value)} style={{ flex: 1 }} />
              </div>
              <button disabled={busy !== "" || !keyId || !amount} onClick={() => {
                const usd = parseFloat(amount);
                if (!window.confirm(`Apply ${usd >= 0 ? "+" : ""}$${usd.toFixed(2)} to ${keyId}?`)) return;
                run("credit", () => api.grantCredit(keyId, usd, note), (r) => `applied ${fmt.usd(r.applied_usd)} to ${r.key_id}; balance now ${fmt.usd(r.balance_usd)}`);
              }}>
                Apply credit
              </button>
            </div>
          </div>
        </div>

        <div>
          <h2>Runtime configuration</h2>
          <div className="panel kv">
            {cfg ? (
              Object.entries(cfg).map(([k, v]) => (
                <div key={k} className="kv-row">
                  <span className="muted mono small">{k}</span>
                  <span className="mono small">{Array.isArray(v) ? v.join(", ") || "—" : typeof v === "boolean" ? (v ? "on" : "off") : String(v ?? "—")}</span>
                </div>
              ))
            ) : (
              <span className="muted">…</span>
            )}
          </div>
          <p className="muted small">
            Secrets are never shown; booleans only say whether one is configured. Changing these requires a redeploy.
          </p>

          <h2>Metrics</h2>
          <div className="panel">
            <div className="row" style={{ marginBottom: 8 }}>
              <button className="secondary" disabled={busy !== ""} onClick={() => run("metrics", () => api.metricsText(), (t) => { setMetrics(t); return "metrics refreshed"; })}>
                Load Prometheus metrics
              </button>
              <span className="muted small">sc_* counters and histograms from this instance</span>
            </div>
            {metrics && (
              <pre className="mono small" style={{ maxHeight: 360, overflow: "auto", margin: 0 }}>
                {metrics.split("\n").filter((l) => l.startsWith("sc_")).join("\n") || "(no sc_ series yet)"}
              </pre>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
