## Context

The ledger is a single SQLite database accessed through `internal/store`, with photos stored as BLOBs in `entry_photos`: the original plus `display_*` (1600 px) and `thumb_*` (320 px) derivatives. The admin surface in `internal/admin` already owns authentication, CSRF, and the one existing original-photo download. There is no transfer format, no bulk read of entries, and no path that writes entries from a file.

## Goals / Non-Goals

**Goals:**
- Move the entire ledger, including photo bytes, out of an instance through the admin interface.
- Restore an export into an empty or populated instance without duplicating rows.
- Report what an import would do before it writes anything.
- Keep the binary self-contained by using `encoding/json`, `encoding/csv`, `archive/zip` and `database/sql` only.

**Non-Goals:**
- Background, scheduled or automatic backup.
- Remote storage targets.
- Bidirectional sync or conflict resolution beyond skip and replace.
- Exporting the compile-time seed as a separate artifact.

## Decisions

1. **One entry shape for every format**: JSON and CSV export use the same fields (`id`, `word`, `meaning`, `person`, `created_at`, `has_photo`, `photo_content_type`, `photo_sha256`), and `entries.json` inside the ZIP uses the same shape. JSON wraps the rows in `{"format_version":1,"exported_at":"...","entry_count":N,"photo_count":M,"entries":[...]}`; CSV writes the field names as its header row.

2. **ZIP streams with `archive/zip` over the response writer**: The archive is built in entry order, writing `manifest.json` first, then `entries.json`, then one `photos/{id}.jpg` per entry that has a photo. Only one photo is held in memory at a time, so the export has no total row cap; `photos/{id}.jpg` always contains the display derivative, which the pipeline stores as JPEG. A photo that predates the derivative columns falls back to the original bytes and is recorded with its actual content type in `entries.json`.

3. **`manifest.json` is the format contract**: It contains `format_version` (integer, currently `1`), `exported_at` (RFC 3339 UTC), `entry_count` and `photo_count`. An import reads `format_version` and refuses an unknown value rather than guessing.

4. **Import is two passes over the same upload**: `POST /admin/import` with `mode=preview` parses the file and returns would-create, would-skip-duplicate and would-conflict counts without writing. `mode=apply` re-parses the uploaded file and performs the same classification inside one transaction. The file is uploaded twice; that is accepted in exchange for a stateless preview with no server-side staging.

5. **Conflict means the unique `(word, meaning)` index**: A row whose case-insensitive `word` and `meaning` already exist is a duplicate. The default `conflict=skip` leaves the existing row untouched. `conflict=replace` updates the existing row's `person` and `created_at` and replaces its photo when the import carries one, but never changes `word` or `meaning`, which form the identity. Replace mode is reported as a conflict count in the preview.

6. **Import reuses the normal write path**: New rows and replacements go through the same transaction as `CreateEntry`/`UpdateEntry`, including the FTS insert, so search stays consistent. Photos are validated with the existing upload limits before insertion.

7. **Caps are constants, not environment variables**: `maxImportBytes` (64 MiB request body), `maxImportEntries` (20,000 rows) and the existing 10 MiB per-photo limit are checked after parsing and before the transaction opens. A request over any cap returns 413 with a message naming the cap.

8. **Access control and CSRF are the existing ones**: Every export and import route sits behind `requireAuth`; both `POST /admin/import` modes sit behind `withCSRF`. Export downloads set `Cache-Control: no-store` and a `Content-Disposition: attachment` filename that includes the export date.

## Risks / Trade-offs

- **[Large ZIP on a slow client]** -> The archive streams and holds one photo at a time; a client that disconnects simply ends the handler, leaving no partial file on the server.
- **[Import double upload]** -> The preview is stateless, so an operator who edits the file between preview and apply sees the apply response counts diverge from the preview; the apply result is the authoritative record.
- **[Replace overwrites curated metadata]** -> Replace is opt-in per request and limited to `person`, `created_at` and the photo; the preview reports the exact conflict count before the admin confirms.
- **[Photo quality loss on round-trip]** -> The ZIP carries the 1600 px display derivative, not the original; this is documented so an operator who needs originals uses the per-photo admin download.
- **[Duplicate detection is case-insensitive only]** -> Rows that differ by whitespace or punctuation are treated as distinct, matching the existing index behaviour rather than inventing normalisation.
