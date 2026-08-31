package council

import (
	"os"
	"testing"
)

// The embedded constitution must stay byte-identical to the canonical copy under
// security/. `cp` it when you change one.
func TestConstitutionCopyInSync(t *testing.T) {
	canon, err := os.ReadFile("../../../security/constitution/AYNI_CONSTITUTION.md")
	if err != nil {
		t.Skip("canonical constitution not found from this checkout")
	}
	if string(canon) != constitutionMarkdown {
		t.Fatal("coordinator/internal/council/constitution.md is out of sync with security/constitution/AYNI_CONSTITUTION.md")
	}
}

func TestRosterMeetsQualificationRules(t *testing.T) {
	r := Roster()
	if r["seat_count"].(int) != 10 {
		t.Fatalf("seat_count = %v", r["seat_count"])
	}
	if r["providers"].(int) < 5 {
		t.Fatalf("providers = %v, want >= 5", r["providers"])
	}
	if r["open_weight"].(int) < 2 {
		t.Fatalf("open_weight = %v, want >= 2", r["open_weight"])
	}
	seats := r["seats"].([]Seat)
	perProvider := map[string]int{}
	for _, s := range seats {
		perProvider[s.Provider]++
	}
	for p, n := range perProvider {
		if n > 2 {
			t.Fatalf("provider %s holds %d seats (max 2)", p, n)
		}
	}
}

func TestDecisionChainVerifies(t *testing.T) {
	d := Decisions()
	if !d["chain_valid"].(bool) {
		t.Fatal("public decision hash chain does not verify")
	}
	recs := demoDecisions()
	// tamper -> chain must break
	recs[0].Title = "changed"
	if verifyChain(recs) {
		t.Fatal("tampered decision still verified")
	}
}

func TestConstitutionPrinciplesExtracted(t *testing.T) {
	p := principles()
	if len(p) != 10 {
		t.Fatalf("extracted %d principles, want 10: %v", len(p), p)
	}
}
