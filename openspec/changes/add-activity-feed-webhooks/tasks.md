## 1. Atom document

- [x] 1.1 Add `internal/web/feed.go` with an `encoding/xml` structure for an Atom 1.0 document declaring `xmlns="http://www.w3.org/2005/Atom"`.
- [x] 1.2 Build the feed header with `id` set to `{base}/feed.xml`, a title, a feed-level `author` and `updated` set to the newest entry's `created_at`.
- [x] 1.3 Map each entry to `id` and `link` of `{base}/e/{id}`, `title` of the word, `summary` of the meaning and `updated` of `created_at` in RFC 3339.
- [x] 1.4 Emit an `author` element with the person's name only when `person` is non-empty.
- [x] 1.5 Escape all entry text through the XML encoder so a word or meaning containing `&`, `<` or quotes produces valid XML.

## 2. Feed route, limit and caching

- [x] 2.1 Register `GET /feed.xml` in the `internal/web` mux alongside the existing `/e/{id}` route.
- [x] 2.2 Read `limit`, clamp it to 1 through 100 with a default of 50, and pass it as `PerPage` with `Sort: "created_at"` and `Order: "desc"` to `store.ListEntries`.
- [x] 2.3 Set `Content-Type: application/atom+xml; charset=utf-8`, a strong `ETag` from the newest entry id and returned count, and `Cache-Control: public, max-age=60`.
- [x] 2.4 Compare `If-None-Match` with the computed tag and return 304 with no body on a match.
- [x] 2.5 Return 200 with an empty but valid Atom document when the ledger has no entries.
- [x] 2.6 Add the autodiscovery link for `/feed.xml` to the head in `internal/web/templates/base.html`.
- [x] 2.7 Add the same autodiscovery link to the head in `internal/web/templates/permalink.html`. (also added to the stats page and the not-found page, which have their own document shells.)

## 3. Webhook configuration and dispatch

- [x] 3.1 Add a webhook section to `internal/config/config.go` reading `LEDGER_WEBHOOK_URL` and `LEDGER_WEBHOOK_SECRET`.
- [x] 3.2 Add `webhook.url` and `webhook.secret` to `internal/settings/settings.go`, resolved with the existing override-then-environment precedence, and return an empty effective URL when neither source has a value.
- [x] 3.3 Add `internal/webhook/webhook.go` with a dispatcher holding the effective URL, secret, logger and an `*http.Client` with a 5 second timeout. (the dispatcher resolves the effective settings per send rather than snapshotting them at construction, so a runtime settings change applies immediately; it still holds the resolver, logger and client.)
- [x] 3.4 Implement payload construction with the fields `event`, `id`, `word`, `meaning`, `person`, `created_at` and `url`, and set `event` to `entry.created`.
- [x] 3.5 Send the `POST` with `Content-Type: application/json` and a `User-Agent` identifying the server, then log a non-2xx status or transport error at warn level with the entry id.
- [x] 3.6 Call the dispatcher from `internal/web/web.go` after a successful web-form create and from `internal/httpapi/api.go` after a successful API create, before the response is written.
- [x] 3.7 Start dispatch in a goroutine so a slow receiver does not delay the response and a failure never changes the create result.
- [x] 3.8 Skip dispatch entirely when the effective URL is empty.

## 4. HMAC signing

- [x] 4.1 Compute an HMAC-SHA256 digest over the exact JSON body bytes using the effective secret as the key.
- [x] 4.2 Set `X-VanDyke-Signature` to `sha256=<lowercase hex>` on the request when a secret is effective.
- [x] 4.3 Omit the header when no secret is effective and never log the secret or the signature input.
- [x] 4.4 Send the identical body bytes that were signed, without re-encoding or whitespace changes on the way out.

## 5. Admin settings and test-fire

- [x] 5.1 Extend `internal/admin/settings.go` to load and save the webhook URL and secret alongside the Umami values, keeping the existing validation style. (the settings page uses separate Umami and webhook forms with a `settings_form` marker, so saving one section does not blank the other; save errors re-render the submitted values.)
- [x] 5.2 Add the webhook fields to `internal/admin/templates/settings.html`: a URL input showing the effective value, and a secret input that is always blank and labelled as write-only.
- [x] 5.3 Add a clear-secret control that removes the stored `webhook.secret` without touching `webhook.url`.
- [x] 5.4 Register `POST /admin/settings/webhook/test` behind the existing admin session and CSRF middleware.
- [x] 5.5 Send the production payload shape on test-fire using the newest entry, or zeroed fields when the ledger is empty, wait up to 5 seconds, and render the HTTP status or error on the settings page.
- [x] 5.6 Report "no webhook configured" and make no request when the effective URL is empty.
- [x] 5.7 Confirm the settings page and the test-fire result render without a CSP change because no new script source is introduced.

## 6. Documentation

- [x] 6.1 Update `README.md` with the `/feed.xml` URL, the `limit` parameter, autodiscovery, `LEDGER_WEBHOOK_URL`, `LEDGER_WEBHOOK_SECRET`, the `webhook.url` and `webhook.secret` settings, and the signature header format. (also added both variables to `.env.example` and `docker-compose.yml` so container deployments can set them.)
- [x] 6.2 Add `docs/superpowers/specs/2026-11-10-activity-feed-webhooks-design.md` describing the feed mapping, the payload contract and the signing scheme.
- [x] 6.3 Link the new spec from `docs/index.md`.
- [x] 6.4 Document that delivery is best-effort with no retries and that receivers should de-duplicate on `id`.

## 7. Testing

- [x] 7.1 Add a unit test in `internal/web` asserting the feed is valid Atom, ordered newest first, and maps word, meaning, person, `created_at` and permalink as specified.
- [x] 7.2 Add tests for `limit` defaults, the 100 cap, a non-numeric value and a sub-1 value.
- [x] 7.3 Add tests for the ETag and for 304 on a matching `If-None-Match`.
- [x] 7.4 Add a test that the autodiscovery link appears on the ledger index and on a permalink page. (plus the stats page.)
- [x] 7.5 Add a test using `httptest.Server` that verifies the payload fields, the `Content-Type` and the `X-VanDyke-Signature` HMAC over the received body.
- [x] 7.6 Add a test that an unreachable or 500-returning receiver leaves entry creation successful and logs the failure.
- [x] 7.7 Add a test that no request is made when the effective URL is empty.
- [x] 7.8 Add an admin test for the test-fire action covering success, failure, the missing-URL case and a missing CSRF token.
- [x] 7.9 Run `go test ./...` and `go vet ./...` and record the results in the change notes. (`go build ./...`, `go vet ./...` and `go test -count=1 ./...` all pass.)
- [x] 7.10 Verify the feed in a real reader and with `curl -i` for status, `Content-Type`, `ETag` and a conditional 304. (verified with `curl -i`: 200, `application/atom+xml; charset=utf-8`, `ETag: "feed-78-50"`, matching `If-None-Match` → 304 with no body; the XML was parsed by an ElementTree reader because no GUI feed reader is available in this environment.)
