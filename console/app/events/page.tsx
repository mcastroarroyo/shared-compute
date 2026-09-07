"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, fmt, hasAdmin, type Event } from "@/lib/api";

// Live tail of the coordinator's operational events: every Info+ log line and
// every operator action (AUDIT). Newest first, filter by level or text.

const LEVELS = ["", "ERROR", "WARN", "AUDIT", "INFO"] as const;

export default function Events() {
  const [events, setEvents] = useState<Event[]>([]);
  const [level, setLevel] = useState<string>("");
  const [q, setQ] = useState("");
  const [live, setLive] = useState(true);
  const [stats, setStats] = useState({ stored: 0, warnings_1h: 0, errors_1h: 0 });
  const [err, setErr] = useState("");
  const qRef = useRef(q);
  qRef.current = q;

  const load = useCallback(async () => {
    if (!hasAdmin()) return;
    setErr("");
    try {
      const r = await api.events(300, level, qRef.current);
      setEvents(r.events || []);
      setStats({ stored: r.stored, warnings_1h: r.warnings_1h, errors_1h: r.errors_1h });
    } catch (e: any) {
      setErr(e.message);
    }
  }, [level]);

  useEffect(() => {
    load();
  }, [load, q]);

  useEffect(() => {
    if (!live) return;
    const t = setInterval(load, 4_000);
    return () => clearInterval(t);
  }, [live, load]);

  return (
    <>
      <div className="row between">
        <h1>Events &amp; logs</h1>
        <span className="muted small">
          {stats.stored} stored · {stats.errors_1h} errors · {stats.warnings_1h} warnings in the last hour
        </span>
      </div>
      {err && <div className="err">{err}</div>}

      <div className="row" style={{ marginBottom: 12 }}>
        {LEVELS.map((l) => (
          <button key={l || "all"} className={level === l ? "" : "secondary"} onClick={() => setLevel(l)}>
            {l || "All"}
          </button>
        ))}
        <input placeholder="search message or attribute…" value={q} onChange={(e) => setQ(e.target.value)} style={{ flex: 1 }} />
        <label className="row" style={{ gap: 6 }}>
          <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} style={{ minWidth: 0 }} />
          <span className="muted small">live</span>
        </label>
        <button className="secondary" onClick={load}>Refresh</button>
      </div>

      <div className="panel" style={{ padding: 0 }}>
        {events.length === 0 ? (
          <div style={{ padding: 16 }} className="muted">no events match</div>
        ) : (
          <div className="log">
            {events.map((e) => (
              <div key={e.seq} className={`log-row ${e.level.toLowerCase()}`}>
                <span className="mono muted small" title={e.ts}>{fmt.time(e.ts)}</span>
                <span className={`pill ${e.level === "ERROR" ? "bad" : e.level === "WARN" ? "warn" : e.level === "AUDIT" ? "audit" : ""}`}>{e.level}</span>
                <span className="log-msg">{e.msg}</span>
                <span className="log-attrs mono small">
                  {Object.entries(e.attrs || {}).map(([k, v]) => (
                    <span key={k}><span className="muted">{k}=</span>{v} </span>
                  ))}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
      <p className="muted small" style={{ marginTop: 10 }}>
        The ring keeps the last 2,000 events in memory per coordinator instance and is content-free by design: prompts
        and completions are never logged. Full history lives in Cloud Logging.
      </p>
    </>
  );
}
