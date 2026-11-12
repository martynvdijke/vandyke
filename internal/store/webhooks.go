package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// The five entry lifecycle events a webhook endpoint can subscribe to.
const (
	WebhookEventCreated  = "entry.created"
	WebhookEventUpdated  = "entry.updated"
	WebhookEventDeleted  = "entry.deleted"
	WebhookEventRestored = "entry.restored"
	WebhookEventPurged   = "entry.purged"
)

// WebhookStatuses are the four delivery states.
const (
	WebhookStatusPending   = "pending"
	WebhookStatusDelivered = "delivered"
	WebhookStatusFailed    = "failed"
	WebhookStatusCancelled = "cancelled"
)

// Webhook presets control how an event is rendered into a receiver body.
const (
	WebhookPresetGeneric = "generic"
	WebhookPresetDiscord = "discord"
	WebhookPresetSlack   = "slack"
)

// Webhook delivery limits and validation bounds.
const (
	// MaxWebhookEndpoints caps the number of configured endpoints.
	MaxWebhookEndpoints = 20
	// MaxWebhookNameLen caps an endpoint name.
	MaxWebhookNameLen = 100
	// MaxWebhookURLLen caps the stored target URL.
	MaxWebhookURLLen = 2048
	// MaxWebhookSecretLen caps a signing secret.
	MaxWebhookSecretLen = 512
	// WebhookMaxAttempts is the total number of attempts for one delivery.
	WebhookMaxAttempts = 8
	// WebhookDeliveredRetention is how long terminal delivery history is kept.
	WebhookDeliveredRetention = 7 * 24 * time.Hour
	// WebhookFailedRetention is how long failed delivery history is kept.
	WebhookFailedRetention = 30 * 24 * time.Hour
)

// ErrWebhookEndpointNotFound is returned when an endpoint does not exist.
var ErrWebhookEndpointNotFound = errors.New("webhook endpoint not found")

// ErrWebhookEndpointLimit is returned when the endpoint cap is reached.
var ErrWebhookEndpointLimit = fmt.Errorf("at most %d webhook endpoints are allowed", MaxWebhookEndpoints)

// ErrWebhookNameTaken is returned for a case-insensitive duplicate name.
var ErrWebhookNameTaken = errors.New("a webhook endpoint with that name already exists")

// ErrWebhookDeliveryNotFound is returned when a delivery does not exist.
var ErrWebhookDeliveryNotFound = errors.New("webhook delivery not found")

// ErrWebhookDeliveryNotRetryable is returned when a delivery cannot be
// manually retried because it is not in the failed state.
var ErrWebhookDeliveryNotRetryable = errors.New("webhook delivery is not in a retryable state")

// webhookEvents is the canonical, sorted event list.
var webhookEvents = []string{
	WebhookEventCreated,
	WebhookEventDeleted,
	WebhookEventPurged,
	WebhookEventRestored,
	WebhookEventUpdated,
}

// WebhookEvents returns a copy of the five supported event names in sorted
// order.
func WebhookEvents() []string {
	return append([]string(nil), webhookEvents...)
}

// ValidWebhookEvent reports whether name is a supported event.
func ValidWebhookEvent(name string) bool {
	for _, event := range webhookEvents {
		if event == name {
			return true
		}
	}
	return false
}

// ValidWebhookPreset reports whether preset is supported.
func ValidWebhookPreset(preset string) bool {
	switch preset {
	case WebhookPresetGeneric, WebhookPresetDiscord, WebhookPresetSlack:
		return true
	}
	return false
}

// WebhookEndpoint is one configured delivery target.
type WebhookEndpoint struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	Secret       string    `json:"-"`
	Enabled      bool      `json:"enabled"`
	Events       []string  `json:"events"`
	Preset       string    `json:"preset"`
	AllowPrivate bool      `json:"allow_private"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// WebhookDelivery is one delivery attempt record. EndpointID is nil for the
// synthesized legacy target.
type WebhookDelivery struct {
	ID            int64      `json:"id"`
	EndpointID    *int64     `json:"endpoint_id"`
	Event         string     `json:"event"`
	EntryID       *int64     `json:"entry_id"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	LastStatus    *int       `json:"last_status"`
	LastError     string     `json:"last_error"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	Payload       string     `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// WebhookDeliveryFilter selects delivery log rows.
