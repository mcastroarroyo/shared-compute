"use client";
import { useState } from "react";
import { submitInitiative } from "../lib/api";

export function InitiativeForm() {
  const [f, setF] = useState({ name: "", email: "", title: "", summary: "", link: "" });
  const [hp, setHp] = useState("");
  const [state, setState] = useState<"idle" | "sending" | "done" | "error">("idle");
  const [msg, setMsg] = useState("");
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    setF({ ...f, [k]: e.target.value });

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setState("sending");
    setMsg("");
    try {
      await submitInitiative({ ...f, link: f.link || undefined, hp });
      setState("done");
    } catch (err) {
      setState("error");
      setMsg(err instanceof Error ? err.message : "Something went wrong.");
    }
  }

  if (state === "done") {
    return (
      <p className="ok">
        Thank you — your initiative is in the queue. We&rsquo;ll be in touch at {f.email}.
      </p>
    );
  }

  return (
    <form className="form" onSubmit={submit}>
      <input className="hp" tabIndex={-1} autoComplete="off" value={hp}
        onChange={(e) => setHp(e.target.value)} placeholder="Leave empty" />
      <div className="row">
        <label style={{ flex: 1 }}>
          Your name
          <input required value={f.name} onChange={set("name")} />
        </label>
        <label style={{ flex: 1 }}>
          Email
          <input type="email" required value={f.email} onChange={set("email")} />
        </label>
      </div>
      <label>
        Initiative title
        <input required value={f.title} onChange={set("title")}
          placeholder="e.g. Free inference for community clinics" />
      </label>
      <label>
        Summary
        <textarea required rows={5} value={f.summary} onChange={set("summary")}
          placeholder="What is it, who does it help, and how does it share AI value?" />
      </label>
      <label>
        Link <span className="note">(optional — doc, repo, deck)</span>
        <input value={f.link} onChange={set("link")} placeholder="https://" />
      </label>
      <button className="btn btn-primary" disabled={state === "sending"}>
        {state === "sending" ? "Sending…" : "Submit initiative"}
      </button>
      {state === "error" && <p className="err">{msg}</p>}
      <p className="note">
        We store what you enter here to review the proposal. Nothing else.
      </p>
    </form>
  );
}
