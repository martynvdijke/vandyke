## 1. Schema

- [ ] 1.1 Add `internal/store/migrations/013_webhook_endpoints.sql` with `webhook_endpoints` and `webhook_deliveries`, the unique endpoint-name index and the three delivery indexes from the design.
- [ ] 1.2 Keep the migration at `013` so the parallel change can own `014`; add a migration test that applies every embedded migration to an empty database and asserts both tables and all indexes exist.
- [ ] 1.3 Add store tests that deleting an endpoint cascades its deliveries, soft-deleting an entry leaves delivery rows untouched, and purging an entry deletes only its terminal delivery rows.

## 2. Endpoint and delivery storage

- [ ] 2.1 Add `WebhookEndpoint` and `WebhookDelivery` structs with the fields in the schema, including an `EndpointID` that is nil for the legacy target.
- [ ] 2.2 Add `ListWebhookEndpoints`, `GetWebhookEndpoint`, `CreateWebhookEndpoint`, `UpdateWebhookEndpoint`, `DeleteWebhookEndpoint` and `SetWebhookEndpointEnabled` to `internal/store/store.go`.
- [ ] 2.3 Validate endpoint name length and uniqueness, URL length, secret length, preset against `generic`, `discord`, `slack`, and `events` as a non-empty sorted subset of the five event names; reject a 21st endpoint.
- [ ] 2.4 Add `CreateWebhookDelivery`, `UpdateWebhookDelivery`, `ListWebhookDeliveries` with endpoint and status filters and paging, `DueWebhookDeliveries`, `CanWebhookDeliveryRetry` and `PruneWebhookDeliveries`.
- [ ] 2.5 Add `RetryWebhookDeliveryNow`, which sets `status = 'pending'` and `next_attempt_at` to now without resetting `attempts`.
- [ ] 2.6 Add `CancelPendingWebhookDeliveries` for a disabled endpoint; deleting an endpoint cascades its rows instead.
- [ ] 2.7 Add store tests for CRUD, the caps, event validation, due selection, paging, prune cut-offs and retry-now semantics.
- [ ] 2.8 Clear `webhook_deliveries.payload` when a delivery reaches `delivered` or `cancelled`, and delete terminal delivery rows for purged entry ids inside the purge flow; add tests for both.

## 3. Lifecycle events

- [ ] 3.1 Change `SoftDeleteEntries` to select the live rows inside the transaction before the update and return those rows, whose length is the changed count, instead of only a count.
- [ ] 3.2 Change `RestoreEntries` to select the trashed rows before the update and return those rows, whose length is the changed count, instead of only a count.
- [ ] 3.3 Change `PurgeEntries` to select the trashed rows before the delete, return them, and delete terminal delivery rows for those entry ids in the same transaction.
- [ ] 3.4 Change `UpdateEntry` to return the previous snapshot and the updated entry from inside the transaction, so the diff cannot be built from a stale read and the previous photo hash is available.
- [ ] 3.5 Add a `webhook.Change` type and an `Emit`/`EmitBatch` helper that reads the engine's in-memory endpoint snapshot and fans the changed entries out with a non-blocking channel send, using `PUBLIC_BASE_URL` when set and the request-derived base otherwise.
- [ ] 3.6 Emit `entry.created` from `internal/web/web.go` and `internal/httpapi/api.go` after a successful create, replacing the current `Dispatcher.Dispatch` calls.
- [ ] 3.7 Emit `entry.updated` from `internal/admin/entries.go` only when the diff is non-empty, include the changed fields with their previous values, use the 12-character photo SHA-256 for a photo change and add `deleted_at` when the edited row is trashed.
- [ ] 3.8 Emit `entry.deleted`, `entry.restored` and `entry.purged` from the admin handlers using the snapshots returned by the store.
- [ ] 3.9 Leave `ApplyImport` without webhook emission and document the exception in the admin import result or its help text.
- [ ] 3.10 Add tests that a bulk delete of 100 ids emits 100 `entry.deleted` events, a no-op edit emits none, an already-trashed id emits none, an edit of a trashed entry carries `deleted_at`, and an import emits none.

## 4. Payloads and presets

