## 1. Schema

- [x] 1.1 Add `internal/store/migrations/011_admin_roles.sql` adding a text `role` column to `admins` defaulting to `editor`, a nullable `disabled_at` text column to `admins`, and then setting every existing admin row to `owner`.
- [x] 1.2 In the same migration add a `session_ref` text column to `admin_sessions` defaulting to empty, with a unique partial index over non-empty references.
- [x] 1.3 Add `internal/store/migrations/012_admin_audit.sql` with the `admin_audit` table and indexes on `created_at`, `action` and `actor_id`.
- [x] 1.4 Extend `Admin` with `Role string` and `DisabledAt *time.Time`, and `Session` with `SessionRef string`.
- [x] 1.5 Add a migration test that applies all migrations and confirms existing admin rows read back as `owner`.

## 2. Store: admin accounts

- [x] 2.1 Add `ListAdmins` returning every admin ordered by `created_at`.
- [x] 2.2 Add `CreateAdmin` for a local username and password hash with an explicit role.
- [x] 2.3 Add `SetAdminRole`, `DisableAdmin`, `EnableAdmin` and `DeleteAdmin`.
- [x] 2.4 Add `CountEnabledOwners` and a typed last-owner error used by the role, disable and delete paths.
- [x] 2.5 Run each owner-removing mutation and its owner count inside one transaction.
- [x] 2.6 Add `SetAdminPassword` that updates `password_hash` and revokes the target's sessions in one transaction (a self-service change passes the current token hash so that session survives, per the password-change scenario).
- [x] 2.7 Make `CreateFirstAdmin` and the first-login OIDC claim insert `role = 'owner'` (an OIDC login inserts `owner` only when no admin exists yet; later OIDC identities are `editor`, so an IdP login cannot silently gain owner rights on an established instance).
- [x] 2.8 Add store tests for create, role change, disable, enable, delete, the last-owner invariant and disabled-admin login lookup.

## 3. Role enforcement

- [x] 3.1 Make `requireAuth` load the role and reject a session whose admin is disabled.
- [x] 3.2 Add a `requireOwner` wrapper returning 403 for an editor.
- [x] 3.3 Guard entry delete, trash purge, every `/admin/admins*` route and `GET /admin/audit` with `requireOwner`.
- [x] 3.4 Show or hide owner-only controls in admin templates based on the current role.
- [x] 3.5 Add a table-driven test that every owner-only route returns 403 for an editor and 200 or 303 for an owner.

## 4. Admin accounts page

- [x] 4.1 Add `GET /admin/admins` listing username, display name, role, disabled state, created time and last login.
- [x] 4.2 Add `POST /admin/admins` behind `withCSRF` to create a local admin with username, display name, password and role.
- [x] 4.3 Add `POST /admin/admins/{id}/role`, `POST /admin/admins/{id}/disable`, `POST /admin/admins/{id}/enable` and `POST /admin/admins/{id}/delete`, all behind `withCSRF` and `requireOwner`.
- [x] 4.4 Return `store.ErrAdminNotFound` as 404 and the last-owner error as 409 with an explanatory message.
- [x] 4.5 Add handler tests for create, duplicate username, role change, disable, delete and the last-owner rejection.

## 5. Password changes

- [x] 5.1 Add `GET /admin/account` with the self-service password form and a link to the session list.
- [x] 5.2 Add `POST /admin/account/password` behind `withCSRF`, verifying the current password, enforcing a 12-character minimum, hashing with `auth.HashPassword`, and revoking the admin's other sessions.
- [x] 5.3 Add `POST /admin/admins/{id}/password` behind `withCSRF` and `requireOwner` for an owner-initiated reset that revokes the target's sessions.
- [x] 5.4 Add tests for a wrong current password, a too-short password, a successful change and a successful reset.

## 6. Sessions

- [x] 6.1 Generate a 16-byte hex `session_ref` in `CreateSession` and store it with the token hash.
- [x] 6.2 Add `ListSessionsForAdmin` returning reference, creation, expiry and current-session marker.
- [x] 6.3 Add `DeleteSessionByRef` scoped to the requesting admin, and `DeleteOtherSessionsForAdmin` (an owner may revoke any reference per the programme security requirement; editors remain scoped to their own sessions).
- [x] 6.4 Add `GET /admin/sessions`, `POST /admin/sessions/revoke` with a `ref` field, and `POST /admin/sessions/revoke-all`, the latter two behind `withCSRF`.
- [x] 6.5 Call `DeleteExpiredSessions` from `requireAuth` at most once per hour using a mutex-guarded timestamp, logging and ignoring failures.
- [x] 6.6 Add tests for revoke-one, revoke-all keeping the current session, a foreign reference being rejected, and a disabled admin's session being refused.

## 7. Audit

- [x] 7.1 Add `RecordAudit` inserting `actor_id`, `actor_name`, `action`, `subject`, `detail`, `ip` and `created_at`, and log insert failures without failing the caller.
- [x] 7.2 Record `login.success`, `login.failure` with the attempted username, and `logout` in the auth handlers.
- [x] 7.3 Record `entry.create` in the public create handler with a null actor and the client IP, and record `entry.update`, `entry.delete`, `entry.restore` and `entry.purge` in the corresponding admin handlers (entry.create is recorded from `internal/httpapi`; the separate unauthenticated web-form create path is out of this task's named scope).
- [x] 7.4 Record `settings.update`, `admin.create`, `admin.role`, `admin.disable`, `admin.enable`, `admin.delete`, `admin.password_reset`, `session.revoke` and `session.revoke_all`.
- [x] 7.5 Add `ListAudit` with action, actor, from and to filters and a total count for pagination.
- [x] 7.6 Add `GET /admin/audit` behind `requireOwner`, rendering 50 rows per page newest first with the filter form.
- [x] 7.7 Add tests for each recorded action, the filters, pagination and the editor 403.

## 8. Admin UI and wiring

- [x] 8.1 Register every new route in `internal/admin/admin.go` behind the correct wrapper.
- [x] 8.2 Add navigation entries for accounts, sessions and audit, visible by role.
- [x] 8.3 Add templates for the accounts page, the account page, the session list and the audit table.

## 9. Docs

- [x] 9.1 Add `docs/superpowers/specs/2026-11-10-admin-roles-audit-design.md` describing roles, the last-owner invariant, session revocation and the audit vocabulary.
- [x] 9.2 Link the new design entry from `docs/index.md`.
- [x] 9.3 Document the roles and the audit viewer in the `README.md` admin section.

## 10. Testing

- [x] 10.1 Run `go test ./internal/store ./internal/admin ./internal/auth`.
- [x] 10.2 Create an editor, confirm entry editing works and entry delete, trash purge and `/admin/admins` return 403.
- [x] 10.3 With one owner, confirm demote, disable and delete each return 409; create a second owner and confirm they succeed.
- [x] 10.4 Disable an admin with an active session and confirm the next request redirects to login.
- [x] 10.5 Confirm a login failure, an entry update and an admin creation each appear in `/admin/audit` with actor and IP.
- [x] 10.6 Run `go vet ./...` and `go test ./...`.
