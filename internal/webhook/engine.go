package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

const (
	queueCapacity   = 1024
	retryCapacity   = 100
	workerCount     = 4
	retryInterval   = 10 * time.Second
	pruneInterval   = time.Hour
	retryClaimLimit = 100
	drainTimeout    = 5 * time.Second
)

// Metrics is the subset of the metrics registry the engine reports to. It is
// an interface so the delivery package stays independent of the exposition
// implementation, and nil disables reporting.
type Metrics interface {
	IncWebhookDropped()
	ObserveWebhookAttempt(result string)
	ObserveWebhookDelivery(result string)
}

// Endpoint is an immutable delivery target snapshot. ID is zero for the
// synthesized legacy target.
type Endpoint struct {
	ID           int64
	Legacy       bool
	Name         string
	URL          string
	Secret       string
	Preset       string
	Events       map[string]bool
	AllowPrivate bool
}

// Subscribed reports whether the endpoint wants the given event.
func (e Endpoint) Subscribed(event string) bool { return e.Events[event] }

type snapshot struct {
	endpoints  []Endpoint
	hasEnabled bool
}

type job struct {
	endpoint   Endpoint
	change     Change
	base       string
	occurredAt time.Time
}

// Engine is the single background delivery loop: a bounded queue of new
// events, four workers shared with the retry drain, and hourly pruning.
type Engine struct {
	store         *store.Store
	resolve       func() config.Webhook
	publicBaseURL string
	logger        *slog.Logger
	metrics       Metrics

	snapshot atomic.Pointer[snapshot]
	queue    chan job
	retries  chan store.WebhookDelivery

	done      chan struct{}
	closed    atomic.Bool
	startOnce sync.Once
	closeOnce sync.Once
	wg        sync.WaitGroup

	inflightMu sync.Mutex
	inflight   map[int64]bool

	// Hooks for tests: a fake transport client, deterministic jitter and clock.
	clientFor func(allowPrivate bool) *http.Client
	jitter    func() float64
	now       func() time.Time
}

// NewEngine creates the delivery engine. Start must be called before the
// server accepts requests; Close stops the workers and timers.
func NewEngine(st *store.Store, resolve func() config.Webhook, publicBaseURL string, recorder Metrics, logger *slog.Logger) *Engine {
	if resolve == nil {
		resolve = func() config.Webhook { return config.Webhook{} }
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Engine{
		store:         st,
		resolve:       resolve,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		logger:        logger,
		metrics:       recorder,
		queue:         make(chan job, queueCapacity),
		retries:       make(chan store.WebhookDelivery, retryCapacity),
		done:          make(chan struct{}),
		inflight:      make(map[int64]bool),
		clientFor:     newHTTPClient,
		jitter:        rand.Float64,
		now:           time.Now,
	}
}

// Start loads the endpoint snapshot, starts the workers and the retry and
// prune loops. It is idempotent.
func (e *Engine) Start() {
	if e == nil {
		return
	}
	e.startOnce.Do(func() {
		if err := e.Reload(context.Background()); err != nil {
			e.logger.Warn("load webhook endpoints", "error", err)
		}
		for i := 0; i < workerCount; i++ {
			e.wg.Add(1)
			go e.worker()
		}
		e.wg.Add(1)
		go e.retryLoop()
		e.wg.Add(1)
		go e.pruneLoop()
	})
}

// Close stops accepting new work, lets in-flight attempts finish for up to
// five seconds, and stops the timers. It is idempotent.
func (e *Engine) Close() {
	if e == nil {
		return
	}
	e.closeOnce.Do(func() {
		e.closed.Store(true)
		close(e.done)
	})
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(drainTimeout):
	}
}

// Reload rebuilds the in-memory endpoint snapshot. A failure keeps the
// previous snapshot; the retry tick tries again within ten seconds.
func (e *Engine) Reload(ctx context.Context) error {
	if e == nil {
		return nil
	}
	snap, err := e.buildSnapshot(ctx)
	if err != nil {
		return err
	}
	e.snapshot.Store(snap)
	return nil
}

