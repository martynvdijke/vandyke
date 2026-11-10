// Package webhook delivers the optional entry-created notification to one
// operator-configured endpoint. Delivery is best effort: production dispatch is
// fire-and-forget and never delays or changes the create response.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

// EventEntryCreated is the only event the server emits.
const EventEntryCreated = "entry.created"

// userAgent identifies the server on outbound deliveries.
const userAgent = "vanDyke-ledger-webhook/1"

// Timeout bounds one delivery attempt.
const Timeout = 5 * time.Second

// ErrNotConfigured is returned by Send when no effective URL is set.
var ErrNotConfigured = errors.New("no webhook configured")

// Payload is the JSON body of a delivery.
type Payload struct {
	Event     string `json:"event"`
	ID        int64  `json:"id"`
	Word      string `json:"word"`
	Meaning   string `json:"meaning"`
	Person    string `json:"person"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
}

// Dispatcher holds the settings resolver, logger and HTTP client used for
// deliveries. The effective URL and secret are read at send time so an admin
// settings change applies without a restart.
type Dispatcher struct {
	resolve func() config.Webhook
	client  *http.Client
	logger  *slog.Logger
}

// New creates a dispatcher. A nil resolver yields a disabled dispatcher.
func New(resolve func() config.Webhook, logger *slog.Logger) *Dispatcher {
	if resolve == nil {
		resolve = func() config.Webhook { return config.Webhook{} }
	}
	return &Dispatcher{
		resolve: resolve,
		client:  &http.Client{Timeout: Timeout},
		logger:  logger,
	}
}

// Enabled reports whether an effective URL is configured.
func (d *Dispatcher) Enabled() bool {
	return d != nil && d.resolve().Enabled()
}

// BuildPayload maps an entry to the delivery contract. permalink must be the
// absolute {base}/e/{id} URL.
func BuildPayload(entry store.Entry, permalink string) Payload {
	return Payload{
		Event:     EventEntryCreated,
		ID:        entry.ID,
		Word:      entry.Word,
		Meaning:   entry.Meaning,
		Person:    entry.Person,
		CreatedAt: entry.CreatedAt.UTC().Format(time.RFC3339),
		URL:       permalink,
	}
}

// Dispatch starts one delivery in a bounded goroutine and returns immediately.
// Failures are logged and never returned, so the caller's response cannot be
// delayed or altered by the receiver.
func (d *Dispatcher) Dispatch(payload Payload) {
	if d == nil {
		return
	}
	target := d.resolve()
	if !target.Enabled() {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		d.logger.Warn("encode webhook payload", "entry_id", payload.ID, "error", err)
		return
	}
	go d.deliver(target, payload.ID, body)
}

// deliver performs one POST. The body bytes handed in are the bytes that are
// signed and sent.
func (d *Dispatcher) deliver(target config.Webhook, entryID int64, body []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	status, err := d.post(ctx, target, body)
	switch {
	case err != nil:
		d.logger.Warn("webhook delivery failed", "entry_id", entryID, "host", hostOf(target.URL), "error", err)
	case status < http.StatusOK || status >= http.StatusMultipleChoices:
		d.logger.Warn("webhook delivery rejected", "entry_id", entryID, "host", hostOf(target.URL), "status", status)
	}
}

// Send performs one synchronous delivery, used by the admin test-fire action.
// It returns the receiver's HTTP status.
func (d *Dispatcher) Send(ctx context.Context, payload Payload) (int, error) {
	if d == nil {
		return 0, ErrNotConfigured
	}
	target := d.resolve()
	if !target.Enabled() {
		return 0, ErrNotConfigured
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode webhook payload: %w", err)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, Timeout)
		defer cancel()
	}
	return d.post(ctx, target, body)
}

func (d *Dispatcher) post(ctx context.Context, target config.Webhook, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if target.Secret != "" {
		req.Header.Set("X-VanDyke-Signature", Signature(target.Secret, body))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode, nil
}

// Signature returns the sha256=<hex> HMAC-SHA256 digest of body keyed by secret.
func Signature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// hostOf reports the URL host for logging without any embedded credentials.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}
