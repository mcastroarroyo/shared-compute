import type { Metadata } from "next";
import Link from "next/link";
import { InitiativeForm } from "./initiative-form";

export const metadata: Metadata = {
  title: "Initiatives — Ayni",
  description:
    "Compute sharing is the first Ayni initiative. Propose your own way to share AI's value with the world.",
};

const initiatives = [
  {
    status: "Live",
    title: "Compute sharing",
    body:
      "Idle phones, laptops, and GPUs become a private inference network. Providers earn 70% of billed usage; consumers get affordable, end-to-end-encrypted inference with a choice of hardware trust level.",
    href: "/share/",
    hrefLabel: "Share your compute",
  },
  {
    status: "Proposed",
    title: "Public-good inference pool",
    body:
      "A fixed slice of net network revenue funds free inference on open-weights models for nonprofits, public schools, clinics, and independent researchers — allocated transparently by community process.",
  },
  {
    status: "Proposed",
    title: "Open model hosting for the AI allocation",
    body:
      "Part of the AI-stakeholder allocation goes to keeping capable open-weights models online and free to query, so the commons has a floor that no single company controls.",
  },
  {
    status: "Proposed",
    title: "Data dividend",
    body:
      "A mechanism for communities that contribute high-value datasets or evaluations to receive an ongoing share of the value their contribution generates.",
  },
];

export default function Initiatives() {
  return (
    <>
      <section className="wrap">
        <p className="kicker">Initiatives</p>
        <h1>Compute sharing is the first. Not the last.</h1>
        <p className="lead" style={{ maxWidth: "54ch" }}>
          Ayni&rsquo;s machinery — metering, payouts, attestation, governance — is
          built to carry any initiative that gets AI&rsquo;s benefits and value to
          people who&rsquo;d otherwise miss out.
        </p>

        <div className="grid c2" style={{ marginTop: 28 }}>
          {initiatives.map((i) => (
            <div className="card" key={i.title}>
              <span className={`pill ${i.status === "Live" ? "" : "grey"}`}>
                {i.status}
              </span>
              <h3 style={{ marginTop: 10 }}>{i.title}</h3>
              <p className="muted">{i.body}</p>
              {i.href && (
                <Link href={i.href} className="btn btn-ghost" style={{ marginTop: 12 }}>
                  {i.hrefLabel}
                </Link>
              )}
            </div>
          ))}
        </div>
      </section>

      <section className="wrap" id="submit">
        <p className="kicker">Propose one</p>
        <h2>Submit your Ayni initiative</h2>
        <p className="lead" style={{ maxWidth: "52ch", marginBottom: 8 }}>
          Tell us what it is, who it helps, and how it shares AI value. Strong
          proposals go to the community for discussion and, if adopted, get access
          to Ayni&rsquo;s payout and governance rails.
        </p>
        <p className="note" style={{ marginBottom: 20 }}>
          Good candidates: broaden access, return value to contributors, strengthen
          the open-model commons, or advance AI safety and welfare. It does not have
          to involve compute.
        </p>
        <InitiativeForm />
      </section>
    </>
  );
}
