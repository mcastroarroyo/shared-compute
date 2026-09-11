import type { Metadata } from "next";
import Link from "next/link";
import { WaitlistForm } from "../waitlist-form";

export const metadata: Metadata = {
  title: "Run a workload on Ayni",
  description:
    "Submit a batch job, get one quote and a completion estimate, and Ayni runs it across benchmarked community devices — end-to-end encrypted, never logged.",
};

const pre = {
  background: "#0f1b3d",
  color: "#dbe4ff",
  padding: "18px 20px",
  borderRadius: 12,
  overflowX: "auto" as const,
  fontSize: "0.92rem",
};

export default function Rent() {
  return (
    <>
      <section className="hero wrap">
        <p className="kicker">Use the network</p>
        <h1>Run a workload on Ayni.</h1>
        <p className="lead" style={{ maxWidth: "50ch" }}>
          Submit a batch of items, get one price and a completion estimate, and Ayni
          fans it out across benchmarked community devices. Every request is
          end-to-end encrypted, runs on hardware you can require to be attested, and
          is never logged in plaintext. Single requests still work the OpenAI way.
        </p>
        <div className="cta">
          <a href="https://app.ayni-ai.com/run/" className="btn btn-teal">
            Run a workload now
          </a>
          <Link href="/waitlist/?for=rent" className="btn btn-ghost">
            Join the consumer waitlist
          </Link>
          <Link href="/technology/" className="btn btn-ghost">
            How it works
          </Link>
        </div>
      </section>

      <section className="wrap">
        <h2>Quote a workload</h2>
        <div className="prose">
          <p>
            <code>POST /v1/workloads</code> returns a price and an ETA built from the
            live supply — how many eligible devices are online and what they
            actually sustain. Accepting it runs the batch and charges you once, at
            the quoted price.
          </p>
          <pre style={pre}>
{`curl https://api.ayni-ai.com/v1/workloads \\
  -H "Authorization: Bearer $AYNI_KEY" -H "Content-Type: application/json" \\
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m",
       "items":[{"messages":[{"role":"user","content":"Summarize: ..."}],"max_tokens":160},
                {"messages":[{"role":"user","content":"Summarize: ..."}],"max_tokens":160}]}'
# -> { "id":"wl_...", "estimate":{"eta_seconds":...,"eligible_nodes":3},
#      "price":{"total_usd":0.42,"breakdown_usd":{...}}, "accept_url":"/v1/workloads/wl_.../accept" }

curl -X POST https://api.ayni-ai.com/v1/workloads/wl_.../accept \\
  -H "Authorization: Bearer $AYNI_KEY"
# -> { "object":"workload.result", "items":[ ...in submission order... ], "charged_usd":0.42 }`}
          </pre>
          <p>
            Prefer to place the batch yourself? <code>POST /v1/batch</code> takes the
            same items, fans them out immediately, and meters pay-as-you-go.
          </p>
          <p>
            Not in a hurry? Add <code>&quot;spot&quot;: true</code>. Spot workloads run on
            whatever capacity on-demand traffic leaves free — priced at 60% of on-demand,
            with a wider completion-time band, and Ayni may interrupt and requeue them.
          </p>
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
          <pre style={pre}>
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
            Prepaid credits, billed per million tokens by model class, the same unit
            every hosted model uses. Chat and batch are metered at the rate card; a
            quoted workload adds coordination, a failure buffer and payment cost on
            top of the provider&rsquo;s share, which lands about 20% above it. Launch
            rates are placeholders and will be set with the community before general
            availability. Per 1M tokens, input / output:
          </p>
          <ul>
            <li>Micro (≤3B, live today): $0.02 / $0.08 · spot 60% of that</li>
            <li>Small (3–8B): $0.05 / $0.20</li>
            <li>Medium (12–30B quantized): $0.15 / $0.60</li>
            <li>
              <code>device_attested</code> &times;1.4 · <code>confidential</code>{" "}
              &times;3
            </li>
          </ul>
          <p>
            Compare on that number, not on a per-item price. A 220-token log line
            with a one-word answer is about 226 tokens, so one million of them cost
            about $6 on the micro class: $0.027 per million tokens. For reference,
            hosted open models of 1–8B list at roughly $0.02–0.10 per million
            tokens, the commercial nano tier at $0.10–0.40, and mid-tier cloud
            models at $0.30–2.50 (list prices, approximate, batch tiers about half).
          </p>
          <p className="note">
            70% of what you pay goes to the provider that served you; the rest funds
            the coordinator, the community treasury, and the AI-stakeholder
            allocation.
          </p>
        </div>
      </section>

      <section className="wrap">
        <h2>What runs well on the micro class today</h2>
        <div className="prose">
          <p>
            The live model is 0.5B. It is fast and cheap, and it does not reason. The
            workloads that fit are mechanical, short, constrained and large:
          </p>
          <ul>
            <li>
              <b>Normalize and extract.</b> Raw audit, syslog or CloudTrail events into
              one JSON schema: actor, action, target, source, time. Severity by
              deterministic rules on top.
            </li>
            <li>
              <b>Closed-set labelling.</b> Tickets, alerts or documents against a fixed
              list of ten to twenty categories with worked examples.
            </li>
            <li>
              <b>Embeddings</b> for semantic search (on the roadmap: one endpoint).
            </li>
            <li>
              <b>Integrity by redundancy.</b> The same item on three devices in three
              countries with a majority vote, in one call.
            </li>
          </ul>
          <p className="note">
            Judgment (&ldquo;is this an incident?&rdquo;) belongs to the small and
            medium classes on laptops and GPUs. Every price view returns the price
            per million tokens, and a built-in self-test scores the model on
            labelled lines before any money is spent.
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
