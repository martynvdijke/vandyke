## Context

`entries` is the only table that holds ledger content, with a companion `entry_photos` row keyed by `entry_id` and `ON DELETE CASCADE`, and an FTS5 trigram index `entries_fts` maintained by id. Deletion currently removes the FTS rows and the entry rows in one transaction (`internal/store/store.go:1260`), so the photo rows disappear with the entry. All reads go through a small set of store methods, and the public API, the web UI and the admin all share them.

## Goals / Non-Goals

**Goals:**
- Make deletion reversible without keeping a second copy of the data.
- Hide trashed entries from every public surface while keeping them inspectable by an admin.
- Restore and permanently purge single entries and batches of up to 100.
- Keep every change inside the existing migration and store structure.

**Non-Goals:**
- An approval or moderation workflow.
- Revision history for entry fields.
- Recording which admin performed a deletion.
- A background retention job.

## Decisions

1. **Soft delete is a nullable timestamp**: The new migration `009_entry_trash.sql` adds a nullable text `deleted_at` column to `entries` and an index `idx_entries_deleted_at` on it. A null value means live; a non-null RFC 3339 UTC value means trashed. No other column changes.

2. **Soft delete never touches the FTS index or the photos**: `SoftDeleteEntries` sets `deleted_at` on the selected rows and commits. `entries_fts` keeps its row and `entry_photos` keeps its BLOB, so restore is a second single-column update and needs no reindex or re-upload. `PurgeEntries` is the only hard delete: it removes the FTS rows first and then the entry rows in one transaction, and the existing cascade removes the photos.

3. **Filtering is one shared predicate**: `ledgerFilter` always restricts the query to rows with a null `deleted_at`, which covers the listing count, the page query and the FTS-backed search branch. `Stats`, `CountEntries`, `PersonCounts`, `EntryYears` and `Persons` add the same condition to their own queries. The `Stats` photo count changes from a plain photo count to a join that excludes trashed entries.

4. **Public reads fail with not-found, admin reads do not**: `GetEntry` gains the predicate and returns `store.ErrNotFound` for a trashed id. A new `GetEntryIncludingDeleted` returns the row regardless and is used by the admin edit page and the trash view. The public photo handler and the permalink handler check `GetEntry` first and return 404, while the admin-only `GET /admin/entries/{id}/photo/original` uses `GetEntryIncludingDeleted` so an admin can inspect the photo before restoring or purging.

5. **The unique `(word, meaning)` index is unchanged**: A trashed entry still occupies its case-insensitive `(word, meaning)` slot, so a new entry with the same spelling and correction is rejected as a duplicate until the trashed row is purged. This avoids a partial-index migration and a new class of restore conflicts, at the cost of one extra purge step when re-adding a deleted spelling.

6. **Trash actions are explicit and capped**: `GET /admin/trash` lists trashed entries newest-deleted first with `id`, `word`, `meaning`, `person`, `created_at` and `deleted_at`. `POST /admin/trash/restore` and `POST /admin/trash/purge` accept an `ids` form field, deduplicate, reject more than 100 ids with 400 to match `handleDelete`, and run behind `withCSRF`. A single action posts a one-element list.

7. **No retention job**: The binary has no scheduler, so purging is only ever an admin action. The trash view states the current trashed count so an operator can decide when to purge.

## Risks / Trade-offs

- **[Database growth]** -> Trashed rows keep their photo BLOBs until purged; the trash view shows the count and purge is one bulk action.
- **[Spelling blocked by a trashed row]** -> The duplicate response tells the operator the spelling and correction already exist, and the trash view makes the blocking row findable by word so it can be restored or purged before a replacement is added.
- **[Accidental purge]** -> Purge is a distinct action on the trash page, requires CSRF, is capped at 100 ids, and is never triggered automatically.
- **[Admin original-photo route serves trashed bytes]** -> The route is admin-only and exists to support inspecting an entry before restore or purge; public derivative routes still return 404.
- **[Restore of an id already restored]** -> Restore is idempotent: it only touches rows whose `deleted_at` is non-null, and the result count reflects rows actually restored.
