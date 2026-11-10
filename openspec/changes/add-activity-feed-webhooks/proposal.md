## Why

The ledger publishes HTML pages and a JSON API but no syndication: there is no `/feed`, `/rss` or `/atom` route in `cmd/` or `internal/`, so a reader cannot subscribe to new misspellings. The four TRMNL plugins poll `GET /api/v1/entries` and `GET /api/v1/stats` every 120 to 360 seconds because the server has no event-driven notification; the only outbound HTTP calls in the process are OIDC provider discovery and token requests, plus the Umami `<script>` tag rendered into HTML.

## What Changes

- Add an Atom 1.0 feed at `GET /feed.xml` with the newest entries, a `limit` parameter capped at 100, an ETag and `Cache-Control`.
- Add a feed autodiscovery `<link rel="alternate" type="application/atom+xml">` to every page head.
- Add an optional outbound webhook configured by `LEDGER_WEBHOOK_URL` and the `webhook.url` setting, posting a JSON payload when an entry is created.
- Dispatch the webhook without blocking the user's response, logging failures instead of surfacing them.
- Sign the payload with HMAC-SHA256 in an `X-VanDyke-Signature` header when `LEDGER_WEBHOOK_SECRET` or the `webhook.secret` setting is configured.
- Add a test-fire action to the admin settings page that sends the production payload and reports the result.

### Non-goals

- Email notifications.
- Per-event subscriptions or multiple webhook endpoints.
- Guaranteed at-least-once delivery, retry queues or delivery history.
- Inbound webhooks.

## Capabilities

### New Capabilities

- `ledger-feed`: Atom 1.0 syndication of the newest ledger entries with conditional requests and autodiscovery.
- `ledger-webhooks`: Optional outbound webhook on entry creation with HMAC signing and an admin test-fire action.

## Impact

- `internal/web/web.go`: `GET /feed.xml` route, feed handler and the create-entry dispatch call site.
- `internal/web/feed.go`: new Atom document builder.
- `internal/webhook/webhook.go`: new dispatcher for payload construction, signing and delivery.
- `internal/httpapi/api.go`: dispatch after a successful `POST /api/v1/entries`.
- `internal/config/config.go`: `LEDGER_WEBHOOK_URL` and `LEDGER_WEBHOOK_SECRET` environment variables.
- `internal/settings/settings.go`: `webhook.url` and `webhook.secret` runtime settings with the existing override-then-environment precedence.
- `internal/admin/settings.go` and `internal/admin/templates/settings.html`: webhook fields and the test-fire action.
- `internal/web/templates/base.html` and `internal/web/templates/permalink.html`: feed autodiscovery link.
- `README.md`, `docs/superpowers/specs/2026-11-10-activity-feed-webhooks-design.md` and `docs/index.md`.
