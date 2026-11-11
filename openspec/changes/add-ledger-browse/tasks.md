## 1. Store reads

- [ ] 1.1 Add `WordSummary` (`Word`, `Count`, `FirstAt`, `LastAt`, `NewestID`) and `WordIndex` returning one summary per live non-empty `word` group, grouped by `word COLLATE NOCASE`, with the earliest and latest `created_at` and the newest occurrence's id and spelling.
- [ ] 1.2 Include the number of live entries whose `word` is empty in the `WordIndex` result so the page can state the photo-only count without a second query.
- [ ] 1.3 Add `PersonProfile(ctx, name)` returning the displayed spelling, the count, the first and last `created_at` for the case-insensitive `person` group, and `store.ErrNotFound` when no live entry matches.
- [ ] 1.4 Add `RandomEntry(ctx, person string, hasPhoto *bool)` that applies `e.deleted_at IS NULL` plus the optional person and photo predicates, orders by `RANDOM()` and returns `store.ErrNotFound` when no row matches.
- [ ] 1.5 Add `PhotoCursor` (`CreatedAt`, `ID`), `ListPhotoEntries(ctx, cursor, limit)` returning one batch of live entries that have a photo, newest first, plus the total, and an `ErrInvalidCursor` for a token that cannot be decoded.
- [ ] 1.6 Keep every new query free of trashed rows and add store tests proving a trashed entry never appears in the word index, a person profile, a random draw or a photo batch.
- [ ] 1.7 Add store tests for case-insensitive word grouping, the newest-occurrence link target, accented spellings, person profiles across two case variants, and cursor boundaries when two entries share a `created_at`.

## 2. Word index page

- [ ] 2.1 Register `GET /words` in `internal/web/web.go` and add a `wordsPageData` view model with the rail, the active bucket, the rows and the photo-only count.
- [ ] 2.2 Bucket the summaries in Go with `unicode.ToUpper` on the first rune: A–Z to their letter, everything else to `#`.
- [ ] 2.3 Render the fixed rail A–Z plus `#` with per-bucket counts and inert `aria-disabled` letters for empty buckets.
- [ ] 2.4 Render one bucket per document, defaulting to `#` when it has rows and otherwise the first non-empty letter, and accept `?letter=` to choose another.
- [ ] 2.5 Paginate a bucket at 200 rows with `?page=` and `buildPageLinks`, resetting to page 1 when a letter changes.
- [ ] 2.6 Render each row as the spelling, the count, the first and last date, linking the spelling to `/e/{id}` of the newest occurrence.
- [ ] 2.7 Render the photo-only note, linking to `/photos`, only when its count is non-zero.
- [ ] 2.8 Add `internal/web/templates/words.html` with a canonical URL of `{base}/words` and `noindex, follow` for any `?letter=` or `?page=` variant.
- [ ] 2.9 Return the existing not-found page for an unknown `?letter=` value and add handler tests for the default bucket, an explicit letter, empty letters, the `#` bucket and the pager.

## 3. Person page

- [ ] 3.1 Register `GET /p/{person}` in `internal/web/web.go`, trim the decoded path value, and map `store.ErrNotFound` to the not-found page.
- [ ] 3.2 Resolve the displayed spelling and the overall count, first and last slip through `PersonProfile`, and build the canonical URL from that spelling with `url.PathEscape`.
- [ ] 3.3 Render the same `ledger` fragment through `ListEntries` with `ListParams.Person` fixed from the path and `q`, `sort`, `order`, `year`, `from`, `to`, `photo`, `page` and `per_page` taken from the query string.
- [ ] 3.4 Add a base path to `ledgerData` and use it in `SortHref`, `PageHref` and the clear-search link so a person page keeps its scope.
- [ ] 3.5 Make the ledger toolbar use the page base path for both `action` and `hx-get`, and replace its person `<select>` with a static chip on a person page.
- [ ] 3.6 Ignore a `person` query parameter on a person page so the path always wins.
- [ ] 3.7 Add `internal/web/templates/person.html` with the count, the first and last slip, a `noindex, follow` directive, a self-canonical URL, Open Graph title, description and URL, and an Atom autodiscovery link to the person feed.
- [ ] 3.8 Add handler tests for the canonical spelling, two case variants resolving to one page, an unknown name, `/p/`, the scope surviving a sort and a page click, and an ignored `person` parameter.

## 4. Person feed

- [ ] 4.1 Parameterise the Atom builder in `internal/web/feed.go` with a feed path, an entry set and a feed id so the site feed and the person feed share the document shape.
- [ ] 4.2 Register `GET /p/{person}/feed.xml` and serve the person's live entries newest first, with the person as the feed-level author and the `limit` default of 50 capped at 100.
- [ ] 4.3 Derive a strong ETag from the person, the newest entry id and the returned count, set `Cache-Control: public, max-age=60`, and return 304 on a matching `If-None-Match`.
- [ ] 4.4 Return the not-found page for an unknown person and a 404 from the feed when every entry for that person is trashed, matching the person page.
- [ ] 4.5 Add tests for the person-only entry set, the `limit` clamp, the ETag and 304, the unknown-person 404 and the all-trashed 404.
- [ ] 4.6 Confirm `/feed.xml` keeps its current document, id and ETag behaviour after the parameterisation.

## 5. Random slip

