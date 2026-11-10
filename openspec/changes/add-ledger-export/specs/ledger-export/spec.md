## ADDED Requirements

### Requirement: JSON export of the ledger
The system SHALL expose `GET /admin/export.json`, restricted to an authenticated admin, returning every entry in a versioned envelope with `format_version`, `exported_at`, `entry_count`, `photo_count` and `entries`.

#### Scenario: Export a populated ledger as JSON
- **WHEN** an authenticated admin requests `GET /admin/export.json`
- **THEN** the response is `application/json` with `Content-Disposition: attachment`, `format_version` equal to `1`, and one `entries` element per stored entry carrying `id`, `word`, `meaning`, `person`, `created_at`, `has_photo`, `photo_content_type` and `photo_sha256`

#### Scenario: Unauthenticated export is refused
- **WHEN** a client without an admin session requests `GET /admin/export.json`
- **THEN** the request is redirected to the admin login instead of returning ledger data

### Requirement: CSV export of the ledger
The system SHALL expose `GET /admin/export.csv`, restricted to an authenticated admin, returning the same entry fields as the JSON export as RFC 4180 CSV with a header row.

#### Scenario: Export as CSV
- **WHEN** an authenticated admin requests `GET /admin/export.csv`
- **THEN** the response is `text/csv` with `Content-Disposition: attachment`, the first row names the entry fields, and each following row corresponds to one entry

#### Scenario: Values containing commas and quotes
- **WHEN** an entry's `meaning` contains a comma, a double quote or a newline
- **THEN** the field is quoted and internal quotes are doubled so the file parses as CSV

### Requirement: Streaming ZIP archive with photos
The system SHALL expose `GET /admin/export.zip`, restricted to an authenticated admin, streaming an archive that contains `manifest.json`, `entries.json` and `photos/{id}.jpg` for every entry that has a photo.

#### Scenario: Download the archive
- **WHEN** an authenticated admin requests `GET /admin/export.zip`
- **THEN** the archive contains `manifest.json` with `format_version`, `exported_at`, `entry_count` and `photo_count`, an `entries.json` matching the JSON export shape, and one `photos/{id}.jpg` per entry with a photo

#### Scenario: Photo derivative source
- **WHEN** an entry has a display derivative
- **THEN** its `photos/{id}.jpg` contains the display derivative bytes, and when the derivative columns are null the original bytes are written instead and the actual content type is recorded in `entries.json`

### Requirement: Import dry-run preview
The system SHALL provide `GET /admin/import` for the upload form and `POST /admin/import` with `mode=preview`, which parses the uploaded file and reports would-create, would-skip-duplicate and would-conflict counts without writing any row.

#### Scenario: Preview an import
- **WHEN** an authenticated admin uploads an export with `mode=preview`
- **THEN** the response shows the three counts and the database is unchanged

#### Scenario: Preview reports conflicts
- **WHEN** the uploaded file contains a row whose case-insensitive `word` and `meaning` already exist
- **THEN** that row is counted as a conflict and the preview states whether `conflict=skip` or `conflict=replace` was selected

### Requirement: Conflict-aware import apply
The system SHALL apply an import only with `mode=apply` and CSRF, inserting new rows, skipping duplicates by default, and overwriting an existing row only when `conflict=replace` is explicitly requested.

#### Scenario: Apply into an empty ledger
- **WHEN** an admin applies an export to an instance with no entries
- **THEN** every row is inserted with its `person`, `created_at` and photo, and the FTS index contains the new rows

#### Scenario: Duplicate is skipped
- **WHEN** an admin applies a row whose case-insensitive `word` and `meaning` already exist with `conflict=skip`
- **THEN** the existing row and its photo are left unchanged and the row is reported as skipped

#### Scenario: Existing row is replaced
- **WHEN** an admin applies a matching row with `conflict=replace`
- **THEN** `person`, `created_at` and the photo of the existing row are updated, while `word` and `meaning` keep their stored values

#### Scenario: Failure rolls back
- **WHEN** any row fails during apply
- **THEN** the transaction is rolled back, no row from that file is written, and the response names the failing row

### Requirement: Import caps and format version
The system SHALL reject an import that exceeds 64 MiB per request, 20,000 entries, or 10 MiB per photo, and SHALL reject an unknown `format_version` before writing.

#### Scenario: Oversized upload
- **WHEN** an uploaded file exceeds the 64 MiB request cap or declares more than 20,000 entries
- **THEN** the request returns 413 naming the exceeded cap and no row is written

#### Scenario: Unsupported format version
- **WHEN** `manifest.json` or the JSON envelope declares a `format_version` other than `1`
- **THEN** the import is refused with a message naming the supported version
