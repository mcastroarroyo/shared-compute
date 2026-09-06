"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, API, type Me } from "../lib/api";

const WS = API.replace(/^http/, "ws") + "/ws/provider";
const REPO = "https://github.com/mcastroarroyo/shared-compute";

export default function Share() {
  const [me, setMe] = useState<Me | null>(null);
  const [copied, setCopied] = useState("");

  useEffect(() => {
    api.me().then(setMe).catch(() => (location.href = "/"));
  }, []);

  if (!me) return <p className="muted">Loading…</p>;
  const tok = me.provider_token || "<sign in again to mint a token>";

  const oneLiner = `curl -fsSL ${API}/install/provider.sh | SC_REGISTRATION_TOKEN=${tok} bash`;
  const fromSource = `git clone ${REPO} && cd shared-compute
SC_COORDINATOR_URL=${WS} \\
SC_REGISTRATION_TOKEN=${tok} \\
./infra/run-provider.sh`;

  const copy = (label: string, text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(label);
      setTimeout(() => setCopied(""), 1500);
    });
  };

  return (
    <>
      <Link href="/dashboard/" className="muted">
        ← Dashboard
      </Link>
      <h1 style={{ marginTop: 12 }}>Share your computer</h1>
      <section className="card" style={{ margin: "14px 0" }}>
        <h3>Phone app (Android)</h3>
        <p className="muted">
          Install Ayni from Google Play, open Settings in the app and paste this
          registration token, then Save and Start.
        </p>
        <pre className="code" style={{ overflowWrap: "anywhere", whiteSpace: "pre-wrap" }}>{tok}</pre>
        <button className="btn" onClick={() => copy("token", tok)}>
          {copied === "token" ? "Copied" : "Copy token"}
        </button>
      </section>
      <p className="muted" style={{ margin: "8px 0 22px" }}>
        Your machine joins the network as a provider. It runs only while the command is
        running — close the terminal to stop. Earnings for every job it completes accrue
        to <strong>{me.user.email}</strong>.
      </p>

      <div className="card" style={{ marginBottom: 16 }}>
        <h3>One-liner (macOS / Linux)</h3>
        <p className="muted" style={{ fontSize: ".9rem", margin: "6px 0 10px" }}>
          Downloads a small prebuilt provider and a 0.5B model, then connects.
        </p>
        <pre className="cmd">{oneLiner}</pre>
        <button className="btn btn-ghost" style={{ marginTop: 10 }} onClick={() => copy("one", oneLiner)}>
          {copied === "one" ? "Copied" : "Copy"}
        </button>
      </div>

      <div className="card">
        <h3>From source (any OS with Rust + CMake)</h3>
        <p className="muted" style={{ fontSize: ".9rem", margin: "6px 0 10px" }}>
          Builds the provider with the embedded llama.cpp and connects.
        </p>
        <pre className="cmd">{fromSource}</pre>
        <button className="btn btn-ghost" style={{ marginTop: 10 }} onClick={() => copy("src", fromSource)}>
          {copied === "src" ? "Copied" : "Copy"}
        </button>
      </div>

      <p className="muted" style={{ marginTop: 20, fontSize: ".85rem" }}>
        This token is tied to your account. Keep it private — anyone with it can attach a
        machine to the network as you.
      </p>
    </>
  );
}
