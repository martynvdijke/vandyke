## ADDED Requirements

### Requirement: Optional webhook configuration
The server SHALL read the webhook target from the `webhook.url` runtime setting, falling back to the `LEDGER_WEBHOOK_URL` environment variable when that setting is empty, and SHALL read the signing secret from the `webhook.secret` runtime setting, falling back to `LEDGER_WEBHOOK_SECRET`, with an empty effective URL disabling dispatch. The settings form SHALL never render the stored secret and SHALL treat a blank secret field as unchanged.

#### Scenario: Environment only
- **WHEN** `LEDGER_WEBHOOK_URL` is set and no `webhook.url` override is stored
- **THEN** entry creation attempts delivery to that URL

#### Scenario: Runtime override wins
- **WHEN** `webhook.url` is stored and `LEDGER_WEBHOOK_URL` is also set
- **THEN** delivery goes to the stored value and not to the environment value

#### Scenario: Webhook disabled
- **WHEN** neither `webhook.url` nor `LEDGER_WEBHOOK_URL` has a value
- **THEN** entry creation performs no outbound request

#### Scenario: Secret is write-only
- **WHEN** the admin settings page is loaded with a stored `webhook.secret`
- **THEN** the secret field is blank, saving with the field left blank keeps the stored secret, and the clear control removes it

### Requirement: Non-blocking dispatch on entry creation
The server SHALL attempt one webhook delivery after every successful entry creation from the web form and from `POST /api/v1/entries`, on a path that does not delay or alter the user's response, and SHALL log a failed delivery without returning an error to the caller.

#### Scenario: Successful create with a webhook configured
- **WHEN** an entry is created and a webhook URL is effective
- **THEN** the response is returned without waiting for the receiver and one delivery is attempted in the background

#### Scenario: Receiver is unreachable
- **WHEN** the webhook target refuses the connection or does not answer within 5 seconds
- **THEN** the entry is still created, the response is unchanged, and the failure is logged at warn level

#### Scenario: Webhook is not configured
- **WHEN** an entry is created while the effective URL is empty
- **THEN** no outbound request is made and no error is logged

### Requirement: JSON payload contract
The delivery SHALL be a `POST` with `Content-Type: application/json` and a body containing the fields `event`, `id`, `word`, `meaning`, `person`, `created_at` and `url`, where `event` is `entry.created`, `created_at` is RFC 3339, `person` is an empty string when unattributed, and `url` is the absolute `{base}/e/{id}` permalink.

#### Scenario: Payload for a new entry
- **WHEN** entry 123 with word, meaning and person is created
- **THEN** the body is a JSON object with `event` set to `entry.created`, `id` set to 123, the three text fields, an RFC 3339 `created_at`, and `url` set to `{base}/e/123`

#### Scenario: Payload for an unattributed entry
- **WHEN** an entry is created without a person
- **THEN** the `person` field is present and is an empty string

### Requirement: HMAC-SHA256 signature
When an effective secret is configured, the delivery SHALL include an `X-VanDyke-Signature` header of the form `sha256=<hex>` holding the HMAC-SHA256 digest of the exact request body keyed by that secret, and SHALL omit the header when no secret is configured.

#### Scenario: Signed delivery
- **WHEN** `webhook.secret` or `LEDGER_WEBHOOK_SECRET` supplies a secret and an entry is created
- **THEN** the request carries `X-VanDyke-Signature: sha256=<hex>` that verifies against the raw body and the secret

#### Scenario: Unsigned delivery
- **WHEN** no secret is effective
- **THEN** the request carries no `X-VanDyke-Signature` header

### Requirement: Admin test-fire action
The admin settings page SHALL provide a CSRF-protected test-fire action at `POST /admin/settings/webhook/test` that sends the production payload to the effective URL, waits up to 5 seconds, and reports the HTTP status or the error on the page.

#### Scenario: Test-fire succeeds
- **WHEN** an admin submits the test-fire action with a reachable receiver
- **THEN** the settings page reports the receiver's HTTP status and the delivery used the same payload and signature rules as production

#### Scenario: Test-fire fails
- **WHEN** the test-fire receiver refuses the connection or times out
- **THEN** the settings page reports the failure and the stored settings are unchanged

#### Scenario: No URL configured
- **WHEN** an admin submits the test-fire action while the effective URL is empty
- **THEN** the settings page states that no webhook is configured and no request is made
