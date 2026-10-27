-- Optional photo evidence attached to a ledger entry.
CREATE TABLE entry_photos (
    entry_id     INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    content_type TEXT NOT NULL,
    data         BLOB NOT NULL,
    sha256       TEXT NOT NULL,
    size         INTEGER NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
