"use client";
import { useState } from "react";
import { joinWaitlist, type Interest } from "./lib/api";

export function WaitlistForm({ defaultInterest = "both" as Interest }) {
  const [email, setEmail] = useState("");
  const [interest, setInterest] = useState<Interest>(defaultInterest);
  const [note, setNote] = useState("");
  const [hp, setHp] = useState("");
  const [state, setState] = useState<"idle" | "sending" | "done" | "error">("idle");
  const [msg, setMsg] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setState("sending");
    setMsg("");
    try {
      await joinWaitlist({ email, interest, note: note || undefined, hp });
      setState("done");
    } catch (err) {
      setState("error");
      setMsg(err instanceof Error ? err.message : "Something went wrong.");
    }
  }

  if (state === "done") {
    return (
      <p className="ok">
        You&rsquo;re on the list. We&rsquo;ll email {email} when your track opens.
      </p>
    );
  }

  return (
    <form className="form" onSubmit={submit}>
      <input
        className="hp"
        tabIndex={-1}
        autoComplete="off"
        value={hp}
        onChange={(e) => setHp(e.target.value)}
        placeholder="Leave this empty"
      />
      <label>
        Email
        <input
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="you@example.com"
        />
      </label>
      <label>
        I want to
        <select value={interest} onChange={(e) => setInterest(e.target.value as Interest)}>
          <option value="share">Share my idle compute and get paid</option>
          <option value="rent">Rent private inference</option>
          <option value="both">Both</option>
          <option value="initiatives">Bring an initiative to the community</option>
        </select>
      </label>
      <label>
        Anything you want us to know? <span className="note">(optional)</span>
        <textarea rows={3} value={note} onChange={(e) => setNote(e.target.value)} />
      </label>
      <button className="btn btn-primary" disabled={state === "sending"}>
        {state === "sending" ? "Sending…" : "Join the waitlist"}
      </button>
      {state === "error" && <p className="err">{msg}</p>}
      <p className="note">
        We store only your email, choice, and note — nothing else. No spam; one
        message when your track opens.
      </p>
    </form>
  );
}