func (e *Engine) buildSnapshot(ctx context.Context) (*snapshot, error) {
	endpoints, err := e.store.ListWebhookEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	snap := &snapshot{}
	for _, ep := range endpoints {
		if !ep.Enabled {
			continue
		}
		snap.hasEnabled = true
		snap.endpoints = append(snap.endpoints, endpointFromStore(ep))
	}
	return snap, nil
}

func endpointFromStore(ep store.WebhookEndpoint) Endpoint {
	events := make(map[string]bool, len(ep.Events))
	for _, event := range ep.Events {
		events[event] = true
	}
	return Endpoint{
		ID:           ep.ID,
		Name:         ep.Name,
		URL:          ep.URL,
		Secret:       ep.Secret,
		Preset:       ep.Preset,
		Events:       events,
		AllowPrivate: ep.AllowPrivate,
	}
}

// legacyEndpoint synthesizes the environment/settings target. It is treated as
// allow_private because it predates the SSRF control; link-local remains
// blocked by the shared address policy.
func (e *Engine) legacyEndpoint() *Endpoint {
	cfg := e.resolve()
	if !cfg.Enabled() {
		return nil
	}
	return &Endpoint{
		Legacy:       true,
		Name:         "Environment default",
		URL:          strings.TrimSpace(cfg.URL),
		Secret:       cfg.Secret,
		Preset:       store.WebhookPresetGeneric,
		Events:       map[string]bool{store.WebhookEventCreated: true},
		AllowPrivate: true,
	}
}

// LegacyEndpoint returns the synthesized target and whether one is configured.
// The URL and secret are resolved now, so a settings change applies
// immediately; suppression is reported separately by LegacyStatus.
func (e *Engine) LegacyEndpoint() (Endpoint, bool) {
	if e == nil {
		return Endpoint{}, false
	}
	endpoint := e.legacyEndpoint()
	if endpoint == nil {
		return Endpoint{}, false
	}
	return *endpoint, true
}

// LegacyStatus describes the legacy target for the admin pages.
type LegacyStatus struct {
	Configured bool
	Active     bool
	URL        string
	SecretSet  bool
}

// LegacyStatus reports whether the legacy target is configured and whether no
// enabled persisted endpoint suppresses it.
func (e *Engine) LegacyStatus(ctx context.Context) (LegacyStatus, error) {
	if e == nil {
		return LegacyStatus{}, nil
	}
	cfg := e.resolve()
	status := LegacyStatus{
		Configured: cfg.Enabled(),
		URL:        strings.TrimSpace(cfg.URL),
		SecretSet:  cfg.Secret != "",
	}
	if !status.Configured {
		return status, nil
	}
	enabled, err := e.store.CountEnabledWebhookEndpoints(ctx)
	if err != nil {
		return status, err
	}
	status.Active = enabled == 0
	return status, nil
}

// Emit queues one lifecycle event.
func (e *Engine) Emit(change Change) { e.EmitBatch([]Change{change}) }

// EmitBatch fans every change out to the subscribed endpoints using the
// in-memory snapshot. It performs no rendering, DNS lookup or network I/O and
// never blocks the caller.
func (e *Engine) EmitBatch(changes []Change) {
	if e == nil || e.closed.Load() || len(changes) == 0 {
		return
	}
	snap := e.snapshot.Load()
	if snap == nil {
		return
	}
	// The legacy target is resolved on every emission, so editing webhook.url
	// in the settings page applies without a restart or a snapshot reload.
	endpoints := snap.endpoints
	if !snap.hasEnabled {
		if legacy := e.legacyEndpoint(); legacy != nil {
			endpoints = append(append([]Endpoint(nil), endpoints...), *legacy)
		}
	}
	if len(endpoints) == 0 {
		return
	}
	occurredAt := e.now().UTC()
	for _, change := range changes {
		if !store.ValidWebhookEvent(change.Event) {
			continue
		}
		base := e.publicBaseURL
		if base == "" {
			base = change.BaseURL
		}
		for _, endpoint := range endpoints {
			if !endpoint.Subscribed(change.Event) {
				continue
			}
			e.enqueue(job{endpoint: endpoint, change: change, base: base, occurredAt: occurredAt})
		}
	}
}

