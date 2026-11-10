-- Soft deletion: NULL deleted_at marks a live entry, an RFC 3339 UTC
-- timestamp marks a trashed entry awaiting restore or explicit purge.
ALTER TABLE entries ADD COLUMN deleted_at TEXT;

CREATE INDEX IF NOT EXISTS idx_entries_deleted_at ON entries (deleted_at);
