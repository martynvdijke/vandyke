-- FTS5 has no ADD COLUMN, so the trigram index is recreated with the person
-- field. It is derived data: the backfill indexes only live rows, matching the
-- visibility rules of the ledger, and the admin rebuild action can regenerate
-- it at any time.
DROP TABLE IF EXISTS entries_fts;

CREATE VIRTUAL TABLE entries_fts USING fts5(word, meaning, person, tokenize='trigram');

INSERT INTO entries_fts(rowid, word, meaning, person)
SELECT id, word, meaning, person FROM entries WHERE deleted_at IS NULL;