type WebhookDeliveryFilter struct {
	EndpointID *int64
	Status     string
	Page       int
	PerPage    int
}

// WebhookDeliveryPage is a paginated delivery log.
type WebhookDeliveryPage struct {
	Deliveries []WebhookDelivery `json:"items"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PerPage    int               `json:"per_page"`
	Pages      int               `json:"pages"`
}

// WebhookDeliveryCount is a per-endpoint, per-status tally for the admin list.
type WebhookDeliveryCount struct {
	EndpointID *int64
	Status     string
	Count      int
}

// StoreMetrics is the scrape-time snapshot behind the Prometheus DB gauges.
type StoreMetrics struct {
	Total             int
	Live              int
	Trashed           int
	WithPhoto         int
	PendingDeliveries int
}

// ReadyState is the coarse readiness result of CheckReady. It intentionally
// carries no error text so a probe body cannot leak a DSN or path.
type ReadyState struct {
	Database   bool
	Migrations bool
}

// NormalizeWebhookEvents trims, de-duplicates, validates and sorts an event
// subscription list. An empty or unknown list is an error.
func NormalizeWebhookEvents(events []string) ([]string, error) {
	seen := make(map[string]bool, len(events))
	out := make([]string, 0, len(events))
	for _, event := range events {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		if !ValidWebhookEvent(event) {
			return nil, fmt.Errorf("unknown webhook event %q", event)
		}
		if seen[event] {
			continue
		}
		seen[event] = true
		out = append(out, event)
	}
	if len(out) == 0 {
		return nil, errors.New("select at least one event")
	}
	sort.Strings(out)
	return out, nil
}

// validateWebhookEndpoint normalises and checks an endpoint before it is
// written. URL shape only: DNS-based SSRF checks live in internal/webhook
// because they need a resolver and belong to the delivery path too.
func validateWebhookEndpoint(ep WebhookEndpoint) (WebhookEndpoint, error) {
	ep.Name = strings.TrimSpace(ep.Name)
	ep.URL = strings.TrimSpace(ep.URL)
	ep.Secret = strings.TrimSpace(ep.Secret)
	ep.Preset = strings.TrimSpace(strings.ToLower(ep.Preset))
	if ep.Preset == "" {
		ep.Preset = WebhookPresetGeneric
	}

	switch {
	case ep.Name == "":
		return WebhookEndpoint{}, errors.New("name is required")
	case utf8.RuneCountInString(ep.Name) > MaxWebhookNameLen:
		return WebhookEndpoint{}, fmt.Errorf("name must be at most %d characters", MaxWebhookNameLen)
	case ep.URL == "":
		return WebhookEndpoint{}, errors.New("URL is required")
	case len(ep.URL) > MaxWebhookURLLen:
		return WebhookEndpoint{}, fmt.Errorf("URL must be at most %d characters", MaxWebhookURLLen)
	case len(ep.Secret) > MaxWebhookSecretLen:
		return WebhookEndpoint{}, fmt.Errorf("secret must be at most %d characters", MaxWebhookSecretLen)
	case !ValidWebhookPreset(ep.Preset):
		return WebhookEndpoint{}, errors.New("preset must be generic, discord or slack")
	}

	parsed, err := url.Parse(ep.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return WebhookEndpoint{}, errors.New("URL must be an absolute http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return WebhookEndpoint{}, errors.New("URL must use http or https")
	}
	if parsed.User != nil {
		return WebhookEndpoint{}, errors.New("URL must not contain credentials")
	}

	events, err := NormalizeWebhookEvents(ep.Events)
	if err != nil {
		return WebhookEndpoint{}, err
	}
	ep.Events = events
	return ep, nil
}

// CheckWebhookEndpoint normalises and validates an endpoint without writing
// it. Admin handlers use it to turn validation failures into 422 responses
// before touching the database.
func CheckWebhookEndpoint(ep WebhookEndpoint) (WebhookEndpoint, error) {
	return validateWebhookEndpoint(ep)
}

// ListWebhookEndpoints returns every endpoint ordered by name. The configured
// cap keeps this small enough to load on every snapshot rebuild.
func (s *Store) ListWebhookEndpoints(ctx context.Context) ([]WebhookEndpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, url, secret, enabled, events, preset, allow_private, created_at, updated_at
		FROM webhook_endpoints ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("query webhook endpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var endpoints []WebhookEndpoint
	for rows.Next() {
		ep, err := scanWebhookEndpoint(rows)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook endpoints: %w", err)
	}
	return endpoints, nil
}

// CountEnabledWebhookEndpoints reports how many persisted endpoints are
// enabled. The legacy target is active only while this is zero.
func (s *Store) CountEnabledWebhookEndpoints(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhook_endpoints WHERE enabled = 1`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count enabled webhook endpoints: %w", err)
	}
	return count, nil
}

