## ADDED Requirements

### Requirement: A–Z word index
The server SHALL serve `GET /words` listing every distinct misspelling recorded on a live entry, grouped case-insensitively on `word` with the same folding as the unique `(word, meaning)` index, where each group carries its entry count and the date of its first and newest occurrence. Entries with a non-null `deleted_at` and entries with an empty `word` SHALL NOT appear as rows.

#### Scenario: Distinct spellings are grouped
- **WHEN** the ledger holds three live entries with the words `Feutenreus`, `feutenreus` and `feature`
- **THEN** `/words` lists two rows, one for that spelling with a count of 2 and one for `feature` with a count of 1, and the grouped row shows the spelling of its newest occurrence

#### Scenario: Trashed entry is absent
- **WHEN** the only entry with the word `kast` is in the trash
- **THEN** `/words` does not list `kast` and its bucket count does not include it

#### Scenario: Empty word is not a row
- **WHEN** a live entry has an empty `word` and a photo
- **THEN** no word row is produced for it

### Requirement: Letter rail and bucket selection
The page SHALL render a rail of A–Z plus `#` with a count per bucket, where empty buckets render as inert text and never as links, and SHALL render exactly one bucket per document. The default bucket SHALL be `#` when it holds rows and otherwise the first non-empty letter, and `?letter=` SHALL select another bucket.

#### Scenario: Default bucket
- **WHEN** the ledger holds words starting with `b`, `f` and `k` and no other bucket
- **THEN** `/words` renders the `B` bucket and the rail shows counts for `B`, `F` and `K` with every other letter inert

#### Scenario: Explicit letter
- **WHEN** a client requests `GET /words?letter=F`
- **THEN** every rendered row starts with `f` or `F` and the rail marks `F` as the active bucket

#### Scenario: Non-letter bucket
- **WHEN** the ledger holds a word starting with a digit or punctuation mark
- **THEN** that word appears under `#` and not under any letter

#### Scenario: Unknown letter
- **WHEN** a client requests `GET /words?letter=Q` and no word starts with `q`
- **THEN** the response is the existing not-found page

#### Scenario: Empty ledger
- **WHEN** no live entry has a word
- **THEN** `/words` renders the rail, a message that the index is empty, and the photo-only note when it applies, rather than the not-found page

### Requirement: Long buckets paginate
A bucket SHALL render at most 200 rows per document and SHALL accept `?page=` with the existing numbered pager, resetting to the first page when the letter changes.

#### Scenario: A long letter
- **WHEN** 450 live words start with `S` and a client requests `GET /words?letter=S&page=3`
- **THEN** the page lists at most 200 rows, the pager reports three pages, and every row still starts with `s` or `S`

#### Scenario: Pager keeps the letter
- **WHEN** a reader follows the next-page link on a letter page
- **THEN** the target URL carries both the same `letter` and the next `page`

### Requirement: Word rows link to permalinks
Each word row SHALL show the spelling, its count, its first and last occurrence, and SHALL link the spelling to the `/e/{id}` permalink of the newest live entry in that group.

#### Scenario: Following a word
- **WHEN** a reader clicks the spelling on a word row whose newest occurrence is entry `#0042`
- **THEN** they land on `/e/42`

### Requirement: Photo-only slips are reported separately
The page SHALL state how many live entries have no word and only a photograph, SHALL show that statement only when the count is non-zero, and SHALL link it to `GET /photos`.

#### Scenario: Photo-only note
- **WHEN** one live entry has an empty word and a photo
- **THEN** `/words` carries a note naming one photo-only slip that links to `/photos`

#### Scenario: No photo-only entries
- **WHEN** every live entry has a word
- **THEN** `/words` renders no photo-only note

### Requirement: Person page
The server SHALL serve `GET /p/{person}` for a person whose name matches at least one live entry case-insensitively, showing the total count, the date of the first and last slip, and the ledger scoped to that person. The page SHALL build its canonical URL, feed URL and navigation links from the stored spelling of the newest matching entry, so two case variants resolve to one document. An unknown name and a request for `/p/` SHALL return 404.

#### Scenario: Person page renders
- **WHEN** twelve live entries record `M. van Dijke`
- **THEN** `/p/M.%20van%20Dijke` shows a count of twelve, the first and last slip dates, and those twelve entries

#### Scenario: Case-insensitive identity
- **WHEN** entries record both `Marieke` and `marieke` and a client requests `GET /p/marieke`
- **THEN** the page lists every entry from both spellings and declares `{base}/p/<newest spelling>` as its canonical URL

#### Scenario: Unknown person
- **WHEN** a client requests `GET /p/Nobody`
- **THEN** the response is 404 with the existing not-found page

#### Scenario: Empty segment
- **WHEN** a client requests `GET /p/`
- **THEN** the response is 404

#### Scenario: Unicode and encoded names
- **WHEN** a person's stored name contains a space, an accent or a slash
- **THEN** the page is reachable through the percent-encoded path segment and its canonical URL uses the same encoding

