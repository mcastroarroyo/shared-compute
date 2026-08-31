"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "../lib/api";

export default function Earnings() {
  const [e, setE] = useState<{ owed_usd: number; jobs: number; devices: number } | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    api
      .earnings()
      .then(setE)
      .catch((x: any) => {
        if (x.status === 401) location.href = "/";
        else setErr(x.message);
      });
  }, []);

  return (
    <>
      <Link href="/dashboard/" className="muted">
        ← Dashboard
      </Link>
      <h1 style={{ marginTop: 12 }}>Earnings</h1>
      {err && <p className="err">{err}</p>}
      {!e ? (
        <p className="muted">Loading…</p>
      ) : (
        <>
          <div className="card" style={{ maxWidth: 360, marginTop: 12 }}>
            <div className="kv">
              <span>Accrued, unpaid</span>
              <span className="big">${e.owed_usd.toFixed(2)}</span>
            </div>
            <div className="kv">
              <span>Jobs completed</span>
              <span>{e.jobs}</span>
            </div>
            <div className="kv">
              <span>Devices linked</span>
              <span>{e.devices}</span>
            </div>
          </div>
          <p className="muted" style={{ marginTop: 16, fontSize: ".88rem" }}>
            Payouts run on a schedule once a connected Stripe account is set up and a
            minimum is reached. This is the shadow ledger — the operator commits each
            payout batch.
          </p>
        </>
      )}
    </>
  );
}
