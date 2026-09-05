import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Privacy Policy — Ayni",
  description:
    "What Ayni collects, what it never sees, how long it is kept, and how to delete it. Covers the website, the web app, the API, and the Ayni provider app for Android, macOS, Linux and Windows.",
};

const UPDATED = "5 September 2026";
const CONTACT = "mcastroarroyo@gmail.com";

export default function Privacy() {
  return (
    <>
      <section className="hero wrap">
        <p className="kicker">Legal</p>
        <h1>Privacy Policy</h1>
        <p className="lead" style={{ maxWidth: "60ch" }}>
          Ayni is built so that the content of a workload is never visible to us.
          This page explains, in plain language, the small amount of data we do
          hold, why, for how long, and how to remove it.
        </p>
        <p className="note">Last updated {UPDATED}. Applies to ayni-ai.com, app.ayni-ai.com, api.ayni-ai.com and the Ayni provider apps.</p>
      </section>

      <section className="wrap prose">
        <h2>Who we are</h2>
        <p>
          Ayni (&ldquo;we&rdquo;, &ldquo;us&rdquo;) operates a marketplace that runs
          AI inference workloads on idle personal devices. The data controller is
          the operator of ayni-ai.com, reachable at <a href={`mailto:${CONTACT}`}>{CONTACT}</a>.
        </p>

        <h2>What we never see</h2>
        <p>
          Prompts, model outputs and any files inside a workload are encrypted on
          the renter&rsquo;s side to a key held only by the device that runs the
          job. Our coordinator relays sealed bytes it cannot open, and the
          provider app keeps decrypted content in memory only for the duration of
          the job. We do not log, store, sample or train on workload content.
          The design is documented on the <Link href="/security/">Security</Link> page.
        </p>

        <h2>What we collect, and why</h2>
        <h3>Renters (people who buy compute)</h3>
        <ul>
          <li><strong>Account:</strong> the e-mail address and display name returned by the sign-in provider you choose (GitHub). We store no password.</li>
          <li><strong>Billing:</strong> prepaid credit balance and a ledger of debits and top-ups. Card details are entered on Stripe&rsquo;s pages and never reach our servers; we keep Stripe&rsquo;s session and payment identifiers.</li>
          <li><strong>Usage metadata:</strong> per job, the model name, token counts, timing, price and the anonymous identifier of the device that served it. Not the content.</li>
        </ul>
        <h3>Providers (people who share a device)</h3>
        <ul>
          <li><strong>Device identity:</strong> a public key generated on the device and, for the Android app, the hardware attestation certificate chain used to award the <code>device_attested</code> trust tier. This identifies the device, not you.</li>
          <li><strong>Capability and health:</strong> platform, hardware class, RAM, benchmark throughput, battery, charging and thermal state, so the scheduler can place work responsibly.</li>
          <li><strong>Earnings:</strong> a ledger of accrued earnings per device and, if you enrol for payouts, the Stripe Connect account identifier. Identity checks and bank details are collected by Stripe under its own policy.</li>
        </ul>
        <h3>Everyone</h3>
        <ul>
          <li><strong>Operational logs:</strong> request timestamps, status codes, IP address and user agent at our edge, kept to run and secure the service.</li>
          <li><strong>Waitlist:</strong> the e-mail address you give us, used only to contact you about Ayni.</li>
        </ul>

        <h2>What we do not do</h2>
        <ul>
          <li>No advertising, no ad identifiers, no third-party analytics SDKs in the provider app.</li>
          <li>No selling or renting of personal data, ever.</li>
          <li>No access to your contacts, photos, location, microphone or files. The provider app runs only the model files it downloads from our signed registry.</li>
        </ul>

        <h2>Legal bases</h2>
        <p>
          We process account and billing data to perform our contract with you;
          operational logs and abuse prevention under our legitimate interest in
          running a secure service; and waitlist e-mails with your consent, which
          you can withdraw at any time.
        </p>

        <h2>Sharing</h2>
        <p>
          We share data only with processors needed to run the service: Google
          Cloud (hosting, in the United States), Stripe (payments and payouts),
          Cloudflare (web hosting and DNS) and GitHub (sign-in). Each is bound by
          its own data-processing terms. We disclose data to authorities only when
          legally required.
        </p>

        <h2>Retention</h2>
        <ul>
          <li>Account data: until you delete your account.</li>
          <li>Billing and earnings ledgers: 7 years, as required for financial records.</li>
          <li>Job metadata: 90 days in detail, then aggregated.</li>
          <li>Edge logs: 30 days.</li>
        </ul>

        <h2>Your rights</h2>
        <p>
          You can access, correct, export or delete your data, and object to or
          restrict processing. Delete your account from the web app or e-mail
          us at <a href={`mailto:${CONTACT}`}>{CONTACT}</a>; we respond within 30
          days. Deleting a provider device from the app removes its identity from
          the network at the next heartbeat. If you are in the EU/EEA or UK you
          may also complain to your local supervisory authority.
        </p>

        <h2>Children</h2>
        <p>Ayni is not directed at children under 16 and we do not knowingly collect their data.</p>

        <h2>Security</h2>
        <p>
          Transport is TLS everywhere; workloads are additionally end-to-end
          encrypted; secrets live in a cloud key-management service; every job
          carries a signed Workload Manifest that the device verifies before it
          runs anything. Details, threat model and test harness are on the{" "}
          <Link href="/security/">Security</Link> page.
        </p>

        <h2>Changes</h2>
        <p>
          We will post changes here and update the date above. Material changes
          to what we collect will be announced in the app before they apply.
        </p>
      </section>
    </>
  );
}
