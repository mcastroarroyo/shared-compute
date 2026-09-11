"use client";

import { useEffect, useState } from "react";

const API = "https://api.ayni-ai.com";

type Stats = {
  devices_online: number;
  devices_known: number;
  by_kind: { phone: number; pc: number; other: number };
  acu_online: number;
  jobs_24h: number;
  jobs_7d: number;
};

/**
 * Live fleet numbers from the public stats endpoint. Falls back to the health
 * endpoint's provider count so the counter still shows something if the stats
 * route is unavailable. Aggregates only; nothing identifies a person or device.
 */
export default function FleetCounter({ compact = false }: { compact?: boolean }) {
  const [s, setS] = useState<Stats | null>(null);
  const [fallback, setFallback] = useState<number | null>(null);

  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const r = await fetch(`${API}/v1/stats`, { cache: "no-store" });
        if (r.ok) {
          const j = (await r.json()) as Stats;
          if (alive) setS(j);
          return;
        }
      } catch {}
      try {
        const r = await fetch(`${API}/healthz`, { cache: "no-store" });
        const j = await r.json();
        if (alive) setFallback(Number(j.providers ?? 0));
      } catch {}
    };
    load();
    const t = setInterval(load, 30_000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  const online = s ? s.devices_online : fallback;
  const cell = (n: string | number, label: string) => (
    <div style={{ minWidth: compact ? 90 : 120 }}>
      <div style={{ fontSize: compact ? 22 : 30, fontWeight: 700, lineHeight: 1.1 }}>{n}</div>
      <div style={{ fontSize: 13, opacity: 0.7 }}>{label}</div>
    </div>
  );

  return (
    <div
      aria-live="polite"
      style={{
        display: "flex",
        gap: compact ? 20 : 32,
        flexWrap: "wrap",
        padding: compact ? "12px 16px" : "18px 22px",
        border: "1px solid var(--line, #e6e2d8)",
        borderRadius: 12,
        margin: "8px 0 20px",
      }}
    >
      {cell(online ?? "…", "devices online now")}
      {s && cell(s.devices_known, "devices that have joined")}
      {s && cell(`${s.by_kind.phone} / ${s.by_kind.pc}`, "phones / computers online")}
      {s && cell(s.jobs_24h, "jobs in the last 24 h")}
      <div style={{ alignSelf: "center", fontSize: 13, opacity: 0.7, flexBasis: "100%" }}>
        Live from the network, refreshed every 30 seconds. Small numbers are honest numbers: this is
        the community as it stands today.
      </div>
    </div>
  );
}
