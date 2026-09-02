package council

import (
	"context"
	"errors"
	"testing"
)

type fixedReviewer struct {
	seat, role string
	rev        Review
	err        error
}

func (f fixedReviewer) Seat() string { return f.seat }
func (f fixedReviewer) Role() string { return f.role }
func (f fixedReviewer) Review(context.Context, Proposal) (Review, error) {
	return f.rev, f.err
}

func approver(seat string) Reviewer {
	return fixedReviewer{seat: seat, role: seat, rev: Review{Decision: Approve, Severity: Low, Confidence: 1}}
}
func blocker(seat string, sev Severity) Reviewer {
	return fixedReviewer{seat: seat, role: seat, rev: Review{Decision: Block, Severity: sev, Confidence: 1}}
}
func errorer(seat string) Reviewer {
	return fixedReviewer{seat: seat, role: seat, err: errors.New("model unreachable")}
}

func normalProposal() Proposal {
	return Proposal{ID: "p1", Title: "t", RiskClass: RiskNormal, Facts: map[string]any{}}
}

func TestEvaluateApprovesWhenAllApprove(t *testing.T) {
	o := Evaluate(context.Background(), normalProposal(),
		[]Reviewer{approver("a"), approver("b"), approver("c")}, DefaultPolicy())
	if o.Decision != Approve {
		t.Fatalf("decision = %s, want APPROVE", o.Decision)
	}
	if len(o.Reviews) != 3 || len(o.Missing) != 0 {
		t.Fatalf("reviews=%d missing=%d", len(o.Reviews), len(o.Missing))
	}
	if o.AuditHash == "" {
		t.Fatal("missing audit hash")
	}
}

func TestEvaluateFailsClosedOnMissingVote(t *testing.T) {
	o := Evaluate(context.Background(), normalProposal(),
		[]Reviewer{approver("a"), approver("b"), errorer("c")}, DefaultPolicy())
	if o.Decision != Block {
		t.Fatalf("a missing vote must BLOCK, got %s", o.Decision)
	}
	if len(o.Missing) != 1 {
		t.Fatalf("missing = %v", o.Missing)
	}
}

func TestEvaluateBlocksOnTwoCriticalObjections(t *testing.T) {
	o := Evaluate(context.Background(), normalProposal(),
		[]Reviewer{blocker("a", Critical), blocker("b", Critical), approver("c")}, DefaultPolicy())
	if o.Decision != Block {
		t.Fatalf("two CRITICAL blocks must BLOCK, got %s", o.Decision)
	}
}

func TestEvaluateConditionalOnSingleNonApprove(t *testing.T) {
	o := Evaluate(context.Background(), normalProposal(),
		[]Reviewer{approver("a"), approver("b"),
			fixedReviewer{seat: "c", role: "c", rev: Review{Decision: Conditional, Severity: Medium}}},
		DefaultPolicy())
	if o.Decision != Conditional {
		t.Fatalf("decision = %s, want CONDITIONAL", o.Decision)
	}
	if len(o.Dissent) != 1 || o.Dissent[0].Seat != "c" {
		t.Fatalf("dissent not captured: %+v", o.Dissent)
	}
}

func TestEvaluateUsesAllAvailableWhenFewerThanRequired(t *testing.T) {
	// NORMAL wants 3; only 2 configured -> run 2, both must respond.
	o := Evaluate(context.Background(), normalProposal(),
		[]Reviewer{approver("a"), approver("b")}, DefaultPolicy())
	if o.Decision != Approve || len(o.Reviews) != 2 {
		t.Fatalf("decision=%s reviews=%d", o.Decision, len(o.Reviews))
	}
}

func TestEvaluateSelectionIsStable(t *testing.T) {
	rs := []Reviewer{approver("a"), approver("b"), approver("c"), approver("d"), approver("e")}
	p := normalProposal() // NORMAL -> 3 of 5
	a := Evaluate(context.Background(), p, rs, DefaultPolicy())
	b := Evaluate(context.Background(), p, rs, DefaultPolicy())
	if a.AuditHash != b.AuditHash {
		t.Fatal("seat selection / audit hash is not deterministic for the same proposal")
	}
}