- [ ] 4.1 Keep the `generic` `entry.created` object at exactly `event`, `id`, `word`, `meaning`, `person`, `created_at` and `url`; add `occurred_at` only to the four new event types and `changes` only to `entry.updated`.
- [ ] 4.2 Implement the Discord preset with `content`, one embed carrying `title`, `description`, `url` and `timestamp`, and `allowed_mentions.parse` set to an empty list.
- [ ] 4.3 Implement the Slack preset with a `text` fallback and one `section` block of type `mrkdwn`, escaping `&`, `<` and `>`.
- [ ] 4.4 Render the preset per endpoint per attempt, compute `X-VanDyke-Signature` over the exact rendered bytes, and keep `Content-Type: application/json` and the `User-Agent` for all presets.
- [ ] 4.5 Treat a render failure as a terminal delivery failure with a sanitised last error.
- [ ] 4.6 Add golden-shape tests for all three presets, a signature test over a Discord body, an `@everyone` mention test, a photo-only entry fallback test and a Slack `<!channel>` escaping test.

## 5. Delivery engine

- [ ] 5.1 Add `internal/webhook/engine.go` with an atomic endpoint snapshot loaded at `Start`, a 1,024-entry buffered queue for new events, four workers shared with due retries, a 10 second retry ticker that also reloads the snapshot, and an hourly prune ticker.
- [ ] 5.2 Make enqueue non-blocking: drop on a full queue, increment a dropped counter and log the event and entry id without blocking the request.
- [ ] 5.3 Persist one delivery row per endpoint and event with `status = 'pending'` and `next_attempt_at` set to now when a worker picks up the job; increment `attempts`, record `last_status` and a sanitised `last_error`, set `next_attempt_at` from the jittered exponential backoff, and clear it on a terminal status.
- [ ] 5.4 Classify 2xx as delivered, transport errors, timeouts, 408, 425, 429 and 5xx as retryable, all other responses as permanent, and stop after 8 attempts.
- [ ] 5.5 Claim at most 100 due deliveries per retry pass, mark pending rows for disabled endpoints as cancelled, discard an in-memory job whose endpoint row was deleted or disabled before pickup, and reload the endpoint snapshot on the same tick.
- [ ] 5.6 Implement `Start` and `Close`: start before the listener, stop accepting on close, drain in-flight work for up to 5 seconds, and ensure `main` calls `Close` before the deferred store close runs.
- [ ] 5.7 Add tests with an `httptest.Server` for a 500-then-200 retry, an exhausted 8-attempt delivery, a permanent 404, a slow receiver that does not block the caller, a full queue incrementing the drop counter, and a disabled endpoint cancelling pending work.

## 6. Target validation

- [ ] 6.1 Reject any scheme other than `http` and `https`, any URL with userinfo, and any URL without a host.
- [ ] 6.2 Deny loopback, RFC 1918, IPv6 unique-local, carrier-grade NAT 100.64.0.0/10, link-local, unspecified, multicast and IPv4-mapped addresses by default, including when a hostname resolves to one.
- [ ] 6.3 Add the per-endpoint `allow_private` flag, settable only by an owner; it permits loopback, private and carrier-grade NAT ranges but never link-local, unspecified or multicast.
- [ ] 6.4 Re-check the resolved addresses at dial time, dial the validated address while preserving the URL host for TLS, and refuse the delivery when resolution changes to a denied range.
- [ ] 6.5 Disable redirect following, keep the 5 second attempt timeout, cap the response body at 4 KiB and sanitise every stored or logged target error to the host.
- [ ] 6.6 Add tests for a `file://` URL, a URL with userinfo, `127.0.0.1`, `10.0.0.5`, `169.254.169.254`, a hostname resolving to a private address, a redirect to a denied target and the owner-only `allow_private` change.

## 7. Admin webhooks page

- [ ] 7.1 Add `GET /admin/webhooks` behind `requireAuth`, listing endpoints with name, host, masked path and query, enabled state, events, preset, secret-configured state and the delivery counts.
- [ ] 7.2 Add `POST /admin/webhooks` and `POST /admin/webhooks/{id}` behind `withCSRF` for create and edit, with a blank secret meaning unchanged and a clear-secret control.
- [ ] 7.3 Add `POST /admin/webhooks/{id}/delete` and `POST /admin/webhooks/prune` behind `withOwnerCSRF`, and `POST /admin/webhooks/{id}/toggle` behind `withCSRF`.
- [ ] 7.4 Add `POST /admin/webhooks/{id}/test` behind `withCSRF`, sending one `entry.created` for the newest entry with that endpoint's preset and signature, waiting up to 5 seconds and rendering the status or sanitised error.
- [ ] 7.5 Add `POST /admin/webhooks/adopt` behind `withCSRF` to copy the effective legacy URL and secret into an enabled persisted endpoint.
- [ ] 7.6 Add `POST /admin/webhooks/deliveries/{id}/retry` behind `withCSRF` and a `GET /admin/webhooks` delivery section with endpoint and status filters, paging, status, attempts, last status, last error and next attempt.
- [ ] 7.7 Render the legacy target as a read-only row whenever an effective legacy URL exists, marking it active or suppressed, and explain that a persisted enabled endpoint suppresses it.
- [ ] 7.8 Add `internal/admin/templates/webhooks.html`, a navigation link, calls to `engine.Reload` after every successful endpoint mutation, handler tests for every route, role and CSRF failure, audit tests asserting no URL query, path token or secret appears in an audit row, and confirm no new script source or CSP change is needed.
- [ ] 7.9 Add engine tests that a created endpoint suppresses the legacy target on the next event, a disabled endpoint stops receiving, an edited subscription takes effect, and a failed reload keeps the previous snapshot until the retry tick.

