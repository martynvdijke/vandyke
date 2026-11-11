## Context

Every public surface reads through the same small store API. `ListEntries` (`internal/store/store.go:432`) builds its WHERE clause with `ledgerFilter`, which always excludes trashed rows, and `AllEntryIDs` (`internal/store/store.go:918`) already returns the ascending ids of every live entry with one id-only query, because the sitemap needs exactly that. `GetEntry` (`internal/store/store.go:662`) returns `store.ErrNotFound` for a missing or trashed id. There is no scheduler, no clock abstraction and no table for anything time-driven; the only persistent key-value store is `settings` (`internal/store/migrations/004_settings.sql`), which `internal/settings` reads selectively, so unrelated keys are ignored.

The web layer registers four HTML page routes, one form POST, `/feed.xml`, three crawler or browser documents (`/robots.txt`, `/sitemap.xml`, `/site.webmanifest`) and the `/static/` handler in `internal/web/web.go:93-102`. `handlePermalink` already computes an absolute canonical URL and passes `OGImage` to `permalink.html`, which emits `og:image` and `summary_large_image` only when that field is non-empty. The copy control is a `<button data-copy-link>` enhanced by `internal/web/static/js/app.js`. The binary embeds `internal/web/static` and serves it through `cacheable` with `Cache-Control: public, max-age=86400`.

The four TRMNL plugins each have a `src/transform.py` that re-fetches the API itself, a `src/settings.yml` with a pinned numeric plugin id, and four Liquid views. `scripts/package-trmnl.py` iterates `trmnl/plugins/*`, so packaging is directory-driven, while `.github/workflows/trmnl.yaml:35` hard-codes the number of zips it expects.

## Goals / Non-Goals

**Goals:**
- One entry per local calendar day, identical for every visitor, computed and served without an admin action.
- Keep the day's pick stable when entries are added or trashed during the day, with at most one documented exception.
- Reuse the existing `settings` table so the change needs no migration.
- Reuse the feed's ETag and `Cache-Control` pattern for the JSON surface.
- Make permalinks shareable on mobile and previewable everywhere, including text-only entries.
- Keep filtered and search URLs out of search indexes without breaking link discovery.
- Ship a fifth TRMNL plugin through the existing packaging and push pipeline.

**Non-Goals:**
- A scheduler, a background job or a nightly pre-computation.
- Admin control over the day's pick, or a browsable archive of past picks.
- Per-user personalisation or timezone-aware picks per visitor.
- Runtime rendering of per-entry card images.
- Any new JavaScript dependency, share SDK or service worker.

## Decisions

1. **The candidate is a hash of the date, not a random row**: the resolver hashes the byte string `vandyke-daily-slip\x00<YYYY-MM-DD>` with FNV-1a 64 (`hash/fnv`, standard library) and takes the remainder modulo the length of the ascending live-id slice from `AllEntryIDs`. Every process, every request and every visitor therefore computes the same index for the same date and the same id set, which `ORDER BY RANDOM() LIMIT 1` would not do. FNV-1a is chosen over SHA-256 because the input is public, short and needs no collision resistance; the modulo bias is irrelevant for a ledger of a few hundred rows and does not justify a rejection loop. The id scan costs one indexed column read, the same query the sitemap already runs per request.

2. **The pick is pinned in the existing `settings` table**: the first resolution of a date computes the candidate and inserts `daily_slip.<YYYY-MM-DD>` with the decimal entry id, using `INSERT ... ON CONFLICT(key) DO NOTHING`, then reads the row back and serves whatever id is stored. Two simultaneous first requests both insert, exactly one wins, and both read the winner, so the day cannot open on two different entries. This needs no migration and no new table; the key-value table is already the place for process state that must survive a restart. If the feature ever needs history, stats over past picks or admin overrides, the mapping moves to its own table.

