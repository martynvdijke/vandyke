## MODIFIED Requirements

### Requirement: Optional webhook configuration
The server SHALL treat the effective `webhook.url` or `LEDGER_WEBHOOK_URL` URL and the effective `webhook.secret` or `LEDGER_WEBHOOK_SECRET` secret as one synthesized legacy endpoint with the generic preset and only the `entry.created` subscription, and SHALL use that endpoint only while no enabled persisted endpoint exists in `webhook_endpoints`. The settings form SHALL never render the stored secret and SHALL treat a blank secret field as unchanged.

#### Scenario: Environment only
- **WHEN** `LEDGER_WEBHOOK_URL` is set, no `webhook.url` override is stored and no persisted endpoint is enabled
- **THEN** entry creation attempts one generic signed delivery to that URL, with the same body as before this change

#### Scenario: Runtime override wins
- **WHEN** `webhook.url` is stored and `LEDGER_WEBHOOK_URL` is also set
- **THEN** the legacy endpoint uses the stored value and not the environment value

#### Scenario: Persisted endpoint suppresses legacy
- **WHEN** at least one persisted endpoint is enabled
- **THEN** entry creation does not deliver through the legacy target, even when `LEDGER_WEBHOOK_URL` or `webhook.url` is non-empty

#### Scenario: Neither source is configured
- **WHEN** neither `webhook.url` nor `LEDGER_WEBHOOK_URL` has a value and no persisted endpoint exists
- **THEN** entry creation performs no outbound request

#### Scenario: Secret is write-only
- **WHEN** the admin settings page is loaded with a stored `webhook.secret`
- **THEN** the secret field is blank, saving with the field left blank keeps the stored secret, and the clear control removes it

### Requirement: Non-blocking dispatch on entry creation
The server SHALL attempt one delivery per enabled endpoint subscribed to `entry.created` after every successful entry creation from the web form and from `POST /api/v1/entries`, on a path that does not delay or alter the user's response, and SHALL never return a delivery error to the caller.

#### Scenario: Successful create with two subscribed endpoints
- **WHEN** an entry is created and two enabled endpoints subscribe to `entry.created`
- **THEN** the response is returned without waiting for either receiver and two deliveries are attempted in the background

#### Scenario: Receiver is unreachable
- **WHEN** a subscribed endpoint refuses the connection or does not answer within 5 seconds
- **THEN** the entry is still created, the response is unchanged, and the failure is logged and recorded on the delivery

#### Scenario: No subscription
- **WHEN** an entry is created while no enabled endpoint subscribes to `entry.created`
- **THEN** no outbound request is made

### Requirement: JSON payload contract
For a `generic` `entry.created` delivery, the request SHALL be a `POST` with `Content-Type: application/json` and a body containing the fields `event`, `id`, `word`, `meaning`, `person`, `created_at` and `url`, where `event` is `entry.created`, `created_at` is RFC 3339, `person` is an empty string when unattributed, and `url` is the absolute `{base}/e/{id}` permalink; the other four event types and their additional fields are defined by the `ledger-webhooks-multi` capability, and the Discord and Slack presets replace this body with their own shape.

#### Scenario: Payload for a new entry
- **WHEN** entry 123 with word, meaning and person is created and a `generic` endpoint subscribes
- **THEN** the body is a JSON object with `event` set to `entry.created`, `id` set to 123, the three text fields, an RFC 3339 `created_at`, and `url` set to `{base}/e/123`

#### Scenario: Payload for an unattributed entry
- **WHEN** an entry is created without a person and a `generic` endpoint subscribes
- **THEN** the `person` field is present and is an empty string

### Requirement: Admin test-fire action
The admin settings page SHALL provide a CSRF-protected test-fire action at `POST /admin/settings/webhook/test` that sends the generic `entry.created` payload to the legacy target only, waits up to 5 seconds and reports the HTTP status or a sanitised error on the page; per-endpoint test fire is provided by `POST /admin/webhooks/{id}/test` in the `ledger-webhooks-multi` capability.

#### Scenario: Test-fire succeeds
- **WHEN** an admin submits the legacy test-fire action with a reachable legacy target and no enabled persisted endpoint
- **THEN** the settings page reports the receiver's HTTP status and the delivery used the same payload and signature rules as the legacy path

#### Scenario: Test-fire with a persisted endpoint
- **WHEN** an admin submits the legacy test-fire action while a persisted endpoint is enabled
- **THEN** the page reports that the legacy target is suppressed and directs the operator to `/admin/webhooks`, and no request is made to the settings URL

#### Scenario: No legacy URL configured
- **WHEN** an admin submits the test-fire action while the effective legacy URL is empty
- **THEN** the settings page states that no legacy webhook is configured and no request is made
