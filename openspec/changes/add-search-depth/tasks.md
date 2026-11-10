## 1. Query Parser

- [x] 1.1 Add `internal/searchquery` with `Phrase`, `Query`, `Parse`, `Query.Match` and `Query.LikePatterns`.
- [x] 1.2 Implement whitespace AND, `"..."` phrases, leading `-` exclusion and literal handling of an unmatched quote.
- [x] 1.3 Return an empty `Match` result when any term is shorter than three runes or when the query has only exclusions.
- [x] 1.4 Add table tests covering bare terms, phrases, exclusions, short terms, punctuation-only input and the empty query.

## 2. Person In The Index

- [x] 2.1 Add `internal/store/migrations/010_search_person.sql` dropping `entries_fts`, recreating it with `word`, `meaning`, `person` and `tokenize='trigram'`, and backfilling from `entries`.
- [x] 2.2 Extend `indexEntry` to write `person` and update its create and seed call sites. (the seed snapshot has no person field, so the seed call passes an empty person.)
- [x] 2.3 Update the `INSERT OR REPLACE` in `UpdateEntry` to write all three columns.
- [x] 2.4 Update `RebuildSearchIndex` to select and insert `person`, excluding rows with `deleted_at IS NOT NULL`.
- [x] 2.5 Confirm `DeleteEntries` still removes the rowid from the index. (soft delete now deletes the rowid rows in the same transaction and `RestoreEntries` reindexes all three columns, so a trashed row is not merely hidden by the join.)

## 3. Date Filters

- [x] 3.1 Add `From` and `To` as `time.Time` to `store.ListParams` and add the inclusive bounds to `ledgerFilter`, advancing `To` by one day.
- [x] 3.2 Parse `from` and `to` in `internal/httpapi/api.go`, returning `400 invalid_date` when the format is not `YYYY-MM-DD`. (kept `400 invalid_date` as the approved spec and design require; the orchestrator brief mentioned 422.)
- [x] 3.3 Parse `from` and `to` in the web handler, ignoring unparseable values.
- [x] 3.4 Add `from` and `to` date inputs to the toolbar in `base.html` and persist both in `ledgerQuery.values`. (plus minimal matching CSS for the date inputs.)
- [x] 3.5 Show the active range in the ledger meta line next to the year and person filters.
- [x] 3.6 Update the search input placeholder and its hint so phrases and `-` exclusion are discoverable.

## 4. Highlighting

- [x] 4.1 Add a `ledgerRow` view type carrying `WordHTML` and `MeaningHTML` as `template.HTML`, built from `searchquery.Parse`. (it embeds `store.Entry` so the plain JSON shape and fields stay untouched.)
- [x] 4.2 Escape the source text before wrapping each included phrase occurrence in `<mark>`, and skip excluded phrases.
- [x] 4.3 Render `WordHTML` and `MeaningHTML` in `partials.html`, falling back to the plain fields when `highlight` is off.
- [x] 4.4 Add `highlight` to `ledgerQuery` with a default of on, accept `highlight=0`, and persist it in generated links.
- [x] 4.5 Confirm `store.Entry` and the JSON API response are unchanged.

## 5. Admin Rebuild

- [x] 5.1 Register `POST /admin/search/rebuild` behind `requireAuth` and `withCSRF`.
- [x] 5.2 Call `RebuildSearchIndex` and redirect to `/admin/settings?rebuilt=1`.
- [x] 5.3 Add a "Search index" card to `settings.html` with the rebuild form and a confirmation paragraph for `rebuilt=1`.
- [x] 5.4 Log a rebuild failure and render the settings page with an error instead of a false confirmation.

## 6. Documentation

- [x] 6.1 Update the README search section with the grammar, `from`, `to`, `highlight` and the person field.
- [x] 6.2 Add `docs/superpowers/specs/2026-11-10-search-depth-design.md` describing the index change, grammar, date filters, highlighting and rebuild action.
- [x] 6.3 Link the new spec from `docs/index.md`.

## 7. Testing

- [x] 7.1 Add store tests proving `person` matches after create, update and rebuild, and stops matching after delete.
- [x] 7.2 Add store tests for inclusive `from` and `to` boundaries, an open-ended range and the intersection with `year`.
- [x] 7.3 Add API tests for `from`, `to`, `invalid_date` and a person-only match.
- [x] 7.4 Add web tests for `<mark>` placement, the `highlight=0` opt-out and escaping of a query containing markup characters.
- [x] 7.5 Add an admin test for the rebuild route covering success, missing session and missing CSRF.
- [x] 7.6 Run `go test ./...` and `go vet ./...`.
