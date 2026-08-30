"use client";
import { useEffect, useRef, useState } from "react";
import { api, chatStream } from "@/lib/api";

type Msg = { role: "user" | "assistant"; content: string };

export default function Playground() {
  const [models, setModels] = useState<string[]>([]);
  const [model, setModel] = useState("");
  const [input, setInput] = useState("");
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const abort = useRef<AbortController | null>(null);
  const bottom = useRef<HTMLDivElement>(null);

  useEffect(() => {
    api
      .models()
      .then((r) => {
        const ids = (r.data || []).map((m: any) => m.id);
        setModels(ids);
        if (ids[0]) setModel(ids[0]);
      })
      .catch((e) => setErr(e.message));
  }, []);

  useEffect(() => {
    bottom.current?.scrollIntoView({ behavior: "smooth" });
  }, [msgs]);

  async function send() {
    if (!input.trim() || busy) return;
    setErr("");
    const next: Msg[] = [...msgs, { role: "user", content: input.trim() }];
    setMsgs([...next, { role: "assistant", content: "" }]);
    setInput("");
    setBusy(true);
    abort.current = new AbortController();
    try {
      await chatStream(
        model,
        next,
        (d) =>
          setMsgs((m) => {
            const copy = m.slice();
            copy[copy.length - 1] = {
              role: "assistant",
              content: copy[copy.length - 1].content + d,
            };
            return copy;
          }),
        abort.current.signal,
      );
    } catch (e: any) {
      if (e.name !== "AbortError") setErr(e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h1>Playground</h1>
      {err && <div className="err">{err}</div>}
      <div className="row" style={{ marginBottom: 10 }}>
        <select value={model} onChange={(e) => setModel(e.target.value)}>
          {models.length === 0 && <option>no models</option>}
          {models.map((m) => (
            <option key={m}>{m}</option>
          ))}
        </select>
        <button className="secondary" onClick={() => setMsgs([])}>
          Clear
        </button>
      </div>

      <div className="panel" style={{ minHeight: 260 }}>
        {msgs.length === 0 && <span className="muted">Ask something…</span>}
        {msgs.map((m, i) => (
          <div key={i} className={"bubble " + m.role}>
            {m.content || (busy && i === msgs.length - 1 ? "…" : "")}
          </div>
        ))}
        <div ref={bottom} />
      </div>

      <div className="row" style={{ marginTop: 10, alignItems: "flex-end" }}>
        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send();
          }}
          placeholder="⌘/Ctrl + Enter to send"
        />
        {busy ? (
          <button className="secondary" onClick={() => abort.current?.abort()}>
            Stop
          </button>
        ) : (
          <button onClick={send} disabled={!model}>
            Send
          </button>
        )}
      </div>
    </>
  );
}
