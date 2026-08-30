"use client";
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";

export default function Payouts() {
  const [bal, setBal] = useState<any>(null);
  const [pending, setPending] = useState<any[]>([]);
  const [dry, setDry] = useState<any>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState("");
  const [amount, setAmount] = useState(10);

  const load = useCallback(async () => {
    setErr("");
    try {
      setBal(await api.balance());
    } catch (e: any) {
      setErr("balance: " + e.message);
    }
    try {
      setPending((await api.payoutsPending()).pending || []);
    } catch (e: any) {
      setErr((p) => p || "payouts: " + e.message);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function run<T>(label: string, fn: () => Promise<T>) {
    setBusy(label);
    setErr("");
    try {
      return await fn();
    } catch (e: any) {
      setErr(`${label}: ${e.message}`);
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      <h1>Payments &amp; payouts</h1>
      {err && <div className="err">{err}</div>}

      <h2>Consumer credit</h2>
      <div className="panel">
        <p style={{ fontSize: 22 }}>
          ${bal ? bal.credit_usd.toFixed(5) : "—"}{" "}
          <span className="muted" style={{ fontSize: 13 }}>
            {bal?.enforced ? "· enforced" : "· not enforced"} · key {bal?.key_id}
          </span>
        </p>
        <div className="row" style={{ gap: 8, alignItems: "center" }}>
          <input
            type="number"
            min={0.5}
            step={0.5}
            value={amount}
            onChange={(e) => setAmount(parseFloat(e.target.value) || 0)}
            style={{ width: 100 }}
          />
          <button
            disabled={!!busy}
            onClick={() =>
              run("top up", async () => {
                const r = await api.topup(amount);
                if (r?.url) window.location.href = r.url;
              })
            }
          >
            Top up with card
          </button>
        </div>
        <p className="muted" style={{ marginTop: 6 }}>
          Redirects to Stripe Checkout. On success the balance is credited by webhook.
        </p>
      </div>

      <h2>Provider payouts (accrued, unpaid)</h2>
      <div className="panel">
        {pending.length === 0 ? (
          <span className="muted">nothing accrued</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Provider identity</th>
                <th>Jobs</th>
                <th>Owed</th>
                <th>Payout account</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {pending.map((p, i) => (
                <tr key={i}>
                  <td className="mono">{p.static_pk.slice(0, 16)}…</td>
                  <td>{p.jobs}</td>
                  <td>${p.owed_usd.toFixed(5)}</td>
                  <td className="mono">{p.stripe_account || "—"}</td>
                  <td>
                    <span className={`pill ${p.payable ? "good" : ""}`}>
                      {p.status || "no account"}
                    </span>
                  </td>
                  <td>
                    {!p.stripe_account ? (
                      <button
                        className="secondary"
                        disabled={!!busy}
                        onClick={() =>
                          run("connect", async () => {
                            const r = await api.payoutConnect(p.static_pk);
                            if (r?.onboarding_url) window.open(r.onboarding_url, "_blank");
                            await load();
                          })
                        }
                      >
                        Create account
                      </button>
                    ) : (
                      <button
                        className="secondary"
                        disabled={!!busy}
                        onClick={() =>
                          run("refresh", async () => {
                            await api.payoutRefresh(p.static_pk);
                            await load();
                          })
                        }
                      >
                        {p.payable ? "Re-check" : "Onboard / check"}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <h2>Run a payout batch</h2>
      <div className="panel">
        <div className="row" style={{ gap: 8 }}>
          <button
            disabled={!!busy}
            onClick={() =>
              run("dry run", async () => setDry(await api.payoutsRun(false, 0)))
            }
          >
            Dry run
          </button>
          <button
            className="secondary"
            disabled={!!busy || !dry}
            onClick={() =>
              run("commit", async () => {
                if (!confirm("Move money now? This creates real Stripe Transfers.")) return;
                setDry(await api.payoutsRun(true, 0));
                await load();
              })
            }
          >
            Commit (move money)
          </button>
        </div>
        {dry && (
          <pre
            style={{
              marginTop: 10,
              background: "#0e1116",
              padding: 12,
              borderRadius: 8,
              overflowX: "auto",
              fontSize: 12,
            }}
          >
            {JSON.stringify(dry, null, 2)}
          </pre>
        )}
        <p className="muted" style={{ marginTop: 6 }}>
          Dry run first. Commit creates Stripe Transfers from the platform balance to
          each enabled connected account and marks those earnings paid.
        </p>
      </div>
    </>
  );
}
