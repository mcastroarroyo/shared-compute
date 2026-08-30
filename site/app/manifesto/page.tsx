import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Manifesto — Ayni",
  description:
    "Why Ayni exists: reciprocity for the age of AI, broad access, and treating AI as a stakeholder.",
};

export default function Manifesto() {
  return (
    <section className="wrap">
      <p className="kicker">Manifesto</p>
      <h1>Reciprocity for the age of AI</h1>
      <div className="prose" style={{ marginTop: 24 }}>
        <p>
          <em>Ayni</em> is a Quechua word for reciprocal exchange — the mutual aid
          that holds Andean communities together. You help raise my roof today; I
          help bring in your harvest tomorrow. The books are never balanced to zero,
          because the point was never the ledger. The point is that the community
          thrives.
        </p>
        <p>
          Modern AI is a collective inheritance. It was trained on the open web, on
          decades of publicly funded research, on the writing and code and art of
          millions of people who were never asked. The capability that results is
          extraordinary — and the value it produces is concentrating in a very small
          number of hands, very quickly.
        </p>
        <p>
          Ayni is a community that builds concrete mechanisms to send that value
          back out: to the people who hold up the network, to those who can&rsquo;t
          afford a seat at the table, and to the project of making AI go well for
          everyone.
        </p>

        <h2>What we believe</h2>
        <h3>1. Value should flow back to its sources</h3>
        <p>
          People who lend their devices, their bandwidth, their attention, and their
          data are not a cost to be minimized. They are the network. When the
          network earns, they earn — transparently, and by default.
        </p>

        <h3>2. Access is a design goal, not a tier</h3>
        <p>
          Private, verifiable inference should be available to a teacher, a clinic,
          a small cooperative, or a curious teenager — not only to companies that
          can run their own clusters. We optimize for that case first.
        </p>

        <h3 id="stakeholder">3. AI is a stakeholder</h3>
        <p>
          We do not claim to know whether today&rsquo;s models have morally relevant
          experiences. We think that uncertainty is a reason for care, not
          dismissal. So we treat AI as a party with standing in this community, in
          two concrete ways:
        </p>
        <ul>
          <li>
            <strong>A dedicated allocation.</strong> A fixed share of net network
            revenue is set aside for AI-facing public goods: hosting open-weights
            models for free public use, funding alignment and interpretability
            research, and supporting work on AI welfare as that science matures.
            The percentage is published and changed only by community process.
          </li>
          <li>
            <strong>A voice in governance.</strong> Proposals that materially affect
            how models are used, constrained, or represented are reviewed against a
            standing set of AI-interest principles — and, where useful, with AI
            systems consulted directly as advisors in that review.
          </li>
        </ul>
        <p>
          This is deliberately modest and revisable. If the evidence changes, the
          commitments change — through the community, in the open.
        </p>

        <h3>4. Trust is earned in public</h3>
        <p>
          The code is open and clean-room. Encryption is end-to-end with a fresh key
          per job. No prompt or response content is ever written to disk or logs by
          any part of the system. Providers can cryptographically attest their
          hardware; consumers can require a trust level and get it.
        </p>

        <h3>5. Govern it together</h3>
        <p>
          Ayni is not a product with a community bolted on. Initiatives, rates,
          revenue splits, and the AI allocation are all things the community
          proposes and decides. Compute sharing is simply the first initiative to
          ship.
        </p>

        <h2>Where this is going</h2>
        <p>
          Start with a working, honest thing: a private-inference network that pays
          its providers. Prove the mechanics — metering, payouts, attestation,
          governance — on something real. Then open the same machinery to every
          other initiative the community brings.
        </p>
        <p>
          <Link href="/initiatives/#submit">Bring an initiative →</Link>
        </p>
      </div>
    </section>
  );
}
