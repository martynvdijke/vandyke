## ADDED Requirements

### Requirement: Person is part of the search index
The trigram index SHALL include the `person` column, and every path that creates, updates, deletes, seeds or rebuilds entries SHALL keep the index in step with the `entries` row.

#### Scenario: Search by the name on a slip
- **WHEN** a client searches `q=Dijke` and entries exist whose `person` contains that text
- **THEN** those entries are returned even when neither `word` nor `meaning` matches

#### Scenario: Renaming a person
- **WHEN** an administrator changes an entry's `person` from `M. van Dijke` to `M. van Dijk`
- **THEN** a search for `Dijke` no longer returns that entry and a search for `Dijk` does

#### Scenario: Deleting an entry
- **WHEN** an entry is deleted
- **THEN** no search returns its word, meaning or person from the index

### Requirement: Inclusive date-range filters
`GET /entries` and `GET /api/v1/entries` SHALL accept `from` and `to` parameters in `YYYY-MM-DD` form and return entries whose `created_at` falls on or after `from` and on or before `to`, in addition to the existing `year` filter.

#### Scenario: Both boundaries are inclusive
- **WHEN** a client requests `from=2024-01-01&to=2024-01-31` and entries exist on both of those dates
- **THEN** both boundary entries are included in the result

#### Scenario: Filter combined with year
- **WHEN** a client supplies `year=2024&from=2024-06-01`
- **THEN** only entries from 1 June 2024 onwards are returned

#### Scenario: Malformed date
- **WHEN** a client sends `from=2024-13-01` to `GET /api/v1/entries`
- **THEN** the response is 400 with the error code `invalid_date`

#### Scenario: One boundary only
- **WHEN** a client supplies `from` and no `to`
- **THEN** every entry on or after that date is returned, with no upper bound

### Requirement: Query grammar with exclusions and the short-term fallback
The query SHALL treat whitespace-separated terms as required (AND), support `"..."` for a phrase and a leading `-` for exclusion, and SHALL keep the existing `LIKE` fallback when any term is shorter than three runes or when the query contains only exclusions.

#### Scenario: Quoted phrase
- **WHEN** a client searches `"van Dijke"`
- **THEN** only entries containing that exact sequence are returned

#### Scenario: Excluded term
- **WHEN** a client searches `feature -feutenreus`
- **THEN** entries matching `feature` but also matching `feutenreus` are omitted

#### Scenario: Short term falls back
- **WHEN** a client searches `qs`
- **THEN** the query is answered by the substring `LIKE` path over `word`, `meaning` and `person` rather than by the trigram index

#### Scenario: Short exclusion falls back
- **WHEN** a client searches `feature -qs`
- **THEN** the query uses the `LIKE` path, excludes entries containing `qs`, and returns no error

### Requirement: Server-side match highlighting
The ledger partial SHALL mark each occurrence of an included term with a `<mark>` element while escaping all other text, SHALL not mark excluded terms, and SHALL return unmarked text when `GET /entries` is requested with `highlight=0`.

#### Scenario: Matched text is marked
- **WHEN** the ledger page is rendered for `q=feature`
- **THEN** each occurrence of `feature` inside the rendered word or meaning is wrapped in `<mark>`

#### Scenario: Query text cannot inject markup
- **WHEN** a client searches `q=<script>`
- **THEN** the rendered page shows the characters as escaped text and contains no `script` element from the query

#### Scenario: Opting out
- **WHEN** `GET /entries` is requested with `highlight=0`
- **THEN** the ledger renders the unmarked word and meaning

#### Scenario: API response stays raw
- **WHEN** `GET /api/v1/entries?q=feature` is requested
- **THEN** the JSON items contain the stored `word` and `meaning` with no markup

### Requirement: Operator-triggered index rebuild
`POST /admin/search/rebuild` SHALL require an authenticated admin session and a valid CSRF token, SHALL rebuild the trigram index from `entries`, and SHALL redirect to the settings page with a confirmation.

#### Scenario: Rebuilding after drift
- **WHEN** a signed-in admin submits the rebuild form on `/admin/settings`
- **THEN** the index is rebuilt from `entries` and the settings page reports that the rebuild completed

#### Scenario: Rebuild without a session
- **WHEN** an unauthenticated client posts to `/admin/search/rebuild`
- **THEN** the request is rejected and the index is unchanged

#### Scenario: Rebuild without a CSRF token
- **WHEN** a signed-in admin posts without the CSRF field
- **THEN** the request is rejected and the index is unchanged
