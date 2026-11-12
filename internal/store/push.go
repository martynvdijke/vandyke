package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PushDevice is one registered mobile client for push notifications.
type PushDevice struct {
	ID                   int64
	Token                string
	Platform             string
	DeviceName           string
	AppVersion           string
	NotificationsEnabled bool
	CreatedAt            time.Time
	LastSeenAt           time.Time
	DisabledAt           *time.Time
}

// WebPushSubscription is one browser push subscription.
type WebPushSubscription struct {
	ID                   int64
	Endpoint             string
	P256DH               string
	Auth                 string
	UserAgent            string
	AdminID              *int64
	NotificationsEnabled bool
	CreatedAt            time.Time
	LastSeenAt           time.Time
	DisabledAt           *time.Time
}

// AdminNotificationPrefs holds an administrator's notification toggles.
type AdminNotificationPrefs struct {
	AdminID      int64
	Realtime     bool
	WebPush      bool
	EntryCreated bool
}

func parsePushTime(column, raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s %q: %w", column, raw, err)
	}
	return parsed.UTC(), nil
}

func scanPushDevice(row rowScanner) (PushDevice, error) {
	var (
		device     PushDevice
		createdAt  string
		lastSeenAt string
		disabledAt sql.NullString
	)
	if err := row.Scan(&device.ID, &device.Token, &device.Platform, &device.DeviceName,
		&device.AppVersion, &device.NotificationsEnabled, &createdAt, &lastSeenAt, &disabledAt); err != nil {
		return PushDevice{}, err
	}
	var err error
	if device.CreatedAt, err = parsePushTime("created_at", createdAt); err != nil {
		return PushDevice{}, err
	}
	if device.LastSeenAt, err = parsePushTime("last_seen_at", lastSeenAt); err != nil {
		return PushDevice{}, err
	}
	if disabledAt.Valid {
		disabled, err := parsePushTime("disabled_at", disabledAt.String)
		if err != nil {
			return PushDevice{}, err
		}
		device.DisabledAt = &disabled
	}
	return device, nil
}

func scanWebPushSubscription(row rowScanner) (WebPushSubscription, error) {
	var (
		sub        WebPushSubscription
		adminID    sql.NullInt64
		createdAt  string
		lastSeenAt string
		disabledAt sql.NullString
	)
	if err := row.Scan(&sub.ID, &sub.Endpoint, &sub.P256DH, &sub.Auth, &sub.UserAgent,
		&adminID, &sub.NotificationsEnabled, &createdAt, &lastSeenAt, &disabledAt); err != nil {
		return WebPushSubscription{}, err
	}
	if adminID.Valid {
		id := adminID.Int64
		sub.AdminID = &id
	}
	var err error
	if sub.CreatedAt, err = parsePushTime("created_at", createdAt); err != nil {
		return WebPushSubscription{}, err
	}
	if sub.LastSeenAt, err = parsePushTime("last_seen_at", lastSeenAt); err != nil {
		return WebPushSubscription{}, err
	}
	if disabledAt.Valid {
		disabled, err := parsePushTime("disabled_at", disabledAt.String)
		if err != nil {
			return WebPushSubscription{}, err
		}
		sub.DisabledAt = &disabled
	}
	return sub, nil
}

const pushDeviceColumns = `id, token, platform, device_name, app_version,
	notifications_enabled, created_at, last_seen_at, disabled_at`

const webPushSubscriptionColumns = `id, endpoint, p256dh, auth, user_agent,
	admin_id, notifications_enabled, created_at, last_seen_at, disabled_at`

// UpsertPushDevice inserts or refreshes a token. On conflict it updates
// platform/device_name/app_version, re-enables the device (disabled_at = NULL),
// sets notifications_enabled = true, and bumps last_seen_at. Empty platform
// defaults to "android".
func (s *Store) UpsertPushDevice(ctx context.Context, d PushDevice) (PushDevice, error) {
	token := strings.TrimSpace(d.Token)
	if token == "" {
		return PushDevice{}, errors.New("push device token is required")
	}
	platform := strings.TrimSpace(d.Platform)
	if platform == "" {
		platform = "android"
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO push_devices (token, platform, device_name, app_version, notifications_enabled)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT(token) DO UPDATE SET
			platform = excluded.platform,
			device_name = excluded.device_name,
			app_version = excluded.app_version,
			notifications_enabled = 1,
			disabled_at = NULL,
			last_seen_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		token, platform, strings.TrimSpace(d.DeviceName), strings.TrimSpace(d.AppVersion),
	); err != nil {
		return PushDevice{}, fmt.Errorf("upsert push device: %w", err)
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+pushDeviceColumns+` FROM push_devices WHERE token = ?`, token)
	device, err := scanPushDevice(row)
	if err != nil {
		return PushDevice{}, fmt.Errorf("load push device: %w", err)
	}
	return device, nil
}

// DeletePushDevice removes the row entirely.
func (s *Store) DeletePushDevice(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM push_devices WHERE token = ?`, strings.TrimSpace(token),
	); err != nil {
		return fmt.Errorf("delete push device: %w", err)
	}
	return nil
}

// ListActivePushDevices returns enabled, non-disabled devices ordered by id.
func (s *Store) ListActivePushDevices(ctx context.Context) ([]PushDevice, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+pushDeviceColumns+` FROM push_devices
		WHERE notifications_enabled = 1 AND disabled_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query push devices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	devices := make([]PushDevice, 0)
	for rows.Next() {
		device, err := scanPushDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate push devices: %w", err)
	}
	return devices, nil
}

