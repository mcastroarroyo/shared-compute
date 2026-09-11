"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, type Leaderboard } from "../lib/api";

export default function Founders() {
  const [lb, setLb] = useState<Leaderboard | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    const load = () => api.leaderboard().then(setLb).catch((e) => setErr(e.message || "could not load"));
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  }, []);

  return (
    <>
      <Link href="/testers/" className="muted">← Testers</Link>
      <h1 style={{ marginTop: 12 }}>Founding devices</h1>
      <p className="muted" style={{ margin: "6px 0 18px" }}>
        The first {lb ? lb.founding_cap.toLocaleString() : "1,000"} devices to join Ayni are founding devices, permanently.
        {lb && <> So far <strong>{lb.founding_devices}</strong> of {lb.founding_cap.toLocaleString()} have joined.</>}{" "}
        People who invite others appear here only if they chose to, under a name they picked.
      </p>
      <section className="card">
        <h3>Board</h3>
        {err && <p className="err">{err}</p>}
        {lb && lb.rows.length === 0 && (
          <p className="muted" style={{ margin: 0 }}>Nobody has opted in yet. Be the first: sign in, open the testers page, and turn on the board.</p>
        )}
        {lb && lb.rows.length > 0 && (
          <table style={{ width: "100%", borderCollapse: "collapse", fontSize: ".95rem" }}>
            <thead><tr className="muted" style={{ textAlign: "left" }}>
              <th style={{ padding: "6px 4px" }}>#</th><th>Name</th><th>Own devices</th><th>Invited people</th><th>Their devices</th>
            </tr></thead>
            <tbody>
              {lb.rows.map((r) => (
                <tr key={r.rank} style={{ borderTop: "1px solid var(--line)" }}>
                  <td style={{ padding: "8px 4px" }}>{r.rank}</td><td>{r.display_name}</td><td>{r.devices}</td><td>{r.referred_users}</td><td>{r.referred_devices}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
      <p className="muted" style={{ marginTop: 14, fontSize: ".9rem" }}>
        Want in? <Link href="/testers/">Ten minutes on the testers page</Link>, then share your invite link.
      </p>
    </>
  );
}
