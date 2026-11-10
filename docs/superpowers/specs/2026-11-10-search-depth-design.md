# Search depth: person, date ranges, grammar, highlighting and rebuild

**Date:** 2026-11-10
**Status:** implemented
**Change:** `openspec/changes/add-search-depth`

## Why

`entries_fts` indexed only `word` and `meaning`, so a slip's person could not be
searched at all, only filtered by exact name. Filters stopped at one calendar
year, the ledger showed no indication of what matched, and `RebuildSearchIndex`
had no caller, leaving a drifted index with no repair path.

## Index change

FTS5 has no `ADD COLUMN`. Migration `010_search_person.sql` therefore drops
`entries_fts`, recreates it with `word`, `meaning`, `person` and
`tokenize='trigram'`, and backfills from `entries` in the same transaction. The
backfill indexes only rows with `deleted_at IS NULL`; trashed entries become
searchable again when they are restored, because restore rewrites their index
row.

Every maintenance site names all three columns:

- `insertEntryTx` / the seed import (`indexEntry`),
- `UpdateEntry`'s `INSERT OR REPLACE`,
- `replaceEntryTx` in the import path,
- `RestoreEntries`, which reindexes after clearing `deleted_at`,
- `RebuildSearchIndex`, which selects from `entries WHERE deleted_at IS NULL`.

`SoftDeleteEntries` deletes the index rows in the same transaction as the
`deleted_at` update, and `PurgeEntries` still removes them before the entry row,
so search does not rely on the trash flag alone.

## Query grammar

`internal/searchquery` holds the single parser used by both the SQL builder and
the highlighter:

- bare terms are required (AND),
- `"..."` is one phrase,
- a leading `-` excludes the following term or phrase,
- an unmatched quote is a literal character.

`Query.Match()` returns an FTS5 expression or `""`. It returns `""` when there
is no positive term, or when any word (in an include, an exclude, or inside a
phrase) is shorter than three runes, which keeps the existing `LIKE` fallback.
Precedence is made explicit with parentheses, and every term is emitted as an
FTS5 string literal with embedded quotes doubled; user text is never
interpolated into SQL. `Query.LikePatterns()` returns escaped `%...%` patterns
that the store binds as parameters.

The `LIKE` path covers `word`, `meaning` and `person`, applies exclusions with
`NOT (...)`, and keeps the escaped wildcard behaviour (`%`, `_`, `\`).

## Date filters

`store.ListParams` gained `From` and `To` as `time.Time` (zero = unset). The
filter is `created_at >= from` and `created_at < to + 1 day`, matching the
stored RFC 3339 UTC strings and making both boundary dates inclusive. `year`
and the date bounds each add their own clause, so they intersect.

`GET /api/v1/entries` parses `YYYY-MM-DD` and answers `400 invalid_date` when it
cannot. The web page ignores an unparseable bound and leaves it unapplied.

## Highlighting

`ledgerData.Entries` is a `[]ledgerRow` embedding `store.Entry` plus
`WordHTML` and `MeaningHTML` as `template.HTML`. The highlighter walks the raw
text rune by rune, compares case-insensitively against the included phrases,
escapes every segment with `html.EscapeString`, and emits only its own `<mark>`
tags. Excluded phrases are never marked because they are never in
`Query.Include`. `GET /entries?highlight=0` renders the plain fields, and the
generated sort/pagination links preserve the opt-out. The JSON API is unchanged.

## Rebuild action

`POST /admin/search/rebuild` sits behind `requireAuth` and `withCSRF`, calls
`Store.RebuildSearchIndex`, and redirects to `/admin/settings?rebuilt=1`. A
failure is logged and rendered on the settings page as an error rather than a
false confirmation. The settings page carries a "Search index" card describing
the derived nature of the index.

## Tests

- `internal/searchquery`: parse and FTS-expression tables, LIKE escaping.
- `internal/store`: person follows create/update/trash/restore; inclusive
  date bounds, open-ended ranges and the `year` intersection; rebuild
  repopulates the live person and skips trash; phrase and exclusion execution
  through real FTS5.
- `internal/httpapi`: date filters, `invalid_date`, person-only search.
- `internal/web`: `<mark>` placement, excluded terms unmarked, `highlight=0`,
  escaped markup, date range rendering.
- `internal/admin`: rebuild success, missing session, missing CSRF.
