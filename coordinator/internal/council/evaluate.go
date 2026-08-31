package council

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Deterministic, fail-closed Council policy for workload review — the Go
// counterpart of security/ayni_security/council.py. Models advise; this code
// owns the gate. A model cannot deploy, move money, or run a job through it.

type Verdict string

const (
	Approve     Verdict = "APPROVE"
	Conditional Verdict = "CONDITIONAL"
	Block       Verdict = "BLOCK"
)

type Severity string

const (
	Low      Severity = "LOW"
	Medium   Severity = "MEDIUM"
	High     Severity = "HIGH"
	Critical Severity = "CRITICAL"
)

// Risk classes.
const (
	RiskNormal   = "normal"
	RiskHigh     = "high"
	RiskCritical = "security_critical"
)

// Proposal is a review request built from STRUCTURED FACTS ONLY — never prompt or
// completion text.
type Proposal struct {
	ID           string
	Title        string
	RiskClass    string
	Facts        map[string]any
	SourceDigest string
}

// Review is one seat's structured position.
type Review struct {
	Seat             string   `json:"seat"`
	Role             string   `json:"role"`
	Decision         Verdict  `json:"decision"`
	Severity         Severity `json:"severity"`
	Confidence       float64  `json:"confidence"`
	AttackScenarios  []string `json:"attack_scenarios,omitempty"`
	RequiredControls []string `json:"required_controls,omitempty"`
	Note             string   `json:"note,omitempty"`
}

// Outcome is the deterministic verdict.
type Outcome struct {
	ProposalID       string    `json:"proposal_id"`
	RiskClass        string    `json:"risk_class"`
	Decision         Verdict   `json:"decision"`
	Reviews          []Review  `json:"reviews"`
	RequiredControls []string  `json:"required_controls"`
	Dissent          []Review  `json:"dissent"`
	Missing          []string  `json:"missing_seats"`
	AuditHash        string    `json:"audit_hash"`
	EvaluatedAt      time.Time `json:"evaluated_at"`
}

// Acceptable reports whether a workload with this outcome may be accepted/run.
func (o *Outcome) Acceptable() bool {
	return o != nil && o.Decision != Block
}

// Reviewer produces one seat's Review, or an error (which counts as a missing
// vote and fails the policy closed).
type Reviewer interface {
	Seat() string
	Role() string
	Review(ctx context.Context, p Proposal) (Review, error)
}

// Policy is the reviewer count per risk class + the block thresholds.
type Policy struct {
	Normal, High, Critical int
	CriticalBlocks         int
	HighBlocks             int
}

// DefaultPolicy mirrors council.py's CouncilPolicy.
func DefaultPolicy() Policy {
	return Policy{Normal: 3, High: 5, Critical: 10, CriticalBlocks: 2, HighBlocks: 2}
}

func (p Policy) count(risk string) int {
	switch risk {
	case RiskCritical:
		return p.Critical
	case RiskHigh:
		return p.High
	default:
		return p.Normal
	}
}

// Evaluate runs the selected reviewers and applies deterministic policy.
func Evaluate(ctx context.Context, p Proposal, reviewers []Reviewer, pol Policy) Outcome {
	want := pol.count(p.RiskClass)
	if want > len(reviewers) {
		want = len(reviewers)
	}
	// Stable selection: order by hash(proposal_id + seat).
	seed := sha256.Sum256([]byte(p.ID))
	ranked := append([]Reviewer(nil), reviewers...)
	sort.SliceStable(ranked, func(i, j int) bool {
		hi := sha256.Sum256(append(seed[:], ranked[i].Seat()...))
		hj := sha256.Sum256(append(seed[:], ranked[j].Seat()...))
		return string(hi[:]) < string(hj[:])
	})
	selected := ranked[:want]

	var reviews []Review
	var missing []string
	for _, rv := range selected {
		out, err := rv.Review(ctx, p)
		if err != nil {
			missing = append(missing, rv.Seat())
			continue
		}
		out.Seat, out.Role = rv.Seat(), rv.Role()
		if out.Decision == "" {
			out.Decision = Conditional
		}
		reviews = append(reviews, out)
	}

	var blocks, criticalBlocks, highBlocks int
	for _, r := range reviews {
		if r.Decision != Block {
			continue
		}
		blocks++
		if r.Severity == Critical {
			criticalBlocks++
		}
		if r.Severity == High || r.Severity == Critical {
			highBlocks++
		}
	}

	decision := Approve
	switch {
	case len(missing) > 0:
		decision = Block
	case criticalBlocks >= pol.CriticalBlocks:
		decision = Block
	case highBlocks >= pol.HighBlocks:
		decision = Block
	default:
		for _, r := range reviews {
			if r.Decision != Approve {
				decision = Conditional
				break
			}
		}
	}

	controls := map[string]struct{}{}
	for _, r := range reviews {
		for _, c := range r.RequiredControls {
			controls[c] = struct{}{}
		}
	}
	reqControls := make([]string, 0, len(controls))
	for c := range controls {
		reqControls = append(reqControls, c)
	}
	sort.Strings(reqControls)

	// Dissent: any review whose decision is not the plurality.
	counts := map[Verdict]int{}
	for _, r := range reviews {
		counts[r.Decision]++
	}
	plurality := Approve
	for d, n := range counts {
		if n > counts[plurality] {
			plurality = d
		}
	}
	var dissent []Review
	for _, r := range reviews {
		if r.Decision != plurality {
			dissent = append(dissent, r)
		}
	}

	o := Outcome{
		ProposalID: p.ID, RiskClass: p.RiskClass, Decision: decision,
		Reviews: reviews, RequiredControls: reqControls, Dissent: dissent,
		Missing: missing, EvaluatedAt: time.Now().UTC(),
	}
	o.AuditHash = recordHash(map[string]any{
		"proposal_id": p.ID, "source_digest": p.SourceDigest, "risk_class": p.RiskClass,
		"decision": decision, "selected": seatNames(selected), "missing": missing,
		"required_controls": reqControls,
	})
	return o
}

