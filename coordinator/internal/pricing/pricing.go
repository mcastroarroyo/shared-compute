// Package pricing turns a completed job into a consumer charge and a provider
// accrual. Numbers here are the launch placeholders from docs/PAYMENTS.md — revisit
// with real supply/demand data before enabling payouts. Everything is integer
// micro-USD (1e-6 USD); never float money.
package pricing

import "strings"

// Quote is the money outcome of one job.
type Quote struct {
	GrossMicros    int64 // what the consumer is charged
	ProviderMicros int64 // what the provider accrues (before payout fees)
	ModelClass     string
	Tier           string
}

// classRate is per-1,000,000-token pricing in micro-USD, by model hardware class.
type classRate struct{ inPerM, outPerM int64 }

var rateCard = map[string]classRate{
	"MICRO":            {inPerM: 20_000, outPerM: 80_000},
	"SMALL":            {inPerM: 50_000, outPerM: 200_000},
	"MEDIUM":           {inPerM: 150_000, outPerM: 600_000},
	"LARGE":            {inPerM: 400_000, outPerM: 1_600_000},
	"XL":               {inPerM: 900_000, outPerM: 3_600_000},
	"CONFIDENTIAL_GPU": {inPerM: 900_000, outPerM: 3_600_000},
}

// tierMultiplier scales the consumer price when a higher trust tier is served.
func tierMultiplier(tier string) float64 {
	switch strings.ToLower(tier) {
	case "device_attested":
		return 1.4
	case "confidential":
		return 3.0
	default: // community
		return 1.0
	}
}

// revShare is the provider's cut of gross. Confidential keeps more on the platform
// (extra infra), everything else is 70/30.
func revShare(tier string) float64 {
	if strings.EqualFold(tier, "confidential") {
		return 0.60
	}
	return 0.70
}

// QuoteJob prices a job. priceMult scales the whole rate card (1.0 = card as-is;
// a lever for tuning and for exercising payouts on tiny models). qualityMult
// (typically 1.0) is applied to the provider accrual only.
func QuoteJob(modelClass, tier string, promptTokens, completionTokens int, priceMult, qualityMult float64) Quote {
	class := strings.ToUpper(strings.TrimSpace(modelClass))
	r, ok := rateCard[class]
	if !ok {
		r = rateCard["SMALL"] // unknown class: charge as SMALL rather than free
		class = "SMALL"
	}
	if priceMult <= 0 {
		priceMult = 1.0
	}
	inPerM := int64(float64(r.inPerM) * priceMult)
	outPerM := int64(float64(r.outPerM) * priceMult)
	base := int64(promptTokens)*inPerM/1_000_000 + int64(completionTokens)*outPerM/1_000_000
	gross := int64(float64(base) * tierMultiplier(tier))
	if qualityMult <= 0 {
		qualityMult = 1.0
	}
	provider := int64(float64(gross) * revShare(tier) * qualityMult)
	return Quote{GrossMicros: gross, ProviderMicros: provider, ModelClass: class, Tier: tier}
}

// USD renders micro-USD as a dollars string for humans.
func USD(micros int64) float64 { return float64(micros) / 1_000_000 }
