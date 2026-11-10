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
- **Stable permalinks** — every entry has a page at `/e/{id}` with Open Graph
  and Twitter metadata for clean link previews, plus a copy-link button.
- **Atom feed** — `GET /feed.xml` publishes the newest entries as Atom 1.0 with
  a `limit` parameter, a strong ETag and autodiscovery from every page, so a
  reader or RSS client can subscribe without polling the API.
- **Optional outbound webhook** — post one signed `entry.created` JSON event to
  a target of your choice after each addition. Delivery is best effort and never
  delays the response.
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
- **Four TRMNL e-ink plugins** — ledger stats, latest slips, the newest photo
  entry and a combined dashboard, packaged as versioned release downloads.

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
image is a static binary on Alpine running as a non-root user, with a
`/healthz` healthcheck.

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
| `LEDGER_WEBHOOK_URL` | *(empty)* | Optional target for the `entry.created` webhook. Empty disables dispatch. |
| `LEDGER_WEBHOOK_SECRET` | *(empty)* | Optional HMAC-SHA256 key for `X-VanDyke-Signature`. Empty sends unsigned payloads. |
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
| `GET /api/v1/entries` | Paginated list. Query: `q`, `sort` (`word`, `meaning`, `created_at`, `id`), `order` (`asc`/`desc`), `page` (1-based), `per_page` (1–100, default 30), `photo` (`1` for entries with a photo, `0` for entries without), `year` (`YYYY`), `from`/`to` (`YYYY-MM-DD`, inclusive), `person` (exact name, case-insensitive). A malformed `from` or `to` answers `400 invalid_date`. |
| `GET /api/v1/entries/{id}` | Single entry. `404` when missing or in the trash. |
| `POST /api/v1/entries` | Add an entry. JSON body `{"word": "...", "meaning": "...", "person": "..."}` (64 KiB cap) or `multipart/form-data` with `word`, `meaning`, optional `person` and an optional `photo` file. `person` is the slip's author and may be omitted. `201` with `Location`; `409` when the entry already exists; `422` on validation errors; `413` when an upload is too large; `429` when rate limited. |
| `GET /api/v1/entries/{id}/photo` | The entry's public photo. Defaults to the clean display image; add `?size=thumb` for the thumbnail. `404` when the entry has no photo or is in the trash. The original upload is available only to admins. |
| `GET /api/v1/stats` | `total_entries`, `unique_words`, `first_entry_at`, `last_entry_at`, `entries_last_30_days`, `with_photo`, `by_year` (`[{"year": 2026, "count": 12}]`) and `top_persons` (`[{"person": "Marieke", "count": 4}]`, the eight busiest names). |
| `GET /api/v1/persons` | Distinct attributed persons with counts: `{"items": [{"person": "M. van Dijke", "count": 12}], "total": 3}`, ordered case-insensitively. Unattributed entries are not listed. |
| `GET /healthz` | Liveness probe (`{"status":"ok"}`). |

Examples:

```sh
curl -s 'localhost:8080/api/v1/entries?q=boeke&per_page=5'
curl -s 'localhost:8080/api/v1/entries?photo=1&year=2026'
curl -s 'localhost:8080/api/v1/entries?person=Marieke'
curl -s localhost:8080/api/v1/persons
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
return `photo_url` when they have a photo. Duplicates are allowed, exactly as on
the original site.

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

## Atom feed

`GET /feed.xml` serves the newest live entries as Atom 1.0. Each entry uses
`{base}/e/{id}` as both its `id` and `link`, the misspelling as `title`, the
correction as `summary`, the logged date as `updated`, and the person as
`author` when one is recorded. The feed-level author names the ledger, and
trashed entries are never included.

- `limit` defaults to 50 and is capped at 100. A non-numeric or sub-1 value uses
  the default; a larger one is capped. The cap matches `store.MaxPerPage`.
- Responses carry a strong `ETag` derived from the newest entry id and the
  number of rows, plus `Cache-Control: public, max-age=60`. A matching
  `If-None-Match` answers `304` with no body.
- Every public page head carries
  `<link rel="alternate" type="application/atom+xml" title="The vanDyke Corrections Ledger" href="/feed.xml">`.
- Absolute URLs follow `PUBLIC_BASE_URL` and otherwise the request scheme and
  host, exactly like permalinks.

## Permalinks

Every entry has a stable public page at `/e/{id}`. The page shows the full
entry, its date and photo, and includes Open Graph/Twitter metadata for link
previews plus a copy-link button. Entry numbers in the ledger link to their
permalink. Set `PUBLIC_BASE_URL` behind a reverse proxy so canonical and social
URLs use the public hostname; otherwise they are derived from the request.

## Discoverability and accessibility

The binary serves its own crawler and browser metadata, all through the same
middleware as every other route:

- `GET /robots.txt` — `text/plain`; disallows `/admin` and names the sitemap.
- `GET /sitemap.xml` — `application/xml`; the home page, `/stats` and one
  `/e/{id}` permalink per live entry, all absolute URLs.
- `GET /feed.xml` — `application/atom+xml; charset=utf-8`; the newest live
  entries, newest first.
- `GET /site.webmanifest` — `application/manifest+json`; name, short name,
  `start_url` `/`, `standalone` display, `#f4eee1` theme/background colours and
  the existing favicon and Apple touch icon.

Sitemap and robots URLs follow `PUBLIC_BASE_URL` when it is set and otherwise
the request scheme and host, exactly like permalink canonicals. Trashed entries
are absent from the sitemap.

The public ledger works without JavaScript. `GET /entries` returns a complete
document when requested directly (sort, pager and "Clear search" links are plain
navigations), and the bare ledger fragment only when HTMX asks for it. Every
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
can, plus delete entries, purge the trash, manage admins and read the audit
trail. Editors can edit entries, restore trashed entries and change settings.

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
pair fails as a duplicate until the old row is purged.

### Webhooks

Set `LEDGER_WEBHOOK_URL`, or store `webhook.url` on `/admin/settings`, to have
the server post one JSON event after each successful entry creation from the web
form or `POST /api/v1/entries`:

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
  permalink.
- With `LEDGER_WEBHOOK_SECRET` or the `webhook.secret` setting configured, the
  exact body bytes are signed as `X-VanDyke-Signature: sha256=<lowercase hex>`
  using HMAC-SHA256. The header is omitted when no secret is effective.
- Dispatch is fire-and-forget: the create response is not delayed or changed,
  and a failure is only logged at warn level.
- **Delivery is best effort.** There are no retries, no delivery history and no
  at-least-once guarantee. A receiver that must be certain should reconcile
  against `GET /api/v1/entries` or the Atom feed, and should de-duplicate on
  `id` because the same event may arrive more than once.
- Runtime settings win over the environment. The secret is write-only: the admin
  page reports configured/unconfigured, a blank secret field keeps the stored
  value, and the clear control removes it.
- **Send test event** on `/admin/settings` posts the same payload shape for the
  newest entry (or zeroed fields on an empty ledger) and waits up to five
  seconds to report the receiver's HTTP status or a sanitised error.

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
internal/httpapi/      REST API under /api/v1
internal/web/          public HTML interface: templates, CSS, JS, fonts
trmnl/                 four TRMNL plugins, transforms and tests
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
Umami and webhook settings, webhook test-fire), the Atom feed (validity,
escaping, ordering, limit, ETag/304, trash exclusion, autodiscovery), webhook
payloads and HMAC signing, and the public web layer (partial rendering, HTMX
flows, no-JS flows, permalinks and Open Graph metadata, rate-limit errors, Umami
injection, cache headers).

The TRMNL transforms have their own stdlib-only Python tests:

```sh
make trmnl-test
```

## CI, releases and updates

- **CI** (`.github/workflows/ci.yaml`) — builds, vets, lints
  (`golangci-lint`) and tests every push and pull request, and uploads a
  coverage report.
- **TRMNL plugins** (`trmnl.yaml`) — tests the transforms, lints all four
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
