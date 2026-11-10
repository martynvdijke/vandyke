## Why

The `admins` table created in `003_admins.sql` has no role or scope column, so every row is a full administrator. Rows are only ever created by `CreateFirstAdmin` and `UpsertOIDCAdmin`, there is no admin-management page, and no password-change route exists even though `internal/auth/password.go` implements PBKDF2-HMAC-SHA256 with 600,000 iterations. Sessions in `admin_sessions` can be neither listed nor revoked, `DeleteExpiredSessions` runs only once at process start (`internal/admin/admin.go:100`), and there is no audit table: the nearest trail is the stdout request log from `internal/httpx`, which records method, path, status, duration and IP but no identity and is not persisted.

## What Changes

- Add an `admins.role` column with `owner` and `editor`, defaulting every existing row to `owner`, and an `admins.disabled_at` column for disabling an account.
- Restrict destructive entry actions and all admin-management actions to `owner`; editors keep entry edit, trash restore and settings access.
- Add an admin accounts page at `GET /admin/admins` with owner-only routes to create a local admin, change a role, disable, re-enable, delete and reset the password of another admin, enforcing that the last enabled owner cannot be demoted, disabled or deleted.
- Add `GET /admin/account` and `POST /admin/account/password` for self-service password change, plus an owner-initiated reset that revokes the target's sessions.
- Add `admin_sessions.session_ref`, `GET /admin/sessions`, `POST /admin/sessions/revoke` and `POST /admin/sessions/revoke-all`, make a disabled admin's sessions invalid, and sweep expired sessions at most once per hour on an authenticated request.
- Add an `admin_audit` table and write a record on login success and failure, logout, entry create/update/delete/restore/purge, settings changes and every admin change.
- Add `GET /admin/audit`, a paginated and filterable read-only audit viewer.

### Non-goals

- Per-entry or per-field permissions.
- Mapping external identity-provider groups or claims onto roles.
- Two-factor authentication.

## Capabilities

### New Capabilities

- `admin-roles`: Owner and editor roles, admin account management, password changes and session revocation.
- `admin-audit`: A persisted audit trail of authentication, entry, settings and admin actions with a filterable viewer.

## Impact

- New migrations `internal/store/migrations/011_admin_roles.sql` and `internal/store/migrations/012_admin_audit.sql`.
- `internal/store/store.go`: role and disabled columns on `Admin`, `session_ref` on `Session`, new admin and session methods, `RecordAudit` and `ListAudit`.
- `internal/admin`: new `admins.go`, `account.go`, `sessions.go` and `audit.go` handlers plus role checks in `requireAuth` and the mutating entry handlers.
- `internal/auth/password.go` is reused unchanged for hashing and verification.
- Admin templates and navigation for accounts, sessions and audit.
- `store.ErrAdminNotFound` gains its first caller in the admin-management handlers.
- `README.md` admin section and a new `docs/superpowers/specs/` entry linked from `docs/index.md`.
