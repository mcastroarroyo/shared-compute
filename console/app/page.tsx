"use client";
import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { api, fmt, hasAdmin } from "@/lib/api";

type KindCompute = { devices: number; acu: number; decode_tps: number; ram_mb: number; active_jobs: number; attested: number };

export default function Overview() {
  const [ov, setOv] = useState<any>(null);
  const [usage, setUsage] = useState<any[]>([]);
  const [err, setErr] = useState("");
  const [tick, setTick] = useState(0);

  const load = useCallback(async () => {
    if (!hasAdmin()) return;
    setErr("");
    try {
      setOv(await api.overview());
      setUsage((await api.usage(24)).rows || []);
      setTick((t) => t + 1);
    } catch (e: any) {
      setErr(e.message);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 10_000);
    return () => clearInterval(t);
  }, [load]);

  const kinds: Record<string, number> = ov?.providers?.by_kind || {};
  const online: number = ov?.providers?.online ?? 0;
  const compute: Record<string, KindCompute> = ov?.compute?.by_kind || {};
  const order = ["phone", "pc", "other"];
  const pct = (n: number) => (online ? Math.round((n / online) * 100) : 0);

  return (
    <>
      <div className="row between">
        <h1>Overview</h1>
        <span className="muted small">
          {ov ? `updated ${fmt.time(ov.generated_at)} · refresh #${tick}` : "waiting for data"}
        </span>
      </div>
      {err && <div className="err">{err}</div>}

      {ov?.intake?.paused && (
        <div className="banner warn">
          <b>Intake is paused</b> since {fmt.time(ov.intake.since)}: “{ov.intake.message || "maintenance"}”.
          New jobs get 503 until an operator resumes it in <Link href="/controls/">Controls</Link>.
        </div>
      )}

      <div className="kpis">
        <Kpi label="Devices online" value={online} sub={`${ov?.providers?.known ?? 0} known in catalog`} />
        <Kpi label="Users online" value={ov?.users?.online ?? 0} sub={`${ov?.users?.total ?? 0} accounts · ${ov?.users?.with_devices ?? 0} with devices`} />
        <Kpi label="Phones" value={kinds.phone ?? 0} sub={`${pct(kinds.phone ?? 0)}% of fleet`} tone="phone" />
        <Kpi label="PCs / laptops" value={kinds.pc ?? 0} sub={`${pct(kinds.pc ?? 0)}% of fleet`} tone="pc" />
        <Kpi label="Other" value={kinds.other ?? 0} sub={`${pct(kinds.other ?? 0)}% of fleet`} tone="other" />
        <Kpi label="Online compute" value={`${(ov?.compute?.online_acu_total ?? 0).toFixed(2)} ACU`} sub="sum of benchmarked capacity" />
        <Kpi label="Active jobs" value={ov?.providers?.active_jobs ?? 0} sub="in flight right now" />
        <Kpi label="Requests (24h)" value={fmt.n(ov?.traffic_24h?.requests)} sub={`${fmt.n((ov?.traffic_24h?.prompt_tokens ?? 0) + (ov?.traffic_24h?.completion_tokens ?? 0))} tokens`} />
        <Kpi
          label="Errors (1h)"
          value={ov?.events?.errors_1h ?? 0}
          sub={`${ov?.events?.warnings_1h ?? 0} warnings`}
          tone={(ov?.events?.errors_1h ?? 0) > 0 ? "bad" : "good"}
        />
      </div>

      <h2>Fleet by device type</h2>
      <div className="panel">
        {online === 0 ? (
          <span className="muted">no devices connected — the bars fill as providers attach</span>
        ) : (
          <div className="bars">
            {order.map((k) => (
              <div key={k} className="bar-row">
                <span className={`tag ${k}`}>{fmt.kind(k)}</span>
                <div className="bar">
                  <div className={`fill ${k}`} style={{ width: `${pct(kinds[k] ?? 0)}%` }} />
                </div>
                <span className="mono">{kinds[k] ?? 0}</span>
              </div>
            ))}
          </div>
        )}
        <table style={{ marginTop: 12 }}>
          <thead>
            <tr>
              <th>Type</th>
              <th>Devices</th>
              <th>Compute (ACU)</th>
              <th>Sustained tok/s</th>
              <th>RAM</th>
              <th>Attested</th>
              <th>Active jobs</th>
            </tr>
          </thead>
          <tbody>
            {order.map((k) => {
              const c = compute[k] || { devices: 0, acu: 0, decode_tps: 0, ram_mb: 0, active_jobs: 0, attested: 0 };
              return (
                <tr key={k}>
                  <td><span className={`tag ${k}`}>{fmt.kind(k)}</span></td>
                  <td>{c.devices}</td>
                  <td>{c.acu.toFixed(2)}</td>
                  <td>{c.decode_tps.toFixed(1)}</td>
                  <td>{fmt.gb(c.ram_mb)}</td>
                  <td>{c.attested}</td>
                  <td>{c.active_jobs}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
        <div className="row" style={{ marginTop: 12, gap: 24 }}>
          <Chips title="Platform" m={ov?.providers?.by_platform} />
          <Chips title="Trust tier" m={ov?.providers?.by_tier} />
          <Chips title="Class" m={ov?.providers?.by_class} />
          <Chips title="Models served" m={ov?.compute?.models} />
        </div>
      </div>

      <div className="two-col">
        <div>
          <h2>Coordinator</h2>
          <div className="panel kv">
            <Row k="Uptime" v={fmt.dur(ov?.coordinator?.uptime_s ?? 0)} />
            <Row k="Instance" v={ov?.coordinator?.instance || "local"} mono />
            <Row k="Build" v={ov?.coordinator?.revision || "dev"} mono />
            <Row k="Database" v={flag(ov?.coordinator?.database)} />
            <Row k="Accounts (OAuth)" v={flag(ov?.coordinator?.accounts)} />
            <Row k="Stripe" v={flag(ov?.coordinator?.stripe)} />
            <Row k="Manifest signing" v={flag(ov?.coordinator?.signing)} />
            <Row k="Heartbeat" v={`${ov?.coordinator?.heartbeat ?? "—"} s`} />
            <Row k="Waitlist signups" v={fmt.n(ov?.waitlist)} />
          </div>
        </div>
        <div>
          <h2>Ledger (7d)</h2>
          <div className="panel kv">
            <Row k="Jobs completed" v={fmt.n(ov?.ledger_7d?.jobs)} />
            <Row k="Gross billed" v={fmt.usd(ov?.ledger_7d?.gross_usd, 4)} />
            <Row k="Owed to providers" v={fmt.usd(ov?.ledger_7d?.provider_usd, 4)} />
          </div>
          <h2>Last error</h2>
          <div className="panel">
            {ov?.events?.last_error ? (
              <div>
                <span className="pill bad">ERROR</span> <span className="muted">{fmt.ago(ov.events.last_error.ts)}</span>
                <div style={{ marginTop: 6 }}>{ov.events.last_error.msg}</div>
                <div className="mono small muted">{JSON.stringify(ov.events.last_error.attrs || {})}</div>
                <Link href="/events/" className="small">open Events →</Link>
              </div>
            ) : (
              <span className="muted">no errors in the last hour</span>
            )}
          </div>
        </div>
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
                  <td>{fmt.n(r.prompt_tokens)}</td>
                  <td>{fmt.n(r.completion_tokens)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}

function flag(v: any) {
  return v ? "on" : "off";
}

function Kpi({ label, value, sub, tone }: { label: string; value: any; sub?: string; tone?: string }) {
  return (
    <div className={`kpi ${tone || ""}`}>
      <div className="muted small">{label}</div>
      <div className="kpi-value">{value}</div>
      {sub && <div className="muted small">{sub}</div>}
    </div>
  );
}

function Row({ k, v, mono }: { k: string; v: any; mono?: boolean }) {
  return (
    <div className="kv-row">
      <span className="muted">{k}</span>
      <span className={mono ? "mono" : ""}>{v}</span>
    </div>
  );
}

function Chips({ title, m }: { title: string; m?: Record<string, number> }) {
  const entries = Object.entries(m || {}).sort((a, b) => b[1] - a[1]);
  return (
    <div>
      <div className="muted small" style={{ marginBottom: 4 }}>{title}</div>
      {entries.length === 0 ? (
        <span className="muted small">—</span>
      ) : (
        entries.map(([k, v]) => (
          <span key={k} className="pill" style={{ marginRight: 6 }}>
            {k} <b>{v}</b>
          </span>
        ))
      )}
    </div>
  );
}
