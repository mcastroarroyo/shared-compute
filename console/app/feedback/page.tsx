"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api, fmt, hasAdmin } from "@/lib/api";

type Feedback = {
  id: number;
  kind: string;
  email: string;
  device: string;
  message: string;
  app: string;
  version: string;
  user_id?: string;
  created_at: string;
  replies?: { id: number; author: string; body: string; created_at: string }[];
};

const KINDS = ["all", "bug", "feedback", "idea", "tester_request"];

export default function FeedbackPage() {
  const [rows, setRows] = useState<Feedback[]>([]);
  const [err, setErr] = useState("");
  const [kind, setKind] = useState("all");
  const [q, setQ] = useState("");
  const [draft, setDraft] = useState<Record<number, string>>({});

  const reply = async (id: number) => {
    const body = (draft[id] || "").trim();
    if (!body) return;
    try {
      await api.replyFeedback(id, body);
      setDraft({ ...draft, [id]: "" });
      load();
    } catch (e: any) {
      setErr(e.message);
    }
  };

  const load = useCallback(async () => {
    if (!hasAdmin()) return;
    setErr("");
    try {
      const r = await api.feedback();
      setRows(r.feedback || []);
    } catch (e: any) {
      setErr(e.message);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 20_000);
    return () => clearInterval(t);
  }, [load]);

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return rows.filter((r) => (kind === "all" || r.kind === kind) &&
      (!needle || [r.email, r.device, r.message, r.app, r.version].join(" ").toLowerCase().includes(needle)));
  }, [rows, kind, q]);

  const counts = useMemo(() => {
    const c: Record<string, number> = {};
    rows.forEach((r) => { c[r.kind] = (c[r.kind] || 0) + 1; });
    return c;
  }, [rows]);

  const csv = () => {
    const esc = (s: string) => `"${String(s ?? "").replace(/"/g, '""')}"`;
    const lines = [["created_at", "kind", "email", "device", "app", "version", "message"].join(",")]
      .concat(shown.map((r) => [r.created_at, r.kind, r.email, r.device, r.app, r.version, r.message].map(esc).join(",")));
    const blob = new Blob([lines.join("\n")], { type: "text/csv" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob); a.download = "ayni-feedback.csv"; a.click();
  };

  return (
    <main>
      <div className="row between">
        <h1>Tester feedback</h1>
        <span className="muted">
          {rows.length} messages · {Object.entries(counts).map(([k, v]) => `${v} ${k}`).join(" · ")}
        </span>
      </div>
      {err && <p className="err">{err}</p>}
      <div className="row" style={{ gap: 8, margin: "12px 0" }}>
        {KINDS.map((k) => (
          <button key={k} className={"chip " + (kind === k ? "on" : "")} onClick={() => setKind(k)}>
            {k} {k !== "all" && counts[k] ? `(${counts[k]})` : ""}
          </button>
        ))}
        <input className="input" style={{ flex: 1 }} placeholder="filter: email, device, text…" value={q} onChange={(e) => setQ(e.target.value)} />
        <button className="btn" onClick={csv}>Export CSV</button>
        <button className="btn" onClick={load}>Refresh</button>
      </div>
      <div className="card" style={{ padding: 0 }}>
        {shown.length === 0 && <p className="muted" style={{ padding: 16 }}>No feedback yet. Testers post from app.ayni-ai.com/testers, the phone app, or the site.</p>}
        {shown.map((r) => (
          <div key={r.id} style={{ padding: "12px 16px", borderBottom: "1px solid var(--line)" }}>
            <div className="row between" style={{ alignItems: "baseline" }}>
              <span>
                <span className={"pill " + (r.kind === "bug" ? "bad" : r.kind === "tester_request" ? "warn" : "")}>{r.kind}</span>
                <strong style={{ marginLeft: 8 }}>{r.email || "anonymous"}</strong>
                {r.device && <span className="muted"> · {r.device}</span>}
                {r.app && <span className="muted"> · {r.app}{r.version ? ` ${r.version}` : ""}</span>}
              </span>
              <span className="muted">{fmt.ago(r.created_at)}</span>
            </div>
            <p style={{ margin: "8px 0 0", whiteSpace: "pre-wrap" }}>{r.message}</p>
            {(r.replies || []).map((rep) => (
              <div key={rep.id} style={{ margin: "8px 0 0 16px", padding: "8px 12px", borderRadius: 8, background: rep.author === "ayni" ? "rgba(91,141,239,.12)" : "rgba(255,255,255,.05)" }}>
                <span className="muted" style={{ fontSize: ".8rem" }}><strong>{rep.author === "ayni" ? "Ayni" : "Tester"}</strong> · {fmt.ago(rep.created_at)}</span>
                <p style={{ margin: "4px 0 0", whiteSpace: "pre-wrap" }}>{rep.body}</p>
              </div>
            ))}
            <div className="row" style={{ gap: 8, marginTop: 8, marginLeft: 16 }}>
              <input className="input" style={{ flex: 1 }} placeholder="Reply to this tester…" value={draft[r.id] || ""} onChange={(e) => setDraft({ ...draft, [r.id]: e.target.value })}
                onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); reply(r.id); } }} />
              <button className="btn" disabled={!(draft[r.id] || "").trim()} onClick={() => reply(r.id)}>Reply</button>
            </div>
          </div>
        ))}
      </div>
      <p className="muted" style={{ marginTop: 12, fontSize: ".85rem" }}>
        Reply to testers from your own mail; this page only collects. Each entry is also an INFO event in Events.
      </p>
    </main>
  );
}