// GetWebhookEndpoint loads one endpoint.
func (s *Store) GetWebhookEndpoint(ctx context.Context, id int64) (WebhookEndpoint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, url, secret, enabled, events, preset, allow_private, created_at, updated_at
		FROM webhook_endpoints WHERE id = ?`, id)
	ep, err := scanWebhookEndpoint(row)
	if errors.Is(err, sql.ErrNoRows) {
		return WebhookEndpoint{}, ErrWebhookEndpointNotFound
	}
	return ep, err
}

// CreateWebhookEndpoint validates and inserts an endpoint. A new endpoint is
// enabled.
func (s *Store) CreateWebhookEndpoint(ctx context.Context, ep WebhookEndpoint) (WebhookEndpoint, error) {
	ep, err := validateWebhookEndpoint(ep)
	if err != nil {
		return WebhookEndpoint{}, err
	}
	// A newly created endpoint is always enabled; the toggle action is the
	// only way to switch one off.
	ep.Enabled = true
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_endpoints`).Scan(&count); err != nil {
		return WebhookEndpoint{}, fmt.Errorf("count webhook endpoints: %w", err)
	}
	if count >= MaxWebhookEndpoints {
		return WebhookEndpoint{}, ErrWebhookEndpointLimit
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_endpoints (name, url, secret, enabled, events, preset, allow_private)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ep.Name, ep.URL, ep.Secret, boolInt(ep.Enabled), strings.Join(ep.Events, ","), ep.Preset, boolInt(ep.AllowPrivate),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return WebhookEndpoint{}, ErrWebhookNameTaken
		}
		return WebhookEndpoint{}, fmt.Errorf("insert webhook endpoint: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return WebhookEndpoint{}, fmt.Errorf("webhook endpoint id: %w", err)
	}
	return s.GetWebhookEndpoint(ctx, id)
}

