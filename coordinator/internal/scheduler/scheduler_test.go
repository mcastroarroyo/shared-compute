package scheduler

import (
	"testing"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
)

func mkProvider(id, model, tier, class string, memMB, maxCtx int) *registry.Provider {
	return &registry.Provider{
		ID:        id,
		TrustTier: tier,
		Capabilities: protocol.Capabilities{
			Models:        []string{model},
			HardwareClass: class,
			MaxContext:    maxCtx,
		},
		Telemetry: protocol.Telemetry{MemAvailableMB: memMB, ThermalState: "nominal"},
		Send:      func(any) error { return nil },
	}
}

func TestPickNoProvider(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", "model-x", "community", "SMALL", 1000, 8192))
	if _, err := Pick(reg, Requirements{Model: "model-y"}); err != ErrNoProvider {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestPickLeastLoaded(t *testing.T) {
	reg := registry.New()
	busy := mkProvider("busy", "m", "community", "SMALL", 8000, 8192)
	busy.AcquireSlot()
	busy.AcquireSlot()
	idle := mkProvider("idle", "m", "community", "SMALL", 4000, 8192)
	idle.AcquireSlot()
	reg.Add(busy)
	reg.Add(idle)

	got, err := Pick(reg, Requirements{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "idle" {
		t.Fatalf("want idle, got %s", got.ID)
	}
}

func TestPickTierUnmet(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", "m", "community", "SMALL", 1000, 8192))
	if _, err := Pick(reg, Requirements{Model: "m", MinTier: "confidential"}); err != ErrTierUnmet {
		t.Fatalf("want ErrTierUnmet, got %v", err)
	}
}

func TestPickHonorsTier(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("weak", "m", "community", "SMALL", 9000, 8192))
	reg.Add(mkProvider("strong", "m", "device_attested", "SMALL", 1000, 8192))
	got, err := Pick(reg, Requirements{Model: "m", MinTier: "device_attested"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "strong" {
		t.Fatalf("want strong, got %s", got.ID)
	}
}

func TestPickHardwareClass(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("micro", "big", "community", "MICRO", 4000, 8192))
	reg.Add(mkProvider("large", "big", "community", "LARGE", 4000, 8192))
	got, err := Pick(reg, Requirements{Model: "big", HWClass: "LARGE"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "large" {
		t.Fatalf("want large, got %s", got.ID)
	}
	// only MICRO available for a LARGE model -> caps unmet
	reg2 := registry.New()
	reg2.Add(mkProvider("micro", "big", "community", "MICRO", 4000, 8192))
	if _, err := Pick(reg2, Requirements{Model: "big", HWClass: "LARGE"}); err != ErrCapsUnmet {
		t.Fatalf("want ErrCapsUnmet, got %v", err)
	}
}

func TestPickThermalTiebreak(t *testing.T) {
	reg := registry.New()
	hot := mkProvider("hot", "m", "community", "SMALL", 9000, 8192)
	hot.Telemetry.ThermalState = "serious"
	cool := mkProvider("cool", "m", "community", "SMALL", 1000, 8192)
	cool.Telemetry.ThermalState = "nominal"
	reg.Add(hot)
	reg.Add(cool)
	got, err := Pick(reg, Requirements{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "cool" {
		t.Fatalf("want cool, got %s", got.ID)
	}
}

func TestEligibleReturnsAllRankedAndFiltered(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("serves-other", "other", "community", "SMALL", 9000, 8192))
	busy := mkProvider("busy", "m", "community", "SMALL", 9000, 8192)
	busy.AcquireSlot()
	reg.Add(busy)
	reg.Add(mkProvider("idle", "m", "community", "SMALL", 1000, 8192))
	drain := mkProvider("drain", "m", "community", "SMALL", 9000, 8192)
	drain.SetDraining(true)
	reg.Add(drain)

	got, err := Eligible(reg, Requirements{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 eligible (idle, busy), got %d", len(got))
	}
	if got[0].ID != "idle" || got[1].ID != "busy" {
		t.Fatalf("want [idle busy] by least-loaded, got [%s %s]", got[0].ID, got[1].ID)
	}
}

func TestEligibleNoProvider(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", "m", "community", "SMALL", 1000, 8192))
	if _, err := Eligible(reg, Requirements{Model: "nope"}); err != ErrNoProvider {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestPickSkipsDraining(t *testing.T) {
	reg := registry.New()
	d := mkProvider("draining", "m", "community", "SMALL", 9000, 8192)
	d.SetDraining(true)
	reg.Add(d)
	reg.Add(mkProvider("ok", "m", "community", "SMALL", 1000, 8192))
	got, err := Pick(reg, Requirements{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "ok" {
		t.Fatalf("want ok, got %s", got.ID)
	}
}