// DisablePushDevices marks the given tokens disabled (sets disabled_at,
// notifications_enabled = 0) in one statement; unknown tokens are ignored.
func (s *Store) DisablePushDevices(ctx context.Context, tokens []string) error {
	clean := make([]any, 0, len(tokens))
	for _, token := range tokens {
		if t := strings.TrimSpace(token); t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE push_devices SET notifications_enabled = 0,
			disabled_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE token IN (`+placeholders(len(clean))+`)`, clean...,
	); err != nil {
		return fmt.Errorf("disable push devices: %w", err)
	}
	return nil
}

// UpsertWebPushSubscription inserts or refreshes by endpoint. On conflict it
// updates p256dh/auth/user_agent, optionally adopts a non-nil admin_id,
// re-enables the subscription, and bumps last_seen_at.
func (s *Store) UpsertWebPushSubscription(ctx context.Context, sub WebPushSubscription) (WebPushSubscription, error) {
	endpoint := strings.TrimSpace(sub.Endpoint)
	if endpoint == "" {
		return WebPushSubscription{}, errors.New("web push endpoint is required")
	}
	var adminID any
	if sub.AdminID != nil {
		adminID = *sub.AdminID
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO web_push_subscriptions (endpoint, p256dh, auth, user_agent, admin_id, notifications_enabled)
		VALUES (?, ?, ?, ?, ?, 1)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			user_agent = excluded.user_agent,
			admin_id = COALESCE(excluded.admin_id, web_push_subscriptions.admin_id),
			notifications_enabled = 1,
			disabled_at = NULL,
			last_seen_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		endpoint, strings.TrimSpace(sub.P256DH), strings.TrimSpace(sub.Auth),
		strings.TrimSpace(sub.UserAgent), adminID,
	); err != nil {
		return WebPushSubscription{}, fmt.Errorf("upsert web push subscription: %w", err)
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+webPushSubscriptionColumns+` FROM web_push_subscriptions WHERE endpoint = ?`, endpoint)
	stored, err := scanWebPushSubscription(row)
	if err != nil {
		return WebPushSubscription{}, fmt.Errorf("load web push subscription: %w", err)
	}
	return stored, nil
}

// DeleteWebPushSubscription removes the row entirely.
func (s *Store) DeleteWebPushSubscription(ctx context.Context, endpoint string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM web_push_subscriptions WHERE endpoint = ?`, strings.TrimSpace(endpoint),
	); err != nil {
		return fmt.Errorf("delete web push subscription: %w", err)
	}
	return nil
}

// ListActiveWebPushSubscriptions returns enabled, non-disabled subscriptions
// ordered by id.
func (s *Store) ListActiveWebPushSubscriptions(ctx context.Context) ([]WebPushSubscription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+webPushSubscriptionColumns+` FROM web_push_subscriptions
		WHERE notifications_enabled = 1 AND disabled_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query web push subscriptions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	subs := make([]WebPushSubscription, 0)
	for rows.Next() {
		sub, err := scanWebPushSubscription(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate web push subscriptions: %w", err)
	}
	return subs, nil
}

// DisableWebPushSubscriptions marks the given endpoints disabled in one
// statement; unknown endpoints are ignored.
func (s *Store) DisableWebPushSubscriptions(ctx context.Context, endpoints []string) error {
	clean := make([]any, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if e := strings.TrimSpace(endpoint); e != "" {
			clean = append(clean, e)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE web_push_subscriptions SET notifications_enabled = 0,
			disabled_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE endpoint IN (`+placeholders(len(clean))+`)`, clean...,
	); err != nil {
		return fmt.Errorf("disable web push subscriptions: %w", err)
	}
	return nil
}

// GetAdminNotificationPrefs returns the stored row or all-true defaults with
// AdminID set when no row exists (never an error for a missing row).
func (s *Store) GetAdminNotificationPrefs(ctx context.Context, adminID int64) (AdminNotificationPrefs, error) {
	prefs := AdminNotificationPrefs{AdminID: adminID, Realtime: true, WebPush: true, EntryCreated: true}
	err := s.db.QueryRowContext(ctx,
		`SELECT realtime_enabled, web_push_enabled, entry_created
		FROM admin_notification_prefs WHERE admin_id = ?`, adminID,
	).Scan(&prefs.Realtime, &prefs.WebPush, &prefs.EntryCreated)
	if errors.Is(err, sql.ErrNoRows) {
		return prefs, nil
	}
	if err != nil {
		return AdminNotificationPrefs{}, fmt.Errorf("query admin notification prefs: %w", err)
	}
	return prefs, nil
}

// SaveAdminNotificationPrefs upserts all three flags and bumps updated_at.
func (s *Store) SaveAdminNotificationPrefs(ctx context.Context, p AdminNotificationPrefs) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_notification_prefs (admin_id, realtime_enabled, web_push_enabled, entry_created)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(admin_id) DO UPDATE SET
			realtime_enabled = excluded.realtime_enabled,
			web_push_enabled = excluded.web_push_enabled,
			entry_created = excluded.entry_created,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		p.AdminID, boolInt(p.Realtime), boolInt(p.WebPush), boolInt(p.EntryCreated),
	); err != nil {
		return fmt.Errorf("save admin notification prefs: %w", err)
	}
	return nil
}
