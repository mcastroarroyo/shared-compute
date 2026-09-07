"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api, fmt, hasAdmin, type Device } from "@/lib/api";

// Every connected device, joined with its benchmark, owner and ledger, plus the
// catalog of devices that benchmarked before but are offline now.

type Node = {
  static_pk: string;
  online: boolean;
  model: string;
  backend: string;
  decode_tps: number;
  sustained_end_tps: number;
  thermal_decay_pct: number;
  available_ram_mb: number;
  cpu_cores: number;
  acu: number;
  class: string;
  updated_at: string;
};

export default function Devices() {
  const [devs, setDevs] = useState<Device[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [err, setErr] = useState("");
  const [kind, setKind] = useState<"all" | "phone" | "pc" | "other">("all");
  const [q, setQ] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [busy, setBusy] = useState("");
  const [msg, setMsg] = useState("");

  const load = useCallback(async () => {
    if (!hasAdmin()) return;
    setErr("");
    try {
      const [p, n] = await Promise.all([api.providers(), api.nodes()]);
      setDevs(p.providers || []);
      setNodes(n.nodes || []);
    } catch (e: any) {
      setErr(e.message);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 8_000);
    return () => clearInterval(t);
  }, [load]);

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return devs.filter((d) => {
      if (kind !== "all" && d.kind !== kind) return false;
      if (!needle) return true;
      const hay = [d.id, d.static_pk, d.platform, d.arch, d.cpu, d.owner?.email, d.owner?.name, d.class, d.trust_tier, ...(d.models || [])]
        .join(" ")
        .toLowerCase();
      return hay.includes(needle);
    });
  }, [devs, kind, q]);

  const offline = nodes.filter((n) => !n.online);
  const counts = { phone: 0, pc: 0, other: 0 } as Record<string, number>;
  devs.forEach((d) => counts[d.kind]++);

  async function disconnect(d: Device) {
    const reason = window.prompt(`Disconnect ${d.platform} ${d.id.slice(0, 8)} (${d.owner?.email || "no owner"})?\nReason shown to the device:`, "operator maintenance");
    if (reason === null) return;
    setBusy(d.id);
    setMsg("");
    try {
      await api.disconnectProvider(d.id, reason);
      setMsg(`disconnected ${d.id.slice(0, 8)} — it will reconnect on its own unless the owner stops it`);
      await load();
    } catch (e: any) {
      setMsg("failed: " + e.message);
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      <div className="row between">
        <h1>Devices</h1>
        <span className="muted small">
          {devs.length} online · {counts.phone} phones · {counts.pc} PCs · {counts.other} other · {offline.length} offline in catalog
        </span>
      </div>
      {err && <div className="err">{err}</div>}
      {msg && <div className="banner">{msg}</div>}

      <div className="row" style={{ marginBottom: 12 }}>
        {(["all", "phone", "pc", "other"] as const).map((k) => (
          <button key={k} className={kind === k ? "" : "secondary"} onClick={() => setKind(k)}>
            {k === "all" ? `All (${devs.length})` : `${fmt.kind(k)} (${counts[k]})`}
          </button>
        ))}
        <input placeholder="filter: owner, platform, model, class, id…" value={q} onChange={(e) => setQ(e.target.value)} style={{ flex: 1 }} />
      </div>

      <div className="panel" style={{ overflowX: "auto" }}>
        {shown.length === 0 ? (
          <span className="muted">{devs.length === 0 ? "no devices connected" : "nothing matches the filter"}</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Type</th>
                <th>Platform</th>
                <th>Owner</th>
                <th>Tier</th>
                <th>Class · ACU</th>
                <th>tok/s</th>
                <th>RAM</th>
                <th>Jobs</th>
                <th>Battery</th>
                <th>Connected</th>
                <th>Seen</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {shown.map((d) => (
                <DeviceRow key={d.id} d={d} open={open === d.id} onToggle={() => setOpen(open === d.id ? null : d.id)} onDisconnect={() => disconnect(d)} busy={busy === d.id} />
              ))}
            </tbody>
          </table>
        )}
      </div>

      <h2>Offline devices (catalog)</h2>
      <div className="panel" style={{ overflowX: "auto" }}>
        <div className="muted small" style={{ marginBottom: 8 }}>
          Devices that reported a benchmark before but are not connected now. Identity is the payout key; owner is
          shown when the device connects again.
        </div>
        {offline.length === 0 ? (
          <span className="muted">none</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Identity</th>
                <th>Model</th>
                <th>Class · ACU</th>
                <th>tok/s</th>
                <th>RAM</th>
                <th>Cores</th>
                <th>Last benchmark</th>
              </tr>
            </thead>
            <tbody>
              {offline.map((n) => (
                <tr key={n.static_pk}>
                  <td className="mono">{n.static_pk.slice(0, 12)}…</td>
                  <td className="mono">{n.model}</td>
                  <td>{n.class} · {n.acu.toFixed(2)}</td>
                  <td>{n.sustained_end_tps.toFixed(1)}</td>
                  <td>{fmt.gb(n.available_ram_mb)}</td>
                  <td>{n.cpu_cores}</td>
                  <td>{fmt.ago(n.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}

function DeviceRow({ d, open, onToggle, onDisconnect, busy }: { d: Device; open: boolean; onToggle: () => void; onDisconnect: () => void; busy: boolean }) {
  const bat = d.telemetry?.battery_pct;
  const chg = d.telemetry?.charging;
  const sec = Object.entries(d.security || {}).filter(([, v]) => v).map(([k]) => k);
  return (
    <>
      <tr className="clickable" onClick={onToggle}>
        <td><span className={`tag ${d.kind}`}>{fmt.kind(d.kind)}</span></td>
        <td>
          {d.platform}/{d.arch}
          {d.draining && <span className="pill bad" style={{ marginLeft: 6 }}>draining</span>}
        </td>
        <td>{d.owner ? <span title={d.owner.user_id}>{d.owner.email}</span> : <span className="muted">env token</span>}</td>
        <td><span className={`pill ${d.trust_tier === "community" ? "" : "good"}`}>{d.trust_tier}</span></td>
        <td>{d.class ? `${d.class} · ${d.acu.toFixed(2)}` : <span className="muted">no benchmark</span>}</td>
        <td>{d.sustained_end_tps ? d.sustained_end_tps.toFixed(1) : d.decode_tps ? d.decode_tps.toFixed(1) : "—"}</td>
        <td>{fmt.gb(d.ram_mb)}</td>
        <td>{d.active_jobs}{d.jobs_unpaid ? <span className="muted small"> · {d.jobs_unpaid} unpaid</span> : null}</td>
        <td>{bat == null ? <span className="muted">—</span> : `${bat}%${chg ? " ⚡" : ""}`}</td>
        <td title={d.connected_at}>{d.connected_for}</td>
        <td>{fmt.ago(d.last_seen)}</td>
        <td>
          <button className="secondary small" disabled={busy} onClick={(e) => { e.stopPropagation(); onDisconnect(); }}>
            {busy ? "…" : "Disconnect"}
          </button>
        </td>
      </tr>
      {open && (
        <tr className="detail">
          <td colSpan={12}>
            <div className="detail-grid">
              <div><span className="muted">Provider id</span><div className="mono">{d.id}</div></div>
              <div><span className="muted">Identity (payout key)</span><div className="mono">{d.static_pk}</div></div>
              <div><span className="muted">CPU</span><div>{d.cpu || "—"} · {d.cpu_cores || "?"} cores</div></div>
              <div><span className="muted">Backend · class</span><div>{d.backend} · {d.hardware_class}</div></div>
              <div><span className="muted">Models</span><div className="mono">{(d.models || []).join(", ") || "—"}</div></div>
              <div><span className="muted">Max context</span><div>{fmt.n(d.max_context)}</div></div>
              <div><span className="muted">Telemetry</span><div>cpu {Math.round((d.telemetry?.cpu_load || 0) * 100)}% · free {fmt.gb(d.telemetry?.mem_available_mb || 0)} · queue {d.telemetry?.queue_depth ?? 0} · {d.telemetry?.network || "net ?"} · {d.thermal_state || "thermal ?"}</div></div>
              <div><span className="muted">Security</span><div>{sec.length ? sec.join(", ") : "none reported"}</div></div>
              <div><span className="muted">Benchmark</span><div>{d.benchmark_at ? `${fmt.ago(d.benchmark_at)} · ${d.decode_tps.toFixed(1)} → ${d.sustained_end_tps.toFixed(1)} tok/s` : "not yet reported"}</div></div>
              <div><span className="muted">Owed (unpaid)</span><div>{fmt.usd(d.owed_usd, 5)} over {d.jobs_unpaid} jobs</div></div>
              <div><span className="muted">Connected at</span><div>{new Date(d.connected_at).toLocaleString()}</div></div>
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
