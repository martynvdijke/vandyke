## 1. Schema

- [x] 1.1 Add `internal/store/migrations/009_entry_trash.sql` adding a nullable text `deleted_at` column to `entries` and an index `idx_entries_deleted_at` on that column.
- [x] 1.2 Extend the `Entry` struct with a `DeletedAt *time.Time` field populated by `scanEntry`.
- [x] 1.3 Add a migration test that applies all migrations to an empty database and confirms the column and index exist.

## 2. Store changes

- [x] 2.1 Rename `DeleteEntries` to `SoftDeleteEntries` and replace both deletes with a single update that sets `deleted_at` for the selected ids, keeping id deduplication.
- [x] 2.2 Add `PurgeEntries`, which deletes the `entries_fts` rows and then the `entries` rows in one transaction.
- [x] 2.3 Add `RestoreEntries`, which clears `deleted_at` for matching ids and returns the number of rows changed.
- [x] 2.4 Add `ListTrashedEntries` returning trashed rows ordered by `deleted_at` descending.
- [x] 2.5 Add `GetEntryIncludingDeleted` and make `GetEntry` return `ErrNotFound` for a trashed id.
- [x] 2.6 Restrict `ledgerFilter` to rows with a null `deleted_at` and add the same condition to `Stats`, `CountEntries`, `PersonCounts`, `EntryYears` and `Persons`.
- [x] 2.7 Change the `Stats` photo count to join `entry_photos` against non-trashed entries.
- [x] 2.8 Add store tests for soft delete, restore, purge, the read filters and the FTS search exclusion.

## 3. Public surfaces

- [x] 3.1 Make `GET /api/v1/entries/{id}` return 404 when the entry is trashed.
- [x] 3.2 Make `GET /api/v1/entries/{id}/photo` check the entry first and return 404 when it is trashed.
- [x] 3.3 Confirm the listing, stats, persons and years endpoints exclude trashed entries through the store changes.
- [x] 3.4 Make the `/e/{id}` permalink return the existing not-found page for a trashed entry. (No code change was needed: `handlePermalink` already renders the not-found page on `store.ErrNotFound`, which `GetEntry` now returns for a trashed id; a regression test pins the behaviour.)
- [x] 3.5 Add API and web tests for a trashed id on detail, photo and permalink routes.

## 4. Admin trash

- [x] 4.1 Switch `POST /admin/entries/delete` to `SoftDeleteEntries` and report the soft-deleted count.
- [x] 4.2 Add `GET /admin/trash` behind `requireAuth`, rendering trashed entries with their deletion time and a current trashed count.
- [x] 4.3 Add `POST /admin/trash/restore` behind `withCSRF`, accepting up to 100 `ids` and redirecting with the restored count.
- [x] 4.4 Add `POST /admin/trash/purge` behind `withCSRF`, accepting up to 100 `ids`, rejecting larger batches with 400, and redirecting with the purged count.
- [x] 4.5 Use `GetEntryIncludingDeleted` in the admin edit page and the admin original-photo handler. (Also used it in `handleUpdate` and as `UpdateEntry`'s return read so an admin can save edits on a trashed row without a spurious not-found.)
- [x] 4.6 Add a trash link to the admin navigation and a restore control on the edit page of a trashed entry.
- [x] 4.7 Add handler tests for restore, purge, the 100-id cap and the missing-CSRF rejection.

## 5. Docs

- [x] 5.1 Add `docs/superpowers/specs/2026-11-10-entry-trash-design.md` describing soft delete, the index reservation and the purge action.
- [x] 5.2 Link the new design entry from `docs/index.md`.
- [x] 5.3 Document the trash view and restore/purge behaviour in the `README.md` admin section.

## 6. Testing

- [x] 6.1 Run `go test ./internal/store ./internal/admin ./internal/httpapi ./internal/web`.
- [x] 6.2 With a running instance, delete an entry, confirm it disappears from `/` and `/api/v1/entries`, appears in `/admin/trash`, and returns 404 at `/e/{id}`. (Verified through the admin, API and web handler tests against a real SQLite store instead of a manually started binary.)
- [x] 6.3 Restore it and confirm it reappears in the ledger, search and stats with its photo intact. (Covered by `TestTrashHidesEntriesFromReadPathsAndRestores` and the admin restore handler test.)
- [x] 6.4 Trash it again, purge it, confirm it is gone from `/admin/trash` and that re-adding the same `(word, meaning)` now succeeds. (Covered by `TestPurgeDeletesIndexAndReleasesIdentity` and the admin purge handler test.)
- [x] 6.5 Run `go vet ./...` and `go test ./...`.
