# Ledger browse: words, people, random slips and the photo gallery

Status: implemented by the `add-ledger-browse` OpenSpec change.

## Context

The public ledger was searchable but not navigable: every entry point was a
query (`/entries`, the search field, `/stats`, `/e/{id}`). There was no
dictionary of distinct misspellings, no page for one person, no way to land on a
slip by chance, and no way to browse the photographs the media pipeline already
stores. This design adds those surfaces without a schema change, a JavaScript
build step or an external library.

## Word index (`GET /words`)

`store.WordIndex` runs one grouped query over live entries with a non-empty
`word`, partitioned by `word COLLATE NOCASE`, and returns the count, the first
and last `created_at` and the newest occurrence's id and spelling. SQLite's
`NOCASE` folds exactly the ASCII identity the unique `(word, meaning)` index
enforces, so the index agrees with dedupe and with `Stats.UniqueWords`;
re-implementing the fold in Go would drift at the edges. A second scalar count
records live entries whose `word` is empty for the photo-only note.

Bucketing happens in Go on the newest spelling with `unicode.ToUpper` on the
first rune: A–Z keep their letter, everything else — digits, punctuation,
accented and non-Latin scripts — goes to `#`. The rail is fixed at A–Z plus `#`
with a count per bucket and renders empty buckets as inert, `aria-disabled`
text, so the index does not reshuffle as the corpus grows.

Exactly one bucket renders per document: `#` when it holds rows, otherwise the
first non-empty letter, unless `?letter=` names another bucket. An explicit
letter with no rows is `404`; the rail never links to one. A letter pages at 200
rows through the existing numbered pager, and choosing a letter resets the page.
Each row shows the spelling, its count, its first and last date, and links the
spelling to the newest occurrence's `/e/{id}` permalink; linking into a filtered
ledger would drag the query grammar and its quoting rules into a navigation
page.

Photo-only entries are not rows: `#` means "starts with something that is not a
letter", not "empty string". The page instead states how many slips have only a
photograph and links to `/photos`. The unique `(word, meaning)` index makes
`('', '')` a single key, so at most one fully textless entry can exist; the note
counts what exists and promises no more.

## Person identity (`GET /p/{person}`)

`store.PersonProfile` groups a person's live entries with `COLLATE NOCASE`, the
way `PersonCounts` already does, and returns the spelling of the newest
occurrence, the count and the first and last slip. The handler passes the
decoded path value to the existing `ListEntries` person predicate, so a name
containing `/` survives as `%2F`. Canonical, feed and navigation URLs are built
from the returned spelling with `url.PathEscape`, so `/p/marieke` and
`/p/Marieke` are one document with one canonical URL.

The page renders the same ledger fragment. `ledgerData` gains a base path used
by `SortHref`, `PageHref`, the clear-search link and the toolbar's `action` and
`hx-get`; the toolbar's person `<select>` becomes a static chip. A `person`
query parameter is ignored because the path wins. An unknown or empty name is
`404`.

The page is `noindex, follow` and unsitemapped: it is the ledger filtered by one
field, its rows are already indexed permalinks, and one near-identical document
per attributed name would multiply the index.

## Person feed (`GET /p/{person}/feed.xml`)

The site feed's Atom id is `{base}/feed.xml`, so serving a different document
from that id under a query parameter would be invalid Atom. A child URL carries
its own id, ETag and cache entry. The Atom builder is parameterised with a feed
spec (path, id, title, author); the person feed uses the person as the
feed-level author, the same 1–100 `limit` clamp, `Cache-Control: public,
max-age=60` and `304` on a matching `If-None-Match`. The ETag derives from the
person, the newest entry id and the returned count. A person with no live entry —
including one whose entries are all trashed — is `404`, matching the page.

## Random slip (`GET /random`, `GET /api/v1/random`)