## 8. Observability

- [ ] 8.1 Add `internal/metrics` with atomic counters, a mutex-guarded histogram and a handler that writes text exposition 0.0.4 with `# HELP`, `# TYPE`, no timestamps and deterministic ordering.
- [ ] 8.2 Add the middleware that records `method`, `r.Pattern` or `unmatched`, the exact status code, and a duration histogram with the buckets from the design; never use the raw path or an entry id.
- [ ] 8.3 Add `vandyke_entries_total`, `vandyke_entries_live`, `vandyke_entries_trashed`, `vandyke_entries_with_photo` and `vandyke_db_up` from scrape-time queries with a 2 second timeout.
- [ ] 8.4 Add `vandyke_entry_additions_total{outcome}` incremented explicitly in the web and API create branches for created, duplicate, invalid, rate_limited and error, plus `vandyke_rate_limited_requests_total` in both limiter branches.
- [ ] 8.5 Add `vandyke_webhook_deliveries_total{result}`, `vandyke_webhook_attempts_total{result}`, `vandyke_webhook_queue_depth`, `vandyke_webhook_pending_deliveries` and `vandyke_webhook_dropped_total` from the engine and store.
- [ ] 8.6 Add `vandyke_db_size_bytes` from `os.Stat(cfg.DBPath)`, `vandyke_process_start_time_seconds`, `vandyke_process_uptime_seconds` and `vandyke_build_info{version,go_version,goos,goarch}`.
- [ ] 8.7 Register `GET /metrics`, set `Content-Type: text/plain; version=0.0.4; charset=utf-8` and `Cache-Control: no-store`, and add `METRICS_TOKEN` with a constant-time bearer check in `internal/config/config.go`.
- [ ] 8.8 Add `GET /readyz` in `internal/httpapi` using `store.CheckReady`, returning the small 200 or 503 body from the design with a 2 second timeout and no DSN, path or error text.
- [ ] 8.9 Add tests for exposition syntax and metric names, label cardinality for a request to `/e/123` (one `GET /e/{id}` series, no id), the 401 and 200 token paths, DB gauge failure setting `db_up` to 0, readiness 200, a closed database returning 503 and a missing migration returning 503.
- [ ] 8.10 Confirm `go.mod` and `go.sum` are unchanged and `go list -m all` adds no module.

## 9. Wiring and documentation

- [ ] 9.1 Construct one engine in `cmd/vandyke/main.go`, pass it the settings resolver and store, pass the same instance to the web, API and admin constructors in place of their three `webhook.New` calls, start it before the listener and close it before the store on shutdown.
- [ ] 9.2 Pass the version, database path and store to the metrics handler and wrap the mux with the metrics middleware without changing the existing security, logging and recovery order in a way that loses the pattern.
- [ ] 9.3 Document the endpoint fields, the five events, the three presets, the signature rule, retry and drop semantics, delivery payload retention and pruning, SSRF defaults, `METRICS_TOKEN`, `/metrics` and `/readyz` in `README.md`.
- [ ] 9.4 Add every new environment variable to `.env.example` and `docker-compose.yml`, and add a `docs/superpowers/specs/` entry linked from `docs/index.md`.
- [ ] 9.5 State in the README that import does not emit webhooks and that receivers should reconcile after an import.

## 10. Testing

- [ ] 10.1 Run `go build ./...`, `go vet ./...` and `go test ./...`.
- [ ] 10.2 With a running instance, create an endpoint pointing at a local receiver, confirm a create, edit, delete, restore and purge each produce the expected event and signature.
- [ ] 10.3 Set `LEDGER_WEBHOOK_URL` only, confirm the legacy target still receives `entry.created` with the unchanged body, adopt it, and confirm it is suppressed after adoption.
- [ ] 10.4 Stop the receiver, confirm retries appear in the delivery log with increasing attempts, then restore the receiver and confirm a retry succeeds.
- [ ] 10.5 Scrape `/metrics` with `curl -i`, check the content type and parse it with a text-format parser, and confirm `/readyz` returns 200 while a stopped database returns 503.
