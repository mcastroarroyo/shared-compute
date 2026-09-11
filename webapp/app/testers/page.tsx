"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, type Me, type FeedbackThread } from "../lib/api";
import InviteCard from "../invite-card";

const PLAY_TEST_URL = "https://play.google.com/apps/internaltest/4701333072640107331";
// Community channel: GitHub Discussions on the public repo (testers already sign in with GitHub).
const CHAT_URL = process.env.NEXT_PUBLIC_TESTER_CHAT_URL || "https://github.com/mcastroarroyo/shared-compute/discussions";

export default function Testers() {
  const [me, setMe] = useState<Me | null>(null);
  const [kind, setKind] = useState("feedback");
  const [device, setDevice] = useState("");
  const [email, setEmail] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState("");
  const [err, setErr] = useState("");
  const [threads, setThreads] = useState<FeedbackThread[] | null>(null);
  const [replyText, setReplyText] = useState<Record<number, string>>({});

  const loadThreads = () => api.myFeedback().then((r) => setThreads(r.feedback)).catch(() => setThreads([]));
  const sendReply = async (id: number) => {
    const body = (replyText[id] || "").trim();
    if (!body) return;
    try {
      await api.replyFeedback(id, body);
      setReplyText({ ...replyText, [id]: "" });
      loadThreads();
    } catch (e: any) {
      setErr(e.message || "could not send");
    }
  };

  useEffect(() => {
    if (!me) return;
    loadThreads();
    const t = setInterval(loadThreads, 15_000);
    return () => clearInterval(t);
  }, [me]);

  useEffect(() => {
    api.me().then((m) => { setMe(m); setEmail(m.user.email); }).catch(() => setMe(null));
    try {
      const ua = navigator.userAgent;
      const guess = /Android/.test(ua) ? "Android phone" : /Mac/.test(ua) ? "Mac" : /Windows/.test(ua) ? "Windows PC" : /Linux/.test(ua) ? "Linux" : "";
      setDevice(guess);
    } catch { /* ignore */ }
  }, []);

  const send = async () => {
    setBusy(true); setErr(""); setDone("");
    try {
      await api.feedback({ kind, message: msg, email, device });
      setDone("Thank you. It went straight to the team; replies appear under Your messages below.");
      setMsg("");
      loadThreads();
    } catch (e: any) {
      setErr(e.message || "could not send");
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Link href={me ? "/dashboard/" : "/"} className="muted">← {me ? "Dashboard" : "Home"}</Link>
      <h1 style={{ marginTop: 12 }}>Test Ayni</h1>
      <p className="muted" style={{ margin: "6px 0 18px" }}>
        Ayni is in early testing. Ten minutes of your time, on a phone or a laptop, tells us more than a week of our own testing.
        Everything you report lands with the team the same minute.
      </p>

      <div className="grid c3" style={{ marginBottom: 18 }}>
        <div className="card">
          <h3>1 · Get in</h3>
          <p className="muted" style={{ fontSize: ".9rem" }}>
            {me ? "You are signed in." : "Sign in with GitHub or Google on the home page."} Android testers: ask for access below, then install from the{" "}
            <a href={PLAY_TEST_URL} target="_blank" rel="noreferrer">tester link</a>.
          </p>
        </div>
        <div className="card">
          <h3>2 · Try two things</h3>
          <p className="muted" style={{ fontSize: ".9rem" }}>
            <Link href="/share/">Add a device</Link> (phone: 6-letter code; Mac or Linux: one command), then{" "}
            <Link href="/run/">run a workload</Link> and watch the quote, Council verdict and result.
          </p>
        </div>
        <div className="card">
          <h3>3 · Tell us</h3>
          <p className="muted" style={{ fontSize: ".9rem" }}>
            What confused you, what broke, what you expected. Screenshots and the exact text on screen help most.
          </p>
        </div>
      </div>

      {me && <InviteCard />}

      {CHAT_URL && (
        <div className="card" style={{ marginBottom: 18 }}>
          <h3>Tester community</h3>
          <p className="muted" style={{ fontSize: ".9rem" }}>
            Release notes, questions and other testers: <a href={CHAT_URL} target="_blank" rel="noreferrer">GitHub Discussions</a>.
            Private things (your device, your account) go in the form below; the team answers there.
          </p>
        </div>
      )}

      <section className="card">
        <h3>Send feedback</h3>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", margin: "8px 0 12px" }}>
          {[["feedback", "Feedback"], ["bug", "Bug"], ["idea", "Idea"], ["tester_request", "Request Android access"]].map(([k, l]) => (
            <button key={k} type="button" className={"btn " + (kind === k ? "btn-primary" : "btn-ghost")} style={{ padding: "6px 12px", fontSize: ".9rem" }} onClick={() => setKind(k)}>
              {l}
            </button>
          ))}
        </div>
        <label className="muted" style={{ fontSize: ".85rem" }}>Email {me ? "(from your account)" : "(so we can reply; for Android access use your Google account email)"}</label>
        <input className="input" style={{ width: "100%", margin: "4px 0 10px" }} value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />
        <label className="muted" style={{ fontSize: ".85rem" }}>Device</label>
        <input className="input" style={{ width: "100%", margin: "4px 0 10px" }} value={device} onChange={(e) => setDevice(e.target.value)} placeholder="Pixel 9, Android 15 · MacBook M1 · …" />
        <label className="muted" style={{ fontSize: ".85rem" }}>
          {kind === "tester_request" ? "Anything we should know (optional, but say hello)" : "What happened, what you expected"}
        </label>
        <textarea className="input" rows={6} style={{ width: "100%", margin: "4px 0 12px" }} value={msg} onChange={(e) => setMsg(e.target.value)}
          placeholder={kind === "bug" ? "Steps, what you saw, what you expected. Paste any message the app showed." : kind === "tester_request" ? "I have a Pixel 8 and a Windows laptop, happy to test both." : "The pairing code was easy, but…"} />
        <input type="text" name="hp" tabIndex={-1} autoComplete="off" style={{ position: "absolute", left: -9999 }} aria-hidden />
        <button className="btn btn-teal" disabled={busy || msg.trim().length < 3} onClick={send}>
          {busy ? "Sending…" : kind === "tester_request" ? "Request access" : "Send"}
        </button>
        {done && <p className="ok" style={{ marginTop: 10 }}>{done}</p>}
        {err && <p className="err" style={{ marginTop: 10 }}>{err}</p>}
      </section>

      {me && (
        <section className="card" style={{ marginTop: 18 }}>
          <h3>Your messages</h3>
          {!threads && <p className="muted" style={{ margin: 0 }}>Loading…</p>}
          {threads && threads.length === 0 && (
            <p className="muted" style={{ margin: 0 }}>Nothing yet. When you send something above, it appears here with the team's reply.</p>
          )}
          {threads && threads.map((t) => (
            <div key={t.id} style={{ borderTop: "1px solid var(--line)", padding: "12px 0" }}>
              <p style={{ margin: 0 }}>
                <span className="pill">{t.kind}</span>
                <span className="muted" style={{ marginLeft: 8, fontSize: ".85rem" }}>{new Date(t.created_at).toLocaleString()}{t.device ? ` · ${t.device}` : ""}</span>
              </p>
              <p style={{ margin: "6px 0 0", whiteSpace: "pre-wrap" }}>{t.message}</p>
              {t.replies.map((r) => (
                <div key={r.id} style={{ margin: "10px 0 0 16px", padding: "10px 12px", borderRadius: 10, background: r.author === "ayni" ? "#eef6f5" : "#f4f4f7" }}>
                  <p className="muted" style={{ margin: 0, fontSize: ".8rem" }}>
                    <strong style={{ color: r.author === "ayni" ? "#0f6b63" : undefined }}>{r.author === "ayni" ? "Ayni team" : "You"}</strong> · {new Date(r.created_at).toLocaleString()}
                  </p>
                  <p style={{ margin: "4px 0 0", whiteSpace: "pre-wrap" }}>{r.body}</p>
                </div>
              ))}
              <div style={{ display: "flex", gap: 8, marginTop: 10, marginLeft: 16 }}>
                <input className="input" style={{ flex: 1 }} placeholder="Reply…" value={replyText[t.id] || ""} onChange={(e) => setReplyText({ ...replyText, [t.id]: e.target.value })} />
                <button className="btn btn-ghost" disabled={!(replyText[t.id] || "").trim()} onClick={() => sendReply(t.id)}>Send</button>
              </div>
            </div>
          ))}
        </section>
      )}

      <p className="muted" style={{ marginTop: 20, fontSize: ".85rem" }}>
        We store your message, email and device description to reply and to fix things. Nothing else. See the{" "}
        <a href="https://ayni-ai.com/privacy/">privacy policy</a>.
      </p>
    </>
  );
}
