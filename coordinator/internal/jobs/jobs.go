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

// Manager tracks per-job delivery channels.
type Manager struct {
	mu sync.Mutex
	m  map[string]chan Event
}

func New() *Manager { return &Manager{m: make(map[string]chan Event)} }

// Register creates a delivery channel for jobID. The caller must Close(jobID) when done.
func (m *Manager) Register(jobID string) <-chan Event {
	ch := make(chan Event, 64)
	m.mu.Lock()
	m.m[jobID] = ch
	m.mu.Unlock()
	return ch
}

// Deliver forwards an event to the job's channel. Returns false if no such job or the
// buffer is full (caller may treat a full buffer as a slow consumer and cancel).
func (m *Manager) Deliver(jobID string, ev Event) bool {
	m.mu.Lock()
	ch, ok := m.m[jobID]
	m.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- ev:
		return true
	default:
		return false
	}
}

// Close removes and closes the job's channel.
func (m *Manager) Close(jobID string) {
	m.mu.Lock()
	ch, ok := m.m[jobID]
	if ok {
		delete(m.m, jobID)
	}
	m.mu.Unlock()
	if ok {
		close(ch)
	}
}

// Has reports whether a job is currently registered.
func (m *Manager) Has(jobID string) bool {
	m.mu.Lock()
	_, ok := m.m[jobID]
	m.mu.Unlock()
	return ok
}
