## ADDED Requirements

### Requirement: Prometheus metrics endpoint
The system SHALL expose `GET /metrics` in Prometheus text exposition format version 0.0.4, implemented with the Go standard library only and without adding a module to `go.mod`, with `Content-Type: text/plain; version=0.0.4; charset=utf-8`, `# HELP` and `# TYPE` lines for every metric, no timestamps and a deterministic series order.

#### Scenario: Scrape the endpoint
- **WHEN** a client requests `GET /metrics`
- **THEN** the response is 200 with the text content type, one `# TYPE` line per metric, no `# EOF` marker and no sample timestamps

#### Scenario: No new dependency
- **WHEN** the change is built
- **THEN** `go.mod` and `go.sum` are unchanged and the metrics handler imports only the standard library and project packages

### Requirement: Entry count metrics
The system SHALL expose `vandyke_entries_total`, `vandyke_entries_live`, `vandyke_entries_trashed` and `vandyke_entries_with_photo` as gauges collected from the database at scrape time, where `total` includes trashed rows and `with_photo` counts live entries that have a photo.

#### Scenario: Counts after a trash and purge
- **WHEN** the ledger has 10 live entries, one of them with a photo, and 2 trashed entries
- **THEN** the scrape reports `vandyke_entries_total 12`, `vandyke_entries_live 10`, `vandyke_entries_trashed 2` and `vandyke_entries_with_photo 1`

#### Scenario: Database unavailable during scrape
- **WHEN** the count query fails
- **THEN** `vandyke_db_up` is 0, the four entry gauges are omitted, the failure is logged and the endpoint still returns 200 with the process metrics

### Requirement: Addition outcome and rate-limit metrics
The system SHALL expose `vandyke_entry_additions_total` with the bounded `outcome` label values `created`, `duplicate`, `invalid`, `rate_limited` and `error`, incremented in the web and API create paths, and `vandyke_rate_limited_requests_total` incremented whenever either create limiter denies a request.

#### Scenario: Duplicate add
- **WHEN** a create is rejected because the case-insensitive `(word, meaning)` already exists
- **THEN** `vandyke_entry_additions_total{outcome="duplicate"}` increases by one

#### Scenario: Rate-limited add
- **WHEN** the per-IP limiter denies a create
- **THEN** both `vandyke_rate_limited_requests_total` and `vandyke_entry_additions_total{outcome="rate_limited"}` increase by one

#### Scenario: Unbounded values rejected
- **WHEN** a code path produces an outcome outside the five values
- **THEN** the metric is not incremented with that value, so the label set stays bounded

### Requirement: Webhook metrics
The system SHALL expose `vandyke_webhook_deliveries_total` with the `result` label values `delivered`, `failed` and `cancelled`, `vandyke_webhook_attempts_total` with the `result` label values `success`, `retry` and `permanent_failure`, the gauges `vandyke_webhook_queue_depth` and `vandyke_webhook_pending_deliveries`, and the counter `vandyke_webhook_dropped_total`. The delivery counter measures terminal outcomes rather than unique rows, so a manually retried delivery can contribute one `failed` result and, if it later succeeds, one `delivered` result.

#### Scenario: Retry then deliver
- **WHEN** a delivery fails once with 500 and succeeds on the retry
- **THEN** the attempt counters record one `retry` and one `success`, and the delivery counter records one `delivered`

#### Scenario: Manual retry after exhaustion
- **WHEN** an admin retries a delivery that already recorded `failed` and the retry succeeds
- **THEN** the delivery counter also records one `delivered`, so the sum of results exceeds the number of rows

#### Scenario: Dropped event
- **WHEN** the queue is full and an event is dropped
- **THEN** `vandyke_webhook_dropped_total` increases by one and the queue depth does not exceed 1,024

### Requirement: HTTP request metrics
The system SHALL expose `vandyke_http_requests_total` labelled by `method`, `route` and `status`, and the histogram `vandyke_http_request_duration_seconds` labelled by `method` and `route`, where `route` is the registered `http.ServeMux` pattern or `unmatched` and is never the raw request path.

