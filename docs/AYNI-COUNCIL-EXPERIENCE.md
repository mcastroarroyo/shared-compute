# Ayni Council Experience and Disclosure Contract

This document defines the public UX for the Ayni Council without weakening the
security boundary. It belongs to the Security/Council workstream and does not require
changes to marketplace, pricing, scheduler, batch, quote, console, or site code.

## User experience

The Council observatory has four primary views:

1. **Council** — the signed current roster, ten specialist roles, model/provider
   identity, qualification epoch, provider diversity, seat status, and active meeting.
2. **Meetings** — meeting lifecycle, structured positions, evidence digests, required
   controls, vote state, and protected dissent.
3. **Decisions** — append-only public decision ledger, verdict, severity, quorum,
   actions, status, audit hash, and exportable verification record.
4. **Constitution** — current constitutional version, digest, principles, amendment
   history, and the boundary between humans, AI advisors, and deterministic controls.

## Three disclosure levels

| Level | Audience | May contain |
|---|---|---|
| Public | Anyone | Signed roster, public-safe minutes, votes, dissent summaries, decisions, controls, action status, hashes |
| Authenticated | Ayni operators and approved auditors | Structured evidence metadata, internal control ownership, non-public remediation state |
| Restricted | Security responders only | Vulnerability reproductions, exploit details, incident data, sensitive infrastructure evidence |

Customer plaintext, prompts, completions, files, credentials, private keys, provider
tokens, hidden model reasoning, and raw chain-of-thought are excluded from every public
record. Restricted evidence is represented publicly only by a digest and disclosure
reason.

## Public record protocol

`ayni-council-public-record-v1` exposes only:

- decision and proposal IDs;
- title, risk class, decision, and maximum severity;
- aggregate vote totals, including missing votes;
- required controls;
- concise public-safe dissent summaries;
- action IDs, control, owner role, status, and optional due date;
- evidence digests;
- signed roster and Constitution digests;
- previous record hash, publication time, and record hash.

The protocol has no fields for prompts, conversations, model chain-of-thought, arbitrary
attachments, exploit payloads, credentials, or customer content. The executable
reference is `security/ayni_security/publication.py`.

## Security properties

- The UI is read-only with respect to deployment, funds, ledger balances, and nodes.
- Displayed model names must come from a signed roster; the frontend does not invent or
  override Council membership.
- A meeting is not presented as final until deterministic quorum policy seals it.
- Missing critical votes are visible and fail closed.
- Dissent cannot be omitted from a final public record after it has been sealed.
- Published records are hash-linked; edits or deletion break verification.
- Public actions show remediation progress without exposing operational attack detail.
- Council provider credentials remain server-side in a secret manager and never enter
  the browser payload.

## Integration API boundary

The future platform integration should consume read-only security endpoints or signed
static records:

```text
GET /v1/council/roster
GET /v1/council/meetings
GET /v1/council/meetings/{meeting_id}
GET /v1/council/decisions
GET /v1/council/decisions/{decision_id}
GET /v1/council/constitution
```

Only the publication service may construct the public payload. The marketplace agent
may render these records but must not join them with customer workload data, rewrite
votes, infer hidden reasoning, or treat the UI as an enforcement path.

## Prototype data warning

Until live Council providers and a signed roster are configured, the experience must
label model identities, meeting counts, votes, and audit records as demonstration data.
Removing that label requires real signed source records and end-to-end verification.
