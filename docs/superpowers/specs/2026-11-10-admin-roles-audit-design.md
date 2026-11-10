# Admin roles, account management and the audit trail

Date: 2026-11-10

This design adds two admin roles, owner-initiated account management,
self-service password changes, session listing and revocation, and a persisted
audit trail. It builds on the single-tier admin model from the 2026-10-27
admin/OIDC design without changing how sessions are authenticated.

## Roles

Every `admins` row carries a `role` of `owner` or `editor`.

| Role | Can do |
| --- | --- |
| `owner` | Everything an editor can do, plus entry deletion, trash purge, admin management and the audit viewer. |
| `editor` | Create and edit entries, restore trashed entries, manage settings, change its own password and manage its own sessions. |

`011_admin_roles.sql` adds the column with a default of `editor` (the least
privileged value), then promotes every pre-existing row to `owner` so upgrading
an instance never locks its operator out. New local admins default to `editor`;
the creating owner may select `owner`. A fresh instance's first local admin
(`CreateFirstAdmin`) and its first OIDC login (`UpsertOIDCAdmin`) are created as
`owner` so at least one account can always manage admins. Later OIDC identities
are created as `editor`, because an identity provider login should not silently
gain owner rights on an established instance.

## Owner-only enforcement

`requireOwner` wraps `requireAuth` and returns 403 with a plain message, not a
redirect, when the authenticated admin is an editor. It guards:

- `POST /admin/entries/delete`
- `POST /admin/trash/purge`
- `GET /admin/admins` and every `POST /admin/admins*` route
- `GET /admin/audit`

Editors keep entry editing, trash restore, settings, account and session
routes. The role is read from the database row on every request, so a role
change takes effect on the next request without re-login. The admin templates
hide the controls an editor cannot use, but the server-side check is the
authority.

## The last-owner invariant

Demoting, disabling or deleting an owner must never leave zero enabled owners.
Each such mutation runs inside one transaction and counts rows whose
`role = 'owner' AND disabled_at IS NULL`; a count below two aborts the mutation
with `store.ErrLastOwner`, which the handler renders as 409. Because the count
and the write share a transaction, two concurrent requests cannot both remove
the final two owners.

The handlers additionally refuse an owner acting on their own account where the
spec forbids it: self-demotion to a non-owner role, self-disable and
self-delete all return 409. This is a second guard, not a replacement for the
database invariant.

## Account lifecycle

- **Disable** sets `admins.disabled_at`; **enable** clears it. Disabling is the
  reversible action. `GetAdminByUsername` and `GetSession` both filter
  `disabled_at IS NULL`, so a disabled admin is refused at sign-in and any
  cookie they already hold stops working on the next request.
- **Delete** removes the admin row; `admin_sessions` cascades. The last enabled
  owner cannot be deleted.
- **Password change** at `POST /admin/account/password` requires the current
  password, verifies it with `auth.VerifyPassword`, hashes the new value with the
  existing PBKDF2-HMAC-SHA256 helper (600,000 iterations, same encoding), and
  revokes every other session of that admin while keeping the requesting one.
- **Password reset** at `POST /admin/admins/{id}/password` is owner-only,
  requires no current password, and revokes all of the target's sessions.
- Both password routes enforce a 12-character minimum and write no password,
  hash or token to the audit trail.

## Sessions

Each new session gets a 16-byte hex `session_ref` in addition to its token hash.
The value is a handle only: the cookie remains the secret and the database still
stores only the SHA-256 token hash.

- `GET /admin/sessions` lists the current admin's sessions with reference,
  creation, expiry and whether it is the current session.
- `POST /admin/sessions/revoke` deletes by `session_ref`. The delete is scoped to
  the acting admin; an owner may revoke any reference. A reference that does not
  match is refused with 403 and nothing is deleted.
- `POST /admin/sessions/revoke-all` deletes every session of the acting admin
  except the one making the request.
- Pre-migration sessions have an empty reference and are listed as legacy; they
  can only be removed by revoke-all and expire within seven days.
- `requireAuth` sweeps expired sessions at most once per hour, guarded by an
  in-process mutex and timestamp. Failures are logged and ignored; the startup
  sweep is unchanged.

## Audit trail

`012_admin_audit.sql` creates an append-only table with `actor_id` (nullable, no
foreign key), `actor_name` (snapshot), `action`, `subject`, `detail`, `ip` and
`created_at`, indexed on `created_at`, `action` and `actor_id`. `RecordAudit`
inserts one row; a failure is logged to stderr and never rolls back or blocks the
action it describes.

The action vocabulary is fixed:

```
login.success  login.failure  logout
entry.create   entry.update   entry.delete   entry.restore  entry.purge
settings.update
admin.create   admin.role     admin.disable  admin.enable   admin.delete
admin.password_reset
session.revoke session.revoke_all
```

- A failed login records the attempted username with a null `actor_id`.
- `entry.create` from the public API records the new id, the word and the client
  IP with a null actor.
- Audit rows never contain password hashes, session tokens, CSRF tokens or
  secret values. `settings.update` records only the names of changed keys.
- Deleting an admin leaves its historical rows intact because `actor_id` has no
  foreign key and `actor_name` was snapshotted at write time.

`GET /admin/audit` is owner-only, read-only, shows 50 rows per page newest first,
and filters on `action`, `actor`, `from` and `to` with substring matching on the
first two and date bounds on the last two. No route edits or deletes a row.
