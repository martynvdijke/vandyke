## Why

`DeleteEntries` (`internal/store/store.go:1260`) deletes the FTS rows and then the `entries` rows in one transaction, and `entry_photos` cascades on delete. The `entries` table has no `deleted_at` column and no read path is aware of one, so a mis-clicked admin bulk delete removes an entry and its photo permanently. The admin bulk delete accepts at most 100 ids, and no trash view, restore action, purge action or retention window exists.

## What Changes

- Add a nullable `deleted_at` column to `entries` in a new embedded migration, with an index for the trash listing.
- Replace hard deletion with soft deletion in `DeleteEntries`, renaming it `SoftDeleteEntries`, and keep the FTS rows so a restore needs no reindex.
- Exclude soft-deleted rows from `ListEntries` (including the FTS-backed search join), `Stats`, `CountEntries`, `PersonCounts`, `EntryYears`, `Persons` and the public `GetEntry`.
- Add `GET /admin/trash` listing trashed entries with their deletion time, plus `POST /admin/trash/restore` and `POST /admin/trash/purge` for single and bulk operations, each capped at 100 ids and CSRF-protected.
- Return 404 from every public API and permalink surface for a trashed entry while keeping it readable by an admin.
- Keep purging an explicit admin action, because the binary has no scheduler to run a retention job.

### Non-goals

- A multi-step approval workflow before an entry becomes trash.
- Entry revision history or field-level undo.
- Per-user attribution of who deleted an entry.

## Capabilities

### New Capabilities

- `entry-trash`: Soft deletion, an admin trash view, restore, and explicit permanent purge for ledger entries.

## Impact

- New migration `internal/store/migrations/009_entry_trash.sql`.
- `internal/store/store.go`: `DeleteEntries` becomes `SoftDeleteEntries`; new `ListTrashedEntries`, `RestoreEntries`, `PurgeEntries`, `GetEntryIncludingDeleted`, and `deleted_at` filters in the existing read queries.
- `internal/httpapi/api.go` and `internal/web`: trashed entries return 404 on detail, photo and permalink routes.
- `internal/admin/entries.go` and `internal/admin/admin.go`: delete switches to soft delete; new trash routes.
- Admin templates for the trash view and its bulk actions.
- `README.md` admin section and a new `docs/superpowers/specs/` entry linked from `docs/index.md`.
