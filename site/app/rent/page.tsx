import type { Metadata } from "next";
import Link from "next/link";
import { WaitlistForm } from "../waitlist-form";

export const metadata: Metadata = {
  title: "Rent compute from Ayni",
  description:
    "OpenAI-compatible, end-to-end-encrypted inference served by a community network. Choose your hardware trust level.",
};

export default function Rent() {
  return (
    <>
      <section className="hero wrap">
        <p className="kicker">Use the network</p>
        <h1>Rent compute from Ayni.</h1>
        <p className="lead" style={{ maxWidth: "48ch" }}>
          An OpenAI-compatible API served by community devices. Every request is
          end-to-end encrypted, runs on hardware you can require to be attested, and
          is never logged in plaintext.
        </p>
        <div className="cta">
          <Link href="/waitlist/?for=rent" className="btn btn-teal">
            Join the consumer waitlist
          </Link>
          <Link href="/technology/" className="btn btn-ghost">
            How it works
          </Link>
        </div>
      </section>

      <section className="wrap">
        <h2>Drop-in compatible</h2>
        <div className="prose">
          <p>
            Point any OpenAI SDK at the coordinator and pass your key. Streaming and
            blocking <code>/v1/chat/completions</code> and <code>/v1/models</code>{" "}
            work as you&rsquo;d expect.
          </p>
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
{`curl https://api.ayni-ai.com/v1/chat/completions \\
  -H "Authorization: Bearer $AYNI_KEY" \\
  -H "Content-Type: application/json" \\
  -H "X-Provider-Trust-Level: device_attested" \\
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m",
       "messages":[{"role":"user","content":"Hello"}]}'`}
          </pre>
        </div>
      </section>

      <section className="wrap">
        <h2>What you get</h2>
        <div className="grid c3">
          <div className="card">
            <h3>End-to-end encryption</h3>
            <p className="muted">
              The coordinator re-seals each job to the chosen provider with a fresh
              ephemeral key. Tokens come back sealed to that key.
            </p>
          </div>
          <div className="card">
            <h3>Choose your trust level</h3>
            <p className="muted">
              Send <code>X-Provider-Trust-Level: device_attested</code> and the
              request only routes to providers with a verified hardware key and
              locked, verified boot.
            </p>
          </div>
          <div className="card">
            <h3>No prompt logging</h3>
            <p className="muted">
              Enforced by a CI check across the whole codebase. Metering records
              token counts and timing — never content.
            </p>
          </div>
        </div>
      </section>

      <section className="wrap">
        <h2>Pricing</h2>
        <div className="prose">
          <p>
            Prepaid credits, billed per token by model class, with a multiplier for
            higher trust tiers. Launch rates are placeholders and will be set with
            the community before general availability. Indicative, per 1M tokens:
          </p>
          <ul>
            <li>Small models (≤8B): ~$0.05 in / ~$0.20 out</li>
            <li>Medium (12–30B quantized): ~$0.15 in / ~$0.60 out</li>
            <li>
              <code>device_attested</code> &times;1.4 · <code>confidential</code>{" "}
              &times;3
            </li>
          </ul>
          <p className="note">
            70% of what you pay goes to the provider that served you; the rest funds
            the coordinator, the community treasury, and the AI-stakeholder
            allocation.
          </p>
        </div>
      </section>

      <section className="wrap">
        <div className="callout">
          <h2>Join the consumer waitlist</h2>
          <p style={{ maxWidth: "42ch", margin: "8px auto 22px" }}>
            We&rsquo;re onboarding early users now. Tell us what you&rsquo;d build.
          </p>
          <div style={{ maxWidth: 460, margin: "0 auto", textAlign: "left" }}>
            <WaitlistForm defaultInterest="rent" />
          </div>
        </div>
      </section>
    </>
  );
}
