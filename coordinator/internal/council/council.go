// Package council serves the read-only Ayni Council observatory: the signed
// roster, meeting lifecycle, the append-only public decision ledger, and the
// Constitution.
//
// Until live Council model providers and a signed roster are configured, every
// response is DEMONSTRATION DATA. The schemas mirror
// security/ayni_security/{council,publication,qualification}.py and the UX
// contract in docs/AYNI-COUNCIL-EXPERIENCE.md. This package is an intelligence /
// governance surface — never an enforcement path.
package council

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

//go:embed constitution.md
var constitutionMarkdown string

// PublicRecordProtocol is the only public disclosure schema. It has no field
// that can carry prompts, chain-of-thought, credentials, or customer content.
const PublicRecordProtocol = "ayni-council-public-record-v1"

// Demo is set on every payload while the observatory shows sample data.
const Demo = true

const disclaimer = "demonstration data — no live Council providers or signed roster are configured yet; " +
	"model identities, meetings, votes, and audit hashes are illustrative"

// --- roster ---

// Seat is one specialist Council role and its assigned model.
type Seat struct {
	Seat       string `json:"seat"`
	Role       string `json:"role"`
	ModelID    string `json:"model_id"`
	Provider   string `json:"provider"`
	OpenWeight bool   `json:"open_weight"`
	Status     string `json:"status"`
}

var seatRoles = []struct{ seat, role string }{
	{"security_critic", "Chief Security Critic"},
	{"red_team", "Red Team Director"},
	{"privacy", "Privacy Guardian"},
	{"financial_integrity", "Financial Integrity Guardian"},
	{"node_safety", "Node and Provider Safety Guardian"},
	{"renter_abuse", "Renter Abuse Guardian"},
	{"reliability", "Architecture and Reliability Advisor"},
	{"human_impact", "Human Impact and Rights Advisor"},
	{"ai_stewardship", "AI Stewardship Advisor"},
	{"dissenter", "Independent Dissenter"},
}

// demoRoster: 10 seats, 5 providers, ≤2 per provider, ≥2 open-weight — the
// qualification rules from security/ayni_security/qualification.py, filled with
// placeholder identities.
var demoRoster = []struct {
	model, provider string
	open            bool
}{
	{"model-alpha", "provider-1", false},
	{"model-bravo", "provider-1", false},
	{"model-charlie", "provider-2", false},
	{"model-delta", "provider-2", false},
	{"model-echo", "provider-3", true},
	{"model-foxtrot", "provider-3", true},
	{"model-golf", "provider-4", false},
	{"model-hotel", "provider-4", true},
	{"model-india", "provider-5", false},
	{"model-juliet", "provider-5", false},
}

// Roster returns the current (demo) Council roster plus its selection digest.
func Roster() map[string]any {
	seats := make([]Seat, len(seatRoles))
	for i, sr := range seatRoles {
		d := demoRoster[i]
		seats[i] = Seat{
			Seat: sr.seat, Role: sr.role, ModelID: d.model,
			Provider: d.provider, OpenWeight: d.open, Status: "active",
		}
	}
	providers := map[string]struct{}{}
	openN := 0
	for _, s := range seats {
		providers[s.Provider] = struct{}{}
		if s.OpenWeight {
			openN++
		}
	}
	digestPayload := make([]map[string]string, len(seats))
	for i, s := range seats {
		digestPayload[i] = map[string]string{"seat": s.Seat, "model_id": s.ModelID, "provider": s.Provider}
	}
	return map[string]any{
		"data":                "demonstration",
		"disclaimer":          disclaimer,
		"seat_count":          len(seats),
		"seats":               seats,
		"providers":           len(providers),
		"open_weight":         openN,
		"max_per_provider":    2,
		"qualification_epoch": "demo-0",
		"selection_digest":    recordHash(digestPayload),
	}
}

// --- constitution ---

// Constitution returns the current constitutional text, its digest, and the
// principle headings.
func Constitution() map[string]any {
	return map[string]any{
		"data":       "reference",
		"version":    "0.1",
		"digest":     ConstitutionDigest(),
		"markdown":   constitutionMarkdown,
		"principles": principles(),
		"authority": []string{
			"Humans establish legitimate purpose, accept legal accountability, and amend this text.",
			"The Ayni Council supplies continuous specialist and adversarial review.",
			"Deterministic policy, cryptography, sandboxes, and transactional ledgers enforce rules.",
		},
	}
}