// UpdateWebhookEndpoint replaces the editable fields of an endpoint. The
// caller is responsible for keeping the secret when the form left it blank.
func (s *Store) UpdateWebhookEndpoint(ctx context.Context, ep WebhookEndpoint) (WebhookEndpoint, error) {
	ep, err := validateWebhookEndpoint(ep)
	if err != nil {
		return WebhookEndpoint{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_endpoints SET name = ?, url = ?, secret = ?, enabled = ?, events = ?,
			preset = ?, allow_private = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`,
		ep.Name, ep.URL, ep.Secret, boolInt(ep.Enabled), strings.Join(ep.Events, ","),
		ep.Preset, boolInt(ep.AllowPrivate), ep.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return WebhookEndpoint{}, ErrWebhookNameTaken
		}
		return WebhookEndpoint{}, fmt.Errorf("update webhook endpoint: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return WebhookEndpoint{}, fmt.Errorf("updated webhook endpoint rows: %w", err)
	}
	if n == 0 {
		return WebhookEndpoint{}, ErrWebhookEndpointNotFound
	}
	return s.GetWebhookEndpoint(ctx, ep.ID)
}

// SetWebhookEndpointEnabled toggles an endpoint. Disabling also cancels its
// pending deliveries so nothing is sent after the switch is turned off.
func (s *Store) SetWebhookEndpointEnabled(ctx context.Context, id int64, enabled bool) (WebhookEndpoint, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WebhookEndpoint{}, fmt.Errorf("begin webhook endpoint toggle: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE webhook_endpoints SET enabled = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		boolInt(enabled), id,
	)
	if err != nil {
		_ = tx.Rollback()
		return WebhookEndpoint{}, fmt.Errorf("toggle webhook endpoint: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return WebhookEndpoint{}, fmt.Errorf("toggled webhook endpoint rows: %w", err)
	}
	if n == 0 {
		_ = tx.Rollback()
		return WebhookEndpoint{}, ErrWebhookEndpointNotFound
	}
	if !enabled {
		if _, err := tx.ExecContext(ctx,
			`UPDATE webhook_deliveries SET status = ?, next_attempt_at = NULL, payload = '',
				last_error = 'endpoint disabled',
				updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE endpoint_id = ? AND status = ?`,
			WebhookStatusCancelled, id, WebhookStatusPending,
		); err != nil {
			_ = tx.Rollback()
			return WebhookEndpoint{}, fmt.Errorf("cancel pending webhook deliveries: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return WebhookEndpoint{}, fmt.Errorf("commit webhook endpoint toggle: %w", err)
	}
	return s.GetWebhookEndpoint(ctx, id)
}

// DeleteWebhookEndpoint deletes an endpoint; its deliveries cascade.
func (s *Store) DeleteWebhookEndpoint(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webhook_endpoints WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete webhook endpoint: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleted webhook endpoint rows: %w", err)
	}
	if n == 0 {
		return ErrWebhookEndpointNotFound
	}
	return nil
}

// CancelPendingWebhookDeliveries marks every pending delivery for an endpoint
// cancelled. Used when an endpoint is disabled outside the toggle path.
func (s *Store) CancelPendingWebhookDeliveries(ctx context.Context, endpointID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, next_attempt_at = NULL, payload = '',
			last_error = 'endpoint disabled',
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE endpoint_id = ? AND status = ?`,
		WebhookStatusCancelled, endpointID, WebhookStatusPending,
	)
	if err != nil {
		return 0, fmt.Errorf("cancel pending webhook deliveries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("cancelled webhook delivery rows: %w", err)
	}
	return n, nil
}

// CreateWebhookDelivery inserts a delivery record. Status defaults to pending.
func (s *Store) CreateWebhookDelivery(ctx context.Context, d WebhookDelivery) (WebhookDelivery, error) {
	if d.Status == "" {
		d.Status = WebhookStatusPending
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (endpoint_id, event, entry_id, status, attempts,
			last_status, last_error, next_attempt_at, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableID(d.EndpointID), d.Event, nullableID(d.EntryID), d.Status, d.Attempts,
		nullableInt(d.LastStatus), d.LastError, nullableTime(d.NextAttemptAt), d.Payload,
	)
	if err != nil {
		return WebhookDelivery{}, fmt.Errorf("insert webhook delivery: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return WebhookDelivery{}, fmt.Errorf("webhook delivery id: %w", err)
	}
	return s.GetWebhookDelivery(ctx, id)
}

// GetWebhookDelivery loads one delivery record.
func (s *Store) GetWebhookDelivery(ctx context.Context, id int64) (WebhookDelivery, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+webhookDeliveryColumns+` FROM webhook_deliveries WHERE id = ?`, id)
	d, err := scanWebhookDelivery(row)
	if errors.Is(err, sql.ErrNoRows) {
		return WebhookDelivery{}, ErrWebhookDeliveryNotFound
	}
	return d, err
}

// UpdateWebhookDelivery writes the mutable delivery fields. A terminal status
// (delivered or cancelled) clears the stored payload so an entry's text does
// not outlive a successful or abandoned delivery.
func (s *Store) UpdateWebhookDelivery(ctx context.Context, d WebhookDelivery) error {
	payload := d.Payload
	switch d.Status {
	case WebhookStatusDelivered, WebhookStatusCancelled:
		payload = ""
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, attempts = ?, last_status = ?, last_error = ?,
			next_attempt_at = ?, payload = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`,
		d.Status, d.Attempts, nullableInt(d.LastStatus), d.LastError,
		nullableTime(d.NextAttemptAt), payload, d.ID,
	)
	if err != nil {
		return fmt.Errorf("update webhook delivery: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("updated webhook delivery rows: %w", err)
	}
	if n == 0 {
		return ErrWebhookDeliveryNotFound
	}
	return nil
}

// ListWebhookDeliveries returns a filtered, paginated delivery log.
func (s *Store) ListWebhookDeliveries(ctx context.Context, filter WebhookDeliveryFilter) (WebhookDeliveryPage, error) {
	perPage := filter.PerPage
	if perPage <= 0 {
		perPage = 25
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}

	var (
		where []string
		args  []any
	)
	if filter.EndpointID != nil {
		where = append(where, `endpoint_id = ?`)
		args = append(args, *filter.EndpointID)
	}
	if validWebhookStatus(filter.Status) {
		where = append(where, `status = ?`)
		args = append(args, filter.Status)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_deliveries`+clause, args...).Scan(&total); err != nil {
		return WebhookDeliveryPage{}, fmt.Errorf("count webhook deliveries: %w", err)
	}
	pages := 0
	if total > 0 {
		pages = (total + perPage - 1) / perPage
	}
	if pages > 0 && page > pages {
		page = pages
	}

	query := `SELECT ` + webhookDeliveryColumns + ` FROM webhook_deliveries` + clause +
		` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return WebhookDeliveryPage{}, fmt.Errorf("list webhook deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := WebhookDeliveryPage{Total: total, Page: page, Pages: pages, PerPage: perPage}
	for rows.Next() {
		d, err := scanWebhookDelivery(rows)
		if err != nil {
			return WebhookDeliveryPage{}, err
		}
		out.Deliveries = append(out.Deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return WebhookDeliveryPage{}, fmt.Errorf("iterate webhook deliveries: %w", err)
	}
	return out, nil
}

// DueWebhookDeliveries returns pending rows whose next attempt is due.
func (s *Store) DueWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+webhookDeliveryColumns+` FROM webhook_deliveries
		WHERE status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		ORDER BY next_attempt_at ASC, id ASC LIMIT ?`,
		WebhookStatusPending, now.UTC().Format(time.RFC3339), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query due webhook deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var due []WebhookDelivery
	for rows.Next() {
		d, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		due = append(due, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due webhook deliveries: %w", err)
	}
	return due, nil
}

// CanWebhookDeliveryRetry reports whether a delivery exists and is in the
// failed state. Delivered and cancelled rows are history, not retry work.
func (s *Store) CanWebhookDeliveryRetry(ctx context.Context, id int64) (bool, error) {
	d, err := s.GetWebhookDelivery(ctx, id)
	if err != nil {
		return false, err
	}
	return d.Status == WebhookStatusFailed, nil
}

// RetryWebhookDeliveryNow puts a failed delivery back in the pending state
// without resetting its attempt count, so an operator always gets one attempt
// after asking.
func (s *Store) RetryWebhookDeliveryNow(ctx context.Context, id int64, now time.Time) error {
	ok, err := s.CanWebhookDeliveryRetry(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrWebhookDeliveryNotRetryable
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, next_attempt_at = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		WebhookStatusPending, now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339), id, WebhookStatusFailed,
	)
	if err != nil {
		return fmt.Errorf("retry webhook delivery: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("retried webhook delivery rows: %w", err)
	}
	if n == 0 {
		return ErrWebhookDeliveryNotRetryable
	}
	return nil
}

// PruneWebhookDeliveries deletes delivered and cancelled rows older than 7
// days and failed rows older than 30 days. Pending rows are never pruned.
func (s *Store) PruneWebhookDeliveries(ctx context.Context, now time.Time) (int64, error) {
	deliveredCutoff := now.UTC().Add(-WebhookDeliveredRetention).Format(time.RFC3339)
	failedCutoff := now.UTC().Add(-WebhookFailedRetention).Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM webhook_deliveries WHERE
			(status IN (?, ?) AND created_at < ?) OR
			(status = ? AND created_at < ?)`,
		WebhookStatusDelivered, WebhookStatusCancelled, deliveredCutoff,
		WebhookStatusFailed, failedCutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("prune webhook deliveries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("pruned webhook delivery rows: %w", err)
	}
	return n, nil
}

// WebhookDeliveryCounts returns a per-endpoint, per-status tally for the admin
// endpoint list. The legacy target is reported with a nil endpoint id.
func (s *Store) WebhookDeliveryCounts(ctx context.Context) ([]WebhookDeliveryCount, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT endpoint_id, status, COUNT(*) FROM webhook_deliveries
		GROUP BY endpoint_id, status`)
	if err != nil {
		return nil, fmt.Errorf("count webhook deliveries by endpoint: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var counts []WebhookDeliveryCount
	for rows.Next() {
		var (
			c      WebhookDeliveryCount
			epID   sql.NullInt64
			status string
			total  int
		)
		if err := rows.Scan(&epID, &status, &total); err != nil {
			return nil, fmt.Errorf("scan webhook delivery count: %w", err)
		}
		if epID.Valid {
			id := epID.Int64
			c.EndpointID = &id
		}
		c.Status = status
		c.Count = total
		counts = append(counts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook delivery counts: %w", err)
	}
	return counts, nil
}

// MetricsSnapshot returns the DB-backed gauges for one metrics scrape.
func (s *Store) MetricsSnapshot(ctx context.Context) (StoreMetrics, error) {
	var m StoreMetrics
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM entries),
			(SELECT COUNT(*) FROM entries WHERE deleted_at IS NULL),
			(SELECT COUNT(*) FROM entries WHERE deleted_at IS NOT NULL),
			(SELECT COUNT(*) FROM entries e
				WHERE e.deleted_at IS NULL
				AND EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = ?)`,
		WebhookStatusPending,
	).Scan(&m.Total, &m.Live, &m.Trashed, &m.WithPhoto, &m.PendingDeliveries)
	if err != nil {
		return StoreMetrics{}, fmt.Errorf("query metrics snapshot: %w", err)
	}
	return m, nil
}

// CheckReady verifies that SQLite answers and every embedded migration is
// recorded. It returns coarse booleans only; callers must not surface errors.
func (s *Store) CheckReady(ctx context.Context) ReadyState {
	var one int
	if err := s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return ReadyState{}
	}
	state := ReadyState{Database: true}

	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return state
	}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return state
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]bool, len(names))
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return state
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return state
	}
	for _, name := range names {
		if !applied[filepath.Base(name)] {
			return state
		}
	}
	state.Migrations = true
	return state
}

