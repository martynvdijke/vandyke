## ADDED Requirements

### Requirement: Admin roles
The system SHALL store a `role` on every admin row with the values `owner` and `editor`, SHALL treat every admin that existed before the role migration as `owner`, and SHALL create every later local admin as `editor` unless the creating owner selects `owner`.

#### Scenario: Existing admin after the migration
- **WHEN** the role migration runs on a database with existing admin rows
- **THEN** every existing row has `role` equal to `owner` and keeps full access

#### Scenario: New admin defaults to editor
- **WHEN** an owner creates a local admin without selecting a role
- **THEN** the new row has `role` equal to `editor`

### Requirement: Owner-only actions
The system SHALL restrict entry deletion, trash purge, every `/admin/admins*` route and the audit viewer to admins with the `owner` role, and SHALL return 403 to an editor.

#### Scenario: Editor attempts a destructive action
- **WHEN** an authenticated editor posts to the entry delete route or the trash purge route
- **THEN** the response is 403 and no entry is trashed or purged

#### Scenario: Editor attempts admin management
- **WHEN** an authenticated editor requests `/admin/admins` or posts any `/admin/admins*` route
- **THEN** the response is 403 and no admin row changes

#### Scenario: Owner performs the same action
- **WHEN** an authenticated owner posts the same request
- **THEN** the action proceeds as specified elsewhere

### Requirement: Admin account management
The system SHALL provide `GET /admin/admins` for owners and the CSRF-protected routes `POST /admin/admins`, `POST /admin/admins/{id}/role`, `POST /admin/admins/{id}/disable`, `POST /admin/admins/{id}/enable` and `POST /admin/admins/{id}/delete`.

#### Scenario: Create a local admin
- **WHEN** an owner submits a username, display name, password and role to `POST /admin/admins`
- **THEN** a new admin row is created with a PBKDF2 password hash and appears in the accounts list

#### Scenario: Disable and re-enable
- **WHEN** an owner disables an admin and later enables the same admin
- **THEN** `disabled_at` is set and then cleared, and the admin can sign in again only after it is cleared

#### Scenario: Delete an admin
- **WHEN** an owner deletes an admin
- **THEN** the admin row is removed, its sessions cascade away, and its username no longer appears in the accounts list

#### Scenario: Unknown admin id
- **WHEN** a route targets an id that does not exist
- **THEN** the response is 404 from `store.ErrAdminNotFound`

### Requirement: Last owner protection
The system SHALL refuse to demote, disable or delete an admin when doing so would leave no enabled owner, and SHALL report the refusal as 409.

#### Scenario: Demote the only owner
- **WHEN** the only enabled owner posts a role change to `editor` for their own account
- **THEN** the response is 409, the role stays `owner`, and the admin remains enabled

#### Scenario: Remove one of two owners
- **WHEN** two enabled owners exist and one is disabled, demoted or deleted
- **THEN** the action succeeds and one enabled owner remains

### Requirement: Password change and reset
The system SHALL provide `POST /admin/account/password` for a self-service change requiring the current password, and `POST /admin/admins/{id}/password` for an owner-initiated reset, both hashing with the existing PBKDF2-HMAC-SHA256 implementation, enforcing a 12-character minimum, and revoking the affected admin's other sessions.

#### Scenario: Change own password
- **WHEN** an admin submits the correct current password and a new password of at least 12 characters
- **THEN** the stored hash is replaced, the admin's other sessions are revoked, and the current session stays valid

#### Scenario: Wrong current password
- **WHEN** an admin submits an incorrect current password
- **THEN** the change is refused, the stored hash is unchanged, and no session is revoked

#### Scenario: Owner resets another admin
- **WHEN** an owner submits a new password for another admin
- **THEN** that admin's hash is replaced without requiring the old password and all of that admin's sessions are revoked

### Requirement: Session listing and revocation
The system SHALL give each new session a random `session_ref`, list the current admin's sessions with creation and expiry times, allow revoking one session through `POST /admin/sessions/revoke` or all other sessions through `POST /admin/sessions/revoke-all`, reject a disabled admin's session, and sweep expired sessions at most once per hour during authenticated request handling.

#### Scenario: List and revoke one session
- **WHEN** an admin opens `/admin/sessions` and revokes a non-current session by its `session_ref`
- **THEN** that session row is deleted and the current session remains usable

#### Scenario: Sign out everywhere
- **WHEN** an admin posts to `/admin/sessions/revoke-all`
- **THEN** every other session of that admin is deleted and the requesting session remains valid

#### Scenario: Foreign session reference
- **WHEN** an admin posts a `session_ref` that belongs to another admin
- **THEN** no session is deleted and the request is refused

#### Scenario: Disabled admin with an existing session
- **WHEN** an admin is disabled while holding a valid session cookie
- **THEN** the next authenticated request redirects to the login page

#### Scenario: Expired session sweep
- **WHEN** an authenticated request arrives more than an hour after the last sweep
- **THEN** expired session rows are deleted once and the request proceeds normally
