## 1. Slip resolver and store

- [ ] 1.1 Add `GetSetting(ctx, key)` to `internal/store/store.go`, returning the value and whether the key exists.
- [ ] 1.2 Add `SetSettingIfAbsent(ctx, key, value)` to `internal/store/store.go` using `INSERT ... ON CONFLICT(key) DO NOTHING`, then reading the row back so the caller gets the winning value.
- [ ] 1.3 Add `internal/slip/slip.go` with a `Resolver` built from `*store.Store`, an exported `Today(ctx)` that formats the local calendar date, and `ForDay(ctx, day)` so tests never read the wall clock.
- [ ] 1.4 Implement the candidate as FNV-1a 64 of `vandyke-daily-slip\x00<day>` modulo the length of the ascending slice from `AllEntryIDs`.
- [ ] 1.5 Pin the candidate under `daily_slip.<day>`, serve the stored value, and repin only when `GetEntry` reports `store.ErrNotFound` for the stored id.
- [ ] 1.6 Prune other `daily_slip.%` keys on every write, and treat a non-numeric stored value as absent.
- [ ] 1.7 Return a distinct absence result when no live entry exists, and never invent an entry.
- [ ] 1.8 Add store tests for `GetSetting` and `SetSettingIfAbsent`, and resolver tests for the same date twice, a date change, an add mid-day, a trash mid-day, a purged pick with no survivors, a corrupt stored value and two concurrent first resolutions.

## 2. Home page slip

- [ ] 2.1 Add `internal/web/templates/slip.html` with a `<section class="slip" aria-labelledby="slip-title">` showing the heading, the word, the correction, the entry date, the optional person chip, the optional thumbnail and a link to `/e/{id}`.
- [ ] 2.2 Include the section in the `base` template between the hero and the stats block.
- [ ] 2.3 Resolve the slip in `buildPage` only when `q`, `person`, `year`, `from`, `to`, `photo`, `page`, `per_page`, `sort` and `order` are all empty, and pass it to the template as a nullable field.
- [ ] 2.4 Omit the section entirely when the ledger has no live entry or the resolver fails, logging the error instead of failing the page.
- [ ] 2.5 Leave the `ledger` fragment in `internal/web/templates/partials.html` unchanged so HTMX swaps never re-render the slip.
- [ ] 2.6 Add a `data-slip` hook and an `app.js` listener on the toolbar that hides the section while a search term or filter is active and shows it again when the toolbar returns to its default state.
- [ ] 2.7 Add slip styles to `internal/web/static/css/app.css` using the existing card and type tokens.
- [ ] 2.8 Add web tests for an unfiltered `/` containing the section, a `/` with `q` or `year` or `page` omitting it, an empty ledger omitting it, and the `ledger` fragment never containing it.

## 3. Slip API

- [ ] 3.1 Register `GET /api/v1/slip` in `internal/httpapi/api.go`.
- [ ] 3.2 Return `{"date": "<YYYY-MM-DD>", "entry": {...}}` using the existing `store.Entry` JSON shape.
- [ ] 3.3 Set the strong ETag `"slip-<date>-<id>"` and `Cache-Control: public, max-age=60`, and return 304 without a body when `If-None-Match` matches.
- [ ] 3.4 Return 404 with `{"error": {"code": "not_found", "message": "the ledger has no entry for today"}}` when the resolver reports absence.
- [ ] 3.5 Add API tests for the body shape, the cache headers, the 304, the empty-ledger 404 and agreement with the id the home page renders.

## 4. Permalink affordance

- [ ] 4.1 Resolve today's slip id in `handlePermalink` in `internal/web/web.go` and pass a boolean to the template, tolerating resolver errors by omitting the badge.
- [ ] 4.2 Render a "Slip of the day" badge on `internal/web/templates/permalink.html` when the entry is the pinned one, without changing the canonical URL or the existing `og:*` tags.
- [ ] 4.3 Add a web test that the pinned entry's permalink carries the badge and another entry's permalink does not.

## 5. TRMNL plugin

- [ ] 5.1 Create `trmnl/plugins/vandyke-slip/src/transform.py` that reads the `url` custom field and fetches `{base}/api/v1/slip`, returning `date`, `word`, `meaning`, `person` and `photo_url` or an `error` string.
- [ ] 5.2 Create `trmnl/plugins/vandyke-slip/src/settings.yml` with `strategy: polling`, `polling_url: "{{url}}/api/v1/slip"`, a `refresh_interval` of 3600, the `url` custom field and a numeric `id` obtained from TRMNL.
- [ ] 5.3 Add `full.liquid`, `half_horizontal.liquid`, `half_vertical.liquid` and `quadrant.liquid` for the plugin, plus `icon-color.svg`, `icon-mono.svg` and `.trmnlp.yml`.
- [ ] 5.4 Load the new transform in `trmnl/tests/test_vandyke_transform.py` and cover a normal slip, a photo-only slip, a missing `url` and an unreachable instance.
- [ ] 5.5 Change the zip count assertion in `.github/workflows/trmnl.yaml` from 5 to 6 so the fifth plugin and the combined archive are both expected.
- [ ] 5.6 Add the plugin row to the table in `trmnl/README.md` and mention the new endpoint.
- [ ] 5.7 Run `make trmnl-test`, `make trmnl-package` and `trmnlp lint --dir trmnl/plugins/vandyke-slip`.

