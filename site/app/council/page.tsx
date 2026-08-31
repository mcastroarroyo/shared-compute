"use client";

import { useEffect, useState } from "react";
import { council } from "../lib/api";
import { REPO } from "../lib/api";

type Seat = {
  seat: string;
  role: string;
  model_id: string;
  provider: string;
  open_weight: boolean;
  status: string;
};

type Decision = {
  decision_id: string;
  title: string;
  risk_class: string;
  decision: string;
  severity: string;
  votes: Record<string, number>;
  required_controls: string[];
  dissent: { seat: string; summary: string }[];
  actions: { control: string; owner_role: string; status: string }[];
  record_hash: string;
};

export default function CouncilPage() {
  const [roster, setRoster] = useState<any>(null);
  const [decisions, setDecisions] = useState<any>(null);
  const [constitution, setConstitution] = useState<any>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    Promise.all([council.roster(), council.decisions(), council.constitution()])
      .then(([r, d, c]) => {
        setRoster(r);
        setDecisions(d);
        setConstitution(c);
      })
      .catch(() => setErr("Could not reach the coordinator."));
  }, []);

  return (
    <>
      <section className="hero wrap">
        <p className="kicker">Governance</p>
        <h1>The Ayni Council</h1>
        <p className="lead" style={{ maxWidth: "56ch" }}>
          A ten-seat, provider-diverse, fail-closed review body — AI advisors plus
          humans — for architecture, policy, and risky changes. Models reason and
          vote; a deterministic policy decides; every decision is hash-linked and
          publicly verifiable. The Council never moves money, deploys code, or
          commands nodes.
        </p>
        <div className="banner-demo">
          Demonstration data. No live Council model providers or signed roster are
          configured yet — identities, meetings, votes, and hashes below are
          illustrative.
        </div>
      </section>

      {err && (
        <section className="wrap">
          <p className="err">{err}</p>
        </section>
      )}

      <section className="wrap">
        <p className="kicker">Roster</p>
        <h2>Ten specialist seats</h2>
        {roster && (
          <>
            <p className="note" style={{ marginBottom: 14 }}>
              {roster.providers} providers · at most {roster.max_per_provider} seats
              each · {roster.open_weight} open-weight seats · selection digest{" "}
              <code>{String(roster.selection_digest).slice(0, 16)}…</code>
            </p>
            <div style={{ overflowX: "auto" }}>
              <table className="ctable">
                <thead>
                  <tr>
                    <th>Role</th>
                    <th>Model</th>
                    <th>Provider</th>
                    <th>Weights</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {(roster.seats as Seat[]).map((s) => (
                    <tr key={s.seat}>
                      <td>{s.role}</td>
                      <td className="mono">{s.model_id}</td>
                      <td>{s.provider}</td>
                      <td>{s.open_weight ? "open" : "closed"}</td>
                      <td>
                        <span className="pill good">{s.status}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </section>

      <section className="wrap">
        <p className="kicker">Decisions</p>
        <h2>
          Public decision ledger{" "}
          {decisions && (
            <span
              className={`pill ${decisions.chain_valid ? "good" : ""}`}
              style={{ fontSize: "0.7rem", verticalAlign: "middle" }}
            >
              {decisions.chain_valid ? "hash chain verified" : "chain broken"}
            </span>
          )}
        </h2>
        <p className="note" style={{ marginBottom: 14 }}>
          Protocol <code>ayni-council-public-record-v1</code> — no field can carry
          prompts, chain-of-thought, exploits, credentials, or customer content.
        </p>
        {decisions &&
          (decisions.decisions as Decision[]).map((d) => (
            <div className="card" key={d.decision_id} style={{ marginBottom: 16 }}>
              <div className="row" style={{ gap: 8, flexWrap: "wrap", alignItems: "center" }}>
                <span className={`pill ${d.decision === "APPROVE" ? "good" : "grey"}`}>
                  {d.decision}
                </span>
                <span className="pill grey">{d.risk_class}</span>
                <span className="pill grey">severity {d.severity}</span>
              </div>
              <h3 style={{ marginTop: 10 }}>{d.title}</h3>
              <p className="muted" style={{ fontSize: "0.9rem" }}>
                Votes — approve {d.votes.approve} · conditional {d.votes.conditional} ·
                block {d.votes.block} · missing {d.votes.missing}
              </p>
              {d.required_controls.length > 0 && (
                <>
                  <div className="k">Required controls</div>
                  <ul className="tight">
                    {d.required_controls.map((c) => (
                      <li key={c}>{c}</li>
                    ))}
                  </ul>
                </>
              )}
              {d.dissent.length > 0 && (
                <>
                  <div className="k">Protected dissent</div>
                  <ul className="tight">
                    {d.dissent.map((x, i) => (
                      <li key={i}>
                        <b>{x.seat}:</b> {x.summary}
                      </li>
                    ))}
                  </ul>
                </>
              )}
              {d.actions.length > 0 && (
                <>
                  <div className="k">Remediation</div>
                  <ul className="tight">
                    {d.actions.map((a, i) => (
                      <li key={i}>
                        {a.control} — <i>{a.owner_role}</i>{" "}
                        <span className="pill grey" style={{ fontSize: "0.68rem" }}>
                          {a.status}
                        </span>
                      </li>
                    ))}
                  </ul>
                </>
              )}
              <p className="note" style={{ marginTop: 8 }}>
                record hash <code>{d.record_hash.slice(0, 24)}…</code>
              </p>
            </div>
          ))}
      </section>

      <section className="wrap">
        <p className="kicker">Constitution</p>
        <h2>What the Council is bound by</h2>
        {constitution && (
          <>
            <p className="note" style={{ marginBottom: 14 }}>
              v{constitution.version} · digest{" "}
              <code>{String(constitution.digest).slice(0, 16)}…</code>
            </p>
            <ol className="prose" style={{ marginLeft: "1.2em" }}>
              {(constitution.principles as string[]).map((p) => (
                <li key={p} style={{ marginBottom: 4 }}>
                  {p}
                </li>
              ))}
            </ol>
          </>
        )}
        <div className="cta" style={{ marginTop: 18 }}>
          <a
            href={`${REPO}/blob/main/security/constitution/AYNI_CONSTITUTION.md`}
            className="btn btn-ghost"
          >
            Full Constitution
          </a>
          <a href={`${REPO}/tree/main/security`} className="btn btn-ghost">
            Security harness + Council code
          </a>
        </div>
      </section>
    </>
  );
}
