"use client";
import { useEffect, useState } from "react";
import { api, signInURL } from "./lib/api";

const LABELS: Record<string, string> = { github: "GitHub", google: "Google" };

export default function Home() {
  const [providers, setProviders] = useState<string[]>([]);
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    api
      .me()
      .then(() => {
        location.href = "/dashboard/";
      })
      .catch(() => {
        setChecking(false);
        api
          .providers()
          .then((p) => setProviders(p.providers || []))
          .catch(() => setProviders([]));
      });
  }, []);

  if (checking) return <p className="muted">Loading…</p>;

  return (
    <div style={{ maxWidth: 460 }}>
      <h1>Ayni</h1>
      <p className="muted" style={{ margin: "10px 0 26px" }}>
        Share your computer&rsquo;s idle time with the network and get paid, or run a
        batch workload and pay only for what it costs.
      </p>
      {providers.length === 0 ? (
        <p className="muted">Sign-in is not configured yet.</p>
      ) : (
        <div style={{ display: "grid", gap: 10 }}>
          {providers.map((p) => (
            <a key={p} href={signInURL(p)} className="btn btn-primary">
              Continue with {LABELS[p] || p}
            </a>
          ))}
        </div>
      )}
      <p className="muted" style={{ marginTop: 22, fontSize: ".85rem" }}>
        We store your email and name from the provider, nothing else. No password.
      </p>
    </div>
  );
}
