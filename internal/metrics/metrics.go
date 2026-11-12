// Package metrics is a small, dependency-free Prometheus text exposition
// registry. Counters are atomic; the request histogram is mutex-guarded.
package metrics

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// durationBuckets is the fixed histogram bucket set from the design.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var (
	additionOutcomes = []string{"created", "duplicate", "invalid", "rate_limited", "error"}
	deliveryResults  = []string{"delivered", "failed", "cancelled"}
	attemptResults   = []string{"success", "retry", "permanent_failure"}
)

// DBSnapshot is the scrape-time database state behind the DB gauges.
type DBSnapshot struct {
	Total             int
	Live              int
	Trashed           int
	WithPhoto         int
	PendingDeliveries int
}

// Options configures a registry.
type Options struct {
	// Version is the build version reported by vandyke_build_info.
	Version string
	// DBPath is the main SQLite file; its size is reported excluding -wal and
	// -shm.
	DBPath string
	// Logger records a failed DB gauge query. Optional.
	Logger *slog.Logger
	// Snapshot reads the DB-backed counts. Optional; without it the DB series
	// are omitted and db_up is 0.
	Snapshot func(context.Context) (DBSnapshot, error)
	// Start is the process start time; the zero value uses New's clock.
	Start time.Time
}

type requestKey struct {
	method string
	route  string
	status string
}

type durationKey struct {
	method string
	route  string
}

type histogram struct {
	buckets [11]uint64
	inf     uint64
	sum     float64
	count   uint64
}

// Registry holds every process-local metric.
type Registry struct {
	version string
	dbPath  string
	logger  *slog.Logger
	start   time.Time

	snapshot   func(context.Context) (DBSnapshot, error)
	queueDepth func() int

	additions   [5]atomic.Int64
	rateLimited atomic.Int64
	dropped     atomic.Int64
	deliveries  [3]atomic.Int64
	attempts    [3]atomic.Int64

	mu        sync.Mutex
	requests  map[requestKey]*atomic.Int64
	durations map[durationKey]*histogram
}

// New creates a registry. A nil logger discards scrape warnings.
func New(opts Options) *Registry {
	start := opts.Start
	if start.IsZero() {
		start = time.Now()
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Registry{
		version:   opts.Version,
		dbPath:    opts.DBPath,
		logger:    logger,
		start:     start,
		snapshot:  opts.Snapshot,
		requests:  make(map[requestKey]*atomic.Int64),
		durations: make(map[durationKey]*histogram),
	}
}

// SetQueueDepth installs the callback behind vandyke_webhook_queue_depth.
func (r *Registry) SetQueueDepth(fn func() int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.queueDepth = fn
	r.mu.Unlock()
}

// IncEntryAddition counts a create outcome. Unknown outcomes are ignored so
// the label set stays bounded.
func (r *Registry) IncEntryAddition(outcome string) {
	if r == nil {
		return
	}
	for i, known := range additionOutcomes {
		if known == outcome {
			r.additions[i].Add(1)
			return
		}
	}
}

// IncRateLimited counts a request denied by either create limiter.
func (r *Registry) IncRateLimited() {
	if r == nil {
		return
	}
	r.rateLimited.Add(1)
}

// IncWebhookDropped counts an event dropped because the queue was full.
func (r *Registry) IncWebhookDropped() {
	if r == nil {
		return
	}
	r.dropped.Add(1)
}

// ObserveWebhookAttempt counts one delivery attempt result.
func (r *Registry) ObserveWebhookAttempt(result string) {
	if r == nil {
		return
	}
	for i, known := range attemptResults {
		if known == result {
			r.attempts[i].Add(1)
			return
		}
	}
}

// ObserveWebhookDelivery counts one terminal delivery outcome. It measures
// outcomes, not rows, so a manual retry can add a second result.
func (r *Registry) ObserveWebhookDelivery(result string) {
	if r == nil {
		return
	}
	for i, known := range deliveryResults {
		if known == result {
			r.deliveries[i].Add(1)
			return
		}
	}
}

// ObserveHTTP records one completed request. route must be the mux pattern or
// "unmatched"; the raw path is never accepted.
func (r *Registry) ObserveHTTP(method, route string, status int, duration time.Duration) {
	if r == nil {
		return
	}
	if route == "" {
		route = "unmatched"
	}
	statusText := statusString(status)
	key := requestKey{method: method, route: route, status: statusText}

	r.mu.Lock()
	counter, ok := r.requests[key]
	if !ok {
		counter = &atomic.Int64{}
		r.requests[key] = counter
	}
	hkey := durationKey{method: method, route: route}
	hist, ok := r.durations[hkey]
	if !ok {
		hist = &histogram{}
		r.durations[hkey] = hist
	}
	hist.observe(duration.Seconds())
	r.mu.Unlock()

	counter.Add(1)
}

func (h *histogram) observe(seconds float64) {
	for i, bound := range durationBuckets {
		if seconds <= bound {
			h.buckets[i]++
		}
	}
	h.inf++
	h.sum += seconds
	h.count++
}

// uptime returns the process uptime in seconds.
func (r *Registry) uptime() float64 {
	return time.Since(r.start).Seconds()
}

// dbSize returns the main database file size, excluding -wal and -shm.
func (r *Registry) dbSize() (int64, bool) {
	if r.dbPath == "" {
		return 0, false
	}
	info, err := os.Stat(r.dbPath)
	if err != nil {
		return 0, false
	}
	return info.Size(), true
}

// goVersion reports the runtime version, which also feeds build info.
func goVersion() string { return runtime.Version() }
