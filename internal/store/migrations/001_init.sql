-- Initial schema: the misspelling ledger.
CREATE TABLE entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    word       TEXT NOT NULL,
    meaning    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX idx_entries_word ON entries (word COLLATE NOCASE);
CREATE INDEX idx_entries_meaning ON entries (meaning COLLATE NOCASE);
CREATE INDEX idx_entries_created_at ON entries (created_at);
