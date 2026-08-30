import type { Metadata } from "next";
import { REPO, WHITEPAPER, LICENSE } from "../lib/api";

export const metadata: Metadata = {
  title: "Technology — Ayni",
  description:
    "The architecture behind Ayni: a Go coordinator, a portable Rust provider core, per-job end-to-end encryption, and hardware attestation.",
};

export default function Technology() {
  return (
    <section className="wrap">
      <p className="kicker">Technology</p>
      <h1>Built in the open, clean-room.</h1>
      <p className="lead" style={{ maxWidth: "54ch" }}>
        A private-inference network you can read end to end. Generic primitives
        only — WebSockets, libsodium, llama.cpp, standard platform attestation.
      </p>

      <div className="cta" style={{ marginTop: 24 }}>
        <a href={WHITEPAPER} className="btn btn-primary">
          Read the white paper
        </a>
        <a href={REPO} className="btn btn-ghost">
          GitHub repo
        </a>
        <a href={LICENSE} className="btn btn-ghost">
          Apache 2.0 license
        </a>
      </div>

      <div className="prose" style={{ marginTop: 40 }}>
        <h2>The shape of it</h2>
        <p>
          A consumer calls an OpenAI-compatible API over TLS. The{" "}
          <strong>coordinator</strong> (Go) authenticates the key, meters usage,
          and picks a provider by capability, trust tier, load, and thermal/battery
          headroom. It generates a fresh X25519 keypair <em>per job</em>, seals the
          request to the provider&rsquo;s static key, and relays sealed token chunks
          back — decrypting only at the relay boundary, never persisting plaintext.
        </p>
        <p>
          The <strong>provider core</strong> (Rust, portable) runs on macOS, Linux,
          Windows, and Android. It embeds <code>llama.cpp</code>, pulls models from a
          signed registry (per-file SHA-256, Ed25519 manifest signature), verifies
          them before advertising, and streams sealed tokens. On Android it&rsquo;s a
          foreground service with a policy engine and JNI bindings to the same core.
        </p>

        <h2>Encryption envelope</h2>
        <ul>
          <li>Consumer → coordinator: TLS (optionally NaCl-sealed to a published coordinator key).</li>
          <li>
            Coordinator → provider: mandatory <code>crypto_box</code> (X25519 +
            XSalsa20-Poly1305) with a fresh ephemeral keypair per job.
          </li>
          <li>Provider → coordinator: chunks sealed back to that ephemeral public key.</li>
          <li>No component writes prompt or response content to disk or logs. A CI check enforces it.</li>
        </ul>

        <h2>Trust tiers</h2>
        <ul>
          <li>
            <strong>community</strong> — signed binary + network crypto. No
            owner-resistance claim.
          </li>
          <li>
            <strong>device_attested</strong> — a hardware-backed key in Android
            StrongBox / a TEE / a TPM, plus verified boot, with its attestation
            chain verified to the platform vendor&rsquo;s root.
          </li>
          <li>
            <strong>confidential</strong> — CPU TEE + confidential GPU with remote
            attestation; the per-job key is released only after both verify. On the
            roadmap.
          </li>
        </ul>

        <h2>Android hardware attestation</h2>
        <p>
          The app generates an EC P-256 key in StrongBox (or the TEE) with an
          attestation challenge and signs the provider&rsquo;s X25519 identity with
          it. The coordinator verifies the certificate chain to Google&rsquo;s
          hardware-attestation root, checks the key-attestation extension
          (security level, verified boot state, bootloader lock), and checks the
          binding signature and its freshness. Only then is the node{" "}
          <code>device_attested</code>.
        </p>

        <h2>License</h2>
        <p>
          Apache 2.0 — permissive, with an explicit patent grant. Use it
          commercially, fork it, embed it. We only ask that you keep the notices.
        </p>

        <h2>Run it yourself</h2>
        <pre
          style={{
            background: "#0f1b3d",
            color: "#dbe4ff",
            padding: "18px 20px",
            borderRadius: 12,
            overflowX: "auto",
            fontSize: "0.92rem",
          }}
        >
{`git clone ${REPO}
cd shared-compute
make dev        # coordinator + a local provider
make e2e        # encrypted round-trip test`}
        </pre>
      </div>
    </section>
  );
}