func seatNames(rs []Reviewer) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Seat()
	}
	return out
}

// --- workload proposal ---

// ForWorkload builds a review proposal from a quote's structured facts.
func ForWorkload(model, modelClass, tier string, items int, promptTok, complTok int64, redundancy int, spot bool, priceUSD float64, etaSec int64) Proposal {
	risk := RiskNormal
	if items > 2000 || redundancy >= 3 || modelClass == "" {
		risk = RiskHigh
	}
	facts := map[string]any{
		"model": model, "model_class": modelClass, "tier": tier,
		"items": items, "prompt_tokens": promptTok, "completion_tokens": complTok,
		"redundancy": redundancy, "spot": spot,
		"price_usd": priceUSD, "eta_seconds": etaSec,
	}
	fb, _ := json.Marshal(facts)
	return Proposal{
		ID:           "wl-review-" + shortHash(fmt.Sprintf("%s|%d|%d|%d", model, items, complTok, time.Now().UnixNano())),
		Title:        fmt.Sprintf("Workload: %d× %s inference (%s tier)", items, model, tier),
		RiskClass:    risk,
		Facts:        facts,
		SourceDigest: recordHash(json.RawMessage(fb)),
	}
}

// --- deterministic reviewer ---

// DeterministicReviewer approves ordinary workloads and flags structural red
// flags. It is the always-available backstop so the Council never depends on an
// external model being reachable.
type DeterministicReviewer struct{ seat, role string }

func NewDeterministicReviewer(seat, role string) *DeterministicReviewer {
	return &DeterministicReviewer{seat: seat, role: role}
}
func (d *DeterministicReviewer) Seat() string { return d.seat }
func (d *DeterministicReviewer) Role() string { return d.role }

func (d *DeterministicReviewer) Review(_ context.Context, p Proposal) (Review, error) {
	items, _ := toInt(p.Facts["items"])
	red, _ := toInt(p.Facts["redundancy"])
	ct, _ := toInt(p.Facts["completion_tokens"])
	pt, _ := toInt(p.Facts["prompt_tokens"])

	if items <= 0 || ct < 0 || pt < 0 || red < 1 || red > 3 || items > 5000 {
		return Review{
			Decision: Block, Severity: High, Confidence: 1,
			AttackScenarios:  []string{"malformed or out-of-bounds workload parameters"},
			RequiredControls: []string{"reject workloads outside item/redundancy bounds"},
			Note:             "structural bounds check failed",
		}, nil
	}
	rv := Review{Decision: Approve, Severity: Low, Confidence: 0.9,
		Note: "ordinary allow-listed inference batch; no prompt content in scope"}
	if p.RiskClass == RiskHigh {
		rv.Severity = Medium
		rv.RequiredControls = []string{"operator visibility on high-volume / high-redundancy workloads"}
	}
	return rv, nil
}

// --- model reviewer ---

// ModelReviewer is one Council seat backed by a real model API. A malformed or
// unreachable response returns an error → the vote is missing → fail closed.
type ModelReviewer struct {
	seat, role string
	api        string // "anthropic" | "openai"
	key        string
	modelID    string
	hc         *http.Client
}

// NewModelReviewer returns nil if api/key/model are not all set.
func NewModelReviewer(seat, role, api, key, modelID string) *ModelReviewer {
	if api == "" || key == "" || modelID == "" {
		return nil
	}
	return &ModelReviewer{seat: seat, role: role, api: api, key: key, modelID: modelID,
		hc: &http.Client{Timeout: 30 * time.Second}}
}
func (m *ModelReviewer) Seat() string { return m.seat }
func (m *ModelReviewer) Role() string { return m.role }

