## ADDED Requirements

### Requirement: Atom feed of newest entries
The server SHALL serve an Atom 1.0 document at `GET /feed.xml` with `Content-Type: application/atom+xml; charset=utf-8`, listing entries newest first, where each entry uses `{base}/e/{id}` as both `id` and `link`, the word as `title`, the meaning as `summary`, `created_at` as `updated`, an `author` name taken from `person` when that field is non-empty, and a feed-level author naming the ledger.

#### Scenario: Request the feed
- **WHEN** a client requests `GET /feed.xml`
- **THEN** the response is a valid Atom 1.0 document whose entries are ordered by `created_at` descending and whose first entry matches the newest ledger entry

#### Scenario: Entry without an attributed person
- **WHEN** the newest entry has an empty `person`
- **THEN** its Atom entry is emitted without an `author` element and every other field is still present

### Requirement: Bounded limit parameter
The feed SHALL accept a `limit` query parameter with a default of 50 and a maximum of 100, falling back to 50 for a non-numeric or sub-1 value and capping a larger value at 100.

#### Scenario: Request a small feed
- **WHEN** a client requests `GET /feed.xml?limit=10`
- **THEN** at most 10 entries are included

#### Scenario: Request more than the maximum
- **WHEN** a client requests `GET /feed.xml?limit=5000`
- **THEN** at most 100 entries are included

#### Scenario: Invalid limit
- **WHEN** a client requests `GET /feed.xml?limit=all`
- **THEN** the feed is served with the default of 50 entries

### Requirement: Conditional requests and caching
The feed response SHALL carry a strong `ETag` derived from the newest entry id and the returned entry count and a `Cache-Control` value of `public, max-age=60`, and SHALL return 304 without a body when the request's `If-None-Match` matches the current tag.

#### Scenario: Unchanged feed
- **WHEN** a client repeats the request with the `If-None-Match` value from a previous response and no entry has been created since
- **THEN** the server returns 304 and no body

#### Scenario: New entry invalidates the tag
- **WHEN** a new entry is created and the feed is requested with the previous ETag
- **THEN** the server returns 200 with a new ETag and the new entry in the document

### Requirement: Feed autodiscovery
Every HTML page head SHALL include `<link rel="alternate" type="application/atom+xml" title="The vanDyke Corrections Ledger" href="/feed.xml">` so a reader can discover the feed from any page.

#### Scenario: Discover the feed from the ledger
- **WHEN** a browser loads the ledger index page
- **THEN** the page head contains the Atom autodiscovery link pointing at `/feed.xml`

#### Scenario: Discover the feed from a permalink
- **WHEN** a browser loads `/e/{id}`
- **THEN** the permalink page head contains the same Atom autodiscovery link