#### Scenario: Entry detail request
- **WHEN** a client requests `/e/123`
- **THEN** one series is recorded for `method="GET"` and `route="GET /e/{id}"` or the registered equivalent, and no series contains `123` or `/e/123`

#### Scenario: Unmatched request
- **WHEN** a client requests a path no route matches
- **THEN** the request is recorded with `route="unmatched"` and does not create a per-path series

#### Scenario: Duration buckets
- **WHEN** a request completes
- **THEN** every finite bucket whose upper bound is at least the observed duration is incremented, along with the `+Inf` bucket, the `_sum` and the `_count`, using the buckets 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5 and 10 seconds

### Requirement: Database and process metrics
The system SHALL expose `vandyke_db_size_bytes` from the main SQLite file only, excluding the `-wal` and `-shm` files, `vandyke_db_up` as 1 when the scrape-time query succeeds and 0 otherwise, `vandyke_process_start_time_seconds`, `vandyke_process_uptime_seconds` and `vandyke_build_info` labelled by `version`, `go_version`, `goos` and `goarch` with a constant value of 1.

#### Scenario: Build info
- **WHEN** version `1.9.1` is scraped
- **THEN** `vandyke_build_info{version="1.9.1",go_version="...",goos="linux",goarch="..."} 1` is present

#### Scenario: Uptime
- **WHEN** the process has been running for 60 seconds
- **THEN** `vandyke_process_uptime_seconds` is approximately 60 and never decreases between scrapes

### Requirement: Readiness endpoint
The system SHALL expose `GET /readyz` that runs a database query and verifies that every embedded migration version is recorded in `schema_migrations`, returning 200 with `{"status":"ready","checks":{"database":"ok","migrations":"ok"}}` when both pass and 503 with `{"status":"unavailable","checks":{...}}` naming only the coarse failing check otherwise, within a 2 second timeout.

#### Scenario: Ready instance
- **WHEN** SQLite answers and all migrations are applied
- **THEN** `/readyz` returns 200 with the ready body

#### Scenario: Database closed
- **WHEN** the database handle is closed
- **THEN** `/readyz` returns 503 with `database` marked as the failing check

#### Scenario: Migration missing
- **WHEN** `schema_migrations` lacks a version that is embedded in the binary
- **THEN** `/readyz` returns 503 with `migrations` marked as the failing check

#### Scenario: Liveness is unchanged
- **WHEN** a client requests `GET /healthz`
- **THEN** the response is still 200 with `{"status":"ok"}` and does not depend on the database

### Requirement: Observability exposure and access control
The system SHALL expose `/metrics` without authentication by default, SHALL require `Authorization: Bearer <token>` compared in constant time when `METRICS_TOKEN` is set and return 401 otherwise, SHALL leave `/readyz` ungated, and SHALL set `Cache-Control: no-store` on both.

#### Scenario: Token configured
- **WHEN** `METRICS_TOKEN` is set and a request omits the bearer token or sends a wrong one
- **THEN** `/metrics` returns 401 and no metric body

#### Scenario: Correct token
- **WHEN** `METRICS_TOKEN` is set and the request carries the matching bearer token
- **THEN** `/metrics` returns 200 with the exposition

#### Scenario: Readiness without a token
- **WHEN** `METRICS_TOKEN` is set and `/readyz` is requested without a token
- **THEN** `/readyz` still answers 200 or 503 according to readiness

### Requirement: No sensitive data in observability output
The system SHALL NOT expose entry text, entry ids, person names, endpoint URLs, endpoint secrets, the metrics token, the database DSN, the database path, internal file paths or raw error strings through `/metrics` or `/readyz`.

#### Scenario: Metrics labels
- **WHEN** any metric is scraped
- **THEN** no label value contains an entry id, a word, a meaning, a person, an endpoint URL, a secret or the metrics token

#### Scenario: Readiness failure body
- **WHEN** `/readyz` reports a database failure
- **THEN** the body names the failing check only and contains no DSN, path or driver error text
