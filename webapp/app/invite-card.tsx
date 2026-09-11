"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, type Referral } from "./lib/api";

export const REF_KEY = "ayni_ref";

/** Claims a pending invite code (stored by /join/) once the user is signed in. */
export async function claimPendingReferral() {
  try {
    const code = localStorage.getItem(REF_KEY);
    if (!code) return;
    await api.claimReferral(code);
    localStorage.removeItem(REF_KEY);
  } catch {
    /* not signed in yet, or already claimed: keep the code for later */
  }
}

/**
 * The user's invite link, referral counts, founding-device badge and the
 * leaderboard opt-in. Render only when signed in.
 */
export default function InviteCard() {
  const [r, setR] = useState<Referral | null>(null);
  const [name, setName] = useState("");
  const [optIn, setOptIn] = useState(false);
  const [copied, setCopied] = useState(false);
  const [saved, setSaved] = useState("");
  const [err, setErr] = useState("");

  const load = () =>
    api.referral().then((x) => {
      setR(x);
      setName(x.display_name);
      setOptIn(x.leaderboard_opt_in);
    }).catch(() => setR(null));

  useEffect(() => {
    claimPendingReferral().finally(load);
  }, []);

  if (!r) return null;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(r.link);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {}
  };
  const save = async () => {
    setErr(""); setSaved("");
    try {
      await api.setProfile({ display_name: name, leaderboard_opt_in: optIn });
      setSaved("Saved.");
      load();
    } catch (e: any) {
      setErr(e.message || "could not save");
    }
  };

  return (
    <section className="card" style={{ marginBottom: 18 }}>
      <h3>Invite a device</h3>
      <p className="muted" style={{ fontSize: ".9rem", margin: "4px 0 10px" }}>
        Every phone or computer that joins through your link counts for you. The first {r.founding_cap.toLocaleString()} devices
        on the network are founding devices, for good.
        {r.founding && r.founding_rank > 0 && (
          <> <span className="pill" style={{ marginLeft: 6 }}>Founding device #{r.founding_rank}</span></>
        )}
      </p>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <input className="input" readOnly value={r.link} style={{ flex: 1, minWidth: 240 }} onFocus={(e) => e.currentTarget.select()} />
        <button className="btn btn-primary" onClick={copy}>{copied ? "Copied" : "Copy link"}</button>
      </div>
      <p className="muted" style={{ fontSize: ".85rem", margin: "10px 0 0" }}>
        {r.referred_users} {r.referred_users === 1 ? "person" : "people"} joined through your link, bringing {r.referred_devices}{" "}
        {r.referred_devices === 1 ? "device" : "devices"}. <Link href="/founders/">See the founders board.</Link>
      </p>
      <details style={{ marginTop: 12 }}>
        <summary className="muted" style={{ cursor: "pointer", fontSize: ".9rem" }}>Appear on the public founders board (optional)</summary>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", marginTop: 10 }}>
          <input className="input" placeholder="Display name (as shown publicly)" value={name} maxLength={40}
            onChange={(e) => setName(e.target.value)} style={{ flex: 1, minWidth: 200 }} />
          <label className="muted" style={{ fontSize: ".9rem", display: "flex", gap: 6, alignItems: "center" }}>
            <input type="checkbox" checked={optIn} onChange={(e) => setOptIn(e.target.checked)} /> show me
          </label>
          <button className="btn btn-ghost" onClick={save}>Save</button>
        </div>
        <p className="muted" style={{ fontSize: ".8rem", margin: "6px 0 0" }}>
          Only the display name and device counts are shown. Your email and devices stay private.
        </p>
        {saved && <p className="ok" style={{ margin: "6px 0 0" }}>{saved}</p>}
        {err && <p className="err" style={{ margin: "6px 0 0" }}>{err}</p>}
      </details>
    </section>
  );
}
