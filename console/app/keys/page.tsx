"use client";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";

export default function Keys() {
  const [keys, setKeys] = useState<any[]>([]);
  const [label, setLabel] = useState("");
  const [fresh, setFresh] = useState<{ id: string; key: string } | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function load() {
    setErr("");
    try {
      setKeys((await api.keys()).keys || []);
    } catch (e: any) {
      setErr(e.message);
    }
  }
  useEffect(() => {
    load();
  }, []);

  async function create() {
    setBusy(true);
    setErr("");
    try {
      const r = await api.createKey(label.trim() || "unnamed");
      setFresh({ id: r.id, key: r.key });
      setLabel("");
      await load();
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function toggle(id: string, disabled: boolean) {
    try {
      await api.setKey(id, disabled);
      await load();
    } catch (e: any) {
      setErr(e.message);
    }
  }

  return (
    <>
      <h1>API keys</h1>
      {err && <div className="err">{err}</div>}

      <div className="panel row">
        <input
          placeholder="label (e.g. prod-backend)"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
        />
        <button onClick={create} disabled={busy}>
          Create key
        </button>
      </div>

      {fresh && (
        <div className="panel" style={{ marginTop: 12, borderColor: "var(--good)" }}>
          <div className="muted">Copy this now — it is shown once.</div>
          <div className="mono" style={{ fontSize: 14, marginTop: 6, wordBreak: "break-all" }}>
            {fresh.key}
          </div>
          <button
            className="secondary"
            style={{ marginTop: 8 }}
            onClick={() => navigator.clipboard.writeText(fresh.key)}
          >
            Copy
          </button>
        </div>
      )}

      <h2>Existing</h2>
      <div className="panel">
        {keys.length === 0 ? (
          <span className="muted">no keys yet (bootstrap env keys are not listed)</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Label</th>
                <th>Created</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td className="mono">{k.id}</td>
                  <td>{k.label || "—"}</td>
                  <td className="muted">{new Date(k.created_at).toLocaleString()}</td>
                  <td>
                    <span className={"pill " + (k.disabled ? "bad" : "good")}>
                      {k.disabled ? "disabled" : "active"}
                    </span>
                  </td>
                  <td>
                    <button className="secondary" onClick={() => toggle(k.id, !k.disabled)}>
                      {k.disabled ? "Enable" : "Disable"}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
