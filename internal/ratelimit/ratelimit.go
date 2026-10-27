// Package ratelimit provides a per-key token bucket limiter.
//
// Limits are process-local: each replica keeps its own buckets, and counters
// reset on restart. Deployments that need a global budget must enforce it at
// the reverse proxy or in a shared store. The bucket map is bounded and idle
// keys are swept so a long-running process cannot grow without limit.
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
	maxKeys   int
}

// defaultMaxKeys bounds memory if many distinct keys are seen between sweeps.
const defaultMaxKeys = 10000

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
	return newLimiter(perHour/3600, 3600/perHour, burst)
}

// NewPerMinute creates a limiter refilling perMinute tokens for each key.
func NewPerMinute(perMinute float64, burst int) *Limiter {
	if perMinute <= 0 {
		perMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	return newLimiter(perMinute/60, 60/perMinute, burst)
}

func newLimiter(tokensPerSecond, refillSeconds float64, burst int) *Limiter {
	return &Limiter{
		buckets:   make(map[string]*bucket),
		limit:     rate.Limit(tokensPerSecond),
		burst:     burst,
		refillMin: refillSeconds,
		now:       time.Now,
		idle:      24 * time.Hour,
		lastSweep: time.Now(),
		maxKeys:   defaultMaxKeys,
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
		if len(l.buckets) >= l.maxKeys {
			// Still full after the periodic sweep: evict the least recently
			// seen key so the map stays bounded.
			l.evictOldest()
		}
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

func (l *Limiter) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, b := range l.buckets {
		if oldestKey == "" || b.seen.Before(oldest) {
			oldestKey = key
			oldest = b.seen
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
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
