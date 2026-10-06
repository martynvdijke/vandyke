# vanDyke Clone — Design Spec

Date: 2026-10-06
Status: approved
Source site: https://vandyke.thor.edu/ (static Materialize CSS + jQuery tablesorter page, backed by a tiny internal DB)

## Context

The original site is an internal joke of the **LANCo** committee (TesLAN): a ledger of
spelling errors, misheard words and creative typos, mostly by M. van Dijke. Rows have
three fields — **Word** (the mistake), **Meaning** (the intended word) and **Date** —
plus an open "add word" form. Content is mostly Dutch; UI chrome is English.

This project rebuilds it as a modern, self-hostable single-binary application.

## Requirements (from user)

1. Modern-looking clone of https://vandyke.thor.edu/
2. Go backend with SQLite storage
3. Seed the database with **all entries that currently exist** on the site
4. Additional API endpoints
5. Umami self-hosted analytics support (script injection only)

Decisions confirmed with the user:

- **Frontend**: Go `html/template` + HTMX (server-driven interactivity, minimal JS)
- **Add form**: stays open like the original; per-IP rate limiting on writes
- **Umami**: env-configurable tracker script injection only (no bundled Umami stack)

## Architecture

Single Go binary. No JS build step, no CDN dependencies — everything embedded.

```
cmd/vandyke/main.go            entrypoint: config, store, routes, graceful shutdown
internal/config/               env-based configuration
internal/store/                SQLite store (modernc.org/sqlite, CGO-free), migrations, seed
internal/store/migrations/     embedded SQL migrations
internal/store/seed_entries.json  embedded snapshot of the original dataset
internal/ratelimit/            per-IP token bucket (golang.org/x/time/rate)
internal/httpapi/              JSON REST API under /api/v1/
internal/web/                  server-rendered UI, templates, static assets
scripts/scrape_seed.py         one-off scraper that produced seed_entries.json
```

Storage: SQLite in WAL mode, single writer-safe, `busy_timeout=5000`.

## Data model

```sql
CREATE TABLE entries (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  word       TEXT NOT NULL,
  meaning    TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_entries_word        ON entries(word COLLATE NOCASE);
CREATE INDEX idx_entries_created_at  ON entries(created_at);
CREATE INDEX idx_entries_meaning     ON entries(meaning COLLATE NOCASE);
```

- `created_at` stored as ISO-8601 UTC text; original timestamps preserved (`2017-10-02 00:00:00.000000` → `2017-10-02T00:00:00Z`).
- Duplicate historical rows are preserved verbatim (they are real data points in the joke ledger).
- Schema migrations run at startup, tracked in `schema_migrations`.
- Seeding: embedded JSON imported inside one transaction **only when the table is empty** (offline-safe first run).

## REST API

Base `/api/v1`. JSON. Casing snake_case.

| Method | Path              | Description |
|--------|-------------------|-------------|
| GET    | `/entries`        | List; params `q`, `sort` (`word`,`meaning`,`created_at`,`id`), `order` (`asc`,`desc`), `page` (1-based), `per_page` (1..100, default 30) |
| GET    | `/entries/{id}`   | Single entry, 404 when missing |
| POST   | `/entries`        | Create `{"word":...,"meaning":...}` → 201 + `Location` |
| GET    | `/stats`          | totals, unique words, first/last entry timestamp, entries in last 30 days |
| GET    | `/healthz`        | Liveness (outside `/api/v1`) |

Validation: `word` 1–200 chars, `meaning` 1–500 chars after trimming; JSON error envelope
`{"error":{"code":...,"message":...}}`; body capped at 64 KB.

Rate limiting: per-IP token bucket on all writes (API POST and HTML form POST).
Defaults 10/h refill, burst 5; env-tunable. `Retry-After` + 429 on limit.
Client IP from `RemoteAddr` unless `TRUST_PROXY=true`, then first `X-Forwarded-For` hop.

## Web UI — "The Corrections Ledger"

Editorial dictionary aesthetic: warm paper, ink text, correction-red accent; the
misspelling is struck through, then an arrow, then the correction in a display serif.

- Self-hosted fonts: **Fraunces** (display serif) + **Instrument Sans** (UI), Latin WOFF2.
- Light + dark theme via `prefers-color-scheme` and a manual toggle persisted in `localStorage`.
- Masthead tells the LANCo/M. van Dijke story; stats strip (entries, timespan, last added).
- HTMX interactions:
  - live search (300 ms debounce) and `per_page` filter → swaps `#ledger`
  - sortable column headers, pagination — all server-rendered, URL state preserved
  - add-word `<dialog>` → POSTs form, swaps ledger, `HX-Trigger: entryAdded` shows a toast
  - validation errors retarget a `#form-error` slot via `HX-Retarget` (200 response so htmx swaps)
- Footer keeps the original TesLAN / LANCo links.
- Graceful no-JS fallback: form posts normally and redirects; search/sort are plain links.

## Umami integration

Env vars: `UMAMI_URL`, `UMAMI_WEBSITE_ID`, optional `UMAMI_DOMAINS`. When both URL and
website ID are set, the base template injects `<script defer src="..." data-website-id="..."
data-domains="...">`. Client events (`vandyke:search`, `vandyke:entry-added`) are sent via
`window.umami?.track()` — fully guarded, zero effect when analytics is not configured.

## Config

| Env | Default | Purpose |
|-----|---------|---------|
| `PORT` | `8080` | listen port |
| `DB_PATH` | `./data/vandyke.db` | SQLite file (parent dirs created) |
| `TRUST_PROXY` | `false` | trust `X-Forwarded-For` for client IP |
| `ADD_RATE_LIMIT_PER_HOUR` | `10` | per-IP refill rate for writes |
| `ADD_RATE_LIMIT_BURST` | `5` | per-IP burst for writes |
| `UMAMI_URL` | — | tracker script URL |
| `UMAMI_WEBSITE_ID` | — | Umami website ID |
| `UMAMI_DOMAINS` | — | optional `data-domains` |

## Deployment

- Multi-stage Dockerfile: static CGO-free binary on Alpine, non-root user, `/data` volume.
- `docker-compose.yml` runs the app alone (Umami is the operator's own instance).
- `.env.example`, `Makefile`, README with API examples and Umami setup.

## Testing

`go test ./...`:
- store: migrations, seed import (>= 90 rows, exact count from JSON), list/search/sort/pagination, create, stats
- ratelimit: burst then denial
- httpapi: list/get/create/validation/404/429, JSON envelope
- web: index renders, HTMX partial, form POST adds entry, analytics script presence toggling

## Non-goals (YAGNI)

- No auth/user accounts (matches original open form; rate limit only)
- No edit/delete endpoints
- No FTS5 (dataset is tiny; `LIKE` with escaped wildcards suffices)
- No bundled Umami/Postgres stack
