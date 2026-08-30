// Package registry holds the set of currently-connected providers. In-memory for M1;
// backed by Postgres for durable identity from M2 (this API stays the same).
package registry

import (
	"sync"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
)

// Provider is one connected provider device.
type Provider struct {
	ID           string
	StaticPK     [32]byte
	Capabilities protocol.Capabilities
	TrustTier    string
	Telemetry    protocol.Telemetry
	ConnectedAt  time.Time
	LastSeen     time.Time

	// Send delivers a message frame to this provider. Set by the WebSocket hub.
	// Safe for concurrent use.
	Send func(v any) error

	mu       sync.Mutex
	active   int // in-flight jobs assigned here
	draining bool
}

// Load returns a point-in-time view of dynamic counters.
func (p *Provider) Load() (active int, draining bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active, p.draining
}

func (p *Provider) addActive(n int) {
	p.mu.Lock()
	p.active += n
	if p.active < 0 {
		p.active = 0
	}
	p.mu.Unlock()
}

// AcquireSlot marks one job as assigned. ReleaseSlot must be called when it ends.
func (p *Provider) AcquireSlot() { p.addActive(1) }

// ReleaseSlot frees a job slot.
func (p *Provider) ReleaseSlot() { p.addActive(-1) }

// SetDraining stops the scheduler from picking this provider for new jobs.
func (p *Provider) SetDraining(d bool) {
	p.mu.Lock()
	p.draining = d
	p.mu.Unlock()
}

// Serves reports whether the provider currently advertises the given model.
func (p *Provider) Serves(model string) bool {
	for _, m := range p.Capabilities.Models {
		if m == model {
			return true
		}
	}
	return false
}

// Registry is the connected-provider set.
type Registry struct {
	mu sync.RWMutex
	m  map[string]*Provider
}

func New() *Registry { return &Registry{m: make(map[string]*Provider)} }

func (r *Registry) Add(p *Provider) {
	r.mu.Lock()
	r.m[p.ID] = p
	r.mu.Unlock()
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	delete(r.m, id)
	r.mu.Unlock()
}

func (r *Registry) Get(id string) (*Provider, bool) {
	r.mu.RLock()
	p, ok := r.m[id]
	r.mu.RUnlock()
	return p, ok
}

// Snapshot returns all providers.
func (r *Registry) Snapshot() []*Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Provider, 0, len(r.m))
	for _, p := range r.m {
		out = append(out, p)
	}
	return out
}

// Count returns the number of connected providers.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.m)
}

// Models returns the union of all advertised models across connected providers.
func (r *Registry) Models() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]struct{}{}
	for _, p := range r.m {
		for _, m := range p.Capabilities.Models {
			seen[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	return out
}
