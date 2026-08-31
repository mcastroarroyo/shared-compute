package pricing

import "testing"

func TestQuoteMicroCommunity(t *testing.T) {
	// 1000 prompt + 1000 completion tokens, MICRO, community.
	q := QuoteJob("MICRO", "community", 1000, 1000, 1.0, 1.0)
	// in: 1000 * 20000 / 1e6 = 20 ; out: 1000 * 80000 / 1e6 = 80 ; gross = 100
	if q.GrossMicros != 100 {
		t.Fatalf("gross = %d, want 100", q.GrossMicros)
	}
	if q.ProviderMicros != 70 { // 70% of 100
		t.Fatalf("provider = %d, want 70", q.ProviderMicros)
	}
}

func TestTierMultiplierRaisesGross(t *testing.T) {
	base := QuoteJob("SMALL", "community", 0, 1_000_000, 1.0, 1.0)
	att := QuoteJob("SMALL", "device_attested", 0, 1_000_000, 1.0, 1.0)
	if att.GrossMicros <= base.GrossMicros {
		t.Fatalf("attested gross %d should exceed community %d", att.GrossMicros, base.GrossMicros)
	}
	if att.GrossMicros != int64(float64(base.GrossMicros)*1.4) {
		t.Fatalf("attested gross = %d, want 1.4x %d", att.GrossMicros, base.GrossMicros)
	}
}

func TestUnknownClassPricedAsSmall(t *testing.T) {
	q := QuoteJob("BOGUS", "community", 0, 1_000_000, 1.0, 1.0)
	if q.ModelClass != "SMALL" || q.GrossMicros != 200_000 {
		t.Fatalf("unknown class: got class=%s gross=%d", q.ModelClass, q.GrossMicros)
	}
}

func TestConfidentialRevShareLower(t *testing.T) {
	q := QuoteJob("MEDIUM", "confidential", 0, 1_000_000, 1.0, 1.0)
	// gross = 600000 * 3.0 = 1_800_000 ; provider = 60% = 1_080_000
	if q.ProviderMicros != 1_080_000 {
		t.Fatalf("provider = %d, want 1_080_000", q.ProviderMicros)
	}
}

func TestQuoteWorkloadBuildsUpFromCompute(t *testing.T) {
	// 1M prompt + 1M completion, SMALL, community, redundancy 1.
	c := QuoteWorkload("SMALL", "community", 1_000_000, 1_000_000, 1, 0.30, 1.0)

	// provider earnings for that volume = QuoteJob provider share
	base := QuoteJob("SMALL", "community", 1_000_000, 1_000_000, 1.0, 1.0)
	if c.ComputeMicros != base.ProviderMicros {
		t.Fatalf("compute = %d, want provider share %d", c.ComputeMicros, base.ProviderMicros)
	}
	if c.CoordinationMicros != c.ComputeMicros*15/100 {
		t.Fatalf("coordination = %d, want 15%% of compute", c.CoordinationMicros)
	}
	want := c.ComputeMicros + c.CoordinationMicros + c.FailureMicros + c.MarginMicros + c.PaymentMicros
	if c.TotalMicros != want {
		t.Fatalf("total = %d, want sum of parts %d", c.TotalMicros, want)
	}
	// margin is 30% of (compute + coordination + failure)
	pre := c.ComputeMicros + c.CoordinationMicros + c.FailureMicros
	if c.MarginMicros != int64(float64(pre)*0.30) {
		t.Fatalf("margin = %d, want 30%% of %d", c.MarginMicros, pre)
	}
}

func TestQuoteWorkloadRedundancyDoublesComputeKeepsFailureFlat(t *testing.T) {
	one := QuoteWorkload("SMALL", "community", 500_000, 500_000, 1, 0.30, 1.0)
	two := QuoteWorkload("SMALL", "community", 500_000, 500_000, 2, 0.30, 1.0)
	if two.ComputeMicros != one.ComputeMicros*2 {
		t.Fatalf("redundancy 2 compute = %d, want 2x %d", two.ComputeMicros, one.ComputeMicros)
	}
	// failure = compute x (0.08 / redundancy): 0.08*C at r=1, 0.04*(2C) at r=2 -> same.
	if two.FailureMicros != one.FailureMicros {
		t.Fatalf("failure budget should stay flat under redundancy: one=%d two=%d",
			one.FailureMicros, two.FailureMicros)
	}
	if two.TotalMicros <= one.TotalMicros {
		t.Fatalf("redundancy 2 total %d should exceed redundancy 1 total %d", two.TotalMicros, one.TotalMicros)
	}
}

func TestQuoteWorkloadFloor(t *testing.T) {
	c := QuoteWorkload("MICRO", "community", 1, 1, 1, 0.30, 1.0)
	if c.TotalMicros != MinWorkloadMicros {
		t.Fatalf("tiny workload total = %d, want floor %d", c.TotalMicros, MinWorkloadMicros)
	}
}

func TestQuoteWorkloadSpotFactorScalesEverything(t *testing.T) {
	// Spot is modelled as a single rate-card multiplier (0.6): every component,
	// provider pay included, scales together.
	on := QuoteWorkload("SMALL", "community", 1_000_000, 1_000_000, 1, 0.30, 1.0)
	spot := QuoteWorkload("SMALL", "community", 1_000_000, 1_000_000, 1, 0.30, 0.6)

	approx := func(got, want int64) bool {
		d := got - want
		if d < 0 {
			d = -d
		}
		return d <= want/50+1 // within 2%
	}
	if !approx(spot.ComputeMicros, int64(float64(on.ComputeMicros)*0.6)) {
		t.Fatalf("spot compute %d, want ~60%% of %d", spot.ComputeMicros, on.ComputeMicros)
	}
	if !approx(spot.TotalMicros, int64(float64(on.TotalMicros)*0.6)) {
		t.Fatalf("spot total %d, want ~60%% of %d", spot.TotalMicros, on.TotalMicros)
	}
}