func (e *Engine) enqueue(j job) {
	select {
	case e.queue <- j:
	default:
		if e.metrics != nil {
			e.metrics.IncWebhookDropped()
		}
		e.logger.Warn("webhook queue full; event dropped",
			"event", j.change.Event, "entry_id", j.change.Entry.ID)
	}
}

// QueueDepth reports the current number of queued new events.
func (e *Engine) QueueDepth() int {
	if e == nil {
		return 0
	}
	return len(e.queue)
}

// PruneNow applies the retention rules immediately, for the admin action.
func (e *Engine) PruneNow(ctx context.Context) (int64, error) {
	return e.store.PruneWebhookDeliveries(ctx, e.now())
}

// SendTest performs one synchronous delivery using the endpoint's preset,
// secret and target policy. It returns the receiver's status.
func (e *Engine) SendTest(ctx context.Context, endpoint Endpoint, payload Payload) (int, error) {
	if err := ValidateTarget(ctx, endpoint.URL, endpoint.AllowPrivate); err != nil {
		return 0, err
	}
	body, err := Render(endpoint.Preset, payload)
	if err != nil {
		return 0, err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, Timeout)
		defer cancel()
	}
	return e.post(ctx, endpoint, body)
}

func (e *Engine) worker() {
	defer e.wg.Done()
	for {
		select {
		case <-e.done:
			return
		case j := <-e.queue:
			e.handleJob(j)
		case d := <-e.retries:
			e.handleRetry(d)
		}
	}
}

func (e *Engine) handleJob(j job) {
	if e.closed.Load() {
		return
	}
	ctx := context.Background()
	endpoint := j.endpoint
	if endpoint.Legacy {
		// A persisted enabled endpoint suppresses the legacy target, including
		// a job that was queued just before the snapshot was rebuilt.
		snap := e.snapshot.Load()
		if snap == nil || snap.hasEnabled {
			return
		}
		legacy := e.legacyEndpoint()
		if legacy == nil {
			return
		}
		endpoint = *legacy
	} else {
		// The endpoint may have been deleted or disabled after the event was
		// queued; discard rather than inserting against a cascaded row.
		current, err := e.store.GetWebhookEndpoint(ctx, endpoint.ID)
		if err != nil {
			if !errors.Is(err, store.ErrWebhookEndpointNotFound) {
				e.logger.Warn("load webhook endpoint", "endpoint_id", endpoint.ID, "error", err)
			}
			return
		}
		if !current.Enabled {
			return
		}
		endpoint = endpointFromStore(current)
	}

	payload := buildPayload(j.change, j.base, j.occurredAt)
	raw, err := json.Marshal(payload)
	if err != nil {
		e.logger.Warn("encode webhook payload", "entry_id", payload.ID, "error", err)
		return
	}
	now := e.now().UTC()
	delivery, err := e.store.CreateWebhookDelivery(ctx, store.WebhookDelivery{
		EndpointID:    endpointReference(endpoint),
		Event:         payload.Event,
		EntryID:       &payload.ID,
		Status:        store.WebhookStatusPending,
		NextAttemptAt: &now,
		Payload:       string(raw),
	})
	if err != nil {
		e.logger.Warn("persist webhook delivery", "endpoint_id", endpoint.ID, "entry_id", payload.ID, "error", err)
		return
	}
	// The row is inserted as pending and immediately due, so the retry loop
	// would otherwise claim it while this first POST is still in flight. That
	// produced two concurrent attempts on one row, losing counter updates and
	// delivering the same event more times than WebhookMaxAttempts.
	e.markInflight(delivery.ID)
	defer e.clearInflight(delivery.ID)
	e.attempt(ctx, delivery, endpoint, payload)
}

