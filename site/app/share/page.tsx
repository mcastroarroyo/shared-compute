import type { Metadata } from "next";
import Link from "next/link";
import { WaitlistForm } from "../waitlist-form";
import { REPO } from "../lib/api";

export const metadata: Metadata = {
  title: "Share your compute — Ayni",
  description:
    "Sign up to share your idle, unused compute securely with the community and get paid.",
};

export default function Share() {
  return (
    <>
      <section className="hero wrap">
        <p className="kicker">Become a provider</p>
        <h1>Share your idle compute. Get paid.</h1>
        <p className="lead" style={{ maxWidth: "48ch" }}>
          Your phone, laptop, or GPU spends most of its life doing nothing. Lend
          that time to the community as private inference capacity, on your terms,
          and earn your share of every job it runs.
        </p>
        <div className="cta">
          <Link href="/waitlist/?for=share" className="btn btn-primary">
            Join the provider waitlist
          </Link>
          <a href={REPO} className="btn btn-ghost">
            Read the code
          </a>
        </div>
      </section>

      <section className="wrap">
        <h2>On your terms</h2>
        <div className="grid c3">
          <div className="card">
            <h3>Policy-gated</h3>
            <p className="muted">
              Runs only when you allow it: charging, on Wi-Fi, screen off, above a
              battery threshold, within a nightly window, under a temperature cap.
            </p>
          </div>
          <div className="card">
            <h3>Nothing leaks</h3>
            <p className="muted">
              Each job is sealed to your device. The model runs locally; prompts and
              responses are never written to disk or logs, on your machine or ours.
            </p>
          </div>
          <div className="card">
            <h3>Stop anytime</h3>
            <p className="muted">
              One switch. In-progress jobs drain; you leave the network immediately.
            </p>
          </div>
        </div>
      </section>

      <section className="wrap">
        <h2>How you earn</h2>
        <div className="prose">
          <p>
            The coordinator meters every request from the token stream it relays —
            not from anything your device self-reports, so counts can&rsquo;t be
            inflated in either direction.
          </p>
          <ul>
            <li>
              Your accrual is <strong>70%</strong> of what the request was billed
              (60% for confidential-tier jobs, which carry more platform infra).
            </li>
            <li>
              A quality multiplier (uptime, latency, error rate) nudges that up or
              down within a narrow band. It starts at 1.0.
            </li>
            <li>
              Attested hardware earns a premium: consumers pay 1.4&times; for{" "}
              <code>device_attested</code> providers, and that flows through to you.
            </li>
            <li>
              Accruals are paid out on a schedule once past a minimum, via a
              provider payout account you control.
            </li>
          </ul>
          <p className="note">
            Be realistic: a single phone running a small model earns cents, not
            dollars. The value comes from scale, from larger models on real GPUs,
            and from premium private workloads. The point of Ayni is that anyone can
            take part and share in it — see the{" "}
            <a href={`${REPO}/blob/main/docs/PAYMENTS.md`}>payments design</a>.
          </p>
        </div>
      </section>

      <section className="wrap">
        <h2>Trust tiers</h2>
        <div className="grid c3">
          <div className="card">
            <span className="pill grey">Tier 0</span>
            <h3 style={{ marginTop: 8 }}>community</h3>
            <p className="muted">
              Signed binary and network crypto. Any device. The owner is not
              prevented from inspecting what runs — priced accordingly.
            </p>
          </div>
          <div className="card">
            <span className="pill">Tier 1</span>
            <h3 style={{ marginTop: 8 }}>device_attested</h3>
            <p className="muted">
              A hardware-backed key (StrongBox / TEE / TPM) plus verified boot,
              attested to the coordinator. Earns a premium.
            </p>
          </div>
          <div className="card">
            <span className="pill grey">Tier 2</span>
            <h3 style={{ marginTop: 8 }}>confidential</h3>
            <p className="muted">
              CPU TEE and confidential GPU with remote attestation. For the most
              sensitive traffic. On the roadmap.
            </p>
          </div>
        </div>
      </section>

      <section className="wrap">
        <div className="callout">
          <h2>Sign up to share your compute</h2>
          <p style={{ maxWidth: "42ch", margin: "8px auto 22px" }}>
            The Android provider app is in internal testing. Join the waitlist and
            we&rsquo;ll bring you in.
          </p>
          <div style={{ maxWidth: 460, margin: "0 auto", textAlign: "left" }}>
            <WaitlistForm defaultInterest="share" />
          </div>
        </div>
      </section>
    </>
  );
}
