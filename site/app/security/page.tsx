import type { Metadata } from "next";
import Link from "next/link";
import { REPO, WHITEPAPER } from "../lib/api";

export const metadata: Metadata = {
  title: "Security — Ayni",
  description:
    "Ayni's marketplace threat model, offline adversarial harness, signed Workload Manifest v1, and the fail-closed Ayni Council — built and tested in the open.",
};

const sec = `${REPO}/tree/main/security`;

export default function Security() {
  return (
    <>
      <section className="hero wrap">
        <p className="kicker">Trust, in the open</p>
        <h1>Security</h1>
        <p className="lead" style={{ maxWidth: "56ch" }}>
          Once strangers can buy compute and devices can earn money, the failure
          modes change. Ayni publishes its threat model, an adversarial test
          harness that runs on every commit, cryptographic per-job authorization,
          and a fail-closed governance council — code first, claims second.
        </p>
        <div className="cta">
          <a href={sec} className="btn btn-primary">
            security/ in the repo
          </a>
          <a href={WHITEPAPER} className="btn btn-ghost">
            White paper §7–8
          </a>
        </div>
      </section>

      <section className="wrap">
        <p className="kicker">Threat model</p>
        <h2>What we design against</h2>
        <div className="grid c2">
          <div className="card">
            <h3>A compromised coordinator</h3>
            <p className="muted">
              Cannot read the provider hop (sealed per job with a fresh key).
              Cannot turn devices into a botnet: the node independently verifies
              what it is asked to run and enforces limits the coordinator cannot
              raise.
            </p>
          </div>
          <div className="card">
            <h3>A malicious renter</h3>
            <p className="muted">
              No arbitrary code, no arbitrary network egress. Only allow-listed
              runtimes and models, and only the <code>inference</code> operation,
              with <code>network_policy: NONE</code>.
            </p>
          </div>
          <div className="card">
            <h3>A malicious provider</h3>
            <p className="muted">
              Cannot create earnings without verified compute. One payable event
              per <code>(job_id, device)</code>; the coordinator counts completion
              tokens itself and overwrites the provider&rsquo;s self-report before
              billing.
            </p>
          </div>
          <div className="card">
            <h3>Replays, races, retries</h3>
            <p className="muted">
              Single-use nonces; idempotent settlement and debits; a reference
              ledger that asserts credits are conserved and payouts never exceed
              accrued earnings, under concurrency.
            </p>
          </div>
        </div>
        <p className="note" style={{ marginTop: 12 }}>
          Full delta:{" "}
          <a href={`${sec}/threat-model/SECURITY_V0.1.md`}>SECURITY_V0.1.md</a>.
          External penetration test and audit precede general availability.
        </p>
      </section>

      <section className="wrap">
        <p className="kicker">Adversarial CI</p>
        <h2>The cyber harness</h2>
        <div className="prose">
          <p>
            An offline suite of hostile cases — tampered runtime and model hashes,
            widened network policy, shell operations, expired leases, unbounded
            resource limits, wrong-device binding, signed-payload replay,
            post-signature tamper, forged keys — every one of which{" "}
            <strong>must be rejected</strong>. It runs on every push in a dedicated
            job and sends no traffic anywhere.
          </p>
          <pre
            style={{
              background: "#0f1b3d",
              color: "#dbe4ff",
              padding: "16px 18px",
              borderRadius: 12,
              overflowX: "auto",
              fontSize: "0.9rem",
            }}
          >
{`cd security
python -m pip install -r requirements.txt
python -m unittest discover -s tests
python -m ayni_security.harness   # every hostile case: PASS`}
          </pre>
        </div>
      </section>

      <section className="wrap">
        <p className="kicker">Per-job authorization</p>
        <h2>Signed Workload Manifest v1</h2>
        <div className="prose">
          <p>
            Every dispatched job can carry an Ed25519-signed manifest that binds it
            to one device key, one runtime, one model, one resource envelope, and
            an expiry. The provider node verifies the signature against the
            coordinator&rsquo;s published key and checks every resource limit
            against <strong>immutable local ceilings the coordinator cannot
            raise</strong> — refusing to run on any mismatch. A Go↔Rust
            known-answer test keeps the two implementations byte-identical.
          </p>
          <p>
            <Link href="/technology/">How the encryption works →</Link>{" "}
            <a href={`${REPO}/blob/main/docs/WORKLOAD-MANIFEST.md`}>
              Manifest spec →
            </a>
          </p>
        </div>
      </section>

      <section className="wrap">
        <p className="kicker">Governance</p>
        <h2>The Ayni Council</h2>
        <p className="lead" style={{ maxWidth: "54ch" }}>
          Ten specialist seats, provider-diverse, fail-closed. Models reason and
          vote; a deterministic policy decides; every decision is hash-linked and
          publicly verifiable. It never moves money, deploys code, or commands
          nodes.
        </p>
        <div className="cta" style={{ marginTop: 16 }}>
          <Link href="/council/" className="btn btn-primary">
            Open the Council observatory
          </Link>
          <a href={`${sec}/constitution/AYNI_CONSTITUTION.md`} className="btn btn-ghost">
            Constitution
          </a>
        </div>
      </section>
    </>
  );
}