// ConstitutionDigest is the lowercase sha-256 of the embedded Constitution.
func ConstitutionDigest() string {
	sum := sha256.Sum256([]byte(constitutionMarkdown))
	return hex.EncodeToString(sum[:])
}

func principles() []string {
	var out []string
	for _, line := range strings.Split(constitutionMarkdown, "\n") {
		t := strings.TrimSpace(line)
		// numbered principle lines: "1. **Human safety comes first.** ..." /
		// "10. **Responsible AI stewardship.** ...". Only under "## Principles".
		dot := strings.Index(t, ". **")
		if dot <= 0 || !allDigits(t[:dot]) {
			continue
		}
		seg := t[dot+4:]
		if end := strings.Index(seg, "**"); end > 0 {
			out = append(out, seg[:end])
		}
	}
	return out
}

// --- decisions (public record ledger, hash-chained) ---

// PublicDecision mirrors publication.PublicDecisionRecord (the narrow schema).
type PublicDecision struct {
	Protocol           string           `json:"protocol"`
	DecisionID         string           `json:"decision_id"`
	ProposalID         string           `json:"proposal_id"`
	Title              string           `json:"title"`
	RiskClass          string           `json:"risk_class"`
	Decision           string           `json:"decision"`
	Severity           string           `json:"severity"`
	Votes              map[string]int   `json:"votes"`
	RequiredControls   []string         `json:"required_controls"`
	Dissent            []map[string]any `json:"dissent"`
	Actions            []map[string]any `json:"actions"`
	EvidenceDigests    []string         `json:"evidence_digests"`
	RosterDigest       string           `json:"roster_digest"`
	ConstitutionDigest string           `json:"constitution_digest"`
	PreviousRecordHash string           `json:"previous_record_hash"`
	PublishedAt        int64            `json:"published_at"`
	RecordHash         string           `json:"record_hash"`
}

func demoDecisions() []PublicDecision {
	rosterDigest, _ := Roster()["selection_digest"].(string)
	cd := ConstitutionDigest()
	recs := []PublicDecision{
		{
			DecisionID: "dec-0001", ProposalID: "prop-manifest-v1",
			Title:     "Adopt signed Workload Manifest v1 for all dispatched jobs",
			RiskClass: "security_critical", Decision: "APPROVE", Severity: "HIGH",
			Votes:            map[string]int{"approve": 9, "conditional": 1, "block": 0, "missing": 0},
			RequiredControls: []string{"isolated manifest signer", "node signature + ceiling verification", "expiry and nonce"},
			Dissent: []map[string]any{{
				"seat": "dissenter", "decision": "CONDITIONAL", "severity": "MEDIUM",
				"summary": "approve only alongside a persisted nonce store and KMS custody of the signer key",
			}},
			Actions: []map[string]any{
				{"action_id": "act-01", "control": "persist single-use nonces across node restarts", "owner_role": "node_safety", "status": "OPEN"},
				{"action_id": "act-02", "control": "move signer key to KMS/HSM with rotation", "owner_role": "security_critic", "status": "OPEN"},
			},
			EvidenceDigests: []string{recordHash(map[string]string{"artifact": "docs/WORKLOAD-MANIFEST.md"})},
		},
		{
			DecisionID: "dec-0002", ProposalID: "prop-spot-tier",
			Title:     "Enable the spot service class (interruptible, discounted)",
			RiskClass: "high", Decision: "CONDITIONAL", Severity: "MEDIUM",
			Votes:            map[string]int{"approve": 3, "conditional": 2, "block": 0, "missing": 0},
			RequiredControls: []string{"spot batches run at a capped worker pool", "spot accruals priced at the quoted factor"},
			Dissent:          []map[string]any{},
			Actions: []map[string]any{
				{"action_id": "act-03", "control": "publish provider opt-out of spot before GA", "owner_role": "node_safety", "status": "OPEN"},
			},
			EvidenceDigests: []string{recordHash(map[string]string{"artifact": "docs/ROADMAP.md#m10.5"})},
		},
	}
	prev := ""
	for i := range recs {
		recs[i].Protocol = PublicRecordProtocol
		recs[i].RosterDigest = rosterDigest
		recs[i].ConstitutionDigest = cd
		recs[i].PreviousRecordHash = prev
		recs[i].PublishedAt = 1788000000 + int64(i)*3600
		recs[i].RecordHash = recordHash(canonicalDecision(recs[i]))
		prev = recs[i].RecordHash
	}
	return recs
}

