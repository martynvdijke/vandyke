## Context

Admin authentication is a single tier backed by `admins` and `admin_sessions` in SQLite. Local passwords are PBKDF2-HMAC-SHA256 hashes from `internal/auth/password.go`; OIDC admins are provisioned by `UpsertOIDCAdmin` with an issuer and subject. Sessions carry a token hash, a CSRF token and `expires_at`, are read on every authenticated request by `requireAuth`, and are expired only by a startup sweep. `internal/httpx` logs one line per request to stdout with no identity, so nothing durable records who changed what.

## Goals / Non-Goals

**Goals:**
- Distinguish an owner who can destroy data and manage admins from an editor who maintains the ledger.
- Give an owner the tools to create, disable, re-enable and delete local admins without shell access.
- Let any admin change their own password and review or revoke their sessions.
- Persist an append-only trail of security-relevant actions with actor, subject, IP and time.

**Non-Goals:**
- Fine-grained per-entry permissions.
- IdP group or claim mapping.
- Two-factor authentication.
- Tamper-evident or externally shipped logs.

## Decisions

1. **One role column, two values**: `011_admin_roles.sql` adds a text `role` column to `admins` defaulting to `editor`, then sets every existing row to `owner` so pre-existing admins keep full rights. The same migration adds a nullable `disabled_at` text column to `admins` and a `session_ref` text column to `admin_sessions` defaulting to empty, with a unique partial index over non-empty references. `CreateFirstAdmin` and a first-login OIDC claim insert `owner`; every other new row is `editor` unless the creating owner picks `owner`.

2. **Owner-only actions are checked in one helper**: A `requireOwner` wrapper around `requireAuth` guards entry delete, trash purge, all `/admin/admins*` routes and the audit viewer. Editors keep entry edit, trash restore, settings, account and session routes. A forbidden request returns 403 rather than redirecting, so the reason is visible.

3. **The last enabled owner is protected by a count inside the transaction**: Before demoting, disabling or deleting an owner, the store counts admins whose role is `owner` and whose `disabled_at` is null; a result below two aborts with a typed error the handler renders. A second owner must exist before the first can be removed.

4. **Deletion is a hard delete of the admin row; disabling is the reversible action**: `POST /admin/admins/{id}/disable` sets `disabled_at` and `POST /admin/admins/{id}/enable` clears it. Deleting an admin removes the row, and `admin_sessions` cascades. `GetAdminByUsername` and `GetSession` exclude disabled admins so login and existing sessions fail immediately after disabling.

5. **Passwords use the existing primitives**: `POST /admin/account/password` requires the current password, verifies it with `auth.VerifyPassword`, hashes the new one with `auth.HashPassword`, and revokes the admin's other sessions. `POST /admin/admins/{id}/password` is the owner reset, requires no current password, and also revokes the target's sessions. Both require a minimum length of 12 characters.

6. **Session identity for revocation is a random reference**: Each new session stores a 16-byte hex `session_ref` alongside the token hash. The list shows `session_ref`, `created_at`, `expires_at` and whether it is the current session; revoking posts the reference. Sessions created before the migration have an empty reference and can only be removed by revoke-all. `POST /admin/sessions/revoke-all` deletes every session of the current admin except the one making the request.

7. **The sweep is opportunistic, not scheduled**: Because there is no background worker, `requireAuth` calls `DeleteExpiredSessions` at most once per hour, tracked by an in-process timestamp guarded by a mutex. A failed sweep is logged and ignored, and the startup sweep stays in place.

8. **Audit writes are best-effort and never block the action**: `RecordAudit` inserts one row into `admin_audit` (`actor_id`, `actor_name`, `action`, `subject`, `detail`, `ip`, `created_at`). A write failure is logged to stderr and does not roll back the underlying change. `actor_id` has no foreign key so deleting an admin does not erase history; `actor_name` snapshots the username at write time. A failed login with an unknown username and an `entry.create` from the unauthenticated API or web form both store a null `actor_id` with the client IP.

9. **The audit viewer is read-only and paginated**: `GET /admin/audit` accepts `action`, `actor`, `from`, `to` and `page`, filters with substring matching and date bounds, and renders 50 rows per page newest first. There is no edit or delete route for audit rows.

10. **Audit actions are a fixed vocabulary**: `login.success`, `login.failure`, `logout`, `entry.create`, `entry.update`, `entry.delete`, `entry.restore`, `entry.purge`, `settings.update`, `admin.create`, `admin.role`, `admin.disable`, `admin.enable`, `admin.delete`, `admin.password_reset`, `session.revoke`, `session.revoke_all`. The entry restore and purge values are recorded only when the entry-trash capability is present.

## Risks / Trade-offs

- **[Role check drift across handlers]** -> A single `requireOwner` wrapper plus a table-driven test asserting every destructive route rejects an editor.
- **[Locking out the only owner]** -> The last-owner count runs inside the same transaction as the demotion, disable or delete, so two concurrent requests cannot both succeed.
- **[Audit table growth]** -> Rows are small and indexed by `created_at`; the viewer paginates and the design deliberately adds no automatic retention, so an operator can archive with SQL if needed.
- **[Best-effort audit loses a record]** -> A failed insert is logged; losing an audit row never blocks or reverts the action it describes.
- **[Pre-migration sessions cannot be revoked individually]** -> They are shown without a reference and expire within seven days, and revoke-all covers the current admin's full set.
- **[Disabled admin keeps a valid cookie]** -> `GetSession` joins on `disabled_at IS NULL`, so the next authenticated request redirects to login.
