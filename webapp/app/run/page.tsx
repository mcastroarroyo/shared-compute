"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, type Me } from "../lib/api";

const MODEL = "qwen2.5-0.5b-instruct-q4_k_m";

type Quote = {
  id: string;
  price: { total_usd: number };
  estimate: { eta_seconds: number; eligible_nodes: number; prompt_tokens?: number; completion_tokens?: number };
  council: {
    decision: string;
    reviews: { seat: string; role: string; decision: string; severity: string }[];
    required_controls: string[];
    audit_hash: string;
  } | null;
};

function humanETA(s: number) {
  if (s < 90) return `${Math.max(1, Math.round(s))} sec`;
  if (s < 5400) return `${Math.round(s / 60)} min`;
  return `${(s / 3600).toFixed(1)} hr`;
}

export default function Run() {
  const [me, setMe] = useState<Me | null>(null);
  const [doc, setDoc] = useState("");
  const [quote, setQuote] = useState<Quote | null>(null);
  const [result, setResult] = useState<{ text: string; charged: number } | null>(null);
  const [err, setErr] = useState("");
  const [needTopup, setNeedTopup] = useState(false);
  const [busy, setBusy] = useState("");

  useEffect(() => {
    api.me().then(setMe).catch(() => (location.href = "/"));
  }, []);

  async function getQuote() {
    setErr("");
    setResult(null);
    setNeedTopup(false);
    setBusy("quote");
    try {
      const q = await api.quoteWorkload({
        model: MODEL,
        items: [
          {
            messages: [
              {
                role: "user",
                content:
                  "Summarize the following document in 4–6 sentences. Be faithful and concise.\n\n" +
                  doc.slice(0, 12000),
              },
            ],
            max_tokens: 320,
          },
        ],
      });
      setQuote(q);
    } catch (e: any) {
      if (e.code === "council_blocked") {
        setErr("The Ayni Council blocked this workload. " + (e.message || ""));
      } else {
        setErr(e.message);
      }
    } finally {
      setBusy("");
    }
  }

  async function accept() {
    if (!quote) return;
    setErr("");
    setBusy("accept");
    try {
      const r = await api.acceptWorkload(quote.id);
      const item = (r.items || [])[0] || {};
      setResult({
        text: item.message?.content || item.error?.message || "(no output)",
        charged: r.charged_usd ?? 0,
      });
      setQuote(null);
      api.me().then(setMe).catch(() => {});
    } catch (e: any) {
      if (e.status === 402) {
        setNeedTopup(true);
        setErr("Not enough credit for this quote.");
      } else {
        setErr(e.message);
      }
    } finally {
      setBusy("");
    }
  }

  if (!me) return <p className="muted">Loading…</p>;

  return (
    <>
      <Link href="/dashboard/" className="muted">
        ← Dashboard
      </Link>
      <h1 style={{ marginTop: 12 }}>Run a workload</h1>
      <p className="muted" style={{ margin: "8px 0 18px" }}>
        Paste a document. Ayni quotes it, the Council reviews it, and on accept it runs
        across the network. Credit balance: <strong>${me.credit_usd.toFixed(2)}</strong>.
      </p>

      <label htmlFor="doc">Document</label>
      <textarea
        id="doc"
        rows={10}
        value={doc}
        onChange={(e) => setDoc(e.target.value)}
        placeholder="Paste the text you want summarized…"
      />
      <div style={{ marginTop: 12 }}>
        <button className="btn btn-primary" disabled={!doc.trim() || !!busy} onClick={getQuote}>
          {busy === "quote" ? "Quoting…" : "Get a quote"}
        </button>
      </div>

      {err && (
        <p className="err" style={{ marginTop: 14 }}>
          {err}
          {needTopup && (
            <>
              {" "}
              <button
                className="btn btn-ghost"
                style={{ padding: "4px 12px", fontSize: ".85rem" }}
                onClick={async () => {
                  const r = await api.topup(10);
                  if (r.url) location.href = r.url;
                }}
              >
                Add $10 credit
              </button>
            </>
          )}
        </p>
      )}

      {quote && (
        <div className="card" style={{ marginTop: 18 }}>
          <div className="kv">
            <span>Quoted price</span>
            <span className="big">${quote.price.total_usd.toFixed(2)}</span>
          </div>
          {(quote.estimate.prompt_tokens ?? 0) + (quote.estimate.completion_tokens ?? 0) > 0 && (
            <div className="kv">
              <span>Per 1M tokens</span>
              <span>
                $
                {(
                  (quote.price.total_usd /
                    ((quote.estimate.prompt_tokens ?? 0) + (quote.estimate.completion_tokens ?? 0))) *
                  1_000_000
                ).toFixed(3)}
                {" "}({(quote.estimate.prompt_tokens ?? 0) + (quote.estimate.completion_tokens ?? 0)} tokens)
              </span>
            </div>
          )}
          <div className="kv">
            <span>Est. completion</span>
            <span>{humanETA(quote.estimate.eta_seconds)}</span>
          </div>
          <div className="kv">
            <span>Devices available</span>
            <span>{quote.estimate.eligible_nodes}</span>
          </div>
          {quote.council && (
            <div style={{ marginTop: 12 }}>
              <div className="kv">
                <span>Ayni Council</span>
                <span className={`pill ${quote.council.decision === "APPROVE" ? "" : "warn"}`}>
                  {quote.council.decision}
                </span>
              </div>
              <ul className="muted" style={{ margin: "8px 0 0 1.1em", fontSize: ".88rem" }}>
                {quote.council.reviews.map((r, i) => (
                  <li key={i}>
                    {r.role || r.seat}: {r.decision} ({r.severity})
                  </li>
                ))}
              </ul>
              <p className="muted mono" style={{ marginTop: 6, fontSize: ".78rem" }}>
                audit {quote.council.audit_hash.slice(0, 20)}…
              </p>
            </div>
          )}
          <div style={{ marginTop: 14 }}>
            <button className="btn btn-teal" disabled={!!busy} onClick={accept}>
              {busy === "accept" ? "Running…" : `Accept & pay $${quote.price.total_usd.toFixed(2)}`}
            </button>
          </div>
        </div>
      )}

      {result && (
        <div className="card" style={{ marginTop: 18 }}>
          <h3>Summary</h3>
          <p className="ok" style={{ fontSize: ".88rem", margin: "4px 0 10px" }}>
            Done — charged ${result.charged.toFixed(2)}.
          </p>
          <div className="result">{result.text}</div>
        </div>
      )}
    </>
  );
}
