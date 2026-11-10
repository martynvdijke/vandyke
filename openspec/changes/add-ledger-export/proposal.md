## Why

Every ledger entry exists only inside the SQLite file at `DB_PATH`, and neither `cmd/` nor `internal/` contains an export, import or backup path. The only way to get photo bytes out today is the admin-only `GET /admin/entries/{id}/photo/original` handler, one file at a time, while `/api/v1/entries/{id}/photo` serves only the `display` and `thumb` derivatives and rejects `size=original` with 400. The 78 seed rows are embedded at compile time and imported once into an empty table (`internal/store/store.go:360`), so a lost volume cannot be re-seeded for entries added after that import.

## What Changes

- Add `GET /admin/export.json`, returning a versioned JSON envelope with every entry and its photo metadata.
- Add `GET /admin/export.csv`, returning the same fields as a CSV download.
- Add `GET /admin/export.zip`, streaming an archive containing `entries.json`, `manifest.json` and `photos/{id}.jpg` taken from the display derivative.
- Add `GET /admin/import` for the upload form and `POST /admin/import` for a dry-run preview and the applying import, with `mode=preview|apply` and `conflict=skip|replace`.
- Make the default import insert-or-skip against the existing case-insensitive unique `(word, meaning)` index, and overwrite an existing row only when `conflict=replace` is requested.
- Document import size and row caps and enforce them before any write.

### Non-goals

- Scheduled, automatic or on-shutdown backups.
- Remote or object-storage targets such as S3.
- Merging two divergent ledgers beyond insert-or-skip or replace of a matching row.
- Replacing or extending the compile-time seed import.

## Capabilities

### New Capabilities

- `ledger-export`: Admin-only JSON, CSV and ZIP export of the ledger plus a previewed, conflict-aware import.

## Impact

- New `internal/admin/export.go` and `internal/admin/import.go`.
- New export and import routes in `internal/admin/admin.go`.
- New read and insert helpers in `internal/store/store.go` and the entry/photo types it returns.
- Admin navigation template for the export and import pages.
- `README.md` admin section and a new `docs/superpowers/specs/` entry linked from `docs/index.md`.
