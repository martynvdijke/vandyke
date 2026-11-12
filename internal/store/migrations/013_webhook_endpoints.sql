-- Named webhook endpoints: the primary delivery configuration. The legacy
-- webhook.url/webhook.secret settings stay a fallback and are never written
-- here unless an operator adopts them.
CREATE TABLE webhook_endpoints (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL,
    url           TEXT NOT NULL,
    secret        TEXT NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    events        TEXT NOT NULL DEFAULT 'entry.created',
    preset        TEXT NOT NULL DEFAULT 'generic',
    allow_private INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX webhook_endpoints_name ON webhook_endpoints (name COLLATE NOCASE);
CREATE INDEX webhook_endpoints_enabled ON webhook_endpoints (enabled);

-- Delivery history. entry_id deliberately has no foreign key: purging an entry
-- must not erase the evidence that a purge event was delivered. endpoint_id is
-- nullable so the synthesized legacy target can record deliveries too.
CREATE TABLE webhook_deliveries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id     INTEGER REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event           TEXT NOT NULL,
    entry_id        INTEGER,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_status     INTEGER,
    last_error      TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT,
    payload         TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX webhook_deliveries_due ON webhook_deliveries (status, next_attempt_at);
CREATE INDEX webhook_deliveries_endpoint ON webhook_deliveries (endpoint_id, created_at);
CREATE INDEX webhook_deliveries_created_at ON webhook_deliveries (created_at);
