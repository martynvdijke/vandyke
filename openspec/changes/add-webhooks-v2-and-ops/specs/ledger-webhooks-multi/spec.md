## ADDED Requirements

### Requirement: Endpoint configuration
The system SHALL store webhook targets in the `webhook_endpoints` table with a name, an absolute URL, an optional signing secret, an enabled flag, a comma-separated list of subscribed event names, a formatting preset and an `allow_private` flag, and SHALL accept at most 20 endpoints.

#### Scenario: Create an endpoint
- **WHEN** an admin saves a name, an `https://` URL, the events `entry.created` and `entry.updated`, and the `generic` preset
- **THEN** one `webhook_endpoints` row is written with `enabled` set to 1 and the event list stored in a stable sorted order

#### Scenario: Endpoint cap
- **WHEN** 20 endpoints already exist and an admin submits another
- **THEN** the request is rejected with 422 and no row is written

#### Scenario: Duplicate name
- **WHEN** an admin submits a name that differs only in case from an existing endpoint name
- **THEN** the request is rejected with 422 and the existing endpoint is unchanged

#### Scenario: Invalid subscription or preset
- **WHEN** an admin submits an empty event list, an unknown event name or a preset other than `generic`, `discord` or `slack`
- **THEN** the request is rejected with 422 and no row is written

### Requirement: Endpoint changes apply immediately
The system SHALL rebuild its in-memory endpoint snapshot after every endpoint create, edit, enable, disable, delete or adopt, and at startup, so subsequent events use the new configuration without a restart.

#### Scenario: Disable the last endpoint
- **WHEN** an admin disables the only enabled persisted endpoint
- **THEN** the next event is no longer queued for it and the legacy target becomes active if an effective legacy URL exists

#### Scenario: Edit changes the subscription
- **WHEN** an admin removes `entry.updated` from an endpoint's event list
- **THEN** the next update is not queued for that endpoint without restarting the process

#### Scenario: Snapshot reload fails
- **WHEN** a reload after a mutation fails
- **THEN** the previous snapshot stays active, the failure is logged, and the retry tick attempts the reload again within 10 seconds

### Requirement: Entry event types
The system SHALL support exactly the event names `entry.created`, `entry.updated`, `entry.deleted`, `entry.restored` and `entry.purged`, and SHALL deliver an event only to enabled endpoints whose subscription contains that event name.

#### Scenario: Selective subscription
- **WHEN** an endpoint subscribes only to `entry.deleted` and an entry is created
- **THEN** no delivery is queued for that endpoint and no request is made to its URL

#### Scenario: Disabled endpoint
- **WHEN** an endpoint is disabled and any entry event occurs
- **THEN** no new delivery is queued for it and its pending deliveries are marked cancelled

### Requirement: Lifecycle event emission
The system SHALL emit one event per entry row actually changed by `CreateEntry`, `UpdateEntry`, `SoftDeleteEntries`, `RestoreEntries` and `PurgeEntries`, including every changed row in a bulk action of up to 100 ids, and SHALL emit no event for a row that the operation did not change.

#### Scenario: Bulk delete of 100 entries
- **WHEN** an admin deletes 100 live entries in one request
- **THEN** the system emits 100 `entry.deleted` events, one per entry, each with the same payload shape as a single delete

#### Scenario: Already-trashed id
- **WHEN** an admin submits an id that is already trashed to the delete action
- **THEN** no `entry.deleted` event is emitted for that id

#### Scenario: No-op edit
- **WHEN** an admin saves an entry without changing word, meaning, person, created date or photo
- **THEN** no `entry.updated` event is emitted

#### Scenario: Purged entry event
- **WHEN** an admin purges a trashed entry
- **THEN** the system emits `entry.purged` with the entry's last known word, meaning, person and created date before the row is removed

### Requirement: Import exception
The system SHALL NOT emit webhook events for entries created or replaced by `ApplyImport`, and SHALL document that receivers must reconcile after an import.

#### Scenario: Import 20,000 rows
- **WHEN** an admin applies an import of 20,000 entries
- **THEN** no `entry.created` or `entry.updated` delivery is queued for any imported row

### Requirement: Update diff
The system SHALL include an `occurred_at` field and a `changes` object in every `entry.updated` payload, where `changes` contains only the fields that changed among `word`, `meaning`, `person`, `created_at` and `photo`, and each value has `from` and `to`; `photo` SHALL carry the previous and new photo SHA-256 truncated to 12 hex characters, or null when that side has no photo.

#### Scenario: Meaning changed
- **WHEN** an admin changes only the meaning of entry 123
- **THEN** the payload has `changes` containing only `meaning` with the previous and new strings

