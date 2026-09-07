"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { api, API, type Me, type MyDevice } from "../lib/api";

const WS = API.replace(/^http/, "ws") + "/ws/provider";
const REPO = "https://github.com/mcastroarroyo/shared-compute";
const PLAY_TEST_URL = "https://play.google.com/apps/internaltest/4701333072640107331";
const RELEASES = `${REPO}/releases/tag/provider-latest`;

type Kind = "phone" | "mac" | "linux" | "windows";

const KINDS: { id: Kind; title: string; sub: string }[] = [
  { id: "phone", title: "Android phone", sub: "Pair with a 6-letter code" },
  { id: "mac", title: "Mac", sub: "One command in Terminal" },
  { id: "linux", title: "Linux", sub: "One command in a shell" },
  { id: "windows", title: "Windows", sub: "Download and run" },
];

function fmtTps(n: number) {
  return n >= 100 ? Math.round(n).toString() : n.toFixed(1);
}
function kindLabel(d: MyDevice) {
  if (d.kind === "phone") return "Phone";
  if (d.kind === "pc") return d.platform === "macos" ? "Mac" : d.platform === "windows" ? "Windows PC" : "PC / laptop";
  return "Device";
}

export default function Share() {
  const [me, setMe] = useState<Me | null>(null);
  const [kind, setKind] = useState<Kind>("phone");
  const [copied, setCopied] = useState("");
  const [showToken, setShowToken] = useState(false);

  // Pairing code (phone)
  const [code, setCode] = useState<{ code: string; expires_at: string } | null>(null);
  const [left, setLeft] = useState(0);
  const [codeErr, setCodeErr] = useState("");

  // Live device list
  const [devs, setDevs] = useState<MyDevice[] | null>(null);
  const baselineRef = useRef<Set<string> | null>(null);
  const [justJoined, setJustJoined] = useState<MyDevice | null>(null);

  useEffect(() => {
    api.me().then(setMe).catch(() => (location.href = "/"));
  }, []);

  const mintCode = useCallback(async () => {
    setCodeErr("");
    try {
      const c = await api.pairCode();
      setCode(c);
    } catch (e: any) {
      setCodeErr(e.message || "could not create a code");
    }
  }, []);

  useEffect(() => {
    if (kind === "phone" && !code) mintCode();
  }, [kind, code, mintCode]);

  // countdown + auto-renew
  useEffect(() => {
    if (!code) return;
    const tick = () => {
      const s = Math.max(0, Math.round((new Date(code.expires_at).getTime() - Date.now()) / 1000));
      setLeft(s);
      if (s === 0) setCode(null);
    };
    tick();
    const t = setInterval(tick, 1000);
    return () => clearInterval(t);
  }, [code]);

  // poll devices every 4 s; celebrate a newly-online device
  useEffect(() => {
    let stop = false;
    const poll = async () => {
      try {
        const r = await api.devices();
        if (stop) return;
        const online = new Set(r.devices.filter((d) => d.online).map((d) => d.static_pk));
        if (baselineRef.current) {
          const fresh = r.devices.find((d) => d.online && !baselineRef.current!.has(d.static_pk));
          if (fresh) setJustJoined(fresh);
        }
        baselineRef.current = online;
        setDevs(r.devices);
      } catch {
        /* keep last */
      }
    };
    poll();
    const t = setInterval(poll, 4000);
    return () => {
      stop = true;
      clearInterval(t);
    };
  }, []);

  if (!me) return <p className="muted">Loading…</p>;
  const tok = me.provider_token || "";
  const oneLiner = `curl -fsSL ${API}/install/provider.sh | SC_REGISTRATION_TOKEN=${tok} bash`;
  const fromSource = `git clone ${REPO} && cd shared-compute
SC_COORDINATOR_URL=${WS} \\
SC_REGISTRATION_TOKEN=${tok} \\
./infra/run-provider.sh`;
  const winCmd = `$env:SC_REGISTRATION_TOKEN="${tok}"
$env:SC_COORDINATOR_URL="${WS}"
$env:SC_MODEL="qwen2.5-0.5b-instruct-q4_k_m"
$env:SC_MODEL_PATH="$HOME\\ayni\\qwen2.5-0.5b-instruct-q4_k_m.gguf"
.\\provider-daemon-windows-x64.exe`;

  const copy = (label: string, text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(label);
      setTimeout(() => setCopied(""), 1500);
    });
  };
  const mm = String(Math.floor(left / 60));
  const ss = String(left % 60).padStart(2, "0");
  const onlineCount = devs?.filter((d) => d.online).length ?? 0;

  return (
    <>
      <Link href="/dashboard/" className="muted">
        ← Dashboard
      </Link>
      <h1 style={{ marginTop: 12 }}>Add a device</h1>
      <p className="muted" style={{ margin: "6px 0 18px" }}>
        Your device joins the network as a provider and earns 70% of every job it completes.
        Earnings accrue to <strong>{me.user.email}</strong>. You stay in control: stop any time.
      </p>

      {justJoined && (
        <div className="card" style={{ marginBottom: 16, borderColor: "#1f9e94", background: "#f0faf8" }}>
          <p className="ok" style={{ margin: 0 }}>
            ✓ Connected: {kindLabel(justJoined)}
            {justJoined.platform ? ` · ${justJoined.platform}/${justJoined.arch}` : ""}
            {justJoined.tps > 0 ? ` · ${fmtTps(justJoined.tps)} tok/s` : ""}
            {justJoined.trust_tier ? ` · ${justJoined.trust_tier}` : ""}
          </p>
          <p className="muted" style={{ margin: "6px 0 0", fontSize: ".9rem" }}>
            It is now serving jobs while its rules allow. See it under Earnings, or add another device below.
          </p>
        </div>
      )}

      <p style={{ fontWeight: 600, margin: "0 0 8px" }}>1 · What are you adding?</p>
      <div className="grid c4" style={{ marginBottom: 18 }}>
        {KINDS.map((k) => (
          <button
            key={k.id}
            type="button"
            className="card"
            onClick={() => setKind(k.id)}
            style={{
              textAlign: "left",
              cursor: "pointer",
              borderColor: kind === k.id ? "#1b2a63" : undefined,
              boxShadow: kind === k.id ? "0 0 0 2px #1b2a63 inset" : undefined,
            }}
          >
            <h3 style={{ margin: 0 }}>{k.title}</h3>
            <p className="muted" style={{ margin: "4px 0 0", fontSize: ".86rem" }}>{k.sub}</p>
          </button>
        ))}
      </div>

      <p style={{ fontWeight: 600, margin: "0 0 8px" }}>2 · Follow the steps</p>

      {kind === "phone" && (
        <section className="card" style={{ marginBottom: 16 }}>
          <ol style={{ margin: "0 0 14px 18px", padding: 0, lineHeight: 1.7 }}>
            <li>
              Install <strong>Ayni</strong> from Google Play:{" "}
              <a href={PLAY_TEST_URL} target="_blank" rel="noreferrer">open the tester link</a> on the phone and tap Install.
            </li>
            <li>Open the app and type this code:</li>
          </ol>
          <div style={{ display: "flex", alignItems: "center", gap: 16, flexWrap: "wrap" }}>
            <div
              className="mono"
              style={{
                fontSize: "2.4rem",
                letterSpacing: ".18em",
                fontWeight: 800,
                padding: "10px 18px",
                border: "2px dashed var(--line)",
                borderRadius: 12,
                background: "#fff",
                minWidth: 220,
                textAlign: "center",
              }}
            >
              {code ? `${code.code.slice(0, 3)} ${code.code.slice(3)}` : "· · ·"}
            </div>
            <div>
              <p className="muted" style={{ margin: 0, fontSize: ".9rem" }}>
                {code ? `Expires in ${mm}:${ss}` : "Creating a code…"}
              </p>
              <button className="btn btn-ghost" style={{ marginTop: 8, padding: "4px 12px", fontSize: ".85rem" }} onClick={() => setCode(null)}>
                New code
              </button>
            </div>
          </div>
          {codeErr && <p className="err">{codeErr}</p>}
          <ol start={3} style={{ margin: "14px 0 0 18px", padding: 0, lineHeight: 1.7 }}>
            <li>Tap <strong>Pair</strong>. The phone connects and starts sharing while it is charging and on Wi-Fi.</li>
          </ol>
          <p className="muted" style={{ marginTop: 14, fontSize: ".86rem" }}>
            Prefer the long way?{" "}
            <button className="btn btn-ghost" style={{ padding: "2px 10px", fontSize: ".82rem" }} onClick={() => setShowToken((v) => !v)}>
              {showToken ? "Hide token" : "Show registration token"}
            </button>
          </p>
          {showToken && (
            <>
              <pre className="code" style={{ overflowWrap: "anywhere", whiteSpace: "pre-wrap" }}>{tok}</pre>
              <button className="btn btn-ghost" onClick={() => copy("token", tok)}>
                {copied === "token" ? "Copied" : "Copy token"}
              </button>
              <p className="muted" style={{ fontSize: ".82rem" }}>
                In the app: Settings → Show → paste into Registration token → Save settings → Start.
              </p>
            </>
          )}
        </section>
      )}

      {(kind === "mac" || kind === "linux") && (
        <section className="card" style={{ marginBottom: 16 }}>
          <ol style={{ margin: "0 0 12px 18px", padding: 0, lineHeight: 1.7 }}>
            <li>Open {kind === "mac" ? "Terminal (Spotlight → “Terminal”)" : "a shell"}.</li>
            <li>Paste this and press Enter. It downloads a small provider and a 0.5 GB model, then connects:</li>
          </ol>
          <pre className="cmd">{oneLiner}</pre>
          <button className="btn btn-teal" style={{ marginTop: 10 }} onClick={() => copy("one", oneLiner)}>
            {copied === "one" ? "Copied" : "Copy command"}
          </button>
          <ol start={3} style={{ margin: "14px 0 0 18px", padding: 0, lineHeight: 1.7 }}>
            <li>Wait for <span className="mono">registered</span>. Leave the window open; this page will confirm the device below.</li>
            <li>To stop, press Ctrl-C. To share whenever the computer is on, run the same command from a login item or a user service.</li>
          </ol>
          <details style={{ marginTop: 12 }}>
            <summary className="muted" style={{ cursor: "pointer", fontSize: ".9rem" }}>Build from source instead (needs Rust + CMake)</summary>
            <pre className="cmd" style={{ marginTop: 8 }}>{fromSource}</pre>
            <button className="btn btn-ghost" style={{ marginTop: 8 }} onClick={() => copy("src", fromSource)}>
              {copied === "src" ? "Copied" : "Copy"}
            </button>
          </details>
        </section>
      )}

      {kind === "windows" && (
        <section className="card" style={{ marginBottom: 16 }}>
          <ol style={{ margin: "0 0 12px 18px", padding: 0, lineHeight: 1.7 }}>
            <li>
              Download <span className="mono">provider-daemon-windows-x64.exe</span> from the{" "}
              <a href={RELEASES} target="_blank" rel="noreferrer">latest release</a> and the model file{" "}
              <a href="https://models.ayni-ai.com/qwen2.5-0.5b-instruct-q4_k_m/qwen2.5-0.5b-instruct-q4_k_m.gguf" target="_blank" rel="noreferrer">qwen2.5-0.5b-instruct-q4_k_m.gguf</a>{" "}
              into a folder named <span className="mono">ayni</span> in your home directory.
            </li>
            <li>Open PowerShell in that folder and run:</li>
          </ol>
          <pre className="cmd">{winCmd}</pre>
          <button className="btn btn-teal" style={{ marginTop: 10 }} onClick={() => copy("win", winCmd)}>
            {copied === "win" ? "Copied" : "Copy commands"}
          </button>
          <p className="muted" style={{ marginTop: 12, fontSize: ".86rem" }}>
            A one-click Windows installer is on the roadmap. The steps above are the manual path for now.
          </p>
        </section>
      )}

      <p style={{ fontWeight: 600, margin: "18px 0 8px" }}>
        3 · Your devices{" "}
        <span className="muted" style={{ fontWeight: 400 }}>
          {devs ? `· ${onlineCount} online of ${devs.length}` : ""}
        </span>
      </p>
      <section className="card">
        {!devs && <p className="muted" style={{ margin: 0 }}>Checking…</p>}
        {devs && devs.length === 0 && (
          <p className="muted" style={{ margin: 0 }}>
            <span className="mono">●</span> Waiting for your first device… this updates on its own.
          </p>
        )}
        {devs && devs.length > 0 && (
          <div>
            {devs.map((d) => (
              <div key={d.static_pk} className="kv" style={{ alignItems: "center" }}>
                <span>
                  <span style={{ color: d.online ? "#0f6b63" : "#9aa3b5", marginRight: 8 }}>●</span>
                  <strong>{kindLabel(d)}</strong>
                  {d.platform ? <span className="muted"> · {d.platform}/{d.arch}</span> : null}
                  {d.class ? <span className="muted"> · {d.class}</span> : null}
                  {d.trust_tier ? <span className="pill" style={{ marginLeft: 8 }}>{d.trust_tier}</span> : null}
                </span>
                <span className="muted" style={{ textAlign: "right", fontSize: ".88rem" }}>
                  {d.online ? `online · ${d.connected_for}` : "offline"}
                  {d.tps > 0 ? ` · ${fmtTps(d.tps)} tok/s` : ""}
                  {d.jobs_unpaid > 0 ? ` · ${d.jobs_unpaid} jobs` : ""}
                </span>
              </div>
            ))}
            {onlineCount === 0 && (
              <p className="muted" style={{ margin: "10px 0 0", fontSize: ".88rem" }}>
                Nothing online right now. Start the app or the command above and this list updates within seconds.
              </p>
            )}
          </div>
        )}
      </section>

      <p className="muted" style={{ marginTop: 20, fontSize: ".85rem" }}>
        Pairing codes expire in 10 minutes and work once. The registration token is tied to your account; anyone
        who has it can attach a machine as you, so keep it private.
      </p>
    </>
  );
}
