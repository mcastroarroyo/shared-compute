"use client";
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";

// The marketplace's view of supply: every node that has reported a self-benchmark,
// its measured sustained throughput, and the derived Ayni Compute Unit (ACU).
// ACU ~1.0 is a mid-range phone; the class label is a coarse bucket (M1..M5).

type Node = {
  static_pk: string;
  online: boolean;
  model: string;
  backend: string;
  decode_tps: number;
  sustained_start_tps: number;
  sustained_end_tps: number;
  thermal_decay_pct: number;
  mem_bandwidth_gbps: number;
  available_ram_mb: number;
  cpu_cores: number;
  acu: number;
  class: string;
  updated_at: string;
};

export default function Nodes() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [totalAcu, setTotalAcu] = useState(0);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setErr("");
    try {
      const r = await api.nodes();
      setNodes(r.nodes || []);
      setTotalAcu(r.online_acu_total || 0);
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 10_000);
    return () => clearInterval(t);
  }, [load]);

  const online = nodes.filter((n) => n.online).length;

  return (
    <>
      <h1>Nodes</h1>
      {err && <div className="err">{err}</div>}

      <div className="row" style={{ gap: 12, flexWrap: "wrap", marginBottom: 12 }}>
        <div className="panel" style={{ flex: "1 1 140px" }}>
          <div className="muted" style={{ fontSize: 12 }}>Online now</div>
          <div style={{ fontSize: 24 }}>
            {online} <span className="muted" style={{ fontSize: 13 }}>/ {nodes.length} known</span>
          </div>
        </div>
        <div className="panel" style={{ flex: "1 1 140px" }}>
          <div className="muted" style={{ fontSize: 12 }}>Online capacity</div>
          <div style={{ fontSize: 24 }}>
            {totalAcu.toFixed(2)} <span className="muted" style={{ fontSize: 13 }}>ACU</span>
          </div>
        </div>
      </div>

      <div className="panel">
        {loading ? (
          <span className="muted">loading…</span>
        ) : nodes.length === 0 ? (
          <span className="muted">
            no benchmarks yet — nodes report one on first connect
          </span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Identity</th>
                <th></th>
                <th>ACU</th>
                <th>Class</th>
                <th>Model / backend</th>
                <th>Sustained tok/s</th>
                <th>Thermal decay</th>
                <th>Mem GB/s</th>
                <th>RAM</th>
                <th>Cores</th>
                <th>Benchmarked</th>
              </tr>
            </thead>
            <tbody>
              {nodes
                .slice()
                .sort((a, b) => Number(b.online) - Number(a.online) || b.acu - a.acu)
                .map((n) => (
                  <tr key={n.static_pk} style={{ opacity: n.online ? 1 : 0.5 }}>
                    <td className="mono">{n.static_pk.slice(0, 16)}…</td>
                    <td>
                      <span className={`pill ${n.online ? "good" : ""}`}>
                        {n.online ? "online" : "offline"}
                      </span>
                    </td>
                    <td style={{ fontWeight: 600 }}>{n.acu.toFixed(2)}</td>
                    <td>{n.class}</td>
                    <td>
                      {n.model}
                      <span className="muted"> · {n.backend}</span>
                    </td>
                    <td>
                      {n.sustained_start_tps.toFixed(1)} → {n.sustained_end_tps.toFixed(1)}
                    </td>
                    <td>{n.thermal_decay_pct.toFixed(1)}%</td>
                    <td>{n.mem_bandwidth_gbps.toFixed(1)}</td>
                    <td>{(n.available_ram_mb / 1024).toFixed(1)} GB</td>
                    <td>{n.cpu_cores}</td>
                    <td className="muted">
                      {new Date(n.updated_at).toLocaleString()}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        )}
      </div>
      <p className="muted" style={{ marginTop: 8 }}>
        ACU is derived from measured sustained throughput, not advertised specs. Sustained
        tok/s shows the first half of the benchmark run → the second half, so a throttling
        device shows a drop. Scores are comparable across nodes running the same model.
      </p>
    </>
  );
}
