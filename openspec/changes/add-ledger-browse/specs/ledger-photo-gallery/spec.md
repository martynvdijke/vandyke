## ADDED Requirements

### Requirement: Gallery of photographed entries
The server SHALL serve `GET /photos` listing live entries that have a photo, newest first, with the total number of photographs in the ledger. Entries without a photo and entries with a non-null `deleted_at` SHALL NOT appear. An empty gallery SHALL render a stated empty response rather than an empty grid.

#### Scenario: Only photographed entries
- **WHEN** the ledger holds ten live entries, three of which have a photo
- **THEN** `/photos` renders three tiles and states that the ledger holds three photographs

#### Scenario: Trashed entry is absent
- **WHEN** an entry with a photo is in the trash
- **THEN** its tile does not appear in the gallery and the stated total does not include it

#### Scenario: Empty gallery
- **WHEN** no live entry has a photo
- **THEN** `/photos` renders a message that there are no photographs yet and no grid

### Requirement: Cursor paging
The gallery SHALL render at most 24 tiles per batch and SHALL page with a `cursor` query parameter that identifies the last rendered entry by its `created_at` and `id`, so that an entry added while a reader is scrolling can never repeat or skip a tile below the cursor. A cursor that cannot be decoded SHALL answer 400.

#### Scenario: First batch
- **WHEN** the ledger holds 30 photographed entries and a client requests `GET /photos`
- **THEN** 24 tiles are rendered and the response offers a cursor for the remaining six

#### Scenario: Shared timestamps
- **WHEN** two photographed entries share the same `created_at` and the cursor points at the first of them
- **THEN** the second entry appears in the next batch and neither appears twice

#### Scenario: Malformed cursor
- **WHEN** a client requests `GET /photos?cursor=not-a-cursor`
- **THEN** the response is 400 and no tiles are rendered

#### Scenario: Last batch
- **WHEN** the final batch is rendered
- **THEN** the response states that the end of the gallery is reached and carries no further cursor

### Requirement: Infinite scroll with HTMX
The gallery SHALL place a sentinel after the last tile as an anchor whose `href` is the next cursor URL and whose HTMX attributes request the same URL on reveal, replacing the sentinel with the next batch and a fresh sentinel. When the request carries `HX-Request` the response SHALL contain only the batch fragment, and the document response SHALL set `Vary: HX-Request`.

#### Scenario: Revealing the sentinel
- **WHEN** a reader scrolls the sentinel into view with HTMX active
- **THEN** the next batch is appended in place of the sentinel and a new sentinel follows it

#### Scenario: Fragment response
- **WHEN** a client requests `/photos?cursor=<token>` with an `HX-Request` header
- **THEN** the response contains tiles and a sentinel but no document head, masthead or footer

#### Scenario: Cache separation
- **WHEN** a client requests any `/photos` URL
- **THEN** the response carries `Vary: HX-Request` so a plain navigation is never served a bare fragment

### Requirement: Complete no-JavaScript fallback
A request for a cursor URL without `HX-Request` SHALL return a complete HTML document containing the batch, the masthead and the footer, with `rel="prev"` and `rel="next"` links and a link back to the first page, so the whole gallery can be walked without JavaScript.

#### Scenario: Walking without JavaScript
- **WHEN** a reader follows the sentinel anchor with scripting disabled
- **THEN** a complete page loads with the next 24 tiles and links to the previous and following batches

#### Scenario: Returning to the start
- **WHEN** a reader is on any cursor URL
- **THEN** the page offers a link to `/photos` without a cursor

### Requirement: Thumbnails only in the grid
The grid SHALL render each tile's image from the `thumb` variant with `loading="lazy"` and `decoding="async"`, and no `img` element in the grid SHALL reference the `display` variant. Each tile SHALL be an anchor to the `display` variant so the full image stays reachable without JavaScript.

#### Scenario: Grid markup
- **WHEN** a client requests `GET /photos`
- **THEN** every `img` source in the grid ends in `size=thumb` and no `img` source contains `size=display`

#### Scenario: Tile link
- **WHEN** a reader follows a tile with scripting disabled
- **THEN** the browser opens the display image for that entry

### Requirement: Accessible lightbox
The gallery SHALL provide one native `<dialog>` lightbox per page, opened from a tile, containing the entry's display image with alt text derived from the entry's `word` and `meaning` and a visible caption naming the entry number and date. The lightbox SHALL set its image source only when it opens, SHALL close on Esc through the native dialog, and SHALL return focus to the tile that opened it.

#### Scenario: Opening a tile
- **WHEN** a reader activates a tile that has a word and a meaning
- **THEN** the dialog opens with the display image, alt text of `<word> → <meaning>`, and a caption naming the entry and its date

#### Scenario: Photo-only entry
- **WHEN** the entry behind a tile has no word and no meaning
- **THEN** the image alt text names the photograph by entry number and the caption states that the slip has no text

#### Scenario: Closing restores focus
- **WHEN** a reader closes the lightbox with Esc or the close control
- **THEN** focus returns to the tile that opened it

#### Scenario: No display fetch for off-screen tiles
- **WHEN** the page loads with a grid taller than the viewport
- **THEN** no display-variant image is requested until a tile is opened

### Requirement: Gallery metadata and navigation
The gallery SHALL declare `{base}/photos` as its canonical URL, SHALL be listed once in the sitemap, and SHALL be reachable from the masthead and footer of every public page. A cursor variant SHALL carry `noindex, follow` with the bare page as canonical.

#### Scenario: Canonical URL
- **WHEN** a client requests `GET /photos?cursor=<token>`
- **THEN** the head declares a canonical URL of `{base}/photos` and a `robots` directive of `noindex, follow`

#### Scenario: Sitemap and navigation
- **WHEN** a client requests `GET /sitemap.xml` and loads `/`
- **THEN** the sitemap contains an absolute `/photos` location and the page carries a `/photos` link in both the masthead and the footer
