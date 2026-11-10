## Context

The server is a single Go binary with `net/http`, embedded SQLite and server-rendered templates. It has no scheduler, no background worker and no notification system; the only long-lived goroutine is the HTTP listener. Entries are created in two places, the web form handler in `internal/web/web.go` and the JSON API handler in `internal/httpapi/api.go`, both through `store.CreateEntry`. Runtime settings live in the `settings` table keyed by dotted names such as `umami.url`, are edited at `/admin/settings`, and resolve as persisted override first, then environment variable. The CSP header is recomputed per request from the current settings.

## Goals / Non-Goals

**Goals:**
- Publish the newest entries as valid Atom 1.0 at a stable URL.
- Let a reader or RSS client poll conditionally with an ETag.
- Notify one external endpoint on entry creation without delaying the response.
- Sign webhook payloads so a receiver can verify them.
- Give an operator a way to verify the configuration from the admin page.

**Non-Goals:**
- Email notifications, per-event subscriptions, multiple endpoints.
- Retry queues, delivery history or at-least-once guarantees.
- Inbound webhooks.
- A scheduler or background worker.

## Decisions

1. **Atom 1.0 at `/feed.xml`**: The feed is generated with `encoding/xml` from the existing `store.ListEntries` result with `Sort: "created_at"`, `Order: "desc"` and `PerPage` set from `limit`, so no new query or table is needed. The handler resolves `{base}` as the absolute server origin the same way as the existing permalink canonical URL, and the document declares `xmlns="http://www.w3.org/2005/Atom"`, a feed `id` of `{base}/feed.xml`, a feed `updated` of the newest entry timestamp, and one `entry` per row with `id` and `link` set to `{base}/e/{id}`, `title` set to the word, `summary` set to the meaning and `updated` set to `created_at`. An `author` element is emitted only when `person` is non-empty, and the feed itself carries an `author` naming the ledger so the document stays valid when an entry has none.

2. **Bounded `limit`**: `limit` is an integer query parameter with a default of 50 and a maximum of 100. A non-numeric or sub-1 value falls back to 50; a value above 100 is capped at 100. This keeps the feed cheap on a ledger with thousands of rows and matches the existing `per_page` ceiling used by the API.

3. **Strong ETag and a short cache**: The ETag is derived from the newest entry id and the number of rows returned, so an unchanged head of the ledger produces the same tag. `Cache-Control` is `public, max-age=60`; a matching `If-None-Match` returns 304 with no body. A create therefore becomes visible within a minute without a revalidation on every request.

4. **New `internal/webhook` package**: Payload construction, HMAC signing and delivery live in one small package so both create paths share the same code. The payload is a JSON object with `event` set to `entry.created` and the fields `id`, `word`, `meaning`, `person`, `created_at` and `url`. The dispatcher holds the resolved URL and secret, a `*http.Client` with a 5 second timeout and the logger, and `Dispatch` returns immediately after starting a goroutine.

5. **Dispatch after the create succeeds**: Both `internal/web/web.go` and `internal/httpapi/api.go` call the dispatcher after `store.CreateEntry` returns without error and before the response is written. The goroutine uses `context.Background` with a 5 second timeout, so a slow receiver cannot delay the user's response; non-2xx responses and transport errors are logged at warn level and are never returned to the caller.

6. **Settings mirror Umami**: `webhook.url` and `webhook.secret` are stored in the same `settings` table and resolved with the existing override-then-environment precedence, falling back to `LEDGER_WEBHOOK_URL` and `LEDGER_WEBHOOK_SECRET`. An empty effective URL disables dispatch entirely; an empty secret disables only signing.

7. **Write-only secret in the admin form**: The settings page renders the URL but never the stored secret. A blank secret field leaves the stored value unchanged, and a separate clear action removes it, so an operator can rotate or revoke signing without the secret being echoed into HTML.

8. **Test-fire is synchronous**: The admin action at `POST /admin/settings/webhook/test` is a deliberate, CSRF-protected request that sends the production payload for the newest entry, or zeroed fields when the ledger is empty, and waits up to 5 seconds for a result, then renders the HTTP status or error on the settings page. This is separate from production dispatch, which never waits.

## Risks / Trade-offs

- **[No delivery guarantee]** -> Failures are logged with the entry id and target host; a receiver that needs certainty must reconcile against `GET /api/v1/entries` or the feed. Retries are explicitly out of scope.
- **[Goroutine per create]** -> Each create starts at most one short-lived goroutine bounded by the 5 second client timeout; there is no queue and no unbounded accumulation.
- **[Admin-configured URL can point anywhere]** -> The URL is editable only by an authenticated admin, and the settings page and logs show the host, not credentials embedded in the URL.
- **[Secret in request logs or errors]** -> The secret is never logged and never rendered back into HTML; the signature header is the only place it influences output.
- **[Feed staleness]** -> `max-age=60` means a new entry can take a minute to appear in a cached feed; the ETag still makes a refresh cheap and TRMNL polling remains unaffected.
- **[Receiver duplication]** -> Delivery is best effort with no retries, so a receiver that processes the same event twice must de-duplicate on `id`, which the documentation states.
