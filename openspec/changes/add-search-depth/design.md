## Context

`entries_fts` is an FTS5 virtual table with `tokenize='trigram'`, created in `006_search.sql` and backfilled from `entries`; rowid is the entry id. It is maintained by hand: `indexEntry` (`internal/store/store.go:606`) on create and seed, an `INSERT OR REPLACE` inside `UpdateEntry`, and a rowid `DELETE` in `DeleteEntries`. `ledgerFilter` (`store.go:461`) turns the query into either an FTS `MATCH` or a `LIKE` scan, then adds photo, year and exact-person clauses. The web and API handlers each parse their own parameters into `store.ListParams` and render `store.Entry` directly.

## Goals / Non-Goals

**Goals:**
- Search `person` through the same index and the same query as `word` and `meaning`.
- Give the existing filters a date range without removing `year`.
- Show where a query matched, without letting user text become markup.
- Make index repair an operator action.
- Keep one query parser shared by SQL generation and rendering, so the two cannot disagree about what was searched.

**Non-Goals:**
- Fuzzy, prefix-beyond-trigram, or edit-distance matching.
- Stemming, stop words or language analysis.
- An external index, a second database, or embeddings.
- Changing the JSON shape of `store.Entry`.

## Decisions

1. **Recreate the FTS table in migration `010_search_person.sql`**: FTS5 has no `ADD COLUMN`, so the migration drops `entries_fts`, creates it with `word`, `meaning` and `person` and the same trigram tokenizer, and backfills from `entries` in the same transaction. `splitStatements` handles this because the file has no triggers and no semicolons inside literals. Every index-maintenance site (`indexEntry` callers, the `INSERT OR REPLACE` in `UpdateEntry`, `RebuildSearchIndex`) is updated to name all three columns, and the rowid `DELETE` in `DeleteEntries` still removes the row, so a person edit does not leave a stale value in the index.

2. **One parser in `internal/searchquery`**: `Parse(q string)` returns a `Query` holding `Include` and `Exclude` phrases. `Query.Match()` returns an FTS5 expression or `""`; `Query.LikePatterns()` returns the substring patterns for the fallback. Grammar: whitespace separates terms, all terms are required (AND), `"..."` groups a phrase, a leading `-` excludes the following term or phrase, and an unmatched quote is treated as a literal character. `ledgerFilter` calls `Match()`; when it is empty the existing `LIKE` branch runs.

3. **The sub-three-rune fallback is decided per query, not per token**: `ftsMatch` currently rejects the whole query if any token is shorter than three runes (`store.go:509`). That rule is kept, extended to exclusion terms and to phrases: if any positive or negative term is shorter than three runes, `Match()` returns `""` and the LIKE path runs. The LIKE path applies the positive terms as `(word LIKE ? ESCAPE '\' OR meaning LIKE ? ESCAPE '\' OR person LIKE ? ESCAPE '\')`, keeping person searchable in the fallback, and each exclusion as `NOT (...)` over the same three columns. A query of only exclusions also uses the LIKE path, since FTS5 `NOT` is a binary operator and has no positive side to bind to.

4. **Date filters are inclusive and intersect with `year`**: `ListParams` gains `From` and `To` as `time.Time`, with the zero value meaning unset. The clause is `e.created_at >= ?` formatted as `YYYY-MM-DDT00:00:00Z`, and `e.created_at < ?` with `To` advanced by one day, which matches the stored RFC3339 UTC strings and removes the need for SQLite date arithmetic. Handlers parse `YYYY-MM-DD` with `time.Parse("2006-01-02")`; the web page ignores an unparseable value and leaves that filter unapplied, while the API answers `400 invalid_date`. Both filters may be combined with `year`; each contributes its own clause, so the result is the intersection.

5. **Highlighting is a web-layer view concern**: `store.Entry` is unchanged, so the API cannot accidentally ship markup. `ledgerData` builds a `ledgerRow` per entry holding `WordHTML` and `MeaningHTML` as `template.HTML`, produced by a helper that escapes the source text first and then wraps each case-insensitive occurrence of an included phrase in `<mark>`; excluded phrases are never marked. Only `<mark>` is ever emitted, and everything else is entity-escaped by construction. `GET /entries` accepts `highlight=0` to render the plain fields instead. The JSON API returns the existing raw `word` and `meaning`, which is its opt-out.

6. **Rebuild is an authenticated POST**: `POST /admin/search/rebuild` sits behind `requireAuth` and `withCSRF`, calls `Store.RebuildSearchIndex`, and redirects to `/admin/settings?rebuilt=1`. The settings page renders a "Search index" card with a form posting to that path and a confirmation paragraph when `rebuilt=1` is present. No scheduler is involved, which matches the no-background-worker constraint.

## Risks / Trade-offs

- **[Migration locks the table]** -> `010_search_person.sql` drops and rebuilds the index inside the single migration transaction. The table is derived data, so a failed migration is recoverable by the same rebuild action; the seeded ledger is small enough that the transaction is short.
- **[Index drift until repair]** -> the rebuild action makes repair possible, but nothing forces it. The settings card states that the action rebuilds from `entries`, and the behaviour is documented in the README so an operator knows the index is derived and safe to regenerate.
- **[Highlighting cost and correctness]** -> marking scans each rendered row's text once per included phrase. Text is escaped before marking, so a query containing `<` or an entity cannot inject markup, and a test asserts that `<script>` in a query renders as text.
- **[Grammar ambiguity for users]** -> a leading `-` and a quote now have meaning. The search input's placeholder and the README describe both, and a term that is only punctuation falls back to LIKE rather than erroring.
- **[Date filters use UTC]** -> entries are stored in UTC, so a boundary day is UTC-based. The date inputs and the README say `YYYY-MM-DD` without a timezone, and no per-user timezone is introduced.
