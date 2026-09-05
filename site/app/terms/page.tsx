import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Terms of Service — Ayni",
  description:
    "The terms under which renters buy compute and providers share devices on the Ayni marketplace.",
};

const UPDATED = "5 September 2026";
const CONTACT = "mcastroarroyo@gmail.com";

export default function Terms() {
  return (
    <>
      <section className="hero wrap">
        <p className="kicker">Legal</p>
        <h1>Terms of Service</h1>
        <p className="lead" style={{ maxWidth: "60ch" }}>
          Ayni is a marketplace: renters pay for AI inference, providers earn by
          running it on devices they own. These terms set out what each side
          agrees to. They are written to be read.
        </p>
        <p className="note">Last updated {UPDATED}. Early-access service; see &ldquo;Service status&rdquo; below.</p>
      </section>

      <section className="wrap prose">
        <h2>1. The service</h2>
        <p>
          Ayni operates a coordinator that matches inference workloads with
          devices, relays end-to-end encrypted jobs, meters usage and settles
          payment. Ayni does not run the models itself; independent providers do.
          Ayni is not a party to the content of any workload and cannot read it.
        </p>

        <h2>2. Accounts</h2>
        <p>
          You need a GitHub account to sign in to the web app. You are responsible
          for activity under your API keys and for keeping them secret. You must
          be at least 18, or the age of majority where you live, to buy compute
          or receive payouts.
        </p>

        <h2>3. Renters</h2>
        <ul>
          <li><strong>Prepaid credit.</strong> You buy credit in advance through Stripe. Each workload is quoted before it runs; accepting a quote debits the quoted amount. Unused credit is refundable on request within 12 months of purchase, minus payment-processing fees.</li>
          <li><strong>Best effort, honestly priced.</strong> Spot workloads are interruptible and priced accordingly. Quotes include an explicit failure buffer. If a job fails through no fault of yours, its debit is reversed.</li>
          <li><strong>Trust tiers.</strong> You may pin a workload to attested devices. Ayni verifies attestation evidence in good faith but cannot guarantee the behaviour of hardware it does not own.</li>
          <li><strong>Acceptable use.</strong> You may run only the models and runtimes in Ayni&rsquo;s signed registry. You may not use Ayni to generate content that is illegal where you or the provider are, to attack any system, or to attempt to break out of the workload sandbox. The Ayni Council reviews workloads before they run and may refuse them.</li>
        </ul>

        <h2>4. Providers</h2>
        <ul>
          <li><strong>Your device, your rules.</strong> The app runs only when your policy allows (charging, Wi-Fi, battery and temperature limits). You can stop at any time; in-flight jobs are cancelled and rescheduled.</li>
          <li><strong>What runs.</strong> Only signed Workload Manifests for allow-listed models, with no network access from the workload and enforced resource limits. Ayni never runs arbitrary code on your device.</li>
          <li><strong>Earnings.</strong> You accrue earnings per completed job as shown in the app. Payouts are made through Stripe Connect once you complete its onboarding and reach the minimum balance. Ayni&rsquo;s cut is shown on every quote.</li>
          <li><strong>Honesty.</strong> Do not tamper with the app, report false capabilities or return fabricated results. Ayni counts tokens itself and may withhold earnings for jobs that fail verification.</li>
          <li><strong>Costs.</strong> You are responsible for your electricity and data charges. Ayni recommends running only on Wi-Fi and while charging, which the default policy enforces.</li>
        </ul>

        <h2>5. Fees and taxes</h2>
        <p>
          Prices are in US dollars. Ayni&rsquo;s marketplace margin is included in
          every quote. Providers are responsible for taxes on their earnings;
          Stripe collects the information required to issue tax forms where
          applicable.
        </p>

        <h2>6. Service status</h2>
        <p>
          Ayni is in early access. We aim for high availability but make no
          uptime guarantee yet, and we may change, suspend or discontinue features
          with notice in the app. Prepaid credit remains refundable under section 3.
        </p>

        <h2>7. Intellectual property</h2>
        <p>
          The Ayni software is open source under the Apache 2.0 licence. Model
          weights remain under their own licences, listed in the registry. You
          own your prompts and the outputs generated for you, to the extent
          permitted by the model licence.
        </p>

        <h2>8. Liability</h2>
        <p>
          To the fullest extent permitted by law, Ayni is provided &ldquo;as
          is&rdquo;, and our total liability to you for any claim is limited to
          the amount you paid us, or we paid you, in the 12 months before the
          claim. We are not liable for indirect or consequential loss, or for the
          acts of independent providers or renters.
        </p>

        <h2>9. Termination</h2>
        <p>
          You may close your account at any time. We may suspend accounts that
          breach these terms, with a refund of unused credit unless the breach
          involved fraud or abuse.
        </p>

        <h2>10. Changes and contact</h2>
        <p>
          We will post changes here with a new date and notify you in the app of
          material changes at least 14 days before they apply. Questions:{" "}
          <a href={`mailto:${CONTACT}`}>{CONTACT}</a>. Our{" "}
          <Link href="/privacy/">Privacy Policy</Link> explains the data we hold.
        </p>
      </section>
    </>
  );
}