3. **The pin is allowed to change exactly once per day, and only when the pick disappears**: if the stored id no longer resolves through `GetEntry`, the resolver recomputes the candidate from the surviving ids, overwrites the key, and serves that entry. A pick that is trashed at 09:00 would otherwise leave the home page pointing at a 404 for fifteen hours. The alternative — pinning a snapshot of the entry's text — was rejected because it duplicates content and would keep showing an entry the admin deliberately removed. Every other event, including adding entries, restoring entries and purging a different entry, leaves the day's pick untouched.

4. **Pins older than the current date are pruned on write**: after storing the new key, the resolver deletes `daily_slip.%` rows whose key is not the current one. The table therefore holds at most one daily row, and the admin settings loader, which reads only `umami.*` and `webhook.*`, is unaffected by the key. A corrupt or non-numeric stored value is treated as absent and overwritten.

5. **The day is the server's local calendar day**: `Today` formats `time.Now().In(time.Local)` as `2006-01-02` and every other function takes that string. No new environment variable is introduced, because the process time zone is already deployment configuration through `TZ`; a container without `TZ` uses UTC, so midnight is UTC midnight. The database is SQLite on one host, so two instances in different zones are not a supported topology. `ForDay` takes the date string so tests never depend on the wall clock.

6. **One resolver serves every surface**: `internal/slip` is constructed from the `*store.Store` that `web.New` and `httpapi.New` already hold, so no constructor signature changes. The home page, the JSON endpoint and the permalink badge all call the same code, which is what keeps them from disagreeing about the day's entry.

7. **Caching copies the feed, not the API listing**: the JSON response carries the strong ETag `"slip-<date>-<id>"`, `Cache-Control: public, max-age=60` and a 304 on a matching `If-None-Match`, reusing `etagMatches` from `internal/web/feed.go`. The tag changes at midnight or on a repin and at no other time, so a client that polls every minute gets one body per day. The HTML pages keep no `Cache-Control` header, because they embed the ledger listing, the add dialog and the admin session state.

8. **The home-page section is server-rendered and client-hidden while searching**: `buildPage` resolves the slip only when the request carries none of `q`, `person`, `year`, `from`, `to`, `photo`, `page`, `per_page`, `sort` or `order`, so a crawler or a no-JavaScript visitor loading `/` sees it exactly once, and a filtered load never includes it. The HTMX search swaps only `#ledger`, so `app.js` hides the section when the search input or any filter changes, and shows it again when the box is cleared. The section is deliberately not part of the `ledger` fragment.

9. **An empty ledger has no slip**: `AllEntryIDs` returns no ids, the resolver reports absence, the home page omits the section entirely, and `GET /api/v1/slip` answers 404 with the existing `{"error": {"code", "message"}}` envelope and the message `the ledger has no entry for today`. A 204 was rejected because it makes the TRMNL transform indistinguishable from a transport failure.

10. **The permalink badge is additive metadata**: the badge is rendered by comparing the page's entry id with the pinned id for today; the canonical URL stays `/e/{id}`, and the existing `og:*` and `twitter:*` tags are unchanged apart from the image decision below. There is no `/slip` route, so no second URL competes with the permalink for the same content.

11. **`DefinedTerm` is the structured-data type for an entry**: the ledger is a glossary in which the misspelling is the term and the correction is its definition, which maps exactly onto `DefinedTerm.name` and `DefinedTerm.description`, with `inDefinedTermSet` naming the ledger and `url` carrying the permalink. `CorrectionComment` was rejected because schema.org defines it as a correction to a `CreativeWork`, not a spelling variant; `Article` was rejected because the entry is not prose; `Quotation` was rejected because nothing is quoted from a source. A photo-only entry uses `name` `Photo entry #0104`, omits `description` and adds `image`.

12. **`Dataset` is the structured-data type for `/stats`**: the page is a statistical summary of one collection, so `Dataset` with `name`, `description`, `url`, `isAccessibleForFree`, `temporalCoverage` from the first to the last entry, `variableMeasured` and a `DataDownload` pointing at `/api/v1/stats` describes it without inventing an entity. `WebPage` was rejected as too weak to say what the page contains.

