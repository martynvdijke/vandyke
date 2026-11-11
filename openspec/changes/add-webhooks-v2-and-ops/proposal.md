## Why

`internal/webhook/webhook.go` implements exactly one target. `Dispatcher` resolves a single `config.Webhook` from the `webhook.url` and `webhook.secret` settings, which fall back to `LEDGER_WEBHOOK_URL` and `LEDGER_WEBHOOK_SECRET` (`internal/settings/settings.go:169`), emits only `EventEntryCreated` (`internal/webhook/webhook.go:26`), and starts one goroutine per create (`internal/webhook/webhook.go:91`). A failed POST is logged and forgotten (`internal/webhook/webhook.go:109`); there is no queue, no retry, no delivery record and no way to subscribe to edits, deletions, restores or purges even though the ledger now has that full lifecycle in `SoftDeleteEntries`, `RestoreEntries` and `PurgeEntries` (`internal/store/store.go:1920`, `:1958`, `:1997`).

Operationally the process exposes `GET /healthz` only (`internal/httpapi/api.go:51`), which answers `{"status":"ok"}` without touching SQLite. There is no `/metrics` and no readiness probe, so an operator cannot see entry counts, rate-limit denials, webhook backlog or database size, and a container platform cannot distinguish "process started" from "database usable". `cmd/vandyke/main.go` has no background loop: the only long-lived goroutine is the HTTP listener (`cmd/vandyke/main.go:95`), so a retry loop and a prune loop would be the first scheduler in the binary.

## What Changes

- Add `webhook_endpoints` and `webhook_deliveries` in `013_webhook_endpoints.sql`, making named endpoints, per-endpoint event subscriptions, a formatting preset and a persisted delivery log the primary configuration while the single URL/secret pair remains a legacy fallback.
- Emit `entry.created`, `entry.updated`, `entry.deleted`, `entry.restored` and `entry.purged`, one event per affected entry, including bulk actions capped at 100 ids.
- Keep the `generic` `entry.created` body byte-compatible with the current payload; add `discord` and `slack` presets; sign the rendered body so `X-VanDyke-Signature` keeps verifying.
- Add a bounded in-process delivery engine with a queue cap, a worker cap, exponential backoff with jitter, a maximum attempt count and persisted `next_attempt_at`; start it in `main` and stop it before the store closes.
- Add `/admin/webhooks` with endpoint create/edit/delete, enable/disable, a per-endpoint test fire, a delivery log, "retry now" and pruning; audit every change.
- Validate endpoint targets: http and https only, no userinfo, private and loopback targets denied unless an owner opts the endpoint in, link-local denied always, redirects disabled, 5 second timeout and a 4 KiB response cap.
- Add `GET /metrics` in Prometheus text exposition format 0.0.4 using only the standard library, covering entries, addition outcomes, rate limiting, webhook delivery and queue depth, HTTP routes, database size, process uptime and build info.
- Add `GET /readyz` that checks SQLite and applied migrations, separate from the unchanged `/healthz`.
- Keep `LEDGER_WEBHOOK_URL` and `LEDGER_WEBHOOK_SECRET` working as a synthesized legacy endpoint with an adopt action; it stays active only while no persisted endpoint is enabled.

### Non-goals

- Inbound webhooks, email notifications or two-way chat integration.
- Exactly-once delivery; retries are at-least-once.
- Per-entry or per-field endpoint filters; subscription is by event type only.
- Webhook events for bulk imports; an import is a reconciliation action, not an interactive change.
- OpenTelemetry, tracing, external metrics services, pprof endpoints, per-request log shipping or changing `/healthz`.
- Encrypting endpoint secrets at rest; SQLite file permissions remain the boundary.
- A general job scheduler for work other than webhook retry and pruning.

## Capabilities

### New Capabilities

- `ledger-webhooks-multi`: multiple endpoints, five entry event types, generic, Discord and Slack presets, a bounded retry engine, a delivery log and SSRF controls.
- `ledger-observability`: Prometheus metrics and a database-backed readiness probe.

### Modified Capabilities

- `ledger-webhooks`: the single `webhook.url`/`webhook.secret` target becomes a legacy endpoint that is used only while no enabled persisted endpoint exists, and its existing test-fire action is retained for that legacy target.

## Impact

- New migration `internal/store/migrations/013_webhook_endpoints.sql`.
- `internal/store/store.go`: endpoint and delivery storage, due-delivery queries, pruning, `CheckReady`, and lifecycle methods that return the rows they actually changed.
- `internal/webhook/webhook.go` plus new `engine.go`, `preset.go` and `target.go`: event envelope, presets, target validation and the delivery engine; the current `Dispatcher` is replaced.
- `internal/web/web.go` and `internal/httpapi/api.go`: emit creation events through the engine; the API registers `GET /readyz`.
- `internal/admin/entries.go`: emit updated, deleted, restored and purged events from snapshots returned by the store.
- `internal/admin/admin.go` and `internal/admin/templates/settings.html`: accept the shared engine for the legacy test fire and label the legacy webhook section.
- New `internal/admin/webhooks.go` and `internal/admin/templates/webhooks.html`, plus a navigation link in `internal/admin/templates/base.html`.
- `cmd/vandyke/main.go`: construct and start the single engine, pass it to the web, API and admin constructors, wire the metrics registry, stop both on shutdown.
- `internal/config/config.go`: `METRICS_TOKEN`; new `internal/metrics/metrics.go`, `internal/metrics/middleware.go` and `internal/metrics/exposition.go` for the registry, HTTP middleware and exposition.
- `README.md`, `.env.example`, `docker-compose.yml` and `docs/superpowers/specs/`.
