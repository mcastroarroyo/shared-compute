"use client";
import { useEffect, useState } from "react";
import { getCfg, setCfg, api } from "@/lib/api";

export default function Settings() {
  const [base, setBase] = useState("");
  const [admin, setAdmin] = useState("");
  const [consumer, setConsumer] = useState("");
  const [status, setStatus] = useState<string>("");

  useEffect(() => {
    const c = getCfg();
    setBase(c.base);
    setAdmin(c.admin);
    setConsumer(c.consumer);
  }, []);

  function save() {
    setCfg({ base, admin, consumer });
    setStatus("saved");
    setTimeout(() => setStatus(""), 1500);
  }

  async function test() {
    setStatus("testing…");
    try {
      const h = await api.health();
      setStatus(`coordinator OK — ${h.providers} provider(s) connected`);
    } catch (e: any) {
      setStatus("failed: " + e.message);
    }
  }

  return (
    <>
      <h1>Settings</h1>
      <p className="muted">
        Stored in this browser only. Single-operator setup — real multi-user auth arrives in M9.
      </p>
      <div className="panel" style={{ display: "grid", gap: 14, maxWidth: 620 }}>
        <label>
          <div className="muted">Coordinator URL</div>
          <input value={base} onChange={(e) => setBase(e.target.value)} style={{ width: "100%" }} />
        </label>
        <label>
          <div className="muted">Admin token (SC_ADMIN_TOKEN) — for keys / providers / usage</div>
          <input
            type="password"
            value={admin}
            onChange={(e) => setAdmin(e.target.value)}
            style={{ width: "100%" }}
          />
        </label>
        <label>
          <div className="muted">Consumer API key — for the Playground</div>
          <input
            type="password"
            value={consumer}
            onChange={(e) => setConsumer(e.target.value)}
            style={{ width: "100%" }}
          />
        </label>
        <div className="row">
          <button onClick={save}>Save</button>
          <button className="secondary" onClick={test}>
            Test connection
          </button>
          <span className="muted">{status}</span>
        </div>
      </div>
    </>
  );
}
