# Activity feed and webhooks

**Date:** 2026-11-10
**Status:** implemented
**Change:** `openspec/changes/add-activity-feed-webhooks`

## Atom feed (`GET /feed.xml`)

The feed is generated with `encoding/xml` from the existing
`store.ListEntries` call with `Sort: "created_at"`, `Order: "desc"` and
`PerPage` from `limit`. No new table or migration is involved, and trashed
entries are excluded by the store's `deleted_at IS NULL` filter.

Mapping per entry:

| Atom element | Source |
| --- | --- |
| `id` | `{base}/e/{id}` |
| `link/@href` | `{base}/e/{id}` |
| `title` | `word` |
| `summary` | `meaning` |
| `updated` | `created_at` in RFC 3339 UTC |
| `author/name` | `person`, only when non-empty |

The document declares `xmlns="http://www.w3.org/2005/Atom"`, has a feed `id` of
`{base}/feed.xml`, a feed-level author naming the ledger (so an entry without a
person stays valid), a self link, and `updated` set to the newest entry (or the
current time for an empty ledger). `{base}` comes from the same
`PUBLIC_BASE_URL`-then-request-host helper used for permalink canonicals, now
shared as `httpx.BaseURL`; there is no second URL builder. `encoding/xml`
escapes all user text.

`limit` defaults to 50, falls back to 50 for a non-numeric or sub-1 value, and
caps at `store.MaxPerPage` (100). The response carries
`Content-Type: application/atom+xml; charset=utf-8`, a strong ETag
`"feed-<newest id>-<count>"`, and `Cache-Control: public, max-age=60`. A
matching `If-None-Match` (including a weak prefix or `*`) returns 304 with no
body.

Autodiscovery:

```html
<link rel="alternate" type="application/atom+xml" title="The vanDyke Corrections Ledger" href="/feed.xml">
```

is present in the shared `page-head`, the stats page, the permalink page and the
not-found page.

## Webhook

Configuration resolves override first, then environment:

| Setting | Environment | Effect |
| --- | --- | --- |
| `webhook.url` | `LEDGER_WEBHOOK_URL` | Delivery target; empty disables dispatch. |
| `webhook.secret` | `LEDGER_WEBHOOK_SECRET` | HMAC key; empty sends unsigned. |

The `internal/webhook` package owns payload construction, signing and delivery.
`Dispatcher` reads the effective settings at send time, so an admin change
applies without a restart. Both create paths (`internal/web` and
`internal/httpapi`) call `Dispatch` after `store.CreateEntry` succeeds and
before writing the response; `Dispatch` starts one goroutine and returns.

Payload:

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

The POST sets `Content-Type: application/json` and a `User-Agent` identifying
the server. With a secret, the exact marshalled bytes are MAC'd with HMAC-SHA256
and sent as `X-VanDyke-Signature: sha256=<lowercase hex>`; the header is omitted
otherwise. The client has a 5 second timeout. Non-2xx responses and transport
errors are logged at warn level with the entry id and target host; the secret
and the signature input are never logged.

There is no queue, retry loop, ticker or worker pool: one bounded goroutine per
create attempt. Best-effort delivery means a receiver must de-duplicate on `id`.

## Admin settings and test-fire

`/admin/settings` has a Webhook card. It shows the effective URL, a write-only
secret field that is always blank, a clear-secret control, and the environment
fallbacks reported as set/not set. A blank secret keeps the stored value; the
clear control removes it without touching the URL. Saving the Umami or webhook
section does not blank the other, using a `settings_form` marker on each form.

`POST /admin/settings/webhook/test` sits behind `requireAuth` + `withCSRF`. It
sends the production payload for the newest entry (or zeroed fields on an empty
ledger), waits up to five seconds, and renders the receiver's HTTP status or a
URL-sanitised error on the page. With no effective URL it states that no webhook
is configured and makes no request. No new script source is introduced, so the
CSP is unchanged.

## Tests

- `internal/web/feed_test.go`: XML well-formedness and escaping, field mapping,
  author presence/absence, ordering, limit default/cap/invalid, ETag and 304,
  trash exclusion, autodiscovery on index/stats/permalink, empty ledger.
- `internal/web/web_test.go`: dispatch does not block creation, a 500 receiver
  leaves creation successful and logs a warning.
- `internal/webhook/webhook_test.go`: payload fields, content type, HMAC over
  the received body, unsigned delivery, disabled dispatch, synchronous
  status/transport errors.
- `internal/httpapi/api_test.go`: API create dispatches with the absolute
  permalink derived from `PUBLIC_BASE_URL`.
- `internal/admin/admin_test.go`: test-fire success and reported status,
  receiver 500, missing URL, missing CSRF, write-only secret, clear control.
- `internal/settings/settings_test.go`: webhook env fallback, override
  precedence, secret keep/clear, URL validation, no disturbance of Umami.
