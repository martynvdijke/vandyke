## ADDED Requirements

### Requirement: Deterministic daily selection
The system SHALL choose the slip of the day for a local calendar date by hashing the byte string `vandyke-daily-slip\x00<YYYY-MM-DD>` with FNV-1a 64 and using the remainder modulo the number of live entries as an index into the ascending id list returned by `AllEntryIDs`, so the same date and the same id set always yield the same entry regardless of which request, process or visitor computes it.

#### Scenario: Two visitors on the same day
- **WHEN** two visitors load the slip surface within the same second on the same date
- **THEN** both receive the same entry id, because neither selection depends on request state

#### Scenario: The date advances
- **WHEN** the local calendar date changes from one day to the next
- **THEN** the input to the hash changes and the served entry id differs unless the new index happens to land on the same row

#### Scenario: The ledger has no live entry
- **WHEN** every entry is purged or the ledger has never had an entry
- **THEN** the system reports that there is no slip for the day instead of selecting a trashed or non-existent row

#### Scenario: Trashed entries are never candidates
- **WHEN** the candidate index is computed
- **THEN** the id list contains only rows whose `deleted_at` is null, so a trashed entry cannot become the slip

### Requirement: The day's pick is pinned
The system SHALL persist the first resolved entry id for a date under the `settings` key `daily_slip.<YYYY-MM-DD>` before serving it, SHALL serve the stored id for the remainder of that date, and SHALL overwrite the stored id only when the stored entry is no longer readable through `GetEntry`.

#### Scenario: An entry is added mid-day
- **WHEN** an entry is created after the day's pick has been stored
- **THEN** the day's slip remains the stored entry and the new entry is not served

#### Scenario: The pinned entry is trashed mid-day
- **WHEN** the pinned entry is soft-deleted
- **THEN** the next resolution recomputes the candidate from the surviving ids, stores the new id under the same key, and serves that entry for the rest of the day

#### Scenario: Concurrent first requests
- **WHEN** two requests resolve the slip at the same instant on a date with no stored key
- **THEN** exactly one id is stored, because the insert ignores a conflicting key, and both responses carry that id

#### Scenario: A stored value is unreadable
- **WHEN** the stored value is not a positive integer
- **THEN** the value is treated as absent and replaced by a freshly computed candidate

#### Scenario: Old pins are pruned
- **WHEN** a new date is pinned
- **THEN** every other `daily_slip.%` key is deleted, so the table holds at most one daily row

### Requirement: Home page slip section
The system SHALL render a server-side slip section on `GET /{$}` when the request carries none of `q`, `person`, `year`, `from`, `to`, `photo`, `page`, `per_page`, `sort` or `order` and a slip exists, showing the heading, the misspelling, the correction, the entry date, the person when present, the thumbnail when present and a link to the entry's permalink.

#### Scenario: Unfiltered home page
- **WHEN** a visitor loads `/` with no query parameters and the ledger has a slip for today
- **THEN** the response contains the slip section with the same entry the JSON endpoint serves

#### Scenario: Filtered home page
- **WHEN** a visitor loads `/?q=boeke`, `/?year=2026`, `/?person=Marieke`, `/?photo=1` or `/?page=2`
- **THEN** the response omits the slip section

#### Scenario: Searching within the page
- **WHEN** a visitor types a search term into the toolbar on an already loaded home page
- **THEN** the client hides the slip section without a navigation, and shows it again when the toolbar returns to its default state

#### Scenario: HTMX fragment
- **WHEN** the ledger is requested as the `ledger` fragment for an HTMX swap
- **THEN** the fragment does not contain the slip section

#### Scenario: Empty ledger
- **WHEN** the ledger has no live entry
- **THEN** the home page renders without the slip section and without an empty placeholder

### Requirement: Slip JSON endpoint
The system SHALL serve `GET /api/v1/slip` as JSON with a `date` field in `YYYY-MM-DD` form and an `entry` field using the existing entry representation, a strong `ETag` of the form `"slip-<date>-<id>"`, `Cache-Control: public, max-age=60`, and a bodyless 304 when the request's `If-None-Match` matches the current tag.

#### Scenario: Request the slip
- **WHEN** a client requests `GET /api/v1/slip`
- **THEN** the response is 200 with the date and the same entry the home page displays

#### Scenario: Conditional request
- **WHEN** a client repeats the request with the `ETag` from a previous response and the date and pinned id are unchanged
- **THEN** the response is 304 with no body

#### Scenario: No slip exists
- **WHEN** the ledger has no live entry
- **THEN** the response is 404 with the error code `not_found` and the message `the ledger has no entry for today`

#### Scenario: The tag changes after midnight
- **WHEN** the local date advances and the client sends the previous day's `ETag`
- **THEN** the response is 200 with a new tag and the new day's entry

### Requirement: Permalink slip badge
The system SHALL show a "Slip of the day" badge on `GET /e/{id}` when the entry is the pinned slip for the current local date, and SHALL keep the permalink as the canonical URL.

#### Scenario: Open the slip's permalink
- **WHEN** a visitor opens the permalink of the pinned entry
- **THEN** the page shows the badge and its canonical link still points at `/e/{id}`

#### Scenario: Open another entry
- **WHEN** a visitor opens the permalink of an entry that is not today's slip
- **THEN** the page shows no badge and every other element is unchanged

#### Scenario: Resolver failure
- **WHEN** resolving the day's slip returns an error
- **THEN** the permalink is still served without the badge and the error is logged

### Requirement: TRMNL slip plugin
The system SHALL ship a fifth TRMNL plugin at `trmnl/plugins/vandyke-slip` that reads the `url` custom field, requests `{base}/api/v1/slip`, and renders the day's misspelling, correction, date, person and photo, packaged with the existing four by `scripts/package-trmnl.py`.

#### Scenario: Transform a normal slip
- **WHEN** the transform runs against an instance that returns a slip with a word and a correction
- **THEN** the output contains the date, word, meaning and photo URL, and the Liquid views render them without an error block

#### Scenario: Photo-only slip
- **WHEN** the day's entry has no word and no meaning
- **THEN** the transform returns empty strings for both and the views render the photo entry wording used by the other plugins

#### Scenario: Missing or invalid address
- **WHEN** the `url` custom field is empty or does not start with `http://` or `https://`
- **THEN** the transform returns an `error` string and performs no request

#### Scenario: Unreachable instance
- **WHEN** the request fails or the response is not JSON
- **THEN** the transform returns an `error` string instead of raising

#### Scenario: Packaging includes the plugin
- **WHEN** `python3 scripts/package-trmnl.py <version>` runs
- **THEN** `dist/trmnl/vandyke-slip-<version>.zip` exists, the combined archive contains it, and the CI count assertion expects six archives instead of five

#### Scenario: Plugin id is pinned
- **WHEN** the workflow checks the plugin directory
- **THEN** `src/settings.yml` contains a numeric `id`, so `trmnlp push` updates the existing plugin instead of creating a duplicate
