package capability

import (
	"math"
	"testing"
)

// Sanitize bounds a self-reported benchmark to values that physically exist. It
// does NOT establish that the claim is true: nothing attests a provider's own
// benchmark, so a node can still claim to be hardware it is not. Measured ACU for
// real machines is ~1.0 for a mid phone, ~10 for an M3 Max laptop, ~64 for an H100
// and ~482 for an 8x H100, so no clamp here can separate a liar from a legitimate
// GPU node. Only throughput the coordinator observes on completed jobs can do that.
func TestSanitizeBoundsClaimsToPhysicalValues(t *testing.T) {
	got := Sanitize(Fingerprint{
		SustainedEndTPS: 1e12, MemBandwidthGBps: 1e12,
		CPUCores: 1 << 30, AvailableRAMMB: math.MaxUint64,
	})
	if got.SustainedEndTPS > maxTPS || got.MemBandwidthGBps > maxMemGBps ||
		got.CPUCores > maxCPUCores || got.AvailableRAMMB > maxRAMMB {
		t.Fatalf("absurd claim survived Sanitize: %+v", got)
	}
}

// AvailableRAMMB is stored as int64. An unclamped uint64 wraps negative.
func TestSanitizeKeepsRAMInsideInt64(t *testing.T) {
	got := Sanitize(Fingerprint{AvailableRAMMB: math.MaxUint64}).AvailableRAMMB
	if got > maxRAMMB {
		t.Fatalf("RAM not clamped: %d", got)
	}
	if int64(got) < 0 {
		t.Fatalf("clamped RAM still overflows int64: %d -> %d", got, int64(got))
	}
}

func TestSanitizeLeavesRealHardwareAlone(t *testing.T) {
	// A high-end workstation must pass through untouched.
	in := Fingerprint{
		PrefillTPS: 4200, DecodeTPS: 310, SustainedStartTPS: 300, SustainedEndTPS: 285,
		MemBandwidthGBps: 3350, CPUCores: 128, AvailableRAMMB: 512 * 1024,
	}
	if got := Sanitize(in); got != in {
		t.Fatalf("real hardware was clipped:\n got %+v\nwant %+v", got, in)
	}
}

func TestSanitizeRejectsNegativeAndNaN(t *testing.T) {
	got := Sanitize(Fingerprint{
		DecodeTPS: math.NaN(), MemBandwidthGBps: -5, CPUCores: -1,
	})
	if math.IsNaN(got.DecodeTPS) {
		t.Error("NaN survived Sanitize")
	}
	if got.MemBandwidthGBps < 0 || got.CPUCores < 0 {
		t.Errorf("negatives survived: %+v", got)
	}
	if a := ACU(got); math.IsNaN(a) {
		t.Error("ACU is NaN after Sanitize")
	}
}
