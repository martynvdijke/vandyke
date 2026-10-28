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
- **All original data, seeded** — the 100 entries from the original site
  (October 2017 → March 2026) are embedded in the binary and imported into
  SQLite on first start.
- **JSON REST API** — list, search, sort, paginate and add entries; stats and a
  health endpoint. See [API](#api).
- **Add entries from the web or the API** — text entries or photos of a
  misspelling, protected by a per-IP token bucket (default: 10 additions per
  hour, burst of 5).
- **Admin ledger** — sign in to edit every field, replace or remove photos,
  correct logged dates, and delete entries individually or in bulk. Public
  additions stay open; only editing and deletion require an admin.
- **First-run setup and OIDC** — create the first local admin at `/admin/setup`;
  optionally add OpenID Connect for single sign-on. Login attempts are
  rate-limited and sessions are stored in SQLite with CSRF protection.
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
| `TRUST_PROXY` | `false` | Trust `X-Forwarded-For` for client IPs (enable only behind a proxy you control). |
| `ADD_RATE_LIMIT_PER_HOUR` | `10` | Additions allowed per IP per hour. |
| `ADD_RATE_LIMIT_BURST` | `5` | Short-term burst allowance per IP. |
| `UMAMI_URL` | *(empty)* | Full URL of the self-hosted tracker, e.g. `https://umami.example.com/script.js`. |
| `UMAMI_WEBSITE_ID` | *(empty)* | Umami website UUID. Analytics is enabled only when both are set. |
| `UMAMI_DOMAINS` | *(empty)* | Optional comma-separated hostnames passed to `data-domains`. |
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
| `GET /api/v1/entries` | Paginated list. Query: `q`, `sort` (`word`, `meaning`, `created_at`, `id`), `order` (`asc`/`desc`), `page` (1-based), `per_page` (1–100, default 30). |
| `GET /api/v1/entries/{id}` | Single entry. `404` when missing. |
| `POST /api/v1/entries` | Add an entry. JSON body `{"word": "...", "meaning": "..."}` (64 KiB cap) or `multipart/form-data` with `word`, `meaning` and an optional `photo` file. `201` with `Location`; `422` on validation errors; `413` when an upload is too large; `429` when rate limited. |
| `GET /api/v1/entries/{id}/photo` | The entry's photo (`image/jpeg`, `image/png`, `image/gif` or `image/webp`). `404` when the entry has no photo. |
| `GET /api/v1/stats` | `total_entries`, `unique_words`, `first_entry_at`, `last_entry_at`, `entries_last_30_days`. |
| `GET /healthz` | Liveness probe (`{"status":"ok"}`). |

Examples:

```sh
curl -s 'localhost:8080/api/v1/entries?q=boeke&per_page=5'
curl -s localhost:8080/api/v1/entries/1
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
or WebP and at most 10 MB; they are stored in SQLite and returned as `photo_url`
on the entry. Duplicates are allowed, exactly as on the original site.

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
   and photo can all be changed; deleting an entry also deletes its photo.

### OIDC

Set `OIDC_ISSUER`, `OIDC_CLIENT_ID`, and optionally `OIDC_CLIENT_SECRET` and
`OIDC_REDIRECT_URL`. The provider must expose standard discovery metadata
(`/.well-known/openid-configuration`) and support the authorization-code flow
with PKCE. The default callback is `<request scheme and host>/admin/oidc/callback`;
register that URL with your provider unless you set `OIDC_REDIRECT_URL`.

Any identity the provider successfully authenticates is auto-provisioned as an
admin on first login. **The provider's own access policy is therefore the only
boundary** — configure it accordingly. The first local admin still has to be
created at `/admin/setup` before OIDC sign-in is available.

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
**100 entries**, duplicates preserved, timestamps as recorded on the original
site (2017-10-02 → 2026-03-13). It was extracted with
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

The suite covers the store (seeding, search with escaped wildcards, sorting,
pagination, validation, stats, admin/session lifecycle, entry edits and bulk
deletes, settings), password hashing and OIDC discovery/token verification, the
rate limiter, the JSON API, the admin flows (setup, login, CSRF, edit, delete,
Umami settings), and the public web layer (partial rendering, HTMX flows, no-JS
flows, rate-limit errors, Umami injection, cache headers).

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
