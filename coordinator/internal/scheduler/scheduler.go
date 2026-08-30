// Package scheduler selects a provider for a job: filter by model / trust tier / context
// window / hardware class, then rank by load, tier, thermal + battery headroom, free RAM.
package scheduler

import (
	"errors"
	"sort"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
)

var (
	ErrNoProvider = errors.New("no provider available for model")
	ErrTierUnmet  = errors.New("no provider meets the requested trust tier")
	ErrCapsUnmet  = errors.New("no provider meets the model's resource requirements")
)

// Requirements describes what a job needs from a provider.
type Requirements struct {
	Model      string
	MinTier    string // "", "community", "device_attested", "confidential"
	MinContext int    // 0 = don't care
	HWClass    string // "" = don't care; otherwise the model's required class
}

func tierRank(t string) int {
	switch t {
	case "confidential":
		return 2
	case "device_attested":
		return 1
	default:
		return 0
	}
}

func classRank(c string) int {
	switch c {
	case "CONFIDENTIAL_GPU":
		return 5
	case "XL":
		return 4
	case "LARGE":
		return 3
	case "MEDIUM":
		return 2
	case "SMALL":
		return 1
	default: // MICRO or unset
		return 0
	}
}

func thermalScore(s string) int {
	switch s {
	case "nominal", "":
		return 3
	case "fair":
		return 2
	case "serious":
		return 1
	default: // critical
		return 0
	}
}

// batteryHeadroom: charging or no battery (desktop) is best; else scale by charge level.
func batteryHeadroom(p *registry.Provider) int {
	t := p.Telemetry
	if t.Charging == nil || *t.Charging {
		return 100
	}
	if t.BatteryPct == nil {
		return 100
	}
	return *t.BatteryPct
}

// Pick returns the best provider for req, or an error explaining why none matched.
func Pick(reg *registry.Registry, req Requirements) (*registry.Provider, error) {
	wantTier := tierRank(req.MinTier)
	wantClass := classRank(req.HWClass)

	var candidates []*registry.Provider
	sawModel, sawTier := false, false

	for _, p := range reg.Snapshot() {
		if !p.Serves(req.Model) {
			continue
		}
		sawModel = true
		if _, draining := p.Load(); draining {
			continue
		}
		if tierRank(p.TrustTier) < wantTier {
			continue
		}
		sawTier = true
		if req.MinContext > 0 && p.Capabilities.MaxContext > 0 && p.Capabilities.MaxContext < req.MinContext {
			continue
		}
		if wantClass > 0 && classRank(p.Capabilities.HardwareClass) < wantClass {
			continue
		}
		candidates = append(candidates, p)
	}

	if len(candidates) == 0 {
		switch {
		case !sawModel:
			return nil, ErrNoProvider
		case wantTier > 0 && !sawTier:
			return nil, ErrTierUnmet
		default:
			return nil, ErrCapsUnmet
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		ai, _ := a.Load()
		aj, _ := b.Load()
		if ai != aj {
			return ai < aj // least loaded first
		}
		if ti, tj := tierRank(a.TrustTier), tierRank(b.TrustTier); ti != tj {
			return ti > tj // stronger tier
		}
		if si, sj := thermalScore(a.Telemetry.ThermalState), thermalScore(b.Telemetry.ThermalState); si != sj {
			return si > sj // cooler
		}
		if hi, hj := batteryHeadroom(a), batteryHeadroom(b); hi != hj {
			return hi > hj // more battery headroom
		}
		return a.Telemetry.MemAvailableMB > b.Telemetry.MemAvailableMB
	})
	return candidates[0], nil
}
