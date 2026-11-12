-- Entry tags: a curated vocabulary the admin assigns to entries. The name is
-- unique case-insensitively, the slug is derived once and then immutable so a
-- rename never breaks a /tags/{slug} link.
CREATE TABLE tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL,
    color      TEXT NOT NULL DEFAULT '#8c3b3b',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE UNIQUE INDEX idx_tags_name ON tags (name COLLATE NOCASE);
CREATE UNIQUE INDEX idx_tags_slug ON tags (slug COLLATE NOCASE);

-- Assignments cascade from both sides: deleting a tag detaches it from every
-- entry, purging an entry removes its join rows.
CREATE TABLE entry_tags (
    entry_id   INTEGER NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (entry_id, tag_id)
);

CREATE INDEX idx_entry_tags_tag ON entry_tags (tag_id, entry_id);

-- The committee's everyday vocabulary. Seeding here means it happens exactly
-- once (schema_migrations), so a later rename or delete is permanent. No entry
-- is classified by the migration.
INSERT INTO tags (name, slug, color) VALUES
    ('typo', 'typo', '#8c3b3b'),
    ('misheard', 'misheard', '#3b5a8c'),
    ('autocorrect', 'autocorrect', '#3b7a57'),
    ('name', 'name', '#8c6b3b'),
    ('dialect', 'dialect', '#6b3b8c');