const webhookDeliveryColumns = `id, endpoint_id, event, entry_id, status, attempts,
	last_status, last_error, next_attempt_at, payload, created_at, updated_at`

func scanWebhookEndpoint(row rowScanner) (WebhookEndpoint, error) {
	var (
		ep        WebhookEndpoint
		enabled   int
		allow     int
		events    string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&ep.ID, &ep.Name, &ep.URL, &ep.Secret, &enabled, &events,
		&ep.Preset, &allow, &createdAt, &updatedAt); err != nil {
		return WebhookEndpoint{}, err
	}
	ep.Enabled = enabled != 0
	ep.AllowPrivate = allow != 0
	ep.Events = splitWebhookEvents(events)
	var err error
	if ep.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
		return WebhookEndpoint{}, fmt.Errorf("parse webhook endpoint created_at: %w", err)
	}
	if ep.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt); err != nil {
		return WebhookEndpoint{}, fmt.Errorf("parse webhook endpoint updated_at: %w", err)
	}
	return ep, nil
}

func scanWebhookDelivery(row rowScanner) (WebhookDelivery, error) {
	var (
		d             WebhookDelivery
		endpointID    sql.NullInt64
		entryID       sql.NullInt64
		lastStatus    sql.NullInt64
		nextAttemptAt sql.NullString
		createdAt     string
		updatedAt     string
	)
	if err := row.Scan(&d.ID, &endpointID, &d.Event, &entryID, &d.Status, &d.Attempts,
		&lastStatus, &d.LastError, &nextAttemptAt, &d.Payload, &createdAt, &updatedAt); err != nil {
		return WebhookDelivery{}, err
	}
	if endpointID.Valid {
		id := endpointID.Int64
		d.EndpointID = &id
	}
	if entryID.Valid {
		id := entryID.Int64
		d.EntryID = &id
	}
	if lastStatus.Valid {
		status := int(lastStatus.Int64)
		d.LastStatus = &status
	}
	if nextAttemptAt.Valid && nextAttemptAt.String != "" {
		t, err := time.Parse(time.RFC3339, nextAttemptAt.String)
		if err != nil {
			return WebhookDelivery{}, fmt.Errorf("parse webhook delivery next_attempt_at: %w", err)
		}
		d.NextAttemptAt = &t
	}
	var err error
	if d.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
		return WebhookDelivery{}, fmt.Errorf("parse webhook delivery created_at: %w", err)
	}
	if d.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt); err != nil {
		return WebhookDelivery{}, fmt.Errorf("parse webhook delivery updated_at: %w", err)
	}
	return d, nil
}

func splitWebhookEvents(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if event := strings.TrimSpace(part); event != "" {
			out = append(out, event)
		}
	}
	return out
}

func validWebhookStatus(status string) bool {
	switch status {
	case WebhookStatusPending, WebhookStatusDelivered, WebhookStatusFailed, WebhookStatusCancelled:
		return true
	}
	return false
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableID(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}
