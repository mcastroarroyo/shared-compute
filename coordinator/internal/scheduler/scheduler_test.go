package scheduler

import (
	"testing"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
)

func mkProvider(id, model, tier string, memMB int) *registry.Provider {
	return &registry.Provider{
		ID:        id,
		TrustTier: tier,
		Capabilities: protocol.Capabilities{
			Models: []string{model},
		},
		Telemetry: protocol.Telemetry{MemAvailableMB: memMB},
		Send:      func(any) error { return nil },
	}
}

func TestPickNoProvider(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", "model-x", "community", 1000))
	if _, err := Pick(reg, "model-y", ""); err != ErrNoProvider {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestPickLeastLoaded(t *testing.T) {
	reg := registry.New()
	busy := mkProvider("busy", "m", "community", 8000)
	busy.AcquireSlot()
	busy.AcquireSlot()
	idle := mkProvider("idle", "m", "community", 4000)
	idle.AcquireSlot()
	reg.Add(busy)
	reg.Add(idle)

	got, err := Pick(reg, "m", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "idle" {
		t.Fatalf("want idle, got %s", got.ID)
	}
}

func TestPickTierUnmet(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", "m", "community", 1000))
	if _, err := Pick(reg, "m", "confidential"); err != ErrTierUnmet {
		t.Fatalf("want ErrTierUnmet, got %v", err)
	}
}

func TestPickHonorsTier(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("weak", "m", "community", 9000))
	reg.Add(mkProvider("strong", "m", "device_attested", 1000))
	got, err := Pick(reg, "m", "device_attested")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "strong" {
		t.Fatalf("want strong, got %s", got.ID)
	}
}

func TestPickSkipsDraining(t *testing.T) {
	reg := registry.New()
	d := mkProvider("draining", "m", "community", 9000)
	d.SetDraining(true)
	reg.Add(d)
	reg.Add(mkProvider("ok", "m", "community", 1000))
	got, err := Pick(reg, "m", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "ok" {
		t.Fatalf("want ok, got %s", got.ID)
	}
}