func (e *Engine) handleRetry(delivery store.WebhookDelivery) {
	defer e.clearInflight(delivery.ID)
	ctx := context.Background()
	endpoint, ok := e.endpointForDelivery(ctx, delivery)
	if !ok {
		e.cancel(ctx, delivery, "endpoint unavailable")
		return
	}
	if strings.TrimSpace(delivery.Payload) == "" {
		e.finish(ctx, delivery, store.WebhookStatusFailed, delivery.Attempts, nil, "payload unavailable")
		return
	}
	var payload Payload
	if err := json.Unmarshal([]byte(delivery.Payload), &payload); err != nil {
		e.finish(ctx, delivery, store.WebhookStatusFailed, delivery.Attempts, nil, "payload unreadable")
		return
	}
	e.attempt(ctx, delivery, endpoint, payload)
}

func (e *Engine) endpointForDelivery(ctx context.Context, delivery store.WebhookDelivery) (Endpoint, bool) {
	if delivery.EndpointID == nil {
		snap := e.snapshot.Load()
		if snap == nil || snap.hasEnabled {
			return Endpoint{}, false
		}
		legacy := e.legacyEndpoint()
		if legacy == nil {
			return Endpoint{}, false
		}
		return *legacy, true
	}
	current, err := e.store.GetWebhookEndpoint(ctx, *delivery.EndpointID)
	if err != nil || !current.Enabled {
		return Endpoint{}, false
	}
	return endpointFromStore(current), true
}

func (e *Engine) attempt(ctx context.Context, delivery store.WebhookDelivery, endpoint Endpoint, payload Payload) {
	attempts := delivery.Attempts + 1
	body, err := Render(endpoint.Preset, payload)
	if err != nil {
		e.finish(ctx, delivery, store.WebhookStatusFailed, attempts, nil, "render failed")
		return
	}
	status, err := e.post(ctx, endpoint, body)
	if err != nil {
		sanitised := SanitiseError(err, endpoint.URL)
		e.logger.Warn("webhook delivery failed",
			"event", payload.Event, "entry_id", payload.ID,
			"host", TargetHost(endpoint.URL), "error", sanitised)
		if errors.Is(err, ErrTargetDenied) {
			e.finish(ctx, delivery, store.WebhookStatusFailed, attempts, nil, sanitised)
			return
		}
		e.scheduleRetry(ctx, delivery, attempts, nil, sanitised)
		return
	}
	lastStatus := status
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		e.logger.Warn("webhook delivery rejected",
			"event", payload.Event, "entry_id", payload.ID,
			"host", TargetHost(endpoint.URL), "status", status)
	}
	switch {
	case status >= http.StatusOK && status < http.StatusMultipleChoices:
		e.finish(ctx, delivery, store.WebhookStatusDelivered, attempts, &lastStatus, "")
	case status == http.StatusRequestTimeout ||
		status == http.StatusTooEarly ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError:
		e.scheduleRetry(ctx, delivery, attempts, &lastStatus,
			"receiver returned HTTP "+httpStatusText(status))
	default:
		e.finish(ctx, delivery, store.WebhookStatusFailed, attempts, &lastStatus,
			"receiver returned HTTP "+httpStatusText(status))
	}
}

func (e *Engine) scheduleRetry(ctx context.Context, delivery store.WebhookDelivery, attempts int, lastStatus *int, lastError string) {
	if attempts >= store.WebhookMaxAttempts {
		e.finish(ctx, delivery, store.WebhookStatusFailed, attempts, lastStatus, lastError)
		return
	}
	if e.metrics != nil {
		e.metrics.ObserveWebhookAttempt("retry")
	}
	next := e.now().UTC().Add(Backoff(attempts, e.jitter))
	e.record(ctx, delivery, store.WebhookStatusPending, attempts, lastStatus, lastError, &next)
}

