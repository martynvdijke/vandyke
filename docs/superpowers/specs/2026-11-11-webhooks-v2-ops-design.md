# Webhooks v2 and operations

**Date:** 2026-11-11
**Status:** implemented
**Change:** `openspec/changes/add-webhooks-v2-and-ops`

## Webhook endpoints

Named endpoints are stored in `webhook_endpoints` (migration 013). Each row has
a unique case-insensitive name, an absolute `http`/`https` URL, an optional
signing secret, an enabled flag, a comma-separated event list, a `generic` /
`discord` / `slack` preset and an `allow_private` flag. At most 20 endpoints
exist; the event list is normalised to a sorted, de-duplicated subset of
`entry.created`, `entry.updated`, `entry.deleted`, `entry.restored` and
`entry.purged`.

The engine keeps one immutable snapshot of enabled endpoints, rebuilt at start
and after every mutation. Emission reads only that snapshot, stamps
`occurred_at`, and performs a non-blocking send to a 1,024-job queue. Four
workers render, sign, persist and POST; the same workers drain retries so a full
new-event queue cannot starve them. A 10 second ticker reloads the snapshot and
claims up to 100 due rows; an hourly ticker prunes history.

## Payloads and presets

The generic `entry.created` body is frozen at `event`, `id`, `word`, `meaning`,
`person`, `created_at` and `url`. The four newer events add `occurred_at`;
`entry.updated` adds a bounded `changes` object; `deleted_at` is present when the
snapshot is trashed. Discord receives `content`, one embed with required fields,
and an empty `allowed_mentions.parse`. Slack receives a `text` fallback and one
`mrkdwn` section with `&`, `<` and `>` escaped. Rendering happens per attempt,
and the HMAC signature covers the exact rendered bytes.

## Delivery state

When a worker first picks up a job it writes a `webhook_deliveries` row with
`status = pending` and `next_attempt_at = now`, then attempts the delivery.
`2xx` is `delivered`; transport errors, timeouts, `408`, `425`, `429` and `5xx`
are retried with full-jitter exponential backoff (5 second base, 1 hour cap)
until 8 attempts; other responses and render failures are terminal. Terminal
rows clear `payload`. Delivered and cancelled rows are pruned after 7 days,
failed rows after 30 days, pending rows never. Purging an entry deletes terminal
delivery rows for it in the same transaction.

The legacy `webhook.url` / `LEDGER_WEBHOOK_URL` target is synthesized in memory
as "Environment default" with the generic preset and only `entry.created`. It is
active only while no persisted endpoint is enabled, records deliveries with a
null endpoint id, and is treated as `allow_private` so an existing LAN receiver
keeps working. Adopting it copies the effective URL and secret into a real row.

## SSRF policy

Only absolute `http`/`https` URLs without userinfo are accepted. The following
ranges are denied by default: loopback, RFC 1918, IPv6 unique-local,
carrier-grade NAT 100.64.0.0/10, link-local, unspecified, multicast and
IPv4-mapped equivalents. `allow_private` (owner-only) permits loopback, private
and CGNAT ranges. Link-local, unspecified and multicast remain denied. The
policy is checked at save time and again by the dialer; the dialer connects to
the validated address while the Transport keeps the URL host for TLS. Redirects
are not followed, an attempt times out after 5 seconds, and at most 4 KiB of the
response is discarded.

## Admin page

`/admin/webhooks` lists endpoints and the delivery log, creates and edits
endpoints, toggles them, fires a per-endpoint test, adopts the legacy target,
retries a failed delivery and prunes history. Reads use `requireAuth`;
configuration and test/retry actions use `withCSRF`; delete and prune use
`withOwnerCSRF`. Secrets and URL paths/queries are masked, and audit rows record
only the host and the changed field names.

## Metrics and readiness

`internal/metrics` is a standard-library registry with atomic counters, a
mutex-guarded histogram and a text exposition 0.0.4 handler. `route` labels come
from `http.ServeMux` patterns (`GET /e/{id}`) or `unmatched`, never a raw path.
Database gauges are read at scrape time under a 2 second timeout; a failure sets
`vandyke_db_up` to 0 and drops the DB series while the scrape stays 200.
`METRICS_TOKEN` gates `/metrics` with a constant-time bearer check. `/readyz`
runs `SELECT 1` and compares `schema_migrations` with the embedded migration
list, returns a coarse 200/503 body within 2 seconds, and is never gated.
