// Package scheduler selects a provider for a job. M1: least-loaded provider that serves
// the model and matches the requested trust tier. M4 expands this to RAM/VRAM/context/
// latency/price/thermal ranking.
package scheduler

import (
	"errors"
	"sort"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
)

var (
	ErrNoProvider = errors.New("no provider available for model")
	ErrTierUnmet  = errors.New("no provider meets the requested trust tier")
)

// tierRank orders trust tiers from weakest to strongest.
func tierRank(t string) int {
	switch t {
	case "confidential":
		return 2
	case "device_attested":
		return 1
	default: // "community" or unset
		return 0
	}
}

// Pick returns the best provider for model at minTier (""=no constraint), or an error.
func Pick(reg *registry.Registry, model, minTier string) (*registry.Provider, error) {
	want := tierRank(minTier)
	var candidates []*registry.Provider
	sawModel := false

	for _, p := range reg.Snapshot() {
		if !p.Serves(model) {
			continue
		}
		sawModel = true
		if _, draining := p.Load(); draining {
			continue
		}
		if tierRank(p.TrustTier) < want {
			continue
		}
		candidates = append(candidates, p)
	}

	if len(candidates) == 0 {
		if sawModel && want > 0 {
			return nil, ErrTierUnmet
		}
		return nil, ErrNoProvider
	}

	sort.Slice(candidates, func(i, j int) bool {
		ai, _ := candidates[i].Load()
		aj, _ := candidates[j].Load()
		if ai != aj {
			return ai < aj
		}
		// tie-break: stronger tier first, then more free RAM
		if ti, tj := tierRank(candidates[i].TrustTier), tierRank(candidates[j].TrustTier); ti != tj {
			return ti > tj
		}
		return candidates[i].Telemetry.MemAvailableMB > candidates[j].Telemetry.MemAvailableMB
	})
	return candidates[0], nil
}