## 6. Share control

- [ ] 6.1 Replace the copy button in `internal/web/templates/permalink.html` with an anchor pointing at the canonical URL that carries `data-share-url`, `data-share-title` and `data-share-text`.
- [ ] 6.2 Rewrite the handler in `internal/web/static/js/app.js` to call `navigator.share` when it exists, ignore `AbortError`, and otherwise use the existing clipboard then `document.execCommand` fallback.
- [ ] 6.3 On success, set the label to `Link copied` for two seconds and announce the result through the existing `#toasts` polite live region; on clipboard failure keep the current recovery message.
- [ ] 6.4 Update the markup assertion at `internal/web/web_test.go:550` to the new attributes and add a check that the anchor still renders its `href` when JavaScript is not involved.
- [ ] 6.5 Confirm from the served HTML that the control is an anchor with a working `href`, so a no-JavaScript visitor can copy the link from the browser's link menu.

## 7. Structured data, robots, previews and discovery

- [ ] 7.1 Build the permalink JSON-LD as a Go value in `internal/web/web.go` and emit it with `encoding/json` and `template.JS` in `permalink.html`.
- [ ] 7.2 Use `DefinedTerm` with `name`, `description`, `url`, `identifier`, `dateCreated` and `inDefinedTermSet`, mapping the correction to `description`, adding `image` when a photo exists, and using `Photo entry #<id>` when the word is empty.
- [ ] 7.3 Build the `/stats` JSON-LD as a `Dataset` with `name`, `description`, `url`, `isAccessibleForFree`, `temporalCoverage`, `variableMeasured`, `creator` and a `DataDownload` for `/api/v1/stats`, omitting `temporalCoverage` on an empty ledger.
- [ ] 7.4 Add `<link rel="canonical">` to `/stats` and `internal/web/templates/base.html`, pointing at `/stats` and `/` respectively.
- [ ] 7.5 Emit `index,follow` plus the self-canonical on `/` without listing parameters, on `/stats` and on `/e/{id}`.
- [ ] 7.6 Emit `noindex,follow` and no canonical for `/entries` in every form and for `/` carrying any of `q`, `person`, `year`, `from`, `to`, `photo`, `page`, `per_page`, `sort` or `order`.
- [ ] 7.7 Add the committed `internal/web/static/images/og-default.png` at 1200x630 and confirm it is embedded and served under `/static/images/`.
- [ ] 7.8 Emit `og:image`, `og:image:width`, `og:image:height`, `og:image:alt`, `twitter:image` and `twitter:card: summary_large_image` on every permalink, using the photo when present and the fallback otherwise.
- [ ] 7.9 Add the same title, description, URL, image and card tags to `/` and `/stats`.
- [ ] 7.10 Add `GET /opensearch.xml` in `internal/web/web.go` with `Content-Type: application/opensearchdescription+xml; charset=utf-8`, `ShortName`, `Description`, `InputEncoding`, `Image` and a `Url` template of `{base}/?q={searchTerms}`, cached like `robots.txt`.
- [ ] 7.11 Add `<link rel="search" type="application/opensearchdescription+xml" title="vanDyke ledger search" href="/opensearch.xml">` to `base.html`, `permalink.html` and `stats.html`.
- [ ] 7.12 Add web tests for the robots and canonical matrix, the JSON-LD payloads, the fallback image on a text-only entry, the photo image on a photo entry, and the OpenSearch document and discovery link.

## 8. Docs and verification

- [ ] 8.1 Add `docs/superpowers/specs/2026-11-11-daily-slip-and-sharing-design.md` describing the hash, the pin, the repin rule, the robots matrix and the static preview decision.
- [ ] 8.2 Link the new design entry from `docs/index.md`.
- [ ] 8.3 Document `GET /api/v1/slip` in the API table, the slip section and badge in the ledger section, and the sharing, robots, preview and OpenSearch behaviour in `README.md`.
- [ ] 8.4 Run `go test ./internal/store ./internal/slip ./internal/web ./internal/httpapi`.
- [ ] 8.5 With a running instance, load `/` twice and confirm the same slip, load it with `?q=` and confirm the section is gone, and load `/e/{id}` for the pinned entry and confirm the badge.
- [ ] 8.6 Confirm `GET /api/v1/slip` returns the same id as the home page, answers 304 for a repeated `If-None-Match`, and that a text-only permalink now yields an `og:image`.
- [ ] 8.7 Confirm a filtered `/entries` and a `/ ?q=` response carry `noindex,follow`, and that `/opensearch.xml` is reachable and advertises `{base}/?q={searchTerms}`.
- [ ] 8.8 Run `go vet ./...`, `go test ./...` and `python3 trmnl/tests/test_vandyke_transform.py`.
