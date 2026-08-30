"use client";
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";

export default function Overview() {
  const [health, setHealth] = useState<any>(null);
  const [providers, setProviders] = useState<any[]>([]);
  const [models, setModels] = useState<any[]>([]);
  const [usage, setUsage] = useState<any[]>([]);
  const [err, setErr] = useState("");

  const load = useCallback(async () => {
    setErr("");
    try {
      setHealth(await api.health());
      setModels((await api.models()).data || []);
    } catch (e: any) {
      setErr(e.message);
    }
    try {
      setProviders((await api.providers()).providers || []);
      setUsage((await api.usage(24)).rows || []);
    } catch (e: any) {
      setErr((p) => p || "admin: " + e.message + " (set an admin token in Settings)");
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
  }, [load]);

  const totReq = usage.reduce((s, r) => s + r.requests, 0);
  const totTok = usage.reduce((s, r) => s + r.prompt_tokens + r.completion_tokens, 0);

  return (
    <>
      <h1>Overview</h1>
      {err && <div className="err">{err}</div>}

      <div className="row" style={{ gap: 14 }}>
        <div className="panel" style={{ flex: 1, minWidth: 160 }}>
          <div className="muted">Coordinator</div>
          <div style={{ fontSize: 22 }}>{health ? "online" : "—"}</div>
        </div>
        <div className="panel" style={{ flex: 1, minWidth: 160 }}>
          <div className="muted">Providers</div>
          <div style={{ fontSize: 22 }}>{health?.providers ?? "—"}</div>
        </div>
        <div className="panel" style={{ flex: 1, minWidth: 160 }}>
          <div className="muted">Requests (24h)</div>
          <div style={{ fontSize: 22 }}>{totReq}</div>
        </div>
        <div className="panel" style={{ flex: 1, minWidth: 160 }}>
          <div className="muted">Tokens (24h)</div>
          <div style={{ fontSize: 22 }}>{totTok.toLocaleString()}</div>
        </div>
      </div>

      <h2>Connected providers</h2>
      <div className="panel">
        {providers.length === 0 ? (
          <span className="muted">none connected</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Platform</th>
                <th>Backend</th>
                <th>Class</th>
                <th>Tier</th>
                <th>Models</th>
                <th>Jobs</th>
                <th>Thermal</th>
                <th>Uptime</th>
              </tr>
            </thead>
            <tbody>
              {providers.map((p) => (
                <tr key={p.id}>
                  <td className="mono">{p.id.slice(0, 8)}</td>
                  <td>
                    {p.platform}/{p.arch}
                  </td>
                  <td>{p.backend}</td>
                  <td>{p.hardware_class}</td>
                  <td>
                    <span className="pill">{p.trust_tier}</span>
                  </td>
                  <td className="mono">{(p.models || []).join(", ")}</td>
                  <td>{p.active_jobs}</td>
                  <td>{p.thermal_state || "—"}</td>
                  <td>{p.connected_for}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <h2>Models</h2>
      <div className="panel">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Class</th>
              <th>Context</th>
              <th>Quant</th>
            </tr>
          </thead>
          <tbody>
            {models.map((m) => (
              <tr key={m.id}>
                <td className="mono">{m.id}</td>
                <td>{m.hardware_class || "—"}</td>
                <td>{m.context_length || "—"}</td>
                <td>{m.quantization || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <h2>Usage by key · model (24h)</h2>
      <div className="panel">
        {usage.length === 0 ? (
          <span className="muted">no usage recorded</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Key</th>
                <th>Model</th>
                <th>Requests</th>
                <th>Prompt tok</th>
                <th>Completion tok</th>
              </tr>
            </thead>
            <tbody>
              {usage.map((r, i) => (
                <tr key={i}>
                  <td className="mono">{r.key_id}</td>
                  <td className="mono">{r.model}</td>
                  <td>{r.requests}</td>
                  <td>{r.prompt_tokens}</td>
                  <td>{r.completion_tokens}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
