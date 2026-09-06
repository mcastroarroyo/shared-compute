"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { api, type Me } from "../lib/api";

/**
 * Landing page for Stripe Checkout. The coordinator sends people back here with
 * ?topup=success or ?topup=cancel; the credit itself is applied by the Stripe
 * webhook, so on success we poll the balance briefly until it moves.
 */
export default function Billing() {
  const [status, setStatus] = useState<"success" | "cancel" | "">("");
  const [me, setMe] = useState<Me | null>(null);
  const [initial, setInitial] = useState<number | null>(null);

  useEffect(() => {
    const q = new URLSearchParams(window.location.search).get("topup");
    setStatus(q === "success" ? "success" : q === "cancel" ? "cancel" : "");
    let tries = 0;
    const tick = () =>
      api
        .me()
        .then((m) => {
          setMe(m);
          setInitial((i) => (i === null ? m.credit_usd : i));
          if (q === "success" && tries++ < 10) setTimeout(tick, 2000);
        })
        .catch(() => (location.href = "/"));
    tick();
  }, []);

  const credited = me && initial !== null && me.credit_usd > initial;

  return (
    <>
      <Link href="/dashboard/" className="muted">
        ← Dashboard
      </Link>
      <h1 style={{ marginTop: 12 }}>
        {status === "success" ? "Payment received" : status === "cancel" ? "Payment cancelled" : "Billing"}
      </h1>
      {status === "success" && (
        <p className="muted">
          {credited
            ? "Your credits are in. You can run workloads now."
            : "Stripe confirmed the payment. Your balance updates as soon as the receipt reaches us, usually within a few seconds."}
        </p>
      )}
      {status === "cancel" && <p className="muted">Nothing was charged. You can add credit any time from the dashboard.</p>}
      {me && (
        <section className="card" style={{ maxWidth: 420, marginTop: 18 }}>
          <div className="row">
            <span>Credit balance</span>
            <span className="big">${me.credit_usd.toFixed(2)}</span>
          </div>
        </section>
      )}
      <p style={{ marginTop: 22 }}>
        <Link href="/run/" className="btn">Run a workload</Link>
      </p>
    </>
  );
}
