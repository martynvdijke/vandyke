-- Push notification registrations and per-admin preferences.
CREATE TABLE push_devices (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    token                 TEXT NOT NULL UNIQUE,
    platform              TEXT NOT NULL DEFAULT 'android',
    device_name           TEXT NOT NULL DEFAULT '',
    app_version           TEXT NOT NULL DEFAULT '',
    notifications_enabled INTEGER NOT NULL DEFAULT 1,
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    last_seen_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    disabled_at           TEXT
);
CREATE INDEX push_devices_active ON push_devices (notifications_enabled, disabled_at);

CREATE TABLE web_push_subscriptions (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint              TEXT NOT NULL UNIQUE,
    p256dh                TEXT NOT NULL,
    auth                  TEXT NOT NULL,
    user_agent            TEXT NOT NULL DEFAULT '',
    admin_id              INTEGER REFERENCES admins (id) ON DELETE SET NULL,
    notifications_enabled INTEGER NOT NULL DEFAULT 1,
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    last_seen_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    disabled_at           TEXT
);
CREATE INDEX web_push_subscriptions_active ON web_push_subscriptions (notifications_enabled, disabled_at);
CREATE INDEX web_push_subscriptions_admin ON web_push_subscriptions (admin_id);

CREATE TABLE admin_notification_prefs (
    admin_id          INTEGER PRIMARY KEY REFERENCES admins (id) ON DELETE CASCADE,
    realtime_enabled  INTEGER NOT NULL DEFAULT 1,
    web_push_enabled  INTEGER NOT NULL DEFAULT 1,
    entry_created     INTEGER NOT NULL DEFAULT 1,
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
