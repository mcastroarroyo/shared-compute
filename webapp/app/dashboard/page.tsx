"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, type Me } from "../lib/api";

export default function Dashboard() {
  const [me, setMe] = useState<Me | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () =>
    api
      .me()
      .then(setMe)
      .catch(() => {
        location.href = "/";
      });

  useEffect(() => {
    load();
  }, []);

  if (err) return <p className="err">{err}</p>;
  if (!me) return <p className="muted">Loading…</p>;

  return (
    <>
      <h1>Welcome{me.user.name ? `, ${me.user.name.split(" ")[0]}` : ""}</h1>
      <p className="muted" style={{ margin: "8px 0 24px" }}>
        Credit balance: <strong>${me.credit_usd.toFixed(2)}</strong>{" "}
        <button
          className="btn btn-ghost"
          style={{ padding: "4px 12px", fontSize: ".85rem", marginLeft: 8 }}
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            try {
              const r = await api.topup(10);
              if (r.url) location.href = r.url;
            } catch (e: any) {
              setErr(e.message);
            } finally {
              setBusy(false);
            }
          }}
        >
          Add $10
        </button>
      </p>

      <div className="grid c3">
        <Link href="/share/" className="card">
          <h3>Share your computer</h3>
          <p className="muted">
            Run a small provider on your laptop or desktop. It earns a share of every
            job it completes.
          </p>
        </Link>
        <Link href="/run/" className="card">
          <h3>Run a workload</h3>
          <p className="muted">
            Summarize a document (more soon). Get a Council-reviewed quote, accept, and
            it runs across the network.
          </p>
        </Link>
        <Link href="/earnings/" className="card">
          <h3>Earnings</h3>
          <p className="muted">What your shared machines have earned, and payout status.</p>
        </Link>
      </div>
    </>
  );
}