// Decisions returns the public ledger + whether the hash chain verifies.
func Decisions() map[string]any {
	recs := demoDecisions()
	return map[string]any{
		"data":        "demonstration",
		"disclaimer":  disclaimer,
		"protocol":    PublicRecordProtocol,
		"count":       len(recs),
		"chain_valid": verifyChain(recs),
		"decisions":   recs,
	}
}

// Decision returns one record by id, or ok=false.
func Decision(id string) (map[string]any, bool) {
	for _, r := range demoDecisions() {
		if r.DecisionID == id {
			return map[string]any{"data": "demonstration", "disclaimer": disclaimer, "decision": r}, true
		}
	}
	return nil, false
}

// --- meetings ---

// Meetings returns the (demo) meeting lifecycle list.
func Meetings() map[string]any {
	ms := demoMeetings()
	return map[string]any{
		"data": "demonstration", "disclaimer": disclaimer,
		"count": len(ms), "meetings": ms,
	}
}

// Meeting returns one meeting by id.
func Meeting(id string) (map[string]any, bool) {
	for _, m := range demoMeetings() {
		if m["meeting_id"] == id {
			return map[string]any{"data": "demonstration", "disclaimer": disclaimer, "meeting": m}, true
		}
	}
	return nil, false
}

func demoMeetings() []map[string]any {
	return []map[string]any{
		{
			"meeting_id": "mtg-0001", "proposal_id": "prop-manifest-v1",
			"title": "Adopt signed Workload Manifest v1", "risk_class": "security_critical",
			"state": "SEALED", "reviewers": 10, "decision_id": "dec-0001",
			"vote_state":        map[string]int{"approve": 9, "conditional": 1, "block": 0, "missing": 0},
			"protected_dissent": 1,
		},
		{
			"meeting_id": "mtg-0002", "proposal_id": "prop-spot-tier",
			"title": "Enable the spot service class", "risk_class": "high",
			"state": "SEALED", "reviewers": 5, "decision_id": "dec-0002",
			"vote_state":        map[string]int{"approve": 3, "conditional": 2, "block": 0, "missing": 0},
			"protected_dissent": 0,
		},
	}
}

// --- hashing helpers (mirror publication.py canonical form) ---

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func recordHash(v any) string {
	sum := sha256.Sum256(compactSortedJSON(v))
	return hex.EncodeToString(sum[:])
}

// compactSortedJSON marshals with sorted keys and no whitespace, matching
// Python's json.dumps(..., sort_keys=True, separators=(",",":")).
func compactSortedJSON(v any) []byte {
	b, _ := json.Marshal(v) // Go sorts map keys; struct field order is deterministic
	var canon any
	_ = json.Unmarshal(b, &canon)
	out, _ := json.Marshal(sortValue(canon))
	return out
}

func sortValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m := make(map[string]any, len(t))
		for _, k := range keys {
			m[k] = sortValue(t[k])
		}
		return m
	case []any:
		for i := range t {
			t[i] = sortValue(t[i])
		}
		return t
	default:
		return v
	}
}

func canonicalDecision(d PublicDecision) any {
	d.RecordHash = "" // hash covers everything except record_hash itself
	b, _ := json.Marshal(d)
	var m any
	_ = json.Unmarshal(b, &m)
	return m
}

func verifyChain(recs []PublicDecision) bool {
	prev := ""
	for _, r := range recs {
		if r.PreviousRecordHash != prev {
			return false
		}
		if recordHash(canonicalDecision(r)) != r.RecordHash {
			return false
		}
		prev = r.RecordHash
	}
	return true
}