const reviewInstruction = `You are one seat on the Ayni Council, reviewing a compute-marketplace workload.
You receive STRUCTURED FACTS ONLY — never prompt or completion text.
Decide whether Ayni should quote and run this workload.
Respond with ONLY a compact JSON object, no prose, with exactly these keys:
{"decision":"APPROVE|CONDITIONAL|BLOCK","severity":"LOW|MEDIUM|HIGH|CRITICAL",
 "confidence":0..1,"attack_scenarios":[],"required_controls":[]}
BLOCK only for a concrete abuse or safety concern you can name.`

func (m *ModelReviewer) Review(ctx context.Context, p Proposal) (Review, error) {
	facts, _ := json.Marshal(p.Facts)
	prompt := reviewInstruction + "\n\nseat: " + m.seat + "\nrole: " + m.role +
		"\nrisk_class: " + p.RiskClass + "\nfacts: " + string(facts)

	var raw string
	var err error
	switch m.api {
	case "anthropic":
		raw, err = m.callAnthropic(ctx, prompt)
	case "openai":
		raw, err = m.callOpenAI(ctx, prompt)
	default:
		return Review{}, fmt.Errorf("unknown council model api %q", m.api)
	}
	if err != nil {
		return Review{}, err
	}
	return parseReview(raw)
}

func parseReview(s string) (Review, error) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '{'); i > 0 {
		s = s[i:]
	}
	if i := strings.LastIndexByte(s, '}'); i >= 0 && i < len(s)-1 {
		s = s[:i+1]
	}
	var d struct {
		Decision         string   `json:"decision"`
		Severity         string   `json:"severity"`
		Confidence       float64  `json:"confidence"`
		AttackScenarios  []string `json:"attack_scenarios"`
		RequiredControls []string `json:"required_controls"`
	}
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		return Review{}, fmt.Errorf("council model response not valid JSON: %w", err)
	}
	dec := Verdict(strings.ToUpper(d.Decision))
	sev := Severity(strings.ToUpper(d.Severity))
	if dec != Approve && dec != Conditional && dec != Block {
		return Review{}, fmt.Errorf("council model response bad decision %q", d.Decision)
	}
	if sev != Low && sev != Medium && sev != High && sev != Critical {
		sev = Medium
	}
	if d.Confidence < 0 || d.Confidence > 1 {
		d.Confidence = 0.5
	}
	if len(d.AttackScenarios) > 20 || len(d.RequiredControls) > 20 {
		return Review{}, fmt.Errorf("council model response oversized")
	}
	return Review{
		Decision: dec, Severity: sev, Confidence: d.Confidence,
		AttackScenarios: d.AttackScenarios, RequiredControls: d.RequiredControls,
	}, nil
}

func (m *ModelReviewer) callAnthropic(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model": m.modelID, "max_tokens": 400,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(string(body)))
	req.Header.Set("x-api-key", m.key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	res, err := m.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("anthropic %d: %s", res.StatusCode, string(b))
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(b, &out); err != nil || len(out.Content) == 0 {
		return "", fmt.Errorf("anthropic: unexpected response")
	}
	return out.Content[0].Text, nil
}

func (m *ModelReviewer) callOpenAI(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model": m.modelID, "max_tokens": 400,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+m.key)
	req.Header.Set("Content-Type", "application/json")
	res, err := m.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("openai %d: %s", res.StatusCode, string(b))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: unexpected response")
	}
	return out.Choices[0].Message.Content, nil
}

// --- recording real decisions into the public ledger ---

var (
	recentMu sync.Mutex
	recent   []PublicDecision
)

// RecordWorkloadDecision appends a real Council outcome to the public ledger so
// it shows on /v1/council/decisions alongside the seeded demo records.
func RecordWorkloadDecision(o Outcome, title string) {
	rd, _ := Roster()["selection_digest"].(string)
	votes := map[string]int{"approve": 0, "conditional": 0, "block": 0, "missing": len(o.Missing)}
	for _, r := range o.Reviews {
		switch r.Decision {
		case Approve:
			votes["approve"]++
		case Conditional:
			votes["conditional"]++
		case Block:
			votes["block"]++
		}
	}
	sev := Low
	for _, r := range o.Reviews {
		if sevRank(r.Severity) > sevRank(sev) {
			sev = r.Severity
		}
	}
	dis := make([]map[string]any, 0, len(o.Dissent))
	for _, r := range o.Dissent {
		dis = append(dis, map[string]any{
			"seat": r.Seat, "decision": string(r.Decision), "severity": string(r.Severity),
			"summary": r.Note,
		})
	}
	rec := PublicDecision{
		Protocol:   PublicRecordProtocol,
		DecisionID: "wl-" + shortHash(o.AuditHash), ProposalID: o.ProposalID,
		Title: title, RiskClass: o.RiskClass, Decision: string(o.Decision), Severity: string(sev),
		Votes: votes, RequiredControls: o.RequiredControls, Dissent: dis,
		Actions: []map[string]any{}, EvidenceDigests: []string{o.AuditHash},
		RosterDigest: rd, ConstitutionDigest: ConstitutionDigest(),
		PublishedAt: o.EvaluatedAt.Unix(),
	}

	recentMu.Lock()
	defer recentMu.Unlock()
	if len(recent) > 0 {
		rec.PreviousRecordHash = recent[len(recent)-1].RecordHash
	} else {
		rec.PreviousRecordHash = lastDemoHash()
	}
	rec.RecordHash = recordHash(canonicalDecision(rec))
	recent = append(recent, rec)
	if len(recent) > 50 {
		recent = recent[len(recent)-50:]
	}
}

