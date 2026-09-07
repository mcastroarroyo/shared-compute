import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Testers wanted — Ayni",
  description:
    "Help test Ayni: share a phone or laptop for ten minutes, run one workload, tell us what broke. Early access, direct line to the team, honest status.",
};

const APP = "https://app.ayni-ai.com";
const DISCUSS = "https://github.com/mcastroarroyo/shared-compute/discussions";

export default function Testers() {
  return (
    <section className="wrap" style={{ maxWidth: 760 }}>
      <p className="kicker">Testers wanted</p>
      <h1>Ten minutes on your phone or laptop.</h1>
      <p className="lead">
        Ayni is in early testing. We need people with an Android phone, a Mac or a Linux box to add a
        device, run one workload, and tell us what confused them. Every message reaches the team the
        same minute, and you get a direct reply.
      </p>

      <div className="cta" style={{ margin: "24px 0 32px" }}>
        <a href={`${APP}/testers/`} className="btn btn-primary">Start testing</a>
        <a href={DISCUSS} className="btn btn-ghost">Tester community</a>
      </div>

      <h2>What you will do</h2>
      <ol style={{ lineHeight: 1.8 }}>
        <li>Sign in at <a href={APP}>app.ayni-ai.com</a> with GitHub or Google. No password, no card.</li>
        <li>
          <strong>Add a device.</strong> Phone: install the test app and type a 6-letter code. Mac or Linux: paste one
          command in a terminal. The page confirms the device within seconds.
        </li>
        <li><strong>Run a workload.</strong> Paste a paragraph, get a quote and a Council verdict, accept, read the summary.</li>
        <li><strong>Tell us.</strong> Bugs, confusion, ideas. Screenshots and the exact text on screen help most.</li>
      </ol>

      <h2>Honest status</h2>
      <ul style={{ lineHeight: 1.8 }}>
        <li>One small model (0.5B) in production; a phone does about 18 tokens per second, a laptop 80 to 160.</li>
        <li>Earnings are real but tiny: cents, not dollars. You are here for the early look and the influence, not the income.</li>
        <li>The Android app is in Google Play internal testing; request access on the testers page with your Google account email.</li>
        <li>Everything is open source (Apache 2.0). Read the code, the threat model and the white paper before you trust a claim.</li>
      </ul>

      <h2>What we promise</h2>
      <ul style={{ lineHeight: 1.8 }}>
        <li>A reply to every message, usually within a day, in the same place you wrote it.</li>
        <li>Release notes in the community when your report ships, with your name if you want it.</li>
        <li>No ads, no tracking, no access to your files. Jobs are sealed to your device with a one-time key and never logged.</li>
      </ul>

      <p className="muted" style={{ marginTop: 28 }}>
        Questions first? Open a thread in <a href={DISCUSS}>Discussions</a> or write to hello@ayni-ai.com.
      </p>
    </section>
  );
}
