// Package ratelimit is a per-key token-bucket limiter (in-memory; Redis-backed in M9).
package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter allows `rate` requests per minute per key, with burst == rate.
type Limiter struct {
	ratePerSec float64
	burst      float64
	mu         sync.Mutex
	buckets    map[string]*bucket
}

// New returns a limiter for ratePerMin requests/minute. ratePerMin <= 0 disables it.
func New(ratePerMin int) *Limiter {
	return &Limiter{
		ratePerSec: float64(ratePerMin) / 60.0,
		burst:      float64(ratePerMin),
		buckets:    make(map[string]*bucket),
	}
}

// Allow reports whether a request for key may proceed, and if not, how long until it can.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l.ratePerSec <= 0 {
		return true, 0
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.ratePerSec
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1-b.tokens)/l.ratePerSec*float64(time.Second)) + time.Millisecond
	return false, wait
}
