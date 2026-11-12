# vanDyke

The LANCo misspelling ledger — a modern, self-hosted rebuild of
[vandyke.thor.edu](https://vandyke.thor.edu/).

One self-contained Go binary: SQLite storage, a server-rendered HTMX interface,
a JSON API, per-IP rate limiting, an admin ledger with local or OIDC login, and
optional self-hosted Umami analytics. No JavaScript build step, no external
services required.

## Features

- **The Corrections Ledger** — an editorial paper-and-ink interface built with
  Go templates, HTMX and a small hand-written stylesheet. Light and dark theme,
  animated rows, keyboard- and screen-reader-friendly.
- **All original data, seeded** — the 78 unique entries from the original site
  (October 2017 → March 2026) are embedded in the binary and imported into
  SQLite on first start. Duplicate submissions are collapsed to their earliest
  occurrence.
- **No accidental duplicates** — adding an entry whose misspelling and
  correction already exist is rejected case-insensitively.
- **JSON REST API** — list, search, filter, sort, paginate and add entries;
  stats and a health endpoint. See [API](#api).
- **Add entries from the web or the API** — text entries or photos of a
  misspelling, protected by a per-IP token bucket (default: 10 additions per
  hour, burst of 5). The add dialog opens the camera directly on mobile.
- **Fast, private photos** — uploads keep an admin-only original, while the
  public ledger gets a resized display image and a small thumbnail. EXIF
  metadata is removed and oversized dimensions are rejected.
- **Partial search and filters** — SQLite FTS5 trigram search finds fragments
  like `euten` inside `feutenreus`, across the misspelling, the correction and
  the person. Quote a phrase, prefix a term with `-` to exclude it, filter by
  photo, year or an inclusive `from`/`to` date range, and see matches marked in
  the ledger.
- **Entry tags** — a curated vocabulary (`typo`, `misheard`, `autocorrect`,
  `name`, `dialect` and any tags an admin adds) that composes with every other
  filter. Tags appear as chips on ledger rows and permalinks, have a public
  index at `/tags`, canonical pages at `/tags/{slug}`, a tagged Atom feed and
  their own sitemap entries.
- **Year in review** — `/y/{yyyy}` recaps any year with live entries: totals,
  the first and last slip, the busiest month, a twelve-month chart, the five
  most frequent names, the longest misspelling and the photo ratio. Computed on
  read, server-rendered and cacheable.
- **Browse by word, person or chance** — `/words` is an A–Z dictionary of every
  distinct misspelling with counts, first and last dates and a permalink from
  each spelling; `/p/{person}` scopes the ledger to one attributed name;
  `/random` redirects to a live slip and `/api/v1/random` returns one as JSON.
- **Slip of the day** — `/` features one entry chosen by a deterministic hash
  of the local date over the live ids, pinned in SQLite so every visitor sees
  the same slip all day. It has a JSON surface at `/api/v1/slip`, a badge on the
  pinned permalink and its own TRMNL plugin, and disappears while you search.
- **Photo gallery** — `/photos` shows every photographed slip as a thumbnail
  grid, newest first, paged by cursor, with HTMX infinite scroll that degrades
  to a complete no-JavaScript page and an accessible native `<dialog>`
  lightbox that fetches the display image only when opened.
- **Stable permalinks** — every entry has a page at `/e/{id}` with Open Graph
  and Twitter metadata for clean link previews, a `DefinedTerm` JSON-LD block,
  and a share control that uses the Web Share API where available and copies
  the canonical link otherwise. A committed 1200×630 fallback image gives
  text-only entries a large preview card too.
- **Atom feed** — `GET /feed.xml` publishes the newest entries as Atom 1.0 with
  a `limit` and `tag` parameter, a strong ETag and autodiscovery from every page,
  so a reader or RSS client can subscribe without polling the API. Every person
  page also has its own feed at `/p/{person}/feed.xml`.
- **Webhooks with durable retries** — up to 20 named endpoints, each with its
  own secret, event subscription (`created`, `updated`, `deleted`, `restored`,
  `purged`) and generic, Discord or Slack preset. Delivery is non-blocking,
  persisted, retried with jittered backoff and visible in a delivery log.
- **Admin ledger** — sign in to edit every field, replace or remove photos,
  correct logged dates, and delete entries individually or in bulk. Public
  additions stay open; only editing and deletion require an admin.
- **First-run setup and OIDC** — create the first admin at `/admin/setup` with a
  username and password, or let the first OpenID Connect sign-in claim the
  instance. Login attempts are rate-limited and sessions are stored in SQLite
  with CSRF protection.
- **Optional self-hosted Umami** — configure the tracker through the admin
  settings page or environment variables. Page views and custom events
  (`search`, `entry-added`, `theme-toggle`) are sent only when enabled; the CSP
  is adjusted at runtime.
- **Five TRMNL e-ink plugins** — the slip of the day, ledger stats, latest
  slips, the newest photo entry and a combined dashboard, packaged as versioned
  release downloads.

## Quickstart

### Local

```sh
go run ./cmd/vandyke
# → http://localhost:8080
```

The database is created at `./data/vandyke.db` and seeded on first run.

### Docker

```sh
cp .env.example .env   # optional, adjust as needed
docker compose up -d --build
# → http://localhost:8080
```

The SQLite file lives in `./data` (mounted at `/data` in the container). The
image is a static binary on Alpine. Its entrypoint claims the mounted
directory, then runs the server as the unprivileged `vandyke` user (uid 10001),
so a bind mount needs no manual `chown`. `/healthz` is the healthcheck.

### Make targets

```sh
make run          # run locally
make build        # static binary into bin/
make test         # full test suite
make vet          # go vet
make docker       # build the image
make compose-up   # start with docker compose
make trmnl-test   # run the TRMNL transform tests
make trmnl-lint   # lint the TRMNL plugins (requires trmnlp)
make trmnl-package # build versioned TRMNL plugin zips into dist/
```

## Configuration

Everything is configured through the environment (or a `.env` file for Docker
Compose). See [`.env.example`](.env.example).

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port. |
| `DB_PATH` | `./data/vandyke.db` | SQLite database path; the directory is created automatically. |
| `PUBLIC_BASE_URL` | *(derived)* | Optional absolute base URL, e.g. `https://vandyke.example.com`, used for canonical and Open Graph URLs. Derived from the request when empty. |
| `TRUST_PROXY` | `false` | Trust `X-Forwarded-For` for client IPs (enable only behind a proxy you control). |
| `ADD_RATE_LIMIT_PER_HOUR` | `10` | Additions allowed per IP per hour. |
| `ADD_RATE_LIMIT_BURST` | `5` | Short-term burst allowance per IP. |
| `UMAMI_URL` | *(empty)* | Full URL of the self-hosted tracker, e.g. `https://umami.example.com/script.js`. |
| `UMAMI_WEBSITE_ID` | *(empty)* | Umami website UUID. Analytics is enabled only when both are set. |
| `UMAMI_DOMAINS` | *(empty)* | Optional comma-separated hostnames passed to `data-domains`. |
| `LEDGER_WEBHOOK_URL` | *(empty)* | Legacy single webhook target, used only while no persisted endpoint is enabled. Prefer `/admin/webhooks`. Empty disables the legacy path. |
| `LEDGER_WEBHOOK_SECRET` | *(empty)* | Legacy HMAC-SHA256 key for `X-VanDyke-Signature`. Empty sends unsigned payloads. |
| `METRICS_TOKEN` | *(empty)* | When set, `GET /metrics` requires `Authorization: Bearer <token>` and returns 401 otherwise. `/readyz` is never gated. |
| `ADMIN_SETUP_TOKEN` | *(empty)* | Optional token required by `/admin/setup`. Without it the first visitor can claim a fresh instance. |
| `ADMIN_LOGIN_RATE_PER_MINUTE` | `10` | Login attempts allowed per IP per minute. |
| `ADMIN_LOGIN_RATE_BURST` | `5` | Short-term login burst allowance per IP. |
| `OIDC_ISSUER` | *(empty)* | OpenID Connect issuer URL. Enables OIDC when set with a client ID. |
| `OIDC_CLIENT_ID` | *(empty)* | OIDC client ID. |
| `OIDC_CLIENT_SECRET` | *(empty)* | OIDC client secret, if the provider requires one. |
| `OIDC_REDIRECT_URL` | *(derived)* | Fixed callback URL, e.g. `https://vandyke.example.com/admin/oidc/callback`. |

## API

Base path `/api/v1`. Responses are JSON with `snake_case` keys. Errors use a
stable envelope:

```json
{ "error": { "code": "not_found", "message": "entry does not exist" } }
```

| Method & path | Description |
| --- | --- |
| `GET /api/v1/entries` | Paginated list. Query: `q`, `sort` (`word`, `meaning`, `created_at`, `id`), `order` (`asc`/`desc`), `page` (1-based), `per_page` (1–100, default 30), `photo` (`1` for entries with a photo, `0` for entries without), `year` (`YYYY`), `from`/`to` (`YYYY-MM-DD`, inclusive), `person` (exact name, case-insensitive), `tag` (a tag slug; repeat for an intersection). Every filter combines by intersection. A malformed `from` or `to` answers `400 invalid_date`. |
| `GET /api/v1/entries/{id}` | Single entry. `404` when missing or in the trash. |
| `POST /api/v1/entries` | Add an entry. JSON body `{"word": "...", "meaning": "...", "person": "..."}` (64 KiB cap) or `multipart/form-data` with `word`, `meaning`, optional `person` and an optional `photo` file. `person` is the slip's author and may be omitted. `201` with `Location`; `409` when the entry already exists; `422` on validation errors; `413` when an upload is too large; `429` when rate limited. |
| `GET /api/v1/entries/{id}/photo` | The entry's public photo. Defaults to the clean display image; add `?size=thumb` for the thumbnail. `404` when the entry has no photo or is in the trash. The original upload is available only to admins. |
| `GET /api/v1/stats` | `total_entries`, `unique_words`, `first_entry_at`, `last_entry_at`, `entries_last_30_days`, `with_photo`, `by_year` (`[{"year": 2026, "count": 12}]`) and `top_persons` (`[{"person": "Marieke", "count": 4}]`, the eight busiest names). |
| `GET /api/v1/persons` | Distinct attributed persons with counts: `{"items": [{"person": "M. van Dijke", "count": 12}], "total": 3}`, ordered case-insensitively. Unattributed entries are not listed. |
| `GET /api/v1/random` | One randomly drawn live entry, the same object as `GET /api/v1/entries/{id}`, with `Cache-Control: no-store`. Optional `photo` and `person` filters. `404 not_found` when nothing matches. This is a sibling route; `GET /api/v1/entries/random` still answers `400 invalid_id`. |
| `GET /api/v1/slip` | The deterministic slip of the day: `{"date": "2026-11-11", "entry": {...}}`, where `entry` is the same object as `GET /api/v1/entries/{id}`. A strong `ETag` of the form `"slip-<date>-<id>"` and `Cache-Control: public, max-age=60`; a matching `If-None-Match` answers `304`. `404 not_found` when the ledger has no live entry. |
| `GET /healthz` | Liveness probe (`{"status":"ok"}`). Never touches the database. |
| `GET /readyz` | Readiness probe: SQLite answers and every embedded migration is applied. `200` with `{"status":"ready","checks":{"database":"ok","migrations":"ok"}}`, or `503` with `unavailable` and the coarse failing check. |
| `GET /metrics` | Prometheus text exposition 0.0.4. Public unless `METRICS_TOKEN` is set. |
| `GET /api/v1/events` | Server-sent event stream of new entries. |
| `GET /api/v1/push/config` | Public notification config: channel flags, Firebase client options, VAPID public key. |
| `POST /api/v1/push/devices` | Register or refresh an Android FCM token. |
| `DELETE /api/v1/push/devices` | Remove an Android FCM token. |
| `POST /api/v1/push/subscriptions` | Register or refresh a browser Web Push subscription. |
| `DELETE /api/v1/push/subscriptions` | Remove a browser Web Push subscription. |

Examples:

```sh
curl -s 'localhost:8080/api/v1/entries?q=boeke&per_page=5'
curl -s 'localhost:8080/api/v1/entries?photo=1&year=2026'
curl -s 'localhost:8080/api/v1/entries?person=Marieke'
curl -s 'localhost:8080/api/v1/entries?tag=typo&year=2024'
curl -s 'localhost:8080/api/v1/entries?tag=typo&tag=name'
curl -s localhost:8080/api/v1/persons
curl -s localhost:8080/api/v1/random
curl -s localhost:8080/api/v1/slip
curl -s 'localhost:8080/api/v1/random?person=Marieke&photo=1'
curl -s localhost:8080/api/v1/entries/1
curl -s -o thumb.jpg 'localhost:8080/api/v1/entries/1/photo?size=thumb'
curl -s localhost:8080/api/v1/stats
curl -s -X POST localhost:8080/api/v1/entries \
  -H 'Content-Type: application/json' \
  -d '{"word":"nieuw woord","meaning":"nieuwe correctie"}'
curl -s -X POST localhost:8080/api/v1/entries \
  -F 'word=nieuw woord' -F 'meaning=nieuwe correctie' -F 'photo=@slip.png'
```

Validation: without a photo, `word` is 1–200 characters and `meaning` is 1–500
characters; both are trimmed. Entries created with a photo may leave the text
fields empty, so a photo alone is a valid entry. Photos must be JPEG, PNG, GIF
or WebP and at most 10 MB; images above 12,000 pixels per side or 40 megapixels
are rejected. Uploads are stored in SQLite as the original plus a display image
(max 1600 px) and thumbnail (max 320 px). The public variants are re-encoded as
JPEG without EXIF metadata; the original is retained for admin download. Entries
return `photo_url` when they have a photo. Duplicate submissions are rejected:
the unique index on `(word, meaning)` compares case-insensitively (migration
`007_dedupe_entries.sql`), so adding an existing pair answers `409
duplicate_entry` from the API and `409` with a form error from the web UI.

## Search

`GET /entries` and `GET /api/v1/entries` share one search grammar and one
trigram index over the misspelling, the correction and the person:

- Whitespace-separated terms are all required (AND): `feature euten`.
- `"..."` matches an exact phrase: `"van Dijke"`.
- A leading `-` excludes a term or phrase: `feature -feutenreus`.
- `from` and `to` take `YYYY-MM-DD` and are inclusive. Either may be supplied
  alone, and both combine with `year` (the result is the intersection).
- Any term shorter than three runes, or a query made only of exclusions, is
  answered by a substring `LIKE` scan over the same three fields; this is the
  same fallback the ledger has always used for short fragments.

The HTML ledger marks included matches with `<mark>`; all entry text is escaped
before marking, and the query itself can never become markup. Add `highlight=0`
to `GET /entries` for the unmarked text. The JSON API always returns the stored
`word`, `meaning` and `person` unchanged.

The trigram index is derived data, kept up to date on every add, edit, delete,
restore, import and seed. An admin can regenerate it from the live entries with
**Rebuild search index** on `/admin/settings`; trashed entries are skipped until
they are restored.

## Tags

Tags record what kind of slip an entry is. The vocabulary is curated by an
admin; entries are never classified automatically and the public cannot tag.
The migration seeds `typo`, `misheard`, `autocorrect`, `name` and `dialect`, and
an admin can rename, recolour, add or delete tags at `/admin/tags`. A slug is
derived once from the name at creation and never changes, so renaming a tag
never breaks a link.

- `GET /tags` lists every tag with its live entry count, including tags with
  zero entries.
- `GET /tags/{slug}` is the canonical page for one tag: the ordinary ledger,
  filtered to that tag, with its own canonical URL. An unknown slug is `404`.
- `GET /entries?tag=typo` and `GET /api/v1/entries?tag=typo` filter the list.
  Repeat `tag` for an intersection (`?tag=typo&tag=name` returns only entries
  carrying both), and combine it with `q`, `from`, `to`, `year`, `photo` and
  `person`. An unknown tag returns `200` with an empty result.
- Ledger rows and `/e/{id}` pages show one chip per tag, each linking to the
  filtered ledger.
- `GET /feed.xml?tag=typo` filters the Atom feed; the tag is part of the feed
  id, self link and ETag, so tag feeds cache independently.

Trashed entries never count, never appear on a tag page and never enter a tag
feed or the sitemap. Tags survive a trash-and-restore cycle and disappear only
when the entry is purged.

## Year in review

`GET /y/{yyyy}` recaps one calendar year: the number of live entries, the first
and last slip with dates and permalinks, the busiest month, a bar for each of
the twelve months, the five most frequent names, the longest misspelling, the
number and percentage of entries with a photo, and a link to
`/entries?year={yyyy}`.

Every figure is computed on read from the same table the ledger uses; nothing
is stored. Ties are deterministic (the earliest month, the person with the
alphabetically earlier name, the earlier misspelling of the same length). A
year with no live entries is `404`. The page sends
`ETag: "year-{yyyy}-{hash}"` and `Cache-Control: public, max-age=300`, answers
a matching `If-None-Match` with `304`, and is linked from the `/stats` by-year
chart for every year that has entries. It renders without JavaScript, and the
month chart and person ranking each have a visually hidden table alternative.

## Browsing: words, people, a random slip and the gallery

The ledger is searchable, but these pages make it navigable without a query.

- `GET /words` is an A–Z index of every distinct misspelling recorded on a live
  entry, grouped case-insensitively the way the unique `(word, meaning)` index
  folds it. The fixed rail shows A–Z plus `#` (digits, punctuation, accented and
  non-Latin spellings) with a count per bucket; an empty bucket is inert text,
  never a link. One bucket renders per document, defaulting to `#` when it has
  rows and otherwise the first non-empty letter. `?letter=F` selects a bucket
  (an empty or unknown one is `404`), `?page=` pages a long letter at 200 rows,
  and every row shows the spelling, its entry count, its first and last date and
  links to the newest occurrence's permalink. A note appears only when a live
  slip has a photograph but no text, and links to `/photos`.
- `GET /p/{person}` scopes the ordinary ledger to one attributed name. The name
  matches case-insensitively, so `/p/marieke` and `/p/Marieke` are one document,
  and the page canonicalises to the spelling of the newest matching slip. It
  shows the count and the first and last slip, and it keeps its scope in the
  search box, sort headers, pager and clear-search link; a `person` query
  parameter is ignored because the path wins. Names with spaces, accents or a
  slash work through the percent-encoded path segment. An unknown or empty name
  is `404`. The page is `noindex, follow` and is not in the sitemap: every row
  already links to an indexed permalink.
- `GET /p/{person}/feed.xml` is that person's Atom 1.0 feed: only their live
  entries, newest first, with the person as the feed-level author and a feed id
  of `{base}/p/{person}/feed.xml`. `limit` defaults to 50 and is capped at 100.
  Responses carry a strong `ETag` and `Cache-Control: public, max-age=60`, and a
  matching `If-None-Match` answers `304`. A person with no live slip is `404`.
- `GET /random` draws one live entry and answers `303 See Other` to its
  `/e/{id}` permalink, with `Cache-Control: no-store` and `X-Robots-Tag:
  noindex`, so a reload does not reroll. Optional `photo=1` and `person=` filters
  narrow the draw; an empty or unmatched ledger is the not-found page.
  `GET /api/v1/random` returns the same entry object as the JSON permalink with
  `Cache-Control: no-store`.
- `GET /photos` is the gallery of every live entry that has a photo, newest
  first, 24 tiles per batch. `?cursor=` is an opaque token for the last rendered
  tile's `(created_at, id)`, so an entry added while a reader scrolls can never
  repeat or skip a tile below the cursor; a malformed cursor is `400` rather
  than a silent restart. The sentinel after the last tile is a real link with
  `hx-trigger="revealed"`, so HTMX appends the next batch, while the same URL
  without `HX-Request` is a complete page with `rel="prev"`/`rel="next"` links
  and a link back to `/photos`. `/photos` sets `Vary: HX-Request`. Grid images
  are always the `thumb` variant with `loading="lazy"`; a single native
  `<dialog>` lightbox loads the display image only when a tile is opened and
  returns focus to that tile on close. The bare gallery is indexable; cursor
  variants are `noindex, follow` with `/photos` as canonical.

## Slip of the day

One entry is featured for the whole local calendar day. It is not random: the
resolver hashes `vandyke-daily-slip\0<YYYY-MM-DD>` with FNV-1a 64, uses the
remainder modulo the ascending live ids as an index, and pins the chosen id in
the `settings` table under `daily_slip.<YYYY-MM-DD>`. Every process and visitor
therefore computes and serves the same entry, and adding (or trashing a
different) entry during the day does not change it.

- `GET /` renders the slip between the hero and the statistics block, with the
  misspelling, the correction, the date, an optional person chip, an optional
  thumbnail and a link to its permalink. It is omitted while any listing
  parameter is present (`q`, `person`, `year`, `from`, `to`, `photo`, `page`,
  `per_page`, `sort`, `order`) and hidden by JavaScript as soon as a search term
  or filter becomes active. The HTMX ledger fragment never contains it.
- `GET /api/v1/slip` returns `{"date": "...", "entry": {...}}` with the entry
  object used everywhere else, a strong ETag `"slip-<date>-<id>"`, a 60-second
  public cache and a `304` for a repeated `If-None-Match`.
- The pinned entry's `/e/{id}` page shows a **Slip of the day** badge; its
  canonical URL is unchanged.
- The pinned id is replaced only when the entry no longer resolves (it was
  trashed or purged), because a removed pick must not leave the home page
  pointing at a 404 for the rest of the day. Pins older than the current date
  are pruned on every write, so the table holds at most one.
- The day is the server's local date; set `TZ` in the deployment to control it.
  A ledger with no live entry has no slip: the section disappears and the JSON
  surface answers `404 not_found` with `the ledger has no entry for today`.

`GET /api/v1/slip` is documented in the [API](#api) table, and the
`trmnl/plugins/vandyke-slip` plugin polls it hourly.

## Atom feed

`GET /feed.xml` serves the newest live entries as Atom 1.0. Each entry uses
`{base}/e/{id}` as both its `id` and `link`, the misspelling as `title`, the
correction as `summary`, the logged date as `updated`, and the person as
`author` when one is recorded. The feed-level author names the ledger, and
trashed entries are never included.

- `limit` defaults to 50 and is capped at 100. A non-numeric or sub-1 value uses
  the default; a larger one is capped. The cap matches `store.MaxPerPage`.
- `tag` (a slug) filters the feed. The tag is folded into the feed id and self
  link (`/feed.xml?tag=typo`) and into the ETag, so one tag's feed never shares
  a cache entry with another's. An unknown tag is a valid empty feed.
- `GET /p/{person}/feed.xml` is the same document shape scoped to one person.
  Its id, self link and ETag include the person, so two names never share a
  cache entry, and a case variant resolves to the canonical spelling's feed.
- Responses carry a strong `ETag` derived from the newest entry id, the number
  of rows and any tag filter, plus `Cache-Control: public, max-age=60`. A
  matching `If-None-Match` answers `304` with no body.
- Every public page head carries
  `<link rel="alternate" type="application/atom+xml" title="The vanDyke Corrections Ledger" href="/feed.xml">`.
- Absolute URLs follow `PUBLIC_BASE_URL` and otherwise the request scheme and
  host, exactly like permalinks.

## Permalinks

Every entry has a stable public page at `/e/{id}`. The page shows the full
entry, its date, its photo and its tag chips, and includes Open Graph/Twitter
metadata for link previews. Entry numbers in the ledger link to their
permalink. Set `PUBLIC_BASE_URL` behind a reverse proxy so canonical and social
URLs use the public hostname; otherwise they are derived from the request.

- **Share or copy.** The primary control is a real anchor pointing at the
  canonical URL. Where `navigator.share` exists it opens the share sheet; a
  cancelled sheet is a no-op, and any other failure falls back to the clipboard
  (`navigator.clipboard.writeText`, then `document.execCommand`). Success shows
  `Link copied` and announces `Link copied.` in the page's polite live region.
  Without JavaScript the anchor is an ordinary link the browser can copy.
- **Structured data.** Each permalink carries one JSON-LD `DefinedTerm`: the
  misspelling as `name`, the correction as `description`, the permalink as
  `url`, the entry id as `identifier`, the creation time as `dateCreated` and
  `inDefinedTermSet` naming the ledger. A photo-only entry uses
  `Photo entry #<id>` and adds the absolute photo as `image`.
- **Preview image.** Every permalink declares `og:image` and `twitter:image`
  with `twitter:card: summary_large_image`; entries without a photo use the
  committed `1200×630` asset at `/static/images/og-default.png`, so a
  text-only slip still gets a large card. Card images are never generated at
  request time.
- **Indexing.** `/e/{id}` is `index,follow` with a self-canonical; `/entries`
  in every form — and `/` carrying any listing parameter — is `noindex,follow`
  without a canonical. Every live permalink stays in `/sitemap.xml`.

## Discoverability and accessibility

The binary serves its own crawler and browser metadata, all through the same
middleware as every other route:

- `GET /robots.txt` — `text/plain`; disallows `/admin` and names the sitemap.
- `GET /sitemap.xml` — `application/xml`; the home page, `/stats`, `/words`,
  `/photos`, `/tags`, every tag with at least one live entry, every year with at
  least one live entry, and one `/e/{id}` permalink per live entry, all absolute
  URLs. Empty tags, empty years, person pages, `/random` and query-string filter
  URLs are omitted.
- `GET /feed.xml` — `application/atom+xml; charset=utf-8`; the newest live
  entries, newest first, plus one feed per person at
  `/p/{person}/feed.xml`.
- `GET /site.webmanifest` — `application/manifest+json`; name, short name,
  `start_url` `/`, `standalone` display, `#f4eee1` theme/background colours and
  the existing favicon and Apple touch icon.
- `GET /opensearch.xml` — `application/opensearchdescription+xml; charset=utf-8`;
  an OpenSearch 1.1 description document with the short name `vanDyke`, UTF-8
  input encoding and the URL template `{base}/?q={searchTerms}`. Every HTML
  head advertises it with `<link rel="search" ...>`, so a browser can add the
  ledger as a search provider. The template uses the ordinary search grammar
  (quoted phrases and `-` exclusions included); no suggestions endpoint is
  advertised.

Canonical and robots signals are decided per surface: `/`, `/stats` and
`/e/{id}` are `index,follow` with a self-canonical built from
`PUBLIC_BASE_URL` (or the request). `/entries` in every form, and `/` carrying
any listing parameter, are `noindex,follow` and carry no canonical, because a
canonical pointing elsewhere beside `noindex` sends conflicting signals.
`/stats` also emits a schema.org `Dataset` with its temporal coverage, measured
values and a `DataDownload` for `/api/v1/stats`.

Sitemap and robots URLs follow `PUBLIC_BASE_URL` when it is set and otherwise
the request scheme and host, exactly like permalink canonicals. Trashed entries
are absent from the sitemap.

The public ledger works without JavaScript. `GET /entries` returns a complete
document when requested directly (sort, pager and "Clear search" links are plain
navigations), and the bare ledger fragment only when HTMX asks for it. The same
rule applies to `/photos`: a cursor URL is a complete page when navigated to
directly and a bare batch only when `HX-Request` is present (with `Vary:
HX-Request` keeping a shared cache from mixing the two). Every
page carries a skip link to `main#content`, the theme toggle reports
`aria-pressed`, dialog triggers report `aria-expanded`, the statistics charts
have visually hidden tables as text alternatives, and the admin login, setup and
edit forms associate their hints and error messages with the inputs.

## Admin and authentication

The ledger is public to read and to add to. Editing and deleting require an
admin account at `/admin`.

1. Open `/admin/setup` on a fresh database and create the first local admin.
   If `ADMIN_SETUP_TOKEN` is set, enter that token too. The setup page closes
   permanently once an admin exists.
2. Sign in at `/admin/login`. Sessions last seven days, are stored in SQLite,
   and are protected by an `HttpOnly`, `SameSite=Lax` cookie and a CSRF token
   on every mutating form.
3. Edit an entry with **Edit** in the admin ledger. Word, meaning, logged date
   and photo can all be changed. Deleting an entry moves it to the trash and
   keeps its photo until you restore or purge it.

### Roles and admin accounts

Each admin is an **owner** or an **editor**. Owners can do everything an editor
can, plus delete entries and tags, purge the trash, manage admins and read the
audit trail. Editors can edit entries, restore trashed entries and change
settings.

- `/admin/admins` (owners only) lists every account and creates, promotes,
  demotes, disables, re-enables, deletes and resets the password of another
  admin.
- The last enabled owner can never be demoted, disabled or deleted: the server
  refuses with `409`. Promote or enable another owner first. Owners also cannot
  demote, disable or delete their own account.
- Disabling is reversible and takes effect immediately: the disabled admin is
  refused at sign-in and any session cookie they already hold is rejected on the
  next request.
- `/admin/account` changes your own password (current password required, at
  least 12 characters) and signs out your other sessions.

### Sessions and the audit trail

- `/admin/sessions` lists your sessions with a reference, creation and expiry
  time, and lets you revoke one session or every other session. An owner may
  revoke any reference; an editor can only revoke their own.
- `/admin/audit` (owners only) is a read-only, paginated trail of sign-ins,
  entry changes, settings saves, admin changes and session revocations. It
  records actor, action, subject, client IP and time, and never stores passwords,
  hashes or session tokens. Filter it by action, actor and date range. No route
  edits or deletes audit rows.

### Trash

Deleting an entry in the admin ledger or with the bulk delete form moves it to
`/admin/trash`; nothing is removed from the database yet. Trashed entries are
hidden from the public site, `/api/v1`, search, statistics and `/e/{id}`, which
all behave as if the entry does not exist, while an admin can still open it to
inspect it. Its search-index row is removed on delete and written again on
restore, so search never has to trust the trash flag alone.

- `GET /admin/trash` lists trashed entries with their logged and deletion
  times, newest deletion first, and shows the current trashed count.
- **Restore** clears the deletion marker and puts the entry back on every read
  path with its original photo. Restore is idempotent.
- **Purge** permanently deletes the entry, its search-index row and its photo.
  Use it only when you are sure the entry is no longer needed.

Both actions work per row or on a selection, accept at most 100 ids per
request, and are CSRF-protected. There is no retention job: an entry stays in
the trash until an admin explicitly purges it. A trashed entry still occupies
its case-insensitive `(misspelling, correction)` slot, so re-adding the same
pair fails as a duplicate until the old row is purged. Tags on a trashed entry
are kept and return with it on restore; purging removes them with the entry.

### Tags

`/admin/tags` manages the vocabulary. Each row shows a **live** count (entries
not in the trash) and a **total** count (every assignment, trash included), a
rename and colour form, and a delete control. Create and save are available to
every admin and CSRF-protected; delete is owner-only, CSRF-protected, and its
confirmation states how many entries lose the tag because it removes the tag
from all of them at once. Names are unique case-insensitively and capped at 40
characters; the slug is derived once from the name and never changes. Every
create, rename, colour change and deletion writes an audit row
(`tag.create`, `tag.rename`, `tag.color`, `tag.delete`).

Assigning tags is part of the ordinary entry edit form: the tag fieldset lists
every tag as a checkbox, the submitted set replaces the entry's tags, and
clearing every box removes them all. Each change writes an `entry.tags` audit
row with the sorted resulting slugs.

### Webhooks

Delivery targets live at `/admin/webhooks`. Each endpoint has a name, an
absolute `http`/`https` URL, an optional signing secret, an enabled flag, a
comma-separated subscription and a formatting preset. Up to 20 endpoints are
allowed, and names are unique case-insensitively. Saving, editing, enabling,
disabling, adopting or deleting an endpoint rebuilds the in-memory target
snapshot immediately; no restart is needed.

The five events are `entry.created`, `entry.updated`, `entry.deleted`,
`entry.restored` and `entry.purged`. One event is emitted per entry actually
changed, including every changed row in a bulk action of up to 100 ids, so a
100-entry delete produces 100 events. An already-trashed id, a no-op edit and a
bulk import produce none. An import is a reconciliation action; after an import,
receivers should reconcile from `GET /api/v1/entries` or an export. There is no
import event.

The `generic` `entry.created` body is stable:

```json
{
  "event": "entry.created",
  "id": 123,
  "word": "feuten",
  "meaning": "feature",
  "person": "M. van Dijke",
  "created_at": "2026-11-10T09:00:00Z",
  "url": "https://vandyke.example.com/e/123"
}
```

- The request is `POST` with `Content-Type: application/json`. `person` is an
  empty string when the entry is unattributed, and `url` is the absolute
  `/e/{id}` permalink using `PUBLIC_BASE_URL` when set.
- The four newer events add `occurred_at` in RFC 3339. `entry.updated` also
  carries `changes` (`word`, `meaning`, `person`, `created_at`, `photo`, each
  `{"from": ..., "to": ...}`), and editing a trashed entry adds `deleted_at`.
  `entry.deleted` and `entry.purged` always carry `deleted_at`; `entry.restored`
  does not. A photo change is reported as the previous and new original
  SHA-256 truncated to 12 hex characters, or `null` when that side has no photo.
- With a secret configured, the exact bytes that were sent are signed as
  `X-VanDyke-Signature: sha256=<lowercase hex>` using HMAC-SHA256. The header is
  omitted when the endpoint has no secret.
- **Presets.** `generic` sends the envelope above. `discord` sends a non-empty
  `content`, one embed with `title`, `description`, `url` and `timestamp`, and
  `allowed_mentions.parse: []` so a word containing `@everyone` cannot ping a
  channel. `slack` sends a `text` fallback and one `section` block of type
  `mrkdwn`, escaping `&`, `<` and `>` so `<!channel>` cannot inject a mention.
  A photo-only entry falls back to `Entry #{id}` and the permalink for the
  required non-empty fields.
- **Delivery is non-blocking and at-least-once.** Emission only stamps the
  event and appends to a 1,024-job queue; rendering, signing, DNS and the HTTP
  request happen on one of four workers. When the queue is full the event is
  dropped, logged, and counted in `vandyke_webhook_dropped_total`; the request
  still succeeds.
- **Retries are durable.** A worker persists one `webhook_deliveries` row when
  it picks up a job. `2xx` is delivered; transport errors, timeouts, `408`,
  `425`, `429` and `5xx` are retried with full-jitter exponential backoff
  (5 second base, 1 hour cap) up to 8 attempts; any other response, a redirect,
  or a render failure is terminal. Because delivery is at-least-once, receivers
  should de-duplicate on `event` plus `id` (plus `occurred_at` where present).
- **Delivery log and retention.** `/admin/webhooks` shows endpoint, event, entry
  id, status, attempts, last status, a sanitised last error and the next attempt
  time. "Retry now" puts a failed row back in the queue without resetting its
  attempt count and writes an audit row. Delivered and cancelled rows are kept
  for 7 days, failed rows for 30 days; pending rows are never pruned. An hourly
  job prunes automatically, and an owner can run the same prune explicitly.
  Payload text is cleared as soon as a delivery is delivered or cancelled, and
  purging an entry deletes its terminal delivery history.
- **Secrets and URLs are write-only in the UI.** The list masks the URL path and
  query (Discord and Slack put a token there), the edit form shows the URL but
  never the stored secret, a blank secret field keeps the stored value, and a
  separate control clears it. Logs and audit rows record only the host and
  whether a secret changed.
- **SSRF defaults.** Only absolute `http`/`https` URLs without credentials are
  accepted. Loopback, RFC 1918, IPv6 unique-local, carrier-grade NAT
  (100.64.0.0/10, including Tailscale), link-local, unspecified, multicast and
  IPv4-mapped equivalents are denied by default. An owner can set
  `allow_private` to permit loopback, private and CGNAT targets for a LAN
  receiver such as Home Assistant or ntfy; link-local, unspecified and
  multicast stay denied. The address policy is checked when an endpoint is
  saved and again by the dialer on every attempt. Redirects are not followed,
  an attempt times out after 5 seconds, and at most 4 KiB of the response is
  read and discarded.
- **Per-endpoint test fire.** `POST /admin/webhooks/{id}/test` sends one
  `entry.created` for the newest entry with that endpoint's preset and secret,
  applies the same target policy, waits up to 5 seconds and reports the HTTP
  status or a sanitised error without changing the endpoint.

#### Legacy single target

`LEDGER_WEBHOOK_URL`, or `webhook.url` on `/admin/settings`, keeps working as a
read-only synthesized endpoint named "Environment default". It uses the generic
preset, subscribes only to `entry.created`, records deliveries with a null
endpoint id and is treated as `allow_private` so an existing LAN target keeps
working (link-local is still refused). It is active only while no persisted
endpoint is enabled; any enabled endpoint suppresses it, so a migration cannot
double-deliver. **Adopt as an endpoint** on `/admin/webhooks` copies the
effective URL and secret into a real endpoint. The settings page labels the
section as legacy, and its "Send legacy test event" action reports when the
target is suppressed.

### Notifications

Every new entry can be announced over three independent channels. Notifications are
off until the master switch is enabled, and each channel toggles separately.
Configure everything under Admin → Settings → Notifications; credentials are stored
in the database and entered at runtime, so no `google-services.json` is compiled into
the Android build.

| Channel | Transport | Reaches |
| --- | --- | --- |
| Realtime | `GET /api/v1/events` (server-sent events) | Open web pages and foregrounded apps |
| Web Push | VAPID + service worker (`/sw.js`) | Browsers the visitor subscribed |
| Android | FCM HTTP v1 | Android installs that registered an FCM token |

Copy comes from templates with the placeholders `{word}`, `{meaning}`, `{person}`,
`{id}`, and `{base}`. The click URL must contain `{id}` and may be absolute or
site-relative, for example `/e/{id}`.

**Web Push.** Use *Generate VAPID keys* in the settings card to create a key pair;
the private key is write-only and never rendered back. Browsers only allow
subscriptions on HTTPS origins (or `localhost`), and the service worker is served
from `/sw.js` so it controls the whole origin.

**Android (FCM).** Paste the Firebase service account's `project_id`,
`client_email`, and `private_key`. The public client values (`api_key`, `app_id`,
`sender_id`) are exposed by `GET /api/v1/push/config` so the app can initialize
Firebase at runtime. Installs register with `POST /api/v1/push/devices`; tokens that
FCM reports as unregistered are disabled automatically instead of retried.

**Per-user preferences.** Each admin can mute realtime, Web Push, or new-entry
notifications at `/admin/account`. An install or browser can stop receiving pushes
by calling the matching `DELETE` endpoint.

**Test and troubleshooting.** *Send test notification* fans a sample out over every
enabled channel and reports how many targets succeeded. Delivery is
fire-and-forget: a failing receiver never blocks or fails the entry creation that
triggered it.

When the matching stored value is blank, these environment variables are used:
`NOTIFICATIONS_ENABLED`, `NOTIFICATIONS_REALTIME_ENABLED`,
`NOTIFICATIONS_WEBPUSH_ENABLED`, `NOTIFICATIONS_ANDROID_ENABLED`,
`NOTIFICATIONS_ENTRY_CREATED`, `NOTIFICATIONS_TITLE`, `NOTIFICATIONS_BODY`,
`NOTIFICATIONS_CLICK_URL`, `NOTIFICATIONS_VAPID_SUBJECT`,
`NOTIFICATIONS_VAPID_PUBLIC_KEY`, `NOTIFICATIONS_VAPID_PRIVATE_KEY`,
`NOTIFICATIONS_FCM_PROJECT_ID`, `NOTIFICATIONS_FCM_CLIENT_EMAIL`,
`NOTIFICATIONS_FCM_PRIVATE_KEY`, `NOTIFICATIONS_FIREBASE_API_KEY`,
`NOTIFICATIONS_FIREBASE_APP_ID`, `NOTIFICATIONS_FIREBASE_SENDER_ID`.

### Export

Every export route requires an admin session and downloads as an attachment
with `Cache-Control: no-store`:

- `GET /admin/export.json` — versioned JSON envelope with `format_version` `1`,
  `exported_at`, `entry_count`, `photo_count` and one object per entry.
- `GET /admin/export.csv` — RFC 4180 CSV with a header row naming the same
  fields as the JSON export.
- `GET /admin/export.zip` — streaming archive with `manifest.json`,
  `entries.json` and `photos/{id}.jpg`.

The ZIP carries the 1600 px display derivative, not the original photo. Use the
per-photo admin download when the original bytes are required.

### Import

`GET /admin/import` uploads a JSON export or a ZIP archive. `mode=preview`
classifies every row without writing (would-create, would-skip, would-conflict);
`mode=apply` writes the whole file in one transaction. `conflict=skip` (default)
leaves a matching `(word, meaning)` row untouched; `conflict=replace` updates
its `person`, `created_at` and photo but never its word or meaning. An error on
any row rolls the whole file back.

Caps: 64 MiB per request, 20,000 entries and 10 MiB per photo. Requests over a
cap are refused with 413.

Import does not emit webhook events: a 20,000-row file would flood a deliberately
small queue or deliver an arbitrary prefix. Receivers that need to reconcile
after an import should read `GET /api/v1/entries` or an export.

### OIDC

Set `OIDC_ISSUER`, `OIDC_CLIENT_ID`, and optionally `OIDC_CLIENT_SECRET` and
`OIDC_REDIRECT_URL`. The provider must expose standard discovery metadata
(`/.well-known/openid-configuration`) and support the authorization-code flow
with PKCE. The default callback is `<request scheme and host>/admin/oidc/callback`;
register that URL with your provider unless you set `OIDC_REDIRECT_URL`.

Any identity the provider successfully authenticates is auto-provisioned as an
admin on first login. **The provider's own access policy is therefore the only
boundary** — configure it accordingly.

On a fresh instance you can go either way: `/admin/setup` still creates a local
username and password, and while no admin exists that same page offers
"Continue with OpenID Connect", where the first identity to finish signing in
becomes the first admin. Once an admin exists, sign-in is available from the
`Sign in` link in the site navigation or directly at `/admin/login`.

## Observability

`GET /metrics` serves Prometheus text exposition format 0.0.4, hand-written with
the standard library (no module is added to `go.mod`). It is public by default,
matching the already-public ledger and API, so Prometheus can scrape it from
another container. Set `METRICS_TOKEN` to require
`Authorization: Bearer <token>`, compared in constant time; a missing or wrong
token is a `401` with no body. The endpoint sets
`Content-Type: text/plain; version=0.0.4; charset=utf-8` and
`Cache-Control: no-store`.

Exported series:

| Metric | Type | Meaning |
| --- | --- | --- |
| `vandyke_entries_total` | gauge | All entries, including trashed rows. |
| `vandyke_entries_live` / `vandyke_entries_trashed` | gauge | Current live and trashed counts. |
| `vandyke_entries_with_photo` | gauge | Live entries with a photo. |
| `vandyke_entry_additions_total{outcome}` | counter | `created`, `duplicate`, `invalid`, `rate_limited`, `error`. |
| `vandyke_rate_limited_requests_total` | counter | Creates denied by either limiter. |
| `vandyke_webhook_deliveries_total{result}` | counter | `delivered`, `failed`, `cancelled` outcomes (a manual retry can add a second outcome for one row). |
| `vandyke_webhook_attempts_total{result}` | counter | `success`, `retry`, `permanent_failure`. |
| `vandyke_webhook_queue_depth` / `vandyke_webhook_pending_deliveries` | gauge | In-process queue length and persisted pending rows. |
| `vandyke_webhook_dropped_total` | counter | Events dropped on a full queue. |
| `vandyke_http_requests_total{method,route,status}` | counter | Completed requests. |
| `vandyke_http_request_duration_seconds{method,route}` | histogram | Buckets 5ms to 10s plus `+Inf`. |
| `vandyke_db_size_bytes`, `vandyke_db_up` | gauge | Main SQLite file size (excluding `-wal`/`-shm`) and scrape-time query success. |
| `vandyke_process_start_time_seconds`, `vandyke_process_uptime_seconds` | gauge | Process lifetime. |
| `vandyke_build_info{version,go_version,goos,goarch}` | gauge | Constant 1. |

`route` is always the registered mux pattern (for example `GET /e/{id}`) or
`unmatched`, never the raw path, so no entry id becomes a series. Database
gauges are read at scrape time under a 2 second timeout: if the query fails,
`vandyke_db_up` is `0`, the DB series are omitted, the failure is logged, and the
scrape still answers `200` with the process metrics. Counters reset on restart.

`GET /readyz` is separate from `GET /healthz`. It runs `SELECT 1` and verifies
that every embedded migration is recorded in `schema_migrations`, under a 2
second timeout, and returns `200` with
`{"status":"ready","checks":{"database":"ok","migrations":"ok"}}` or `503` with
`unavailable` and the coarse failing check. It never contains the DSN, database
path, an error string or a credential, and it is never gated by
`METRICS_TOKEN`, because probes must work without credentials. `/healthz` keeps
its historical always-`200` body.

## Umami analytics

vanDyke only injects the tracker script — you run Umami yourself:

1. Deploy Umami with the
   [official Docker setup](https://umami.is/docs/install) (Umami's own docs
   cover the database and reverse proxy).
2. Add a website in the Umami dashboard and copy its UUID.
3. Set `UMAMI_URL` (the full `…/script.js` URL) and `UMAMI_WEBSITE_ID`, or
   sign in and use **Analytics settings** at `/admin/settings`.
4. Optionally restrict tracking with `UMAMI_DOMAINS`.

Values saved in the admin interface override the environment per field; clear
a field to fall back to its `UMAMI_*` variable. The Content-Security-Policy
automatically allows the effective tracker origin and updates when settings
change. Without a URL and website ID no script is loaded and no request leaves
the page. Events sent: `search` (debounced query, ≥ 2 characters),
`entry-added`, `theme-toggle`.

## TRMNL plugins

The ledger has four companion e-ink plugins in [`trmnl/`](trmnl/README.md):

| Plugin | TRMNL ID | Shows |
| --- | ---: | --- |
| `vandyke-stats` | 521911 | Ledger totals, unique misspellings, last 30 days and the last slip |
| `vandyke-latest` | 521909 | The three newest misspellings |
| `vandyke-photo` | 521910 | The newest entry with a photo |
| `vandyke-dashboard` | 521908 | Stats, latest entries and the newest photo |

Each plugin takes one setting: the base URL of the ledger. The plugin IDs are
pinned in `src/settings.yml`, so `trmnlp push` updates the existing TRMNL
plugins. Every release attaches per-plugin zips and a combined archive to the
GitHub release; `make trmnl-package` builds the same archives locally under
`dist/trmnl/`.

## Data provenance

The seed dataset in `internal/store/seed_entries.json` is a snapshot of
[vandyke.thor.edu](https://vandyke.thor.edu/) taken on **2026-10-06**:
**78 entries**, deduplicated by misspelling and correction (case-insensitive,
earliest timestamp kept), with timestamps as recorded on the original site
(2017-10-02 → 2026-03-13). It was extracted with
`scripts/scrape_seed.py`. The seed is imported exactly once, when the `entries`
table is empty — the original page remains the canonical source if the two ever
diverge.

## Project layout

```
cmd/vandyke/           main: config, HTTP server, graceful shutdown
internal/config/       environment configuration
internal/store/        SQLite store, migrations, embedded seed data
internal/auth/         password hashing, sessions, OIDC
internal/admin/        authenticated admin interface under /admin
internal/settings/     runtime Umami settings with env fallback
internal/media/        photo derivatives, EXIF stripping and backfill
internal/ratelimit/    per-IP token bucket
internal/httpx/        shared HTTP helpers (client IP, JSON envelopes, logging)
internal/metrics/      Prometheus registry, HTTP middleware and text exposition
internal/webhook/      endpoint snapshots, presets, SSRF policy and delivery engine
internal/httpapi/      REST API under /api/v1, /healthz, /readyz and /metrics
internal/web/          public HTML interface: templates, CSS, JS, fonts
trmnl/                 five TRMNL plugins, transforms and tests
scripts/               seed scraper, font fetcher, TRMNL packaging
```

## Security notes

Reading and adding entries remain intentionally open, like the original: anyone
who can reach the site can add an entry. Editing and deleting require an admin
login, and login attempts are rate-limited. New instances should either set
`ADMIN_SETUP_TOKEN` or be claimed at `/admin/setup` before they are exposed.
OIDC deployments inherit the provider's access policy because any authenticated
identity becomes an admin.

Responses carry `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`
and a CSP tailored to the effective Umami settings.

## Testing

```sh
go test ./...
```

The suite covers the store (seeding, FTS partial search, person and date-range
search, query grammar, search-index updates and rebuild, sorting, pagination,
validation, stats, admin/session lifecycle, entry edits and bulk deletes,
settings), photo derivatives and backfill, password
hashing and OIDC discovery/token verification, the rate limiter, the JSON API,
the admin flows (setup, login, CSRF, edit, delete, original photo download,
Umami and webhook settings, webhook test-fire, the webhook endpoint page and
its role/CSRF rules), the Atom feed (validity,
escaping, ordering, limit, ETag/304, trash exclusion, autodiscovery), webhook
payloads, presets, HMAC signing, SSRF policy, retry/backoff/drop accounting,
migrations 001-014, `/metrics` exposition and `/readyz`, and the public web layer (partial rendering, HTMX
flows, no-JS flows, permalinks and Open Graph metadata, rate-limit errors, Umami
injection, cache headers, the A–Z word index, person pages and feeds, the random
draw and the cursor-paged photo gallery).

The TRMNL transforms have their own stdlib-only Python tests:

```sh
make trmnl-test
```

## CI, releases and updates

- **CI** (`.github/workflows/ci.yaml`) — builds, vets, lints
  (`golangci-lint`) and tests every push and pull request, and uploads a
  coverage report.
- **TRMNL plugins** (`trmnl.yaml`) — tests the transforms, lints all five
  plugins, and pushes them to TRMNL on `main` when `TRML_TOKEN` is configured.
- **Releases** (`release.yaml`) — [semantic-release](https://semantic-release.gitbook.io/)
  turns Conventional Commits into tags, GitHub releases and a changelog,
  attaches the versioned TRMNL plugin zips, then publishes the Docker image to
  `martynvandijke/vandyke` on Docker Hub and `ghcr.io/martynvdijke/vandyke` on
  GHCR.
- **Renovate** (`renovate.json`, `renovate.yml`) — scheduled dependency
  updates with automerge.
- **Docs** (`docs.yml`) — builds the `docs/` folder with Zensical and deploys
  it to GitHub Pages (enable Pages → Source: GitHub Actions once).
- **Workflow lint** (`workflow-lint.yml`) and **stale branches**
  (`stale-branches.yml`) — actionlint/zizmor/pinact checks and branch hygiene.

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/):
`feat:` bumps the minor version, `fix:` and friends bump the patch version.
