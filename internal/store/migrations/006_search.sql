CREATE VIRTUAL TABLE entries_fts USING fts5(word, meaning, tokenize='trigram');
INSERT INTO entries_fts(rowid, word, meaning) SELECT id, word, meaning FROM entries;