Random lives at sibling paths, never under an id path. `GET /e/{id}` and
`GET /api/v1/entries/{id}` parse a numeric id and answer not-found or
`invalid_id` for anything else, so a `random` segment under either would be
consumed by the wildcard; carving out a literal `/e/random` would put a non-id
token inside a namespace documented as numeric. `GET /api/v1/entries/random`
keeps answering `400 invalid_id`.

`GET /random` answers `303 See Other` to `/e/{id}`, the status the create flow
already uses to hand a client to a GET, so the permalink stays the one canonical
document and a reload does not reroll. It sends `Cache-Control: no-store` and
`X-Robots-Tag: noindex`; it is not in the sitemap and robots.txt is unchanged,
because disallowing `/random` would stop a crawler reading that header. The API
route returns the same entry object as the JSON permalink. Both accept only
`photo` and `person`, and answer `404` when nothing matches, including on an
empty ledger.

`store.RandomEntry` reuses `ledgerFilter`, so `deleted_at IS NULL`, the photo
predicate and the case-insensitive person predicate are exactly the ledger's.

## Photo gallery (`GET /photos`)

The gallery walks live entries with a photo, newest first, 24 per batch.
Pagination is by cursor, not page number: the token is URL-safe base64 of the
last tile's stored `created_at` and `id`, and the next batch selects rows with
`created_at < ? OR (created_at = ? AND id < ?)` after it. A new row can only
land above the cursor, so a scroll cannot repeat or skip a tile; page numbers
would shift as entries are added. A malformed token is `400` rather than a
silent restart, which would loop the sentinel forever.

The sentinel is a real anchor: `href` and `hx-get` name the next cursor URL,
`hx-target="this"`, `hx-swap="outerHTML"`, `hx-trigger="revealed"`. With HTMX
the next batch and a fresh sentinel replace it; without HTMX the same URL is a
complete document with `rel="prev"`/`rel="next"` links and a link back to
`/photos`. The handler returns the batch fragment only when `HX-Request` is
present and always sets `Vary: HX-Request`, so a shared cache cannot hand a
fragment to a plain navigation.

The previous document's cursor is derived with one extra query: the batch above
is the 24 tiles directly newer than this batch's first tile, and its own cursor
is the row just above that. When no such row exists the prev link is `/photos`.

Each tile is an anchor to the display variant wrapping a `thumb` image with
`loading="lazy"` and `decoding="async"` in an `aspect-ratio` box, so a grid of
off-screen tiles never fetches a display image. A single native
`<dialog id="photo-lightbox">` holds one image; `app.js` copies
`data-lightbox-src`, `data-lightbox-alt` and `data-lightbox-caption` into it and
calls `showModal()` only on open. The native dialog supplies Esc, the focus trap
and the backdrop; the close handler returns focus to the opening anchor. Without
JavaScript the anchor is an ordinary link to the display image.

Alt text comes from the entry: `<word> → <meaning>` when both exist, the word
alone otherwise, and "Photograph attached to entry #0042" when the slip has no
text. Captions name the entry, its date and its person, and say plainly when the
slip has no text.

## Navigation and indexing

`Words` and `Photos` join the shared masthead after `Stats`, the footer and the
permalink's standalone header. `/words` and `/photos` join the sitemap after
`/stats`; `/random` and `/p/` do not. The bare `/words` and `/photos` are
indexable with a self canonical; the `?letter=`, `?page=` and `?cursor=`
variants are `noindex, follow` with the bare page as canonical. Person pages are
always `noindex, follow`.

## Verification

Store and handler tests cover case-insensitive grouping, the newest-occurrence
link target, accented spellings, photo-only handling, letter bucketing and
pagination, person canonicalisation and scoped controls, the not-found paths,
random never drawing a trashed row, per-person feed ids and limits, the gallery
cursor (including shared timestamps and `prev`), the fragment-versus-document
response, `Vary: HX-Request`, thumb-only grid images and the robots/canonical
matrix.
