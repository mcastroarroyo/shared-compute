// Package capability turns a device self-benchmark into a normalized score the
// marketplace can compare and price against: the Ayni Compute Unit (ACU).
//
// ACU is derived from *measured sustained* performance, never advertised specs.
// It is anchored so a mid-range phone — ~14 sustained tok/s on a 3B Q4 model,
// ~12 GB/s memory bandwidth, 8 cores, ~6 GB usable RAM — scores ~1.0.
package capability

import "math"

// Fingerprint is the salient, comparable slice of a benchmark_report.
type Fingerprint struct {
	Model             string
	Backend           string
	PrefillTPS        float64
	DecodeTPS         float64
	SustainedStartTPS float64
	SustainedEndTPS   float64
	MemBandwidthGBps  float64
	AvailableRAMMB    uint64
	CPUCores          int
	ThermalState      string
}

// anchors: the reference mid-phone.
const (
	refSustainedTPS = 14.0
	refMemGBps      = 12.0
	refCores        = 8.0
	refRAMMB        = 6000.0
)

// ACU scores a fingerprint. ~1.0 is a mid-range phone; higher is faster.
func ACU(fp Fingerprint) float64 {
	tps := fp.SustainedEndTPS
	if tps <= 0 {
		tps = fp.DecodeTPS // older reports without a sustained run
	}
	cores := float64(fp.CPUCores)
	if cores <= 0 {
		cores = refCores
	}
	ram := float64(fp.AvailableRAMMB)
	if ram <= 0 {
		ram = refRAMMB
	}
	mem := fp.MemBandwidthGBps
	if mem <= 0 {
		mem = refMemGBps
	}

	// Sustained inference throughput dominates; the rest are secondary signals.
	acu := 0.70*(tps/refSustainedTPS) +
		0.15*(mem/refMemGBps) +
		0.08*(cores/refCores) +
		0.07*(ram/refRAMMB)

	return clamp(round2(acu), 0.02, 1000)
}

// ThermalDecayPct is how much sustained throughput fell from start to end of the
// benchmark run (0 = steady, higher = throttles under load).
func ThermalDecayPct(fp Fingerprint) float64 {
	if fp.SustainedStartTPS <= 0 || fp.SustainedEndTPS <= 0 {
		return 0
	}
	d := (fp.SustainedStartTPS - fp.SustainedEndTPS) / fp.SustainedStartTPS * 100
	return round2(clamp(d, 0, 100))
}

// Class is a coarse label. M* = mobile-class; D*/G* (desktop/GPU) land here once
// those providers report.
func Class(fp Fingerprint) string {
	acu := ACU(fp)
	switch {
	case acu < 0.4:
		return "M1"
	case acu < 0.8:
		return "M2"
	case acu < 1.5:
		return "M3"
	case acu < 3.0:
		return "M4"
	default:
		return "M5"
	}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func round2(v float64) float64        { return math.Round(v*100) / 100 }

// Round2 rounds to 2 decimals (exported for callers aggregating ACU).
func Round2(v float64) float64 { return round2(v) }

// Physical ceilings for a self-reported benchmark. A provider runs its own
// benchmark and sends us the numbers, so nothing here is attested: the report is
// a claim, not a measurement we witnessed.
//
// Be clear about what these bounds do and do not buy. They keep a report inside
// the range of things that physically exist, and they keep AvailableRAMMB inside
// int64 so storage cannot wrap negative. They do NOT establish that a claim is
// honest. Measured ACU is about 1.0 for a mid phone, 10 for an M3 Max laptop, 64
// for an H100 and 482 for an 8x H100, so any ceiling loose enough to admit a real
// GPU node is also loose enough to admit a phone pretending to be one. Separating
// those two needs throughput the coordinator observes on completed jobs, which we
// do not do yet. Until then, ACU is a claim that feeds quote estimates, fan-out
// weighting and the public capacity figure.
const (
	maxTPS      = 1e6
	maxMemGBps  = 1e4
	maxRAMMB    = 8 << 20 // 8 TiB
	maxCPUCores = 4096
)

// Sanitize clamps a self-reported fingerprint to physically plausible values.
// Call it on ingest, before the report is scored or stored, so that storage and
// scoring can never disagree about what the node claimed.
func Sanitize(fp Fingerprint) Fingerprint {
	// clamp propagates NaN, and JSON cannot carry NaN today, but Sanitize is the
	// boundary every report crosses: make it total rather than assume a transport.
	fp.PrefillTPS = zeroIfNaN(fp.PrefillTPS)
	fp.DecodeTPS = zeroIfNaN(fp.DecodeTPS)
	fp.SustainedStartTPS = zeroIfNaN(fp.SustainedStartTPS)
	fp.SustainedEndTPS = zeroIfNaN(fp.SustainedEndTPS)
	fp.MemBandwidthGBps = zeroIfNaN(fp.MemBandwidthGBps)

	fp.PrefillTPS = clamp(fp.PrefillTPS, 0, maxTPS)
	fp.DecodeTPS = clamp(fp.DecodeTPS, 0, maxTPS)
	fp.SustainedStartTPS = clamp(fp.SustainedStartTPS, 0, maxTPS)
	fp.SustainedEndTPS = clamp(fp.SustainedEndTPS, 0, maxTPS)
	fp.MemBandwidthGBps = clamp(fp.MemBandwidthGBps, 0, maxMemGBps)
	if fp.AvailableRAMMB > maxRAMMB {
		fp.AvailableRAMMB = maxRAMMB
	}
	if fp.CPUCores < 0 {
		fp.CPUCores = 0
	} else if fp.CPUCores > maxCPUCores {
		fp.CPUCores = maxCPUCores
	}
	return fp
}

func zeroIfNaN(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return v
}