#### Scenario: Photo replaced
- **WHEN** an admin replaces an entry photo with a different photo
- **THEN** `changes` contains `photo` with different non-null `from` and `to` hashes

#### Scenario: Photo removed
- **WHEN** an admin removes the photo from an entry that had one
- **THEN** `changes` contains `photo` with `from` set to the old hash and `to` set to null

#### Scenario: Edit of a trashed entry
- **WHEN** an admin edits a trashed entry
- **THEN** the `entry.updated` payload includes `deleted_at` so a receiver can distinguish it from a live edit

### Requirement: Generic payload contract
The system SHALL send the `generic` preset as a JSON object with `Content-Type: application/json` containing `event`, `id`, `word`, `meaning`, `person`, `created_at` and `url`; the `entry.created` body SHALL contain exactly those seven keys, with `created_at` in RFC 3339 and `url` as the absolute `{base}/e/{id}` permalink. For `entry.updated`, the body SHALL also contain `changes` and `occurred_at` in RFC 3339, plus `deleted_at` when the edited row is trashed; for `entry.deleted` and `entry.purged`, it SHALL contain `occurred_at` and `deleted_at`; for `entry.restored`, it SHALL contain `occurred_at` and no `deleted_at`.

#### Scenario: Created payload unchanged
- **WHEN** entry 123 is created and a `generic` endpoint subscribes to `entry.created`
- **THEN** the body has exactly the keys `event`, `id`, `word`, `meaning`, `person`, `created_at` and `url`, with `event` set to `entry.created` and `url` set to `{base}/e/123`

#### Scenario: Unattributed entry
- **WHEN** an entry is created without a person
- **THEN** the `person` key is present and is an empty string

#### Scenario: New event type fields
- **WHEN** a `generic` endpoint receives `entry.deleted` or `entry.purged`
- **THEN** the body contains the same seven keys plus `occurred_at` and `deleted_at`, because the snapshot is trashed

### Requirement: Discord preset
The system SHALL send the `discord` preset as a JSON object with a non-empty `content` string that names the event type and an `embeds` array containing one object with `title`, `description`, `url` and `timestamp`, and SHALL set `allowed_mentions.parse` to an empty array; when the entry has an empty word or meaning, `title` and `description` SHALL use a non-empty fallback naming the entry id or permalink rather than an empty string.

#### Scenario: Created entry for Discord
- **WHEN** an endpoint with the `discord` preset receives `entry.created`
- **THEN** `content` names the event, the embed's `title` is the word, `description` is the meaning, `url` is the absolute permalink and `timestamp` is the RFC 3339 created time

#### Scenario: Deleted entry for Discord
- **WHEN** an endpoint with the `discord` preset receives `entry.deleted`
- **THEN** `content` names `entry.deleted` and the embed still identifies the entry and its permalink

#### Scenario: Photo-only entry for Discord
- **WHEN** an entry has a photo but an empty word and meaning
- **THEN** the embed `title` and `description` are non-empty fallbacks and the body is still valid for Discord

#### Scenario: Mention injection
- **WHEN** a word or meaning contains `@everyone` or a role mention
- **THEN** the body still carries `allowed_mentions.parse` as an empty array and no mention can notify a channel

### Requirement: Slack preset
The system SHALL send the `slack` preset as a JSON object with a non-empty `text` fallback string that names the event type and a `blocks` array containing at least one `section` block whose `text.type` is `mrkdwn`, and SHALL escape `&`, `<` and `>` in entry text before rendering; when the entry has an empty word and meaning, the fallback and block SHALL name the entry id or permalink instead of being empty.

#### Scenario: Created entry for Slack
- **WHEN** an endpoint with the `slack` preset receives `entry.created`
- **THEN** `text` names the event and one `section` block contains the word, meaning and permalink in `mrkdwn`

#### Scenario: Channel mention injection
- **WHEN** a word or meaning contains `<!channel>`
- **THEN** the rendered body contains the escaped text and not an active Slack mention

### Requirement: HMAC signing over the rendered body
When an endpoint has a secret, the system SHALL include `X-VanDyke-Signature: sha256=<lowercase hex>` holding the HMAC-SHA256 digest of the exact request body bytes sent, for every preset, and SHALL omit the header when the endpoint has no secret.

#### Scenario: Signed Discord delivery
- **WHEN** a `discord` endpoint with a secret receives an event
- **THEN** the signature verifies against the received Discord JSON body and that endpoint's secret

#### Scenario: Unsigned endpoint
- **WHEN** an endpoint has an empty secret
- **THEN** the request carries no `X-VanDyke-Signature` header

### Requirement: Non-blocking enqueue
The system SHALL enqueue webhook work without performing a database read, rendering, DNS lookup or network request on the request path, and SHALL bound the queue at 1,024 jobs.

