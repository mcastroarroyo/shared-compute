// Package events keeps a bounded in-memory ring of operational events — every
// log record at Info and above, plus explicit audit entries for admin actions —
// so the admin console can answer "what just happened?" without a log pipeline.
//
// The ring is content-free by construction: the coordinator never logs prompt or
// completion text (make lint-nolog enforces that), and attribute values are
// truncated so a stray large field cannot bloat memory.
package events

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Event is one ring entry.
type Event struct {
	Seq   uint64            `json:"seq"`
	TS    time.Time         `json:"ts"`
	Level string            `json:"level"` // DEBUG | INFO | WARN | ERROR | AUDIT
	Msg   string            `json:"msg"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Ring is a fixed-capacity FIFO of events. Safe for concurrent use.
type Ring struct {
	mu   sync.Mutex
	buf  []Event
	head int // next write index
	full bool
	seq  atomic.Uint64
}

// NewRing allocates a ring holding at most capacity events.
func NewRing(capacity int) *Ring {
	if capacity < 16 {
		capacity = 16
	}
	return &Ring{buf: make([]Event, capacity)}
}

const maxAttrLen = 240

// Push appends one event and returns it (with its sequence number assigned).
func (r *Ring) Push(level, msg string, attrs map[string]string) Event {
	ev := Event{Seq: r.seq.Add(1), TS: time.Now().UTC(), Level: level, Msg: msg, Attrs: attrs}
	r.mu.Lock()
	r.buf[r.head] = ev
	r.head = (r.head + 1) % len(r.buf)
	if r.head == 0 {
		r.full = true
	}
	r.mu.Unlock()
	return ev
}

// snapshot returns events oldest-first.
func (r *Ring) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]Event, r.head)
		copy(out, r.buf[:r.head])
		return out
	}
	out := make([]Event, 0, len(r.buf))
	out = append(out, r.buf[r.head:]...)
	out = append(out, r.buf[:r.head]...)
	return out
}

// Recent returns up to limit events, newest first, filtered by level (exact,
// case-insensitive, "" = all), a substring query over msg and attrs, and a
// minimum sequence number (0 = no floor) for incremental polling.
func (r *Ring) Recent(limit int, level, q string, sinceSeq uint64) []Event {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	level = strings.ToUpper(strings.TrimSpace(level))
	q = strings.ToLower(strings.TrimSpace(q))
	all := r.snapshot()
	out := make([]Event, 0, limit)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		ev := all[i]
		if ev.Seq <= sinceSeq {
			break
		}
		if level != "" && ev.Level != level {
			continue
		}
		if q != "" && !matches(ev, q) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

func matches(ev Event, q string) bool {
	if strings.Contains(strings.ToLower(ev.Msg), q) {
		return true
	}
	for k, v := range ev.Attrs {
		if strings.Contains(strings.ToLower(k), q) || strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

// Counts reports how many WARN and ERROR events landed in the last window and
// the most recent ERROR (nil if none in the window).
func (r *Ring) Counts(window time.Duration) (warns, errs int, lastErr *Event) {
	cutoff := time.Now().Add(-window)
	all := r.snapshot()
	for i := len(all) - 1; i >= 0; i-- {
		ev := all[i]
		if ev.TS.Before(cutoff) {
			break
		}
		switch ev.Level {
		case "WARN":
			warns++
		case "ERROR":
			errs++
			if lastErr == nil {
				e := ev
				lastErr = &e
			}
		}
	}
	return
}

// Len returns the number of stored events.
func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return len(r.buf)
	}
	return r.head
}

// Default is the process-wide ring the slog tee and Audit write into.
var Default = NewRing(2000)

// Audit records an operator action. Callers pass only identifiers and amounts,
// never secrets.
func Audit(action string, attrs map[string]string) Event {
	return Default.Push("AUDIT", action, attrs)
}

// Handler is a slog.Handler that copies records at or above min into a Ring
// and then forwards them to the wrapped handler unchanged.
type Handler struct {
	next  slog.Handler
	ring  *Ring
	min   slog.Level
	attrs []slog.Attr
	group string
}

// NewHandler wraps next so every record at or above min is also kept in ring.
func NewHandler(next slog.Handler, ring *Ring, min slog.Level) *Handler {
	return &Handler{next: next, ring: ring, min: min}
}

func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= h.min {
		attrs := make(map[string]string, rec.NumAttrs()+len(h.attrs))
		for _, a := range h.attrs {
			put(attrs, h.group, a)
		}
		rec.Attrs(func(a slog.Attr) bool {
			put(attrs, h.group, a)
			return true
		})
		if len(attrs) == 0 {
			attrs = nil
		}
		h.ring.Push(levelName(rec.Level), rec.Message, attrs)
	}
	return h.next.Handle(ctx, rec)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &Handler{next: h.next.WithAttrs(attrs), ring: h.ring, min: h.min, attrs: merged, group: h.group}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &Handler{next: h.next.WithGroup(name), ring: h.ring, min: h.min, attrs: h.attrs, group: g}
}

// contentKeys are never copied into the ring, as a belt-and-braces guard on
// top of lint-nolog: even if a future log line carried user text, the console
// would not display it.
var contentKeys = map[string]bool{"prompt": true, "content": true, "messages": true, "completion": true, "document": true, "text": true}

func put(m map[string]string, group string, a slog.Attr) {
	key := a.Key
	if group != "" {
		key = group + "." + key
	}
	if contentKeys[strings.ToLower(a.Key)] {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		for _, ga := range a.Value.Group() {
			put(m, key, ga)
		}
		return
	}
	v := fmt.Sprint(a.Value.Any())
	if len(v) > maxAttrLen {
		v = v[:maxAttrLen] + "…"
	}
	m[key] = v
}

func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
