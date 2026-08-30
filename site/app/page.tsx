import Link from "next/link";
import { WaitlistForm } from "./waitlist-form";
import { WorkloadBox } from "./workload-box";
import { REPO, WHITEPAPER, LICENSE } from "./lib/api";

export default function Home() {
  return (
    <>
      <section className="hero wrap">
        <p className="kicker">Ayni · Quechua for reciprocity</p>
        <h1>The world&rsquo;s unused compute, on demand.</h1>
        <p className="lead">
          Tell Ayni what you want to run. It finds the right idle devices, gives you
          one price and a completion estimate, executes the work, verifies it, and
          pays every device that helped.
        </p>
        <div className="cta">
          <Link href="/rent/" className="btn btn-primary">
            Run a workload
          </Link>
          <Link href="/share/" className="btn btn-teal">
            Share your compute
          </Link>
          <a href={WHITEPAPER} className="btn btn-ghost">
            Read the white paper
          </a>
        </div>
        <p className="tags">
          Open source · end-to-end encrypted · benchmarked devices · no prompt logging
        </p>
      </section>

      <section className="wrap">
        <p className="kicker">Live quote</p>
        <h2>Price a batch job right now</h2>
        <p className="lead" style={{ maxWidth: "54ch" }}>
          Describe the work. This calls the real coordinator and returns the same
          quote an API client would get — price, completion estimate, and how much
          goes to the devices that run it.
        </p>
        <div style={{ marginTop: 22 }}>
          <WorkloadBox />
        </div>
        <p className="note" style={{ marginTop: 12 }}>
          Estimate only — nothing runs and nothing is stored until you accept a quote
          with an API key.
        </p>
      </section>

      <section className="wrap">
        <p className="kicker">What Ayni means</p>
        <h2>Reciprocity, not charity</h2>
        <div className="prose">
          <p>
            In the Andes, <em>ayni</em> is a system of mutual aid: today I help
            build your house, tomorrow you help harvest my field. No one keeps a
            ledger of debts — the community&rsquo;s wellbeing is the point.
          </p>
          <p>
            We think AI needs that. The technology is a collective inheritance —
            built on the open web, open research, and the work of millions of
            people. Its benefits should flow back the same way.
          </p>
        </div>
        <div className="grid c3" style={{ marginTop: 28 }}>
          <div className="card">
            <span className="k">Principle 01</span>
            <h3>Reciprocity</h3>
            <p className="muted">
              Value created by the network flows back to the people and machines
              that make it possible.
            </p>
          </div>
          <div className="card">
            <span className="k">Principle 02</span>
            <h3>Broad access</h3>
            <p className="muted">
              Private, affordable inference for anyone — not only those who can
              afford a data center.
            </p>
          </div>
          <div className="card">
            <span className="k">Principle 03</span>
            <h3>AI as a stakeholder</h3>
            <p className="muted">
              We treat the moral status of AI as an open question worth taking
              seriously, and we set aside value for it.{" "}
              <Link href="/manifesto/#stakeholder">How that works →</Link>
            </p>
          </div>
        </div>
      </section>

      <section className="wrap">
        <p className="kicker">How it works</p>
        <h2>Quote, run, verify, pay</h2>
        <div className="steps c4" style={{ marginTop: 24 }}>
          <div className="step">
            <h3>You describe the work</h3>
            <p className="muted">
              A model and a set of items — 200 or 5,000. Ayni benchmarks every
              device itself, so it knows what each one can actually sustain.
            </p>
          </div>
          <div className="step">
            <h3>One quote</h3>
            <p className="muted">
              Price and completion estimate up front: device pay + coordination +
              a failure buffer + payment costs + Ayni&rsquo;s margin. No per-bid
              haggling.
            </p>
          </div>
          <div className="step">
            <h3>Fan-out execution</h3>
            <p className="muted">
              On accept, the coordinator splits the batch across eligible devices,
              runs it in parallel end-to-end encrypted, and reassembles results in
              order.
            </p>
          </div>
          <div className="step">
            <h3>Everyone gets paid</h3>
            <p className="muted">
              Each device accrues its share of every item it ran. You&rsquo;re
              charged once, at the quoted price.
            </p>
          </div>
        </div>
        <div className="cta" style={{ marginTop: 24 }}>
          <Link href="/rent/" className="btn btn-primary">
            See the API
          </Link>
          <Link href="/technology/" className="btn btn-ghost">
            How the encryption works
          </Link>
        </div>
      </section>

      <section className="wrap">
        <p className="kicker">The supply side</p>
        <h2>Compute sharing</h2>
        <p className="lead" style={{ maxWidth: "52ch" }}>
          Turn the idle time on your phone, laptop, or GPU into private inference
          for others — and get paid your share.
        </p>
        <div className="steps" style={{ marginTop: 24 }}>
          <div className="step">
            <h3>Contribute a device</h3>
            <p className="muted">
              Install the provider app. It runs only when you allow it — charging,
              on Wi-Fi, above a battery threshold.
            </p>
          </div>
          <div className="step">
            <h3>Jobs run locally, encrypted</h3>
            <p className="muted">
              Each request is sealed to your device with NaCl <code>crypto_box</code>.
              The model runs on your hardware; plaintext never touches disk or logs.
            </p>
          </div>
          <div className="step">
            <h3>Earn your share</h3>
            <p className="muted">
              Usage is metered by the coordinator. Your accrual is 70% of what the
              request was billed, paid out on a schedule.
            </p>
          </div>
        </div>
        <div className="cta" style={{ marginTop: 24 }}>
          <Link href="/share/" className="btn btn-primary">
            Share your compute
          </Link>
          <Link href="/technology/" className="btn btn-ghost">
            How the encryption works
          </Link>
        </div>
      </section>

      <section className="wrap">
        <p className="kicker">Open to more</p>
        <h2>Compute sharing is one of many</h2>
        <p className="lead" style={{ maxWidth: "52ch" }}>
          Anything that spreads AI&rsquo;s benefits and value to the world belongs
          here. Bring yours.
        </p>
        <div className="grid c3" style={{ marginTop: 24 }}>
          <div className="card">
            <span className="pill">Live</span>
            <h3 style={{ marginTop: 10 }}>Compute sharing</h3>
            <p className="muted">
              Idle devices become a private inference network. Providers get paid;
              users get cheap, encrypted inference.
            </p>
          </div>
          <div className="card">
            <span className="pill grey">Proposed</span>
            <h3 style={{ marginTop: 10 }}>Public-good inference</h3>
            <p className="muted">
              A slice of network revenue funds free inference on open-weights
              models for nonprofits, educators, and researchers.
            </p>
          </div>
          <div className="card">
            <span className="pill grey">Your idea</span>
            <h3 style={{ marginTop: 10 }}>Submit an initiative</h3>
            <p className="muted">
              Have a way to share AI value with people who&rsquo;d otherwise miss
              it? Propose it to the community.
            </p>
            <Link
              href="/initiatives/#submit"
              className="btn btn-ghost"
              style={{ marginTop: 12 }}
            >
              Submit yours
            </Link>
          </div>
        </div>
      </section>

      <section className="wrap">
        <p className="kicker">Built in the open</p>
        <h2>The technology behind it</h2>
        <div className="prose">
          <p>
            A Go coordinator authenticates and schedules; a portable Rust core runs
            <code> llama.cpp</code> on the provider&rsquo;s own hardware. Every hop is
            end-to-end encrypted with a fresh key per job. Providers can prove their
            hardware with StrongBox / TPM attestation to reach higher trust tiers.
          </p>
          <p>
            Clean-room implementation, permissive license, reproducible builds.
            Read it, run it, fork it.
          </p>
        </div>
        <div className="cta" style={{ marginTop: 20 }}>
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
      </section>

      <section className="wrap">
        <div className="callout">
          <p className="kicker" style={{ color: "rgba(255,255,255,0.8)" }}>
            Get started
          </p>
          <h2>Join the waitlist</h2>
          <p style={{ maxWidth: "44ch", margin: "8px auto 24px" }}>
            Tell us whether you want to share compute, rent it, or bring an
            initiative. We&rsquo;ll open your track and email you.
          </p>
          <div style={{ maxWidth: 460, margin: "0 auto", textAlign: "left" }}>
            <WaitlistForm />
          </div>
        </div>
      </section>
    </>
  );
}
