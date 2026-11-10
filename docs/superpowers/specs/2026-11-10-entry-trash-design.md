# Entry trash: soft deletion, restore and purge

**Date:** 2026-11-10
**Status:** Implemented
**Change:** `openspec/changes/add-entry-trash`

## Purpose

Before this change the admin bulk delete removed an `entries` row, its FTS row
and its cascaded `entry_photos` row in one transaction. A mis-click destroyed
ledger content with no way back. This design makes deletion reversible without
keeping a second copy of the data.

## Data model

Migration `009_entry_trash.sql` adds one nullable `TEXT` column:

```sql
ALTER TABLE entries ADD COLUMN deleted_at TEXT;
CREATE INDEX IF NOT EXISTS idx_entries_deleted_at ON entries (deleted_at);
```

- `deleted_at IS NULL` — live entry.
- `deleted_at` — RFC 3339 UTC timestamp set when the entry moved to the trash.

No other column changes. `entry_photos` and `entries_fts` are untouched by a
soft delete, so a restore is a single-column update with no reindex or photo
re-upload.

## Read-path filtering

Every existing read path adds `deleted_at IS NULL`:

- `ledgerFilter` covers the listing count, the page query and both the FTS and
  `LIKE` search branches.
- `Stats` (aggregates, by-year, top persons, and the photo count via a join to
  `entry_photos`), `CountEntries`, `EntryYears`, `Persons` and `PersonCounts`.
- `GetEntry` returns `ErrNotFound` for a trashed id. A new
  `GetEntryIncludingDeleted` reads the row regardless and backs the admin edit
  page, the trash view and the admin original-photo download.
- The public API detail and photo handlers and the `/e/{id}` permalink all see
  the `ErrNotFound` and return 404.

## Trash actions

- `SoftDeleteEntries` sets `deleted_at` for the selected live ids (deduplicated)
  and returns the number of rows changed.
- `RestoreEntries` clears `deleted_at` for matching trashed ids only, so a live
  id changes nothing and the count reports rows actually restored.
- `PurgeEntries` is the only hard delete. It removes the FTS rows first and then
  the entry rows in one transaction; the existing `ON DELETE CASCADE` removes
  the photo. Live ids are ignored.

Admin routes, all behind `requireAuth`, with CSRF on the two POSTs:

| Route | Behavior |
| --- | --- |
| `GET /admin/trash` | Lists trashed entries newest-deleted first with word, meaning, person, logged and deletion times. |
| `POST /admin/trash/restore` | Accepts up to 100 `ids`; redirects with the restored count. |
| `POST /admin/trash/purge` | Accepts up to 100 `ids`; over the cap is a 400 and nothing is deleted. |

Both actions redirect back to `/admin/trash` with a flash count. The bulk delete
form (`POST /admin/entries/delete`) now calls `SoftDeleteEntries`.

## Identity reservation

The unique `(word, meaning)` index is unchanged, so a trashed row still occupies
its case-insensitive slot. Re-adding the same misspelling and correction is a
duplicate until the trashed row is purged. This avoids a partial-index migration
and a new class of restore conflicts; the cost is one purge step when replacing
a deleted spelling. The duplicate message and the trash view (searchable by
word via the ledger) make the blocking row findable.

## No retention job

The binary has no scheduler or worker goroutine, so purging is only ever an
explicit admin action. The trash page shows the current trashed count so an
operator can decide when to clean up. Trashed photo BLOBs therefore remain until
purged — the documented trade-off for making deletion reversible.

## Testing

- Migration test asserts the column is nullable `TEXT` and the index exists.
- Store tests cover exclusion from list, FTS search, stats, years, persons and
  person counts; restore idempotence; purge removing FTS and photo rows; and the
  identity reservation until purge.
- API tests cover 404 on detail and photo for a trashed entry plus excluded
  list, stats and persons responses.
- Web tests cover the permalink not-found page and the index staying clean.
- Admin tests cover soft delete through the bulk form, the trash page, restore,
  purge, the 100-id cap, missing-CSRF rejection and inspecting a trashed entry.