// --- post-run review ("Council reviews logs for improvements") ---

// RunStats is the structured, content-free outcome of an executed workload.
type RunStats struct {
	WorkloadID            string
	Model                 string
	Items, OK, Failed     int
	Fanout, Concurrency   int
	WallMS                int64
	PromptTokens          int
	CompletionTokens      int
	QuotedUSD, ChargedUSD float64
}

// Finding is one improvement recommendation.
type Finding struct {
	Severity Severity `json:"severity"`
	Issue    string   `json:"issue"`
	Action   string   `json:"recommended_action"`
}

// RunReview is what the Council posts after a workload finishes.
type RunReview struct {
	WorkloadID string    `json:"workload_id"`
	Model      string    `json:"model"`
	Verdict    string    `json:"verdict"` // CLEAN | REVIEW
	Findings   []Finding `json:"findings"`
	Stats      RunStats  `json:"stats"`
	AuditHash  string    `json:"audit_hash"`
	RecordedAt time.Time `json:"recorded_at"`
}

var (
	runsMu     sync.Mutex
	runReviews []RunReview
)

// ReviewRun applies deterministic heuristics to an executed workload's stats and
// records the review. No prompt or completion text is involved.
func ReviewRun(s RunStats) RunReview {
	f := []Finding{}
	total := s.OK + s.Failed
	if s.Failed > 0 {
		sev := Medium
		if total > 0 && float64(s.Failed)/float64(total) > 0.1 {
			sev = High
		}
		f = append(f, Finding{sev,
			fmt.Sprintf("%d of %d items failed", s.Failed, total),
			"raise redundancy or add speculative straggler re-dispatch for this model class"})
	}
	if s.Fanout <= 1 && s.OK > 4 {
		f = append(f, Finding{Low,
			"the whole batch ran on a single provider",
			"recruit more supply so items run in parallel and wall time drops"})
	}
	if total > 0 && s.WallMS/int64(total) > 4000 {
		f = append(f, Finding{Low,
			fmt.Sprintf("~%d ms per item", s.WallMS/int64(total)),
			"offer a faster model class or GPU-class providers for latency-sensitive callers"})
	}
	if s.ChargedUSD > s.QuotedUSD*1.001 {
		f = append(f, Finding{Medium,
			fmt.Sprintf("charged $%.4f exceeded the quote $%.4f", s.ChargedUSD, s.QuotedUSD),
			"investigate the settlement path — the customer must never pay above the accepted quote"})
	}

	verdict := "CLEAN"
	for _, x := range f {
		if x.Severity == High || x.Severity == Critical {
			verdict = "REVIEW"
		}
	}
	rr := RunReview{
		WorkloadID: s.WorkloadID, Model: s.Model, Verdict: verdict,
		Findings: f, Stats: s, RecordedAt: time.Now().UTC(),
	}
	rr.AuditHash = recordHash(map[string]any{
		"workload_id": s.WorkloadID, "verdict": verdict, "findings": len(f),
		"ok": s.OK, "failed": s.Failed, "fanout": s.Fanout, "wall_ms": s.WallMS,
	})

	runsMu.Lock()
	runReviews = append(runReviews, rr)
	if len(runReviews) > 50 {
		runReviews = runReviews[len(runReviews)-50:]
	}
	runsMu.Unlock()
	return rr
}

// RunReviews returns the recorded post-run reviews (newest last).
func RunReviews() map[string]any {
	runsMu.Lock()
	out := append([]RunReview(nil), runReviews...)
	runsMu.Unlock()
	return map[string]any{"data": "live", "count": len(out), "run_reviews": out}
}

func lastDemoHash() string {
	d := demoDecisions()
	if len(d) == 0 {
		return ""
	}
	return d[len(d)-1].RecordHash
}

func sevRank(s Severity) int {
	switch s {
	case Critical:
		return 3
	case High:
		return 2
	case Medium:
		return 1
	default:
		return 0
	}
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}
