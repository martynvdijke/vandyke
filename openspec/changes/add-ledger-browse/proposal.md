## Why

The ledger is searchable but not navigable. Every public entry point is a query: the paginated list at `GET /entries`, the search field, `GET /stats` and the `/e/{id}` permalink. There is no dictionary view of the distinct misspellings, no page for a person, no way to land on a random slip, and no way to look at the photographs the media pipeline already produces — the ledger shows one 320 px thumbnail per row and a permalink shows one display image.

## What Changes

- Add `GET /words`, an A–Z index of every distinct misspelling with a letter rail, per-bucket counts, a per-letter page, the first and last occurrence of each spelling, and a permalink from every row.
- Add `GET /p/{person}`, a person page with the entry count, the first and last slip, and the ledger scoped to that person, with canonical and Open Graph metadata.
- Add `GET /p/{person}/feed.xml`, an Atom feed of one person's slips.
- Add `GET /random` for HTML and `GET /api/v1/random` for JSON, both accepting the optional `photo` and `person` filters.
- Add `GET /photos`, a thumbnail gallery with cursor paging, HTMX infinite scroll, a no-JavaScript pager and a native `<dialog>` lightbox.
- Put `Words` and `Photos` in the masthead and footer, and add `/words` and `/photos` to the sitemap.

### Non-goals

- Changing search ranking, the trigram index or the query grammar.
- Tags, categories or any other new classification of entries.
- Admin curation of the word index, such as merging spellings or hiding a word.
- Any third-party JavaScript, image-zoom or lightbox library.
- Changing the case-insensitive `(word, meaning)` dedupe rule.
- Changing the media pipeline, the three photo variants or their sizes.

## Capabilities

### New Capabilities

- `ledger-browse`: The A–Z word index, the person page with its Atom feed, and a random slip on both the HTML and JSON surfaces.
- `ledger-photo-gallery`: A visual gallery of entries that have a photo, with cursor paging, a no-JavaScript fallback and an accessible lightbox.

## Impact

- `internal/store/store.go`: new `WordIndex`, `PersonProfile`, `RandomEntry` and `ListPhotoEntries` reads, each applying the existing `deleted_at IS NULL` rule.
- `internal/web/web.go`: routes for `GET /words`, `GET /p/{person}`, `GET /p/{person}/feed.xml`, `GET /random` and `GET /photos`, plus sitemap entries and the random redirect.
- `internal/web/feed.go`: the Atom builder parameterised so the person feed can reuse it.
- `internal/web/templates/base.html`: masthead and footer links, and a ledger toolbar whose `action` and `hx-get` follow the page instead of the literal `/entries`.
- `internal/web/templates/partials.html`: the ledger fragment's sort, pager and clear-search links, which must follow the page base path so a person page keeps its scope.
- `internal/web/templates/words.html`, `person.html` and `photos.html`: new page templates.
- `internal/web/templates/permalink.html`: the standalone permalink header gains the same navigation links.
- `internal/web/static/js/app.js` and `internal/web/static/css/app.css`: lightbox wiring, gallery layout and the reveal sentinel.
- `internal/httpapi/api.go`: `GET /api/v1/random`.
- `README.md`, `docs/superpowers/specs/2026-11-11-ledger-browse-design.md` and `docs/index.md`.
