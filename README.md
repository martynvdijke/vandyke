# vanDyke

The LANCo misspelling ledger — a modern, self-hosted rebuild of
[vandyke.thor.edu](https://vandyke.thor.edu/).

One self-contained Go binary: SQLite storage, a server-rendered HTMX interface,
a JSON API, per-IP rate limiting and optional self-hosted Umami analytics.
No JavaScript build step, no external services required.

## Features

- **The Corrections Ledger** — an editorial paper-and-ink interface built with
  Go templates, HTMX and a small hand-written stylesheet. Light and dark theme,
  animated rows, keyboard- and screen-reader-friendly.
- **All original data, seeded** — the 100 entries from the original site
  (October 2017 → March 2026) are embedded in the binary and imported into
  SQLite on first start.
- **JSON REST API** — list, search, sort, paginate and add entries; stats and a
  health endpoint. See [API](#api).
- **Add entries from the web or the API** — protected by a per-IP token bucket
  (default: 10 additions per hour, burst of 5).
- **Optional self-hosted Umami** — set two environment variables and the
  tracker is injected, with page views and custom events (`search`,
  `entry-added`, `theme-toggle`). No analytics is loaded otherwise, and the CSP
  is adjusted automatically.

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
| `POST /api/v1/entries` | Add an entry. Body `{"word": "...", "meaning": "..."}`, 64 KiB cap. `201` with `Location`; `422` on validation errors; `429` when rate limited. |
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
```

Validation: `word` 1–200 characters, `meaning` 1–500 characters; both are
trimmed. Duplicates are allowed, exactly as on the original site.

## Umami analytics

vanDyke only injects the tracker script — you run Umami yourself:

1. Deploy Umami with the
   [official Docker setup](https://umami.is/docs/install) (Umami's own docs
   cover the database and reverse proxy).
2. Add a website in the Umami dashboard and copy its UUID.
3. Set `UMAMI_URL` (the full `…/script.js` URL) and `UMAMI_WEBSITE_ID`.
4. Optionally restrict tracking with `UMAMI_DOMAINS`.

The Content-Security-Policy automatically allows the tracker origin. Without
both variables no script is loaded and no request leaves the page. Events sent:
`search` (debounced query, ≥ 2 characters), `entry-added`, `theme-toggle`.

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
internal/ratelimit/    per-IP token bucket
internal/httpx/        shared HTTP helpers (client IP, JSON envelopes, logging)
internal/httpapi/      REST API under /api/v1
internal/web/          HTML interface: templates, CSS, JS, fonts
scripts/               one-off seed scraper and font fetcher
```

## Security notes

The ledger is intentionally open, like the original: anyone who can reach the
site can add an entry, and there is no authentication, editing or deletion.
Rate limiting is the only abuse control. Put it behind your intranet or a
reverse proxy with authentication if that is not what you want. Responses carry
`X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options` and a
CSP tailored to the enabled features.

## Testing

```sh
go test ./...
```

The suite covers the store (seeding, search with escaped wildcards, sorting,
pagination, validation, stats), the rate limiter, the JSON API, and the web
layer (partial rendering, HTMX flows, no-JS flows, rate-limit errors, Umami
injection, cache headers).
