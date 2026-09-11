"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "../lib/api";
import { REF_KEY, claimPendingReferral } from "../invite-card";

/**
 * Landing page for invite links: app.ayni-ai.com/join/?c=<code>.
 * Stores the code, and if the visitor is already signed in claims it right away.
 */
export default function Join() {
  const [code, setCode] = useState("");
  const [signedIn, setSignedIn] = useState<boolean | null>(null);

  useEffect(() => {
    const c = new URLSearchParams(window.location.search).get("c") || "";
    if (c) {
      try { localStorage.setItem(REF_KEY, c.toLowerCase()); } catch {}
      setCode(c.toLowerCase());
    }
    api.me().then(async () => {
      setSignedIn(true);
      await claimPendingReferral();
    }).catch(() => setSignedIn(false));
  }, []);

  return (
    <>
      <h1 style={{ marginTop: 12 }}>You have been invited to Ayni.</h1>
      <p className="muted" style={{ margin: "6px 0 18px" }}>
        Ayni turns idle phones and laptops into a private inference network, open source and encrypted per job.
        It is early: a small fleet, test jobs a few times a day, and paid workloads ramping up as buyers arrive.
        Join and your device becomes part of the community that makes it real.
        {code && <> Invite code <code>{code}</code> is saved; it applies when you sign in.</>}
      </p>
      <div className="grid c3" style={{ marginBottom: 18 }}>
        <div className="card"><h3>1 · Sign in</h3><p className="muted" style={{ fontSize: ".9rem" }}>GitHub or Google. No password, no card.</p></div>
        <div className="card"><h3>2 · Add a device</h3><p className="muted" style={{ fontSize: ".9rem" }}>Phone: a 6-letter code. Mac or Linux: one command.</p></div>
        <div className="card"><h3>3 · Watch it work</h3><p className="muted" style={{ fontSize: ".9rem" }}>A test job reaches your device within minutes.</p></div>
      </div>
      <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
        {signedIn ? (
          <Link href="/share/" className="btn btn-primary">Add a device</Link>
        ) : (
          <Link href="/" className="btn btn-primary">Sign in to join</Link>
        )}
        <Link href="/founders/" className="btn btn-ghost">See the founders board</Link>
      </div>
    </>
  );
}