#### Scenario: Slow receiver on create
- **WHEN** an entry is created while the subscribed receiver does not answer within 5 seconds
- **THEN** the create response is returned without waiting and the delivery is attempted in the background

#### Scenario: Full queue
- **WHEN** the queue is full and another event is emitted
- **THEN** the event is dropped, the dropped counter is incremented, a warning records the event and entry id, and the request still succeeds

### Requirement: Retry, backoff and delivery state
The system SHALL persist one `webhook_deliveries` row per endpoint and event when a worker first picks up the job, SHALL treat 2xx as delivered, SHALL retry transport errors, timeouts, 408, 425, 429 and 5xx with full-jitter exponential backoff using a 5 second base and a 1 hour cap, SHALL treat other responses and render failures as permanent, and SHALL stop after 8 attempts.

#### Scenario: Transient failure then success
- **WHEN** a receiver returns 500 on the first attempt and 204 on the retry
- **THEN** the delivery row records two attempts, `last_status` 204 and the terminal status `delivered`

#### Scenario: Permanent failure
- **WHEN** a receiver returns 404
- **THEN** the delivery is marked `failed` after one attempt and no retry is scheduled

#### Scenario: Attempt limit
- **WHEN** a receiver keeps returning 500
- **THEN** the delivery is marked `failed` after the eighth attempt and no further attempt is scheduled

#### Scenario: Disabled endpoint during retry
- **WHEN** an endpoint is disabled while it has pending deliveries
- **THEN** those rows are marked `cancelled` and are not sent

#### Scenario: Endpoint deleted before pickup
- **WHEN** an endpoint is deleted after an event is queued but before a worker persists the delivery
- **THEN** the job is discarded without a foreign-key error and no delivery row is written

### Requirement: Delivery log and retention
The system SHALL provide a delivery log showing endpoint, event, entry id, status, attempts, last status code, last error, next attempt time and creation time, and SHALL prune delivered and cancelled rows older than 7 days and failed rows older than 30 days without deleting pending rows.

#### Scenario: Inspect the log
- **WHEN** an admin opens `/admin/webhooks` after a failed delivery
- **THEN** the row shows `failed`, the attempt count, the last status code, a sanitised last error and no next attempt time

#### Scenario: Automatic prune
- **WHEN** an hourly prune runs
- **THEN** delivered and cancelled rows older than 7 days and failed rows older than 30 days are deleted, while every pending row remains

#### Scenario: Retry now
- **WHEN** an admin chooses "retry now" for a failed delivery
- **THEN** its status becomes `pending`, its `next_attempt_at` is now, its attempt count is unchanged, it is picked up by the next retry tick within 10 seconds, and an audit row records the action

### Requirement: Delivery payload retention
The system SHALL store the generic envelope in `webhook_deliveries.payload` only while a delivery is pending or failed, SHALL clear it when the delivery becomes `delivered` or `cancelled`, and SHALL delete terminal delivery rows for an entry when that entry is purged.

#### Scenario: Delivered row clears its payload
- **WHEN** a delivery reaches `delivered`
- **THEN** its `payload` is empty and the log still shows the event, entry id, status and attempts

#### Scenario: Purge removes delivered history
- **WHEN** an entry is purged
- **THEN** every `delivered` or `cancelled` delivery row for that entry id is deleted, and only a pending or failed notification for that entry may remain

#### Scenario: Failed row keeps enough to retry
- **WHEN** a delivery is `failed` because the receiver was unreachable
- **THEN** its payload is still present so "retry now" can send the same event without re-reading the entry

### Requirement: Admin webhook management
The system SHALL provide `GET /admin/webhooks`, `POST /admin/webhooks`, `POST /admin/webhooks/{id}`, `POST /admin/webhooks/{id}/toggle`, `POST /admin/webhooks/{id}/delete`, `POST /admin/webhooks/{id}/test`, `POST /admin/webhooks/adopt`, `POST /admin/webhooks/deliveries/{id}/retry` and `POST /admin/webhooks/prune`, with reads behind `requireAuth`, configuration and test actions behind CSRF, and delete and prune behind owner CSRF.

#### Scenario: Editor edits an endpoint
- **WHEN** an authenticated editor changes an endpoint URL and submits the form with a valid CSRF token
- **THEN** the endpoint is updated and the audit row names the changed fields and the host, not the URL path or secret

#### Scenario: Editor attempts delete
- **WHEN** an editor submits the delete action
- **THEN** the request is refused with 403 and the endpoint remains

#### Scenario: Non-owner attempts to allow a private target
- **WHEN** an editor submits a form with `allow_private` set and the stored endpoint does not already have it
- **THEN** the stored flag is unchanged