func (e *Engine) finish(ctx context.Context, delivery store.WebhookDelivery, status string, attempts int, lastStatus *int, lastError string) {
	if e.metrics != nil {
		switch status {
		case store.WebhookStatusDelivered:
			e.metrics.ObserveWebhookAttempt("success")
			e.metrics.ObserveWebhookDelivery("delivered")
		default:
			e.metrics.ObserveWebhookAttempt("permanent_failure")
			e.metrics.ObserveWebhookDelivery("failed")
		}
	}
	e.record(ctx, delivery, status, attempts, lastStatus, lastError, nil)
}

func (e *Engine) cancel(ctx context.Context, delivery store.WebhookDelivery, reason string) {
	if e.metrics != nil {
		e.metrics.ObserveWebhookDelivery("cancelled")
	}
	e.record(ctx, delivery, store.WebhookStatusCancelled, delivery.Attempts, delivery.LastStatus, reason, nil)
}

func (e *Engine) record(ctx context.Context, delivery store.WebhookDelivery, status string, attempts int, lastStatus *int, lastError string, next *time.Time) {
	delivery.Status = status
	delivery.Attempts = attempts
	delivery.LastStatus = lastStatus
	delivery.LastError = lastError
	delivery.NextAttemptAt = next
	if err := e.store.UpdateWebhookDelivery(ctx, delivery); err != nil {
		e.logger.Warn("update webhook delivery",
			"delivery_id", delivery.ID, "endpoint_id", delivery.EndpointID, "error", err)
	}
}

func (e *Engine) post(ctx context.Context, endpoint Endpoint, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if endpoint.Secret != "" {
		req.Header.Set("X-VanDyke-Signature", Signature(endpoint.Secret, body))
	}

	resp, err := e.clientFor(endpoint.AllowPrivate).Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode, nil
}

func (e *Engine) retryLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
			// Reloading on the retry tick self-heals a failed mutation reload.
			if err := e.Reload(context.Background()); err != nil {
				e.logger.Warn("reload webhook endpoints", "error", err)
			}
			e.claimDue()
		}
	}
}

func (e *Engine) claimDue() {
	due, err := e.store.DueWebhookDeliveries(context.Background(), e.now().UTC(), retryClaimLimit)
	if err != nil {
		e.logger.Warn("query due webhook deliveries", "error", err)
		return
	}
	for _, delivery := range due {
		if !e.markInflight(delivery.ID) {
			continue
		}
		select {
		case e.retries <- delivery:
		default:
			e.clearInflight(delivery.ID)
			return
		}
	}
}

func (e *Engine) pruneLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
			n, err := e.store.PruneWebhookDeliveries(context.Background(), e.now())
			if err != nil {
				e.logger.Warn("prune webhook deliveries", "error", err)
				continue
			}
			if n > 0 {
				e.logger.Info("pruned webhook deliveries", "count", n)
			}
		}
	}
}

func (e *Engine) markInflight(id int64) bool {
	e.inflightMu.Lock()
	defer e.inflightMu.Unlock()
	if e.inflight[id] {
		return false
	}
	e.inflight[id] = true
	return true
}

func (e *Engine) clearInflight(id int64) {
	e.inflightMu.Lock()
	delete(e.inflight, id)
	e.inflightMu.Unlock()
}

func endpointReference(endpoint Endpoint) *int64 {
	if endpoint.Legacy {
		return nil
	}
	id := endpoint.ID
	return &id
}

// Backoff returns the full-jitter delay after a failed attempt: a uniform value
// in [0, min(5s*2^(attempt-1), 1h)].
func Backoff(attempt int, random func() float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	limit := 5 * time.Second
	for i := 1; i < attempt; i++ {
		if limit >= time.Hour {
			break
		}
		limit *= 2
	}
	if limit > time.Hour {
		limit = time.Hour
	}
	if random == nil {
		random = rand.Float64
	}
	return time.Duration(random() * float64(limit))
}

func httpStatusText(status int) string {
	if text := http.StatusText(status); text != "" {
		return strconv.Itoa(status) + " " + text
	}
	return strconv.Itoa(status)
}
