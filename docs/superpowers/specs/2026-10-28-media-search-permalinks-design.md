# Media, search and permalinks design

Date: 2026-10-28

## Goals

- Serve fast, metadata-free photo derivatives while retaining the original for
  administrators.
- Make partial and typo-friendly search useful, with photo and year filters.
- Give every entry a stable, shareable public page with link-preview metadata.

## Non-goals

- HEIC/AVIF decoding.
- Automatic duplicate rejection; duplicates remain allowed.
- Dynamic per-entry Open Graph image generation.
- Public access to original uploads.

## Photo pipeline

Uploads are still validated and capped at 10 MiB by `store.NewPhoto`. The new
`internal/media` package then:

1. Reads image dimensions and rejects images over 12,000 pixels per side or 40
   megapixels.
2. Decodes JPEG/PNG/GIF with EXIF orientation applied, and WebP with the
   `golang.org/x/image/webp` decoder.
3. Creates a display image (max 1600 px) and thumbnail (max 320 px) using
   Lanczos resizing, composited onto white and encoded as JPEG quality 88.
4. Stores the original plus the two derivatives and their SHA-256 hashes.

The original stays in SQLite for evidence and admin download. Existing photos
are backfilled at startup; if a derivative cannot be generated, the public
handler falls back to the original.

`entry_photos` gains width/height plus display and thumb content type, data,
hash, size and dimensions. Migration `005_photo_derivatives.sql` adds them.

Serving:

- `GET /api/v1/entries/{id}/photo` returns the clean display image.
- `?size=thumb` returns the thumbnail.
- `GET /admin/entries/{id}/photo/original` is authenticated and returns the
  original as an attachment.
- Each variant has its own ETag.

## Search

Migration `006_search.sql` creates `entries_fts`, an FTS5 table using the
trigram tokenizer, and backfills existing rows. The store keeps the index in
sync on create, update, delete and seed.

- Queries with every token at least three characters use FTS5 `MATCH`, quoted
  and joined with `AND`.
- Shorter or punctuation-only queries fall back to escaped `LIKE` matching.
- `photo=1` / `photo=0` filters entries by photo presence.
- `year=YYYY` restricts `created_at` to that calendar year.
- The public and admin ledgers expose both filters and preserve them across
  sorting, pagination and HTMX swaps.
- Available years come from `EntryYears`.

## Permalinks

`GET /e/{id}` renders a standalone public entry page with the entry number,
misspelling, correction, date, display photo, a copy-link button and a back
link. It returns a styled 404 for invalid or missing IDs.

The page emits canonical, Open Graph and Twitter metadata. Photo entries use
the absolute display-image URL as `og:image`; text entries use a summary card.
`PUBLIC_BASE_URL` overrides the derived request base URL for proxies.

## Compatibility

- JSON write payloads and validation are unchanged.
- The default photo endpoint now serves the display derivative rather than the
  original bytes. API consumers that need full resolution must use the admin
  interface.
- TRMNL plugins keep their plugin IDs and consume the default photo URL, which
  now returns the display image.

## Testing

- Media: derivative dimensions and formats, WebP support, dimension rejection,
  corrupt input, backfill.
- Store: trigram partial matching, filters, year listing, index updates on
  update/delete.
- API: display/thumb sizes, invalid size, filters, ETags.
- Web: permalink content and 404, OG tags, thumbnail URLs, filter controls,
  camera capture.
- Admin: authenticated original download, filter rendering.
