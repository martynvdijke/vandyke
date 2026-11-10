# Ledger export and import design

Date: 2026-11-10

## Goals

- Move every entry and its photo out of an instance through the admin interface.
- Restore an export into an empty or populated instance without duplicating rows.
- Report what an import would do before any row is written.
- Keep the binary self-contained with `encoding/json`, `encoding/csv`,
  `archive/zip` and `database/sql` only.

## Non-goals

- Scheduled or automatic backups, remote storage targets, or bidirectional sync.
- Exporting the compile-time seed as a separate artifact.
- Conflict resolution beyond skip and replace.

## Export formats

Every export route requires an admin session, returns
`Content-Disposition: attachment` with a filename containing the export date,
and sets `Cache-Control: no-store`.

### `GET /admin/export.json`

A versioned envelope:

```json
{
  "format_version": 1,
  "exported_at": "2026-11-10T12:00:00Z",
  "entry_count": 78,
  "photo_count": 12,
  "entries": [
    {
      "id": 1,
      "word": "kast systeem",
      "meaning": "kassasysteem",
      "person": "",
      "created_at": "2017-10-02T00:00:00Z",
      "has_photo": true,
      "photo_content_type": "image/jpeg",
      "photo_sha256": "…"
    }
  ]
}
```

### `GET /admin/export.csv`

RFC 4180 CSV with the same field names as the header row:
`id,word,meaning,person,created_at,has_photo,photo_content_type,photo_sha256`.
`encoding/csv` quotes fields containing commas, quotes or newlines and doubles
internal quotes.

### `GET /admin/export.zip`

A streaming `archive/zip` archive written directly to the response, holding one
photo in memory at a time:

1. `manifest.json` — `format_version`, `exported_at`, `entry_count`,
   `photo_count`.
2. `entries.json` — the JSON export envelope.
3. `photos/{id}.jpg` — one file per entry with a photo.

`photos/{id}.jpg` contains the 1600 px display derivative. When the derivative
columns are null the original bytes are written instead, and
`photo_content_type` records the actual original type. A round-trip therefore
loses the original resolution; use the per-photo admin download when originals
are required.

## Import

`GET /admin/import` renders the upload form. `POST /admin/import` accepts a
multipart `file` field plus `mode=preview|apply` and `conflict=skip|replace`,
and requires the session CSRF token. The upload may be the JSON envelope or a
ZIP archive containing `entries.json` and optional `photos/{id}.jpg` files. An
archive with a photo whose entry id is absent is rejected.

### Preview

`mode=preview` parses the file and classifies every row against the
case-insensitive unique `(word, meaning)` index:

- **create** — no stored row matches.
- **skip** — the same `(word, meaning)` appears earlier in the uploaded file.
- **conflict** — a stored row matches; the page states whether `conflict=skip`
  or `conflict=replace` was selected.

Preview opens no transaction and writes nothing.

### Apply

`mode=apply` re-parses the uploaded file and performs the same classification
inside one transaction:

- New rows are inserted through the same insert path as `CreateEntry`, so the
  FTS row is written in the same transaction.
- With `conflict=skip` (default), a matching row is left untouched.
- With `conflict=replace`, the existing row's `person`, `created_at` and photo
  are updated. `word` and `meaning` are the identity and are never changed.
- Any row error rolls the whole transaction back and the response names the
  failing row index and word.

The file is uploaded once for preview and again for apply; the preview is
stateless and keeps no server-side staging.

## Caps

- `maxImportBytes` — 64 MiB request body, also applied to the decompressed
  `entries.json` and the total photo payload.
- `maxImportEntries` — 20,000 rows.
- `MaxPhotoBytes` — 10 MiB per photo, reused from the existing upload limits.

Caps are checked after parsing and before the transaction opens. An over-cap
request returns 413 naming the cap. Photos are validated with the existing
`store.NewPhoto` and `media.Derive` pipeline.

## Access control

All export and import routes sit behind the existing admin session check;
`POST /admin/import` additionally requires the session CSRF token. No new
authentication or download mechanism is introduced.
