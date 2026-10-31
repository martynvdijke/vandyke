-- Collapse entries that share the same misspelling and correction, keeping the
-- earliest row, then prevent future duplicates.
DELETE FROM entries
WHERE id NOT IN (
    SELECT MIN(id) FROM entries GROUP BY word COLLATE NOCASE, meaning COLLATE NOCASE
);
DELETE FROM entries_fts;
INSERT INTO entries_fts(rowid, word, meaning) SELECT id, word, meaning FROM entries;
CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_word_meaning
    ON entries (word COLLATE NOCASE, meaning COLLATE NOCASE);
