// Package ratelimit provides a per-key token bucket limiter.
package ratelimit

import (
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter allows a configurable number of events per hour per key with a
// short-term burst allowance.
type Limiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	limit     rate.Limit
	burst     int
	refillMin float64 // seconds between two tokens, used for Retry-After
	now       func() time.Time
	idle      time.Duration
	lastSweep time.Time
}

type bucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

// New creates a limiter refilling perHour tokens for each key, allowing bursts
// of up to burst events.
func New(perHour float64, burst int) *Limiter {
	if perHour <= 0 {
		perHour = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		buckets:   make(map[string]*bucket),
		limit:     rate.Limit(perHour / 3600),
		burst:     burst,
		refillMin: 3600 / perHour,
		now:       time.Now,
		idle:      24 * time.Hour,
		lastSweep: time.Now(),
	}
}

// Allow reports whether an event for key may proceed.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if now.Sub(l.lastSweep) > 10*time.Minute {
		l.sweep(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	return b.limiter.AllowN(now, 1)
}

// RetryAfterSeconds is a hint for the Retry-After header after a denial.
func (l *Limiter) RetryAfterSeconds() int {
	return int(math.Ceil(l.refillMin))
}

func (l *Limiter) sweep(now time.Time) {
	for key, b := range l.buckets {
		if now.Sub(b.seen) > l.idle {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}

// Len reports the number of tracked keys (used in tests).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
