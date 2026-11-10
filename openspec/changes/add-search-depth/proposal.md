## Why

`entries_fts` covers `word` and `meaning` only, while `person` is a filter that matches one exact name (`internal/store/store.go:489`) and cannot be searched at all. A single token shorter than three runes makes `ftsMatch` return empty (`store.go:502`, `store.go:509`) and the whole query falls back to `LIKE '%q%'` (`store.go:470`), which is correct but gives no way to exclude a term or search a phrase. Filters stop at a single calendar year, the ledger shows raw text with no indication of what matched, and `RebuildSearchIndex` (`store.go:616`) has no caller, so a drifted index can only be repaired by hand.

## What Changes

- Add `person` to the trigram index with a migration that recreates `entries_fts`, and maintain it on every create, update, delete, seed and rebuild path.
- Add inclusive `from` and `to` date filters on `GET /entries` and `GET /api/v1/entries`, accepting `YYYY-MM-DD` alongside the existing `year` filter.
- Parse queries into positive phrases and `-` exclusions in a new `internal/searchquery` package shared by the SQL builder and the renderer.
- Highlight matched positive terms in the ledger partial with a single `<mark>` element and full escaping of everything else, with `highlight=0` on `GET /entries` as the opt-out; the JSON API keeps returning unmarked text.
- Add `POST /admin/search/rebuild`, a CSRF-protected action on the admin settings page that calls `RebuildSearchIndex`.
- Keep the sub-three-rune `LIKE` fallback, including for exclusion terms.

### Non-goals

- Fuzzy or edit-distance matching.
- Stemming or language-specific analysis.
- An external search engine or a second datastore.
- Semantic or vector search.

## Capabilities

### New Capabilities

- `ledger-search`: Searchable fields, date-range filters, a small query grammar, match highlighting and an operator-triggered index rebuild.

## Impact

- `internal/store/migrations/010_search_person.sql`: new migration recreating `entries_fts` with `person`.
- `internal/store/store.go`: `ListParams.From` and `ListParams.To`, index maintenance including `person`, `RebuildSearchIndex` writing `person`.
- `internal/searchquery`: new package for parsing, FTS expression building and LIKE fallback decisions.
- `internal/httpapi/api.go`: `from` and `to` parameters with validation.
- `internal/web/web.go`, `internal/web/templates/base.html`, `internal/web/templates/partials.html`: date inputs, highlight rendering.
- `internal/admin/admin.go`, `internal/admin/templates/settings.html`: rebuild route and control.
