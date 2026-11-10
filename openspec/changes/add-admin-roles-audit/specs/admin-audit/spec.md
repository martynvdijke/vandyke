## ADDED Requirements

### Requirement: Audit record storage
The system SHALL persist one row in `admin_audit` for each audited action, containing `actor_id`, `actor_name`, `action`, `subject`, `detail`, `ip` and `created_at`, and SHALL NOT roll back the audited action when the insert fails.

#### Scenario: Successful audited action
- **WHEN** an audited action completes
- **THEN** one `admin_audit` row exists with the acting admin's id and username, the action name, the subject, the client IP and a UTC timestamp

#### Scenario: Audit insert failure
- **WHEN** the `admin_audit` insert fails
- **THEN** the failure is logged and the underlying action still commits

#### Scenario: Actor is deleted later
- **WHEN** an admin who appears in audit rows is deleted
- **THEN** the audit rows remain, because `actor_id` has no foreign key and `actor_name` is a snapshot

### Requirement: Authentication events are audited
The system SHALL record `login.success`, `login.failure` and `logout`, including the attempted username and client IP for a failed login.

#### Scenario: Successful login
- **WHEN** an admin signs in with valid credentials
- **THEN** a `login.success` row records the admin id, username and IP

#### Scenario: Failed login with a known username
- **WHEN** a sign-in attempt uses an existing username and a wrong password
- **THEN** a `login.failure` row records the username and IP without a session being created

#### Scenario: Failed login with an unknown username
- **WHEN** a sign-in attempt uses a username that does not exist
- **THEN** a `login.failure` row records the attempted username with a null `actor_id`

#### Scenario: Logout
- **WHEN** an authenticated admin posts to the logout route
- **THEN** a `logout` row records the admin id and IP before the session is deleted

### Requirement: Entry lifecycle is audited
The system SHALL record `entry.create`, `entry.update`, `entry.delete`, `entry.restore` and `entry.purge` with the entry id and the changed fields or the affected count.

#### Scenario: Entry created through the public API
- **WHEN** a client creates an entry through `POST /api/v1/entries`
- **THEN** an `entry.create` row records the new entry id, the submitted word, the client IP and a null `actor_id`, because the public API carries no admin identity

#### Scenario: Entry updated
- **WHEN** an admin saves an edit to an entry
- **THEN** an `entry.update` row records the entry id and the names of the fields that changed

#### Scenario: Entry trashed and restored
- **WHEN** an admin trashes an entry and later restores it
- **THEN** `entry.delete` and `entry.restore` rows record the entry id in that order

#### Scenario: Entry purged
- **WHEN** an admin permanently purges a trashed entry
- **THEN** an `entry.purge` row records the entry id

### Requirement: Settings and admin changes are audited
The system SHALL record `settings.update` and `admin.create`, `admin.role`, `admin.disable`, `admin.enable`, `admin.delete`, `admin.password_reset`, `session.revoke` and `session.revoke_all`.

#### Scenario: Settings saved
- **WHEN** an admin saves the runtime settings form
- **THEN** a `settings.update` row records which setting keys changed, without storing secret values

#### Scenario: Admin role changed
- **WHEN** an owner changes another admin's role
- **THEN** an `admin.role` row records the subject admin id and the old and new role

#### Scenario: Owner resets a password
- **WHEN** an owner resets another admin's password
- **THEN** an `admin.password_reset` row records the subject admin id, and no password value is written to the audit row

#### Scenario: Sessions revoked
- **WHEN** an admin revokes one session or all other sessions
- **THEN** a `session.revoke` or `session.revoke_all` row records the number of sessions deleted

### Requirement: Audit viewer
The system SHALL provide `GET /admin/audit`, restricted to owners, listing audit rows newest first with 50 rows per page and filters for `action`, `actor`, `from` and `to`, and SHALL expose no route that edits or deletes an audit row.

#### Scenario: View the audit trail
- **WHEN** an owner opens `/admin/audit`
- **THEN** the page shows the newest 50 rows with time, actor, action, subject, detail and IP, plus a pagination control

#### Scenario: Filter by action and date
- **WHEN** an owner filters on `action=login.failure` and a `from` and `to` date
- **THEN** only matching rows are listed and the total reflects the filter

#### Scenario: Editor is refused
- **WHEN** an authenticated editor requests `/admin/audit`
- **THEN** the response is 403

#### Scenario: No mutation routes
- **WHEN** a client posts to any `/admin/audit` path
- **THEN** the request is not routed and no audit row is changed