- [ ] 5.1 Register `GET /random` and `GET /api/v1/random` as siblings of the permalink and collection routes, not as literals under `GET /e/{id}` or `GET /api/v1/entries/{id}`.
- [ ] 5.2 Make `GET /random` read the optional `photo` and `person` parameters, draw through `RandomEntry`, and answer `303 See Other` to `/e/{id}` with `Cache-Control: no-store` and `X-Robots-Tag: noindex`.
- [ ] 5.3 Make `GET /api/v1/random` return the entry object with `Cache-Control: no-store`, and `404 not_found` when the draw is empty.
- [ ] 5.4 Render the not-found page for an empty or unmatched draw on the HTML route rather than redirecting or returning a 500.
- [ ] 5.5 Add tests that a trashed entry is never drawn, that `photo` and `person` narrow the draw, that an empty ledger answers 404 on both routes, and that `GET /api/v1/entries/random` still answers `400 invalid_id`.

## 6. Photo gallery

- [ ] 6.1 Register `GET /photos` in `internal/web/web.go` and render 24 tiles per batch from `ListPhotoEntries`.
- [ ] 6.2 Encode and decode the cursor as URL-safe base64 of the last tile's stored `created_at` and `id` joined by `_`, answer 400 for a malformed token, and never restart the batch silently.
- [ ] 6.3 Return the bare `photos` fragment when `HX-Request` is present and a complete document otherwise, with `Vary: HX-Request`.
- [ ] 6.4 Render the infinite-scroll sentinel as an anchor with both `href` and `hx-get`, `hx-target="this"`, `hx-swap="outerHTML"` and `hx-trigger="revealed"`, and render a closing line instead of a sentinel on the last batch.
- [ ] 6.5 Add `rel="prev"` and `rel="next"` links and a "start from the beginning" link so a cursor URL is a complete, navigable document without JavaScript.
- [ ] 6.6 Render tile images from the `thumb` variant with `loading="lazy"`, `decoding="async"` and an `aspect-ratio` box in CSS, and keep every display-variant URL out of the grid's `img` elements.
- [ ] 6.7 Add a single `<dialog id="photo-lightbox">` to `internal/web/templates/photos.html` and wire `data-lightbox-src`, `data-lightbox-alt` and `data-lightbox-caption` in `internal/web/static/js/app.js`, setting the display `src` only on open.
- [ ] 6.8 Return focus to the opening anchor on close and rely on the native dialog for Esc and focus trapping.
- [ ] 6.9 Add `internal/web/templates/photos.html` with a canonical URL of `{base}/photos`, a `noindex, follow` directive on cursor variants, an Open Graph title and description, and the gallery link in the masthead and footer.
- [ ] 6.10 Add handler tests for the photo-only entry set, the absence of trashed entries, an empty gallery, the fragment versus document response, cursor boundaries, the malformed cursor 400, the sentinel's `href`, the alt text for a worded and an unworded entry, and the absence of `size=display` from the grid's `img` elements.

## 7. Navigation, metadata and sitemap

- [ ] 7.1 Add `Words` and `Photos` to the masthead and footer in `internal/web/templates/base.html`.
- [ ] 7.2 Add the same two links to the standalone header in `internal/web/templates/permalink.html`.
- [ ] 7.3 Add `{base}/words` and `{base}/photos` to `handleSitemap` after `/stats`, keeping `/random` and the person pages out.
- [ ] 7.4 Add `noindex, follow` to the `?letter=`, `?page=` and `?cursor=` variants and confirm the bare `/words` and `/photos` stay indexable.
- [ ] 7.5 Extend the discoverability tests to cover the two new sitemap locations, the new masthead and footer links, and the absence of `/random` and `/p/` from the sitemap.
- [ ] 7.6 Confirm the page heads still carry the manifest, skip link and site feed autodiscovery, and that the person page adds its own feed link without dropping the site one.

## 8. Documentation

- [ ] 8.1 Document `/words`, `/p/{person}`, `/p/{person}/feed.xml`, `/random`, `/api/v1/random` and `/photos` in `README.md`, including the cursor parameter, the filters and the no-JavaScript behaviour.
- [ ] 8.2 Correct the README's statement that duplicates are allowed, which contradicts the unique `(word, meaning)` index added by migration `007_dedupe_entries.sql` and the `409` the API returns.
- [ ] 8.3 Add `docs/superpowers/specs/2026-11-11-ledger-browse-design.md` describing the word index, the person identity rule, the random route choice and the gallery cursor.
- [ ] 8.4 Link the new design entry from `docs/index.md`.

## 9. Testing and verification

- [ ] 9.1 Run `go test ./internal/store ./internal/web ./internal/httpapi`.
- [ ] 9.2 With a running instance, walk `/words` across at least two letters and confirm the counts, dates and permalinks match `/entries` and `/api/v1/stats`.
- [ ] 9.3 Confirm `/p/{person}` survives search, sort and paging, that its feed validates as Atom, and that an unknown name answers 404.
- [ ] 9.4 Confirm `/random` redirects to a live permalink twenty times in a row, that a trashed entry never appears, and that `/api/v1/random` answers JSON.
- [ ] 9.5 With JavaScript disabled, scroll the whole gallery through its prev and next links and open a photo from a tile.
- [ ] 9.6 Tab through the gallery, open and close the lightbox with the keyboard, and confirm focus returns to the tile that opened it.
- [ ] 9.7 Run `go vet ./...` and `go test ./...`.
