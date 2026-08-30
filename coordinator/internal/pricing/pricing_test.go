package pricing

import "testing"

func TestQuoteMicroCommunity(t *testing.T) {
	// 1000 prompt + 1000 completion tokens, MICRO, community.
	q := QuoteJob("MICRO", "community", 1000, 1000, 1.0)
	// in: 1000 * 20000 / 1e6 = 20 ; out: 1000 * 80000 / 1e6 = 80 ; gross = 100
	if q.GrossMicros != 100 {
		t.Fatalf("gross = %d, want 100", q.GrossMicros)
	}
	if q.ProviderMicros != 70 { // 70% of 100
		t.Fatalf("provider = %d, want 70", q.ProviderMicros)
	}
}

func TestTierMultiplierRaisesGross(t *testing.T) {
	base := QuoteJob("SMALL", "community", 0, 1_000_000, 1.0)
	att := QuoteJob("SMALL", "device_attested", 0, 1_000_000, 1.0)
	if att.GrossMicros <= base.GrossMicros {
		t.Fatalf("attested gross %d should exceed community %d", att.GrossMicros, base.GrossMicros)
	}
	if att.GrossMicros != int64(float64(base.GrossMicros)*1.4) {
		t.Fatalf("attested gross = %d, want 1.4x %d", att.GrossMicros, base.GrossMicros)
	}
}

func TestUnknownClassPricedAsSmall(t *testing.T) {
	q := QuoteJob("BOGUS", "community", 0, 1_000_000, 1.0)
	if q.ModelClass != "SMALL" || q.GrossMicros != 200_000 {
		t.Fatalf("unknown class: got class=%s gross=%d", q.ModelClass, q.GrossMicros)
	}
}

func TestConfidentialRevShareLower(t *testing.T) {
	q := QuoteJob("MEDIUM", "confidential", 0, 1_000_000, 1.0)
	// gross = 600000 * 3.0 = 1_800_000 ; provider = 60% = 1_080_000
	if q.ProviderMicros != 1_080_000 {
		t.Fatalf("provider = %d, want 1_080_000", q.ProviderMicros)
	}
}
