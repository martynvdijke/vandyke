## ADDED Requirements

### Requirement: Soft deletion of entries
The system SHALL mark a deleted entry by setting `entries.deleted_at` to the current UTC time instead of removing the row, its FTS record or its photo.

#### Scenario: Admin deletes an entry
- **WHEN** an admin deletes an entry from the ledger or the bulk delete form
- **THEN** the entry row keeps every field, `deleted_at` is set, and its `entries_fts` row and `entry_photos` row remain

#### Scenario: Soft-deleted entry still reserves its identity
- **WHEN** an admin or API client creates an entry whose case-insensitive `word` and `meaning` match a trashed entry
- **THEN** the create is rejected as a duplicate until the trashed entry is purged

### Requirement: Trashed entries are hidden from public reads
The system SHALL exclude entries with a non-null `deleted_at` from listing, search, statistics, person counts, entry years, the person list, the entry detail route, the photo route and the `/e/{id}` permalink.

#### Scenario: Trashed entry missing from the ledger
- **WHEN** an entry is trashed
- **THEN** it no longer appears in `GET /api/v1/entries`, in a query that would match its word or meaning, in `GET /api/v1/stats`, in `GET /api/v1/persons`, or in the year filter options

#### Scenario: Trashed entry returns 404 publicly
- **WHEN** a client requests `GET /api/v1/entries/{id}`, `GET /api/v1/entries/{id}/photo` or `/e/{id}` for a trashed entry
- **THEN** the response is 404 and no entry content or photo bytes are returned

### Requirement: Admin trash view
The system SHALL provide `GET /admin/trash`, restricted to an authenticated admin, listing trashed entries newest-deleted first with `id`, `word`, `meaning`, `person`, `created_at` and `deleted_at`.

#### Scenario: View the trash
- **WHEN** an authenticated admin opens `/admin/trash`
- **THEN** the page lists every trashed entry with its deletion time and shows the current trashed count

#### Scenario: Empty trash
- **WHEN** no entry is trashed
- **THEN** the page states that the trash is empty and offers no restore or purge controls

### Requirement: Restore single and bulk
The system SHALL provide `POST /admin/trash/restore` behind CSRF, accepting up to 100 `ids`, clearing `deleted_at` for the matching trashed entries and returning them to every read path.

#### Scenario: Restore one entry
- **WHEN** an admin posts a single id to `/admin/trash/restore`
- **THEN** `deleted_at` becomes null, the entry reappears in the ledger and search, and its photo is served again

#### Scenario: Restore a batch
- **WHEN** an admin posts 100 ids to `/admin/trash/restore`
- **THEN** every matching trashed entry is restored and the response reports the restored count

#### Scenario: Restore is idempotent
- **WHEN** an id that is already live is posted to `/admin/trash/restore`
- **THEN** no row changes and the restored count does not include it

### Requirement: Permanent purge single and bulk
The system SHALL provide `POST /admin/trash/purge` behind CSRF, accepting up to 100 `ids`, permanently deleting the FTS rows, entry rows and cascaded photos, and SHALL reject a larger batch with 400.

#### Scenario: Purge an entry
- **WHEN** an admin posts a trashed id to `/admin/trash/purge`
- **THEN** the `entries_fts` row and the `entries` row are deleted in one transaction, the photo row cascades away, and the entry no longer appears in the trash

#### Scenario: Batch above the cap
- **WHEN** an admin posts more than 100 ids to `/admin/trash/purge`
- **THEN** the request returns 400 and no row is deleted

#### Scenario: Purge is never automatic
- **WHEN** an entry has been trashed for any length of time
- **THEN** it remains in the trash until an admin explicitly purges it, because no retention job runs