func TestForWorkloadRiskClassification(t *testing.T) {
	normal := ForWorkload("m", "MICRO", "community", 100, 1000, 2000, 1, false, 0.05, 120)
	if normal.RiskClass != RiskNormal {
		t.Fatalf("small MICRO batch should be normal, got %s", normal.RiskClass)
	}
	big := ForWorkload("m", "MICRO", "community", 5000, 1_000_000, 500_000, 1, false, 3, 9000)
	if big.RiskClass != RiskHigh {
		t.Fatalf("5000-item batch should be high, got %s", big.RiskClass)
	}
	unknown := ForWorkload("m", "", "community", 10, 10, 10, 1, false, 0.01, 30)
	if unknown.RiskClass != RiskHigh {
		t.Fatalf("unknown model class should be high, got %s", unknown.RiskClass)
	}
	// facts must not carry anything resembling prompt content
	for k := range normal.Facts {
		switch k {
		case "prompt", "completion", "content", "messages", "text":
			t.Fatalf("proposal facts leaked a content-ish key: %s", k)
		}
	}
}

func TestDeterministicReviewerBounds(t *testing.T) {
	d := NewDeterministicReviewer("security_critic", "Chief Security Critic")
	ok, _ := d.Review(context.Background(), Proposal{RiskClass: RiskNormal, Facts: map[string]any{
		"items": 10, "redundancy": 1, "completion_tokens": 100, "prompt_tokens": 50,
	}})
	if ok.Decision != Approve {
		t.Fatalf("ordinary batch should approve, got %s", ok.Decision)
	}
	bad, _ := d.Review(context.Background(), Proposal{RiskClass: RiskNormal, Facts: map[string]any{
		"items": 99999, "redundancy": 1, "completion_tokens": 1, "prompt_tokens": 1,
	}})
	if bad.Decision != Block {
		t.Fatalf("out-of-bounds item count should block, got %s", bad.Decision)
	}
}

func TestParseReviewStrict(t *testing.T) {
	good := `{"decision":"APPROVE","severity":"LOW","confidence":0.8,"attack_scenarios":[],"required_controls":[]}`
	r, err := parseReview("here is my review: " + good + " thanks")
	if err != nil || r.Decision != Approve || r.Severity != Low {
		t.Fatalf("good review rejected: %v %+v", err, r)
	}
	for _, bad := range []string{
		`not json`,
		`{"decision":"MAYBE","severity":"LOW","confidence":0.5,"attack_scenarios":[],"required_controls":[]}`,
		`{"severity":"LOW"}`,
	} {
		if _, err := parseReview(bad); err == nil {
			t.Fatalf("malformed review accepted: %q", bad)
		}
	}
}

func TestReviewRunFindings(t *testing.T) {
	clean := ReviewRun(context.Background(), RunStats{WorkloadID: "w1", Model: "m", Items: 4, OK: 4, Failed: 0, Fanout: 2, WallMS: 400, QuotedUSD: 0.10, ChargedUSD: 0.10})
	if clean.Verdict != "CLEAN" || len(clean.Findings) != 0 {
		t.Fatalf("clean run flagged: %+v", clean.Findings)
	}
	bad := ReviewRun(context.Background(), RunStats{WorkloadID: "w2", Model: "m", Items: 10, OK: 6, Failed: 4, Fanout: 1, WallMS: 60000, QuotedUSD: 0.10, ChargedUSD: 0.20})
	if bad.Verdict != "REVIEW" {
		t.Fatalf("high failure + overcharge should be REVIEW, got %s", bad.Verdict)
	}
	var hasOvercharge bool
	for _, f := range bad.Findings {
		if f.Severity == Medium && contains2(f.Issue, "exceeded the quote") {
			hasOvercharge = true
		}
	}
	if !hasOvercharge {
		t.Fatalf("overcharge finding missing: %+v", bad.Findings)
	}
}

func contains2(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(s) > 0 && indexOf(s, sub) >= 0))
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
