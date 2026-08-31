// Package jobs routes provider-originated frames (job_chunk / job_done / job_error) for an
// in-flight job back to the relay goroutine that is streaming to the consumer.
package jobs

import "sync"

// Kind discriminates an Event.
type Kind int

const (
	KindChunk Kind = iota
	KindDone
	KindError
)

// Event is a decoded provider frame for one job. Payloads are still sealed here; the relay
// opens them.
type Event struct {
	Kind Kind
	Raw  []byte // the raw JSON of the type-specific frame
}

type assignment struct {
	providerID string
	ch         chan Event
}

// Manager tracks per-job delivery channels and the provider assigned to each job.
type Manager struct {
	mu sync.Mutex
	m  map[string]assignment
}

func New() *Manager { return &Manager{m: make(map[string]assignment)} }

// Register creates a delivery channel bound to providerID. The caller must Close(jobID)
// when done. Frames from every other provider connection are rejected by Deliver
// (threat model AYNI-003: a job's result stream must come from its assigned provider).
func (m *Manager) Register(jobID, providerID string) <-chan Event {
	ch := make(chan Event, 64)
	m.mu.Lock()
	m.m[jobID] = assignment{providerID: providerID, ch: ch}
	m.mu.Unlock()
	return ch
}

// Deliver forwards an event only when both jobID and providerID match the assignment.
// Returns false for unknown jobs, foreign providers, or a full buffer.
func (m *Manager) Deliver(jobID, providerID string, ev Event) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.m[jobID]
	if !ok || a.providerID != providerID {
		return false
	}
	select {
	case a.ch <- ev:
		return true
	default:
		return false
	}
}

// Close removes and closes the job's channel.
func (m *Manager) Close(jobID string) {
	m.mu.Lock()
	a, ok := m.m[jobID]
	if ok {
		delete(m.m, jobID)
	}
	m.mu.Unlock()
	if ok {
		close(a.ch)
	}
}

// Has reports whether a job is currently registered.
func (m *Manager) Has(jobID string) bool {
	m.mu.Lock()
	_, ok := m.m[jobID]
	m.mu.Unlock()
	return ok
}
