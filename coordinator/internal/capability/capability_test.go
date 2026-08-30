package capability

import "testing"

func TestACUAnchor(t *testing.T) {
	mid := Fingerprint{
		SustainedEndTPS: 14, MemBandwidthGBps: 12, CPUCores: 8, AvailableRAMMB: 6000,
	}
	if acu := ACU(mid); acu < 0.95 || acu > 1.05 {
		t.Fatalf("mid-phone ACU = %.3f, want ~1.0", acu)
	}
	if c := Class(mid); c != "M3" {
		t.Fatalf("mid-phone class = %s, want M3", c)
	}
}

func TestACUScales(t *testing.T) {
	slow := Fingerprint{SustainedEndTPS: 4, MemBandwidthGBps: 6, CPUCores: 6, AvailableRAMMB: 3000}
	fast := Fingerprint{SustainedEndTPS: 45, MemBandwidthGBps: 40, CPUCores: 16, AvailableRAMMB: 24000}
	if ACU(slow) >= ACU(fast) {
		t.Fatalf("slow %.2f should be < fast %.2f", ACU(slow), ACU(fast))
	}
	if Class(slow) != "M1" {
		t.Errorf("slow class = %s, want M1", Class(slow))
	}
	if Class(fast) != "M5" {
		t.Errorf("fast class = %s, want M5", Class(fast))
	}
}

func TestThermalDecay(t *testing.T) {
	fp := Fingerprint{SustainedStartTPS: 20, SustainedEndTPS: 15}
	if d := ThermalDecayPct(fp); d != 25 {
		t.Fatalf("decay = %.1f, want 25", d)
	}
}

func TestACUFallbackToDecodeTPS(t *testing.T) {
	// No sustained run recorded — fall back to decode_tps.
	fp := Fingerprint{DecodeTPS: 14, MemBandwidthGBps: 12, CPUCores: 8, AvailableRAMMB: 6000}
	if acu := ACU(fp); acu < 0.95 || acu > 1.05 {
		t.Fatalf("fallback ACU = %.3f, want ~1.0", acu)
	}
}
