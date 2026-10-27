# Admin, OIDC and runtime Umami settings

Date: 2026-10-27

## Goal

Add an authenticated administration interface to the public vanDyke ledger
without changing its open reading and entry-creation behaviour.

## Access model

- Anyone can read the ledger and add entries through the web form or API.
- Editing and deleting entries require an administrator session.
- The first administrator is created at `/admin/setup`.
- `ADMIN_SETUP_TOKEN` optionally protects the setup page until the first
  account exists.
- Optional OIDC login is enabled by `OIDC_ISSUER` and `OIDC_CLIENT_ID`. Any
  identity the provider authenticates is auto-provisioned as an admin; the
  provider's own access policy is the boundary.
- Local passwords use PBKDF2-HMAC-SHA256 with a random salt.
- Sessions are stored in SQLite by token hash and delivered in an `HttpOnly`,
  `SameSite=Lax` cookie. Mutating admin forms require a session-bound CSRF
  token. Login attempts are rate-limited per IP.

## Data model

New migrations add:

- `admins` — local username/password hash or OIDC issuer/subject, profile
  fields and login timestamps.
- `admin_sessions` — token hash, admin, CSRF token and expiry.
- `settings` — key/value overrides for runtime settings.

Entry editing reuses the existing validation: text is required unless a photo
is present. Updating an entry can replace or remove its photo. Deleting entries
cascades to `entry_photos`.

## Admin interface

- `/admin` lists entries with search, sorting and pagination.
- `/admin/entries/{id}/edit` edits word, meaning, logged date and photo.
- Entries can be deleted individually or in bulk; bulk deletion is capped at
  100 entries per request.
- `/admin/settings` edits the Umami tracker URL, website ID and domains.

## Umami settings

Saved values override `UMAMI_URL`, `UMAMI_WEBSITE_ID` and `UMAMI_DOMAINS` per
field; clearing a field restores the environment fallback. The effective
settings are cached in memory and used for both tracker injection and a
per-request Content-Security-Policy, so changes apply without a restart.

## Testing

Store tests cover admin/session lifecycle, OIDC upserts, settings, entry
updates and cascading deletion. Auth tests cover password hashing, tokens,
PKCE and OIDC discovery/token verification against a fake provider. Admin tests
cover setup, login, CSRF, editing, bulk deletion, settings, and OIDC
auto-provisioning.
