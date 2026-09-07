"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api, fmt, hasAdmin } from "@/lib/api";

type User = {
  id: string;
  email: string;
  name: string;
  avatar_url: string;
  created_at: string;
  api_key_id: string;
  devices: number;
  online_devices: number;
  credit_usd: number;
};

export default function Users() {
  const [users, setUsers] = useState<User[]>([]);
  const [note, setNote] = useState("");
  const [err, setErr] = useState("");
  const [q, setQ] = useState("");

  const load = useCallback(async () => {
    if (!hasAdmin()) return;
    setErr("");
    try {
      const r = await api.users();
      setUsers(r.users || []);
      setNote(r.note || "");
    } catch (e: any) {
      setErr(e.message);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, [load]);

  const shown = useMemo(() => {
    const n = q.trim().toLowerCase();
    return users.filter((u) => !n || [u.email, u.name, u.id, u.api_key_id].join(" ").toLowerCase().includes(n));
  }, [users, q]);

  const online = users.filter((u) => u.online_devices > 0).length;
  const credit = users.reduce((s, u) => s + (u.credit_usd || 0), 0);

  return (
    <>
      <div className="row between">
        <h1>Users</h1>
        <span className="muted small">
          {users.length} accounts · {online} with a device online · {fmt.usd(credit)} total credit
        </span>
      </div>
      {err && <div className="err">{err}</div>}
      {note && <div className="banner">{note}</div>}

      <div className="row" style={{ marginBottom: 12 }}>
        <input placeholder="filter by email, name, id, key…" value={q} onChange={(e) => setQ(e.target.value)} style={{ flex: 1 }} />
        <button className="secondary" onClick={load}>Refresh</button>
      </div>

      <div className="panel" style={{ overflowX: "auto" }}>
        {shown.length === 0 ? (
          <span className="muted">{users.length === 0 ? "no accounts yet" : "nothing matches the filter"}</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>User</th>
                <th>Joined</th>
                <th>API key</th>
                <th>Credit</th>
                <th>Devices</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((u) => (
                <tr key={u.id}>
                  <td>
                    <div className="row" style={{ gap: 8 }}>
                      {u.avatar_url ? <img src={u.avatar_url} alt="" width={22} height={22} style={{ borderRadius: 11 }} /> : null}
                      <div>
                        <div>{u.email}</div>
                        <div className="muted small">{u.name || "—"} · <span className="mono">{u.id}</span></div>
                      </div>
                    </div>
                  </td>
                  <td title={u.created_at}>{fmt.ago(u.created_at)}</td>
                  <td className="mono">{u.api_key_id || "—"}</td>
                  <td>{fmt.usd(u.credit_usd)}</td>
                  <td>
                    {u.online_devices}/{u.devices} online
                  </td>
                  <td>
                    {u.online_devices > 0 ? <span className="pill good">sharing now</span> : u.devices > 0 ? <span className="pill">has devices</span> : <span className="pill">consumer only</span>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <p className="muted small" style={{ marginTop: 10 }}>
        Credit can be adjusted for a key in <a href="/controls/">Controls</a>. Devices are counted by payout identity; a
        reinstalled app that regenerates its key shows as a new device.
      </p>
    </>
  );
}