13. **JSON-LD is marshalled in Go and emitted as script content**: the document is built as a Go value, encoded with `encoding/json` (which escapes `<`, `>` and `&` by default, so a `</script>` sequence cannot appear) and injected with `template.JS`. Templates are never asked to build JSON by hand.

14. **Robots and canonical are decided per surface**: `/` without a listing parameter, `/stats` and `/e/{id}` are `index,follow` and self-canonical. `/entries` in every form, and `/` with any listing parameter, are `noindex,follow` and carry no canonical, because a canonical pointing elsewhere together with `noindex` sends conflicting signals. The sitemap keeps its current membership, so permalinks stay discoverable even though `/entries` is no longer a crawl path; the home page still links five recent permalinks and every permalink links back to the ledger.

15. **The preview image is a committed static asset**: `internal/web/static/images/og-default.png`, 1200x630, embedded by the existing `//go:embed static` and served by the existing `/static/` handler. The binary has no rasteriser and no font dependency, and adding one for a joke ledger is not worth the image size; runtime card generation is therefore a non-goal. Photo entries keep their photo as the preview, and both cases declare `twitter:card: summary_large_image`; only the static asset declares `og:image:width` and `og:image:height`, because those dimensions are known and fixed.

16. **Sharing is progressive enhancement on a real anchor**: the control becomes `<a class="btn btn-primary" href="{canonical}" data-share-url data-share-title data-share-text>`. With JavaScript it calls `navigator.share` when available, treats `AbortError` as a user cancel and does nothing, and otherwise falls back to the current clipboard path (`navigator.clipboard.writeText`, then `document.execCommand`). Success changes the label to `Link copied` for two seconds and appends `Link copied.` to `#toasts`, which is already `role="status"` and `aria-live="polite"`. Without JavaScript the anchor is an ordinary link to the canonical permalink, which the browser's link menu can copy.

17. **OpenSearch is one static-shaped document built per request**: `GET /opensearch.xml` writes the description with `httpx.BaseURL`, so the template follows `PUBLIC_BASE_URL` like the sitemap and feed do. The template is `{base}/?q={searchTerms}`, which is the surface the toolbar already submits to and which `searchquery.Parse` already understands, including quoted phrases and `-` exclusions. No suggestions URL is advertised.

## Risks / Trade-offs

- **[The day's pick is trashed mid-day]** -> the resolver repins once from the surviving ids; the trade-off is that the slip can change once, which is preferable to a 404 hero for the rest of the day.
- **[`settings` now holds runtime state next to configuration]** -> the `daily_slip.` prefix is namespaced, old rows are pruned on every write, and the admin settings loader reads only known keys; if history is ever needed the mapping moves to its own table.
- **[Extra query on every home page and permalink load]** -> one id-only scan plus one primary-key lookup, the same order of cost as the sitemap; the JSON surface is cached for 60 seconds and conditional.
- **[Hash distribution is lumpy on a tiny ledger]** -> with two live entries the days alternate; with hundreds the spread is even enough, and a deterministic wobble is preferable to a different entry per request.
- **[A `noindex` `/entries` removes a crawl path]** -> every permalink stays in the sitemap and is linked from the home page and from the ledger, and `follow` keeps the links traversable.
- **[The static preview is generic]** -> `og:title` and `og:description` still carry the word, correction and entry number, and per-entry images remain an explicit non-goal.
- **[Desktop browsers often have no share sheet]** -> the clipboard path is the fallback for every rejection other than an explicit cancel, and the label reports which outcome happened.
- **[A pinned TRMNL plugin id cannot be invented]** -> `.github/workflows/trmnl.yaml` fails the job with `has no pinned TRMNL plugin ID` until the real id is written into `settings.yml`, so an unpinned plugin cannot silently create a duplicate.
- **[The packaging count assertion is hard-coded]** -> `.github/workflows/trmnl.yaml:35` expects 5 zips today (four plugins plus the combined archive); a fifth plugin makes it 6 and the assertion must change in the same commit or CI fails.
