## 1. Store reads for export

- [x] 1.1 Add an export row type carrying `id`, `word`, `meaning`, `person`, `created_at`, `has_photo`, `photo_content_type` and `photo_sha256`.
- [x] 1.2 Add a store method that lists every entry in ascending id order with a join to `entry_photos` for the photo metadata, without pagination.
- [x] 1.3 Add a store method that returns the display-derivative bytes and content type for one entry id, falling back to the original when the derivative columns are null.
- [x] 1.4 Add unit tests for the export row mapping and the derivative fallback.

## 2. Export handlers

- [x] 2.1 Add `GET /admin/export.json` writing the versioned envelope with `format_version`, `exported_at`, `entry_count`, `photo_count` and `entries`.
- [x] 2.2 Add `GET /admin/export.csv` writing the same fields with `encoding/csv` and a header row.
- [x] 2.3 Add `GET /admin/export.zip` streaming `manifest.json`, `entries.json` and `photos/{id}.jpg` with `archive/zip` over the response writer.
- [x] 2.4 Set `Content-Type`, `Content-Disposition: attachment` and `Cache-Control: no-store` on all three responses.
- [x] 2.5 Add handler tests for JSON shape, CSV header and row count, and the ZIP entry names and manifest counts.

## 3. Import parsing and preview

- [x] 3.1 Add `GET /admin/import` rendering an upload form that posts a file with `mode` and `conflict` fields and includes the CSRF token.
- [x] 3.2 Add `POST /admin/import` behind `withCSRF`, accepting a multipart `file` field plus `mode=preview|apply` and `conflict=skip|replace`.
- [x] 3.3 Parse either the JSON envelope or a ZIP containing `entries.json` and optional `photos/{id}.jpg`, rejecting an unknown `format_version`.
- [x] 3.4 Classify each row as create, skip-duplicate or conflict against the case-insensitive `(word, meaning)` index and render the three counts for `mode=preview` without writing.
- [x] 3.5 Enforce `maxImportBytes` (64 MiB), `maxImportEntries` (20,000) and the 10 MiB per-photo limit before opening a transaction, returning 413 with the cap name.
- [x] 3.6 Add parsing tests for malformed JSON, unknown `format_version`, a missing `entries.json`, and a photo whose entry id is absent from the file.

## 4. Import apply

- [x] 4.1 Insert new rows through the existing entry write path so the FTS row is created in the same transaction.
- [x] 4.2 Skip duplicates when `conflict=skip`, leaving the existing row and its photo unchanged.
- [x] 4.3 Update `person`, `created_at` and the photo of an existing row when `conflict=replace`, leaving `word` and `meaning` untouched.
- [x] 4.4 Roll back the whole transaction on any row error and return the failing row index and word.
- [x] 4.5 Render a result page reporting created, skipped, replaced and failed counts, with a link back to `/admin`.
- [x] 4.6 Add tests for insert-on-empty, skip-on-duplicate, replace-on-conflict, and rollback on a mid-file error.

## 5. Admin wiring

- [x] 5.1 Register the export and import routes in `internal/admin/admin.go` behind `requireAuth`, with CSRF on `POST /admin/import`.
- [x] 5.2 Add export and import links to the admin navigation template.
- [x] 5.3 Show the active caps and the display-derivative caveat on the import page.

## 6. Docs

- [x] 6.1 Add `docs/superpowers/specs/2026-11-10-ledger-export-design.md` describing the formats, caps and conflict modes.
- [x] 6.2 Link the new design entry from `docs/index.md`.
- [x] 6.3 Document the export routes and import modes in the `README.md` admin section.

## 7. Testing

- [x] 7.1 Run `go test ./internal/store ./internal/admin`.
- [x] 7.2 Run a manual round-trip: export ZIP from a populated instance, import into an empty instance, and compare entry and photo counts.
- [x] 7.3 Run the same ZIP import twice and confirm the second run reports every row as skipped.
- [x] 7.4 Confirm an unauthenticated request to each export route redirects to login and a POST without CSRF returns 403.
- [x] 7.5 Run `go vet ./...` and `go test ./...`.