### Requirement: Person scope is kept by every control
The search box, the sort headers, the pager and the clear-search link on a person page SHALL target the person's path, and a `person` query parameter SHALL NOT widen or replace the path scope.

#### Scenario: Sorting keeps the person
- **WHEN** a reader clicks the correction sort header on `/p/Marieke`
- **THEN** the request goes to `/p/Marieke` with the new sort and the resulting rows all belong to that person

#### Scenario: Searching keeps the person
- **WHEN** a reader types `feature` into the search box on a person page
- **THEN** the ledger shows only that person's entries matching `feature`

#### Scenario: A person parameter is ignored
- **WHEN** a client requests `GET /p/Marieke?person=Someone%20Else`
- **THEN** the page still lists only `Marieke`'s entries

### Requirement: Person Atom feed
The server SHALL serve `GET /p/{person}/feed.xml` as Atom 1.0 containing only that person's live entries, newest first, with the person as the feed-level author and a feed id of `{base}/p/{person}/feed.xml`. The feed SHALL accept `limit` with a default of 50 and a maximum of 100, SHALL carry a strong `ETag` and `Cache-Control: public, max-age=60`, and SHALL return 304 with no body on a matching `If-None-Match`. A name with no live entry SHALL return 404, matching the person page.

#### Scenario: Person feed content
- **WHEN** `Mariecke` has three live entries and the ledger holds a hundred others
- **THEN** `/p/Mariecke/feed.xml` contains exactly those three entries as Atom `entry` elements

#### Scenario: Feed limit
- **WHEN** a client requests `/p/Mariecke/feed.xml?limit=5000`
- **THEN** at most 100 entries are included

#### Scenario: Conditional request
- **WHEN** a client repeats the request with the previous `If-None-Match` value and no entry has been added since
- **THEN** the server answers 304 with no body

#### Scenario: Unknown person feed
- **WHEN** a client requests `/p/Nobody/feed.xml`
- **THEN** the response is 404

#### Scenario: Every entry for a person is trashed
- **WHEN** every entry attributed to a person is trashed and the feed is requested
- **THEN** the response is 404, matching the person page

### Requirement: Random slip on the HTML surface
The server SHALL serve `GET /random` as a `303 See Other` redirect to the `/e/{id}` permalink of one live entry, chosen at random, accepting the optional `photo` and `person` filters. The response SHALL carry `Cache-Control: no-store` and `X-Robots-Tag: noindex`. An empty or unmatched draw SHALL return 404.

#### Scenario: Redirect to a slip
- **WHEN** a client requests `GET /random`
- **THEN** the response is 303 and its `Location` header names an existing live permalink

#### Scenario: A trashed entry is never drawn
- **WHEN** the ledger holds one live entry and one trashed entry and `/random` is requested repeatedly
- **THEN** every redirect points at the live entry

#### Scenario: Photo filter
- **WHEN** a client requests `GET /random?photo=1`
- **THEN** the redirect points at an entry that has a photo

#### Scenario: Person filter
- **WHEN** a client requests `GET /random?person=Marieke`
- **THEN** the redirect points at an entry attributed to that person, matched case-insensitively

#### Scenario: Empty ledger
- **WHEN** the ledger holds no live entry
- **THEN** `GET /random` returns the not-found page with status 404

### Requirement: Random slip on the API surface
The server SHALL serve `GET /api/v1/random` as `200` with the same entry object as `GET /api/v1/entries/{id}`, accepting the optional `photo` and `person` filters, with `Cache-Control: no-store`, and SHALL return the `not_found` error envelope with status 404 when no entry matches. The route SHALL be a sibling of the collection and SHALL NOT occupy a literal segment under `GET /api/v1/entries/{id}`.

#### Scenario: Random entry object
- **WHEN** a client requests `GET /api/v1/random`
- **THEN** the response is 200 with `id`, `word`, `meaning`, `created_at` and a `photo_url` when the drawn entry has a photo

#### Scenario: No match
- **WHEN** a client requests `GET /api/v1/random?person=Nobody`
- **THEN** the response is 404 with `{"error":{"code":"not_found",...}}`

#### Scenario: The id path keeps its contract
- **WHEN** a client requests `GET /api/v1/entries/random`
- **THEN** the response is `400 invalid_id` and the random route is not involved

### Requirement: Browse pages are navigable and indexed deliberately
Every public page SHALL link to `/words` and `/photos` from the masthead and the footer, the sitemap SHALL list both, and the person pages, the letter and page variants of `/words`, and the cursor variants of `/photos` SHALL carry `noindex, follow` with the bare page as canonical. The `/random` redirect SHALL NOT appear in the sitemap.

#### Scenario: Navigation
- **WHEN** any public page is rendered
- **THEN** its masthead and footer each contain links to `/words` and `/photos`

#### Scenario: Sitemap
- **WHEN** a client requests `GET /sitemap.xml`
- **THEN** the document contains absolute locations for `/words` and `/photos` and none for `/random` or `/p/`

#### Scenario: Indexing a person page
- **WHEN** a browser loads `/p/Marieke`
- **THEN** the head carries a `robots` directive of `noindex, follow` and a canonical URL for the person
