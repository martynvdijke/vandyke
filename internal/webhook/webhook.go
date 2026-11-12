// Package webhook delivers entry lifecycle events to zero or more configured
// endpoints. Delivery is best effort: production emission is non-blocking and
// never delays or changes the caller's response. Failures are retried durably.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/store"
)

// The five supported event names, re-exported from the store so the
// persistence layer and the delivery engine cannot drift apart.
const (
	EventEntryCreated  = store.WebhookEventCreated
	EventEntryUpdated  = store.WebhookEventUpdated
	EventEntryDeleted  = store.WebhookEventDeleted
	EventEntryRestored = store.WebhookEventRestored
	EventEntryPurged   = store.WebhookEventPurged
)

// userAgent identifies the server on outbound deliveries.
const userAgent = "vanDyke-ledger-webhook/1"

// Timeout bounds one delivery attempt.
const Timeout = 5 * time.Second

// ErrNotConfigured is returned when no legacy target is configured.
var ErrNotConfigured = errors.New("no webhook configured")

// Payload is the generic event envelope. The entry.created body contains
// exactly the first seven keys; the four newer event types add occurred_at,
// deleted_at and (for edits) changes.
type Payload struct {
	Event      string                 `json:"event"`
	ID         int64                  `json:"id"`
	Word       string                 `json:"word"`
	Meaning    string                 `json:"meaning"`
	Person     string                 `json:"person"`
	CreatedAt  string                 `json:"created_at"`
	URL        string                 `json:"url"`
	OccurredAt string                 `json:"occurred_at,omitempty"`
	DeletedAt  string                 `json:"deleted_at,omitempty"`
	Changes    map[string]FieldChange `json:"changes,omitempty"`
}

// FieldChange is one edited field's previous and new value.
type FieldChange struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// Change is one entry lifecycle event the caller wants delivered. Entry is the
// snapshot after the change (or, for a purge, the last known row). BaseURL is
// the absolute origin; the engine prefers its configured PUBLIC_BASE_URL.
type Change struct {
	Event   string
	Entry   store.Entry
	Changes map[string]FieldChange
	BaseURL string
}

// ChangesFor builds one Change per entry for a bulk lifecycle action.
func ChangesFor(event string, entries []store.Entry, base string) []Change {
	changes := make([]Change, 0, len(entries))
	for _, entry := range entries {
		changes = append(changes, Change{Event: event, Entry: entry, BaseURL: base})
	}
	return changes
}

// BuildPayload maps an entry to the frozen entry.created contract. permalink
// must be the absolute {base}/e/{id} URL.
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

// buildPayload renders the generic envelope for one change. entry.created is
// frozen at seven keys; only newer event types carry occurred_at, and only a
// trashed snapshot carries deleted_at.
func buildPayload(change Change, base string, occurredAt time.Time) Payload {
	payload := Payload{
		Event:     change.Event,
		ID:        change.Entry.ID,
		Word:      change.Entry.Word,
		Meaning:   change.Entry.Meaning,
		Person:    change.Entry.Person,
		CreatedAt: change.Entry.CreatedAt.UTC().Format(time.RFC3339),
		URL:       strings.TrimRight(base, "/") + fmt.Sprintf("/e/%d", change.Entry.ID),
	}
	if change.Event == EventEntryCreated {
		return payload
	}
	payload.OccurredAt = occurredAt.UTC().Format(time.RFC3339)
	if change.Entry.DeletedAt != nil {
		payload.DeletedAt = change.Entry.DeletedAt.UTC().Format(time.RFC3339)
	}
	if change.Event == EventEntryUpdated && len(change.Changes) > 0 {
		payload.Changes = change.Changes
	}
	return payload
}

// Signature returns the sha256=<hex> HMAC-SHA256 digest of body keyed by secret.
func Signature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// hostOf reports the URL host without any embedded credentials, path or query.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}