#### Scenario: Missing CSRF
- **WHEN** a configuration action is submitted without a valid CSRF token
- **THEN** the request is refused with 403 and nothing changes

### Requirement: Legacy endpoint compatibility
While no enabled persisted endpoint exists, the system SHALL treat a non-empty effective `webhook.url` or `LEDGER_WEBHOOK_URL` as one synthesized legacy endpoint with the generic preset and only the `entry.created` subscription, and SHALL record its deliveries with a null endpoint id; while at least one persisted endpoint is enabled, the system SHALL NOT deliver through the legacy target.

#### Scenario: Environment-only deployment
- **WHEN** `LEDGER_WEBHOOK_URL` is set and the `webhook_endpoints` table has no enabled row
- **THEN** a create produces one signed generic `entry.created` delivery to that URL exactly as before this change

#### Scenario: Persisted endpoint suppresses legacy
- **WHEN** one enabled persisted endpoint exists and `LEDGER_WEBHOOK_URL` is also set
- **THEN** creates are delivered only to the persisted endpoint and no duplicate legacy delivery is made

#### Scenario: Last persisted endpoint disabled
- **WHEN** every persisted endpoint is disabled and an effective legacy URL still exists
- **THEN** the legacy target is active again, and the admin page marks its read-only row as active so the state is visible

#### Scenario: Adopt the legacy target
- **WHEN** an operator chooses "adopt" on the legacy row
- **THEN** a persisted endpoint is created with the effective URL and secret, and the legacy path is suppressed while that endpoint is enabled

### Requirement: Outbound target validation
The system SHALL accept only absolute `http` or `https` URLs without userinfo, SHALL deny loopback, private, link-local, unspecified, multicast and IPv4-mapped targets by default, SHALL permit loopback and private targets only when an owner sets `allow_private`, SHALL never permit link-local, unspecified or multicast targets, SHALL re-check every resolved address at delivery time, SHALL not follow redirects, SHALL time out an attempt after 5 seconds and SHALL read at most 4 KiB of the response body. The synthesized legacy endpoint SHALL be treated as `allow_private` for loopback and private targets so an existing LAN deployment keeps working, while link-local, unspecified and multicast remain denied.

#### Scenario: Metadata address
- **WHEN** an admin saves or tests a URL whose host is `169.254.169.254`
- **THEN** the request is rejected even when `allow_private` is set

#### Scenario: LAN receiver with opt-in
- **WHEN** an owner saves `http://homeassistant.local:8123/api/webhook/abc` with `allow_private` set and the host resolves to `192.168.1.20`
- **THEN** the endpoint is accepted and delivery is attempted to that address

#### Scenario: Legacy LAN target
- **WHEN** `LEDGER_WEBHOOK_URL` points at a private address and no persisted endpoint is enabled
- **THEN** the legacy target is permitted as before, but a link-local address is still refused

#### Scenario: Redirect to a denied target
- **WHEN** an allowed endpoint answers with a redirect to `http://127.0.0.1:8080/`
- **THEN** the redirect is not followed and the delivery is recorded as a permanent failure

#### Scenario: DNS rebinding
- **WHEN** a hostname resolves to a denied address at delivery time even though it resolved to an allowed address when saved
- **THEN** the attempt is refused and the delivery records a sanitised target error

### Requirement: Secret and URL redaction
The system SHALL never render a stored endpoint secret, SHALL treat a blank secret field as unchanged, SHALL mask the URL path and query in the endpoint list, and SHALL record only the host in logs, delivery errors and audit rows.

#### Scenario: Secret is write-only
- **WHEN** an admin opens the edit form for an endpoint with a stored secret
- **THEN** the secret field is blank, saving it blank keeps the stored value, and a separate clear control removes it

#### Scenario: Token-bearing URL in logs
- **WHEN** a Discord URL containing a token in its path fails to deliver
- **THEN** the log and the delivery's last error contain the host only, never the path or token

### Requirement: Per-endpoint test fire
The system SHALL provide `POST /admin/webhooks/{id}/test` that sends one `entry.created` test payload using that endpoint's preset and secret, applies the same target validation as production, waits at most 5 seconds and reports the HTTP status or a sanitised error on the page.

#### Scenario: Test succeeds
- **WHEN** an admin tests a reachable endpoint
- **THEN** the page reports the receiver's status and the delivery used the same rendering and signature rules as production

#### Scenario: Test fails
- **WHEN** the test receiver refuses the connection or times out
- **THEN** the page reports the sanitised failure and the stored endpoint is unchanged

#### Scenario: Test target is denied
- **WHEN** the endpoint's host now resolves to a link-local address
- **THEN** the test makes no request and reports the same target rejection as production
