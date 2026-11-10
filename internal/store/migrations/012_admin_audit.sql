-- Append-only audit trail for authentication, entry, settings and admin
-- actions. actor_id deliberately has no foreign key so deleting an admin does
-- not erase history; actor_name snapshots the username at write time.
CREATE TABLE admin_audit (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id   INTEGER,
    actor_name TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    subject    TEXT NOT NULL DEFAULT '',
    detail     TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX admin_audit_created_at ON admin_audit(created_at);
CREATE INDEX admin_audit_action ON admin_audit(action);
CREATE INDEX admin_audit_actor_id ON admin_audit(actor_id);
