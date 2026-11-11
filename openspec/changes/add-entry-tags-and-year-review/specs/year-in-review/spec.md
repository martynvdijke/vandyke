## ADDED Requirements

### Requirement: Year recap page
The system SHALL serve `GET /y/{yyyy}` as a server-rendered recap of the calendar year `yyyy`, containing the number of live entries, the first and last entry of the year with links to their permalinks, the busiest month and its count, a bar for each of the twelve months, the top five persons, the longest misspelling, the number and percentage of entries carrying a photo, and a link to `GET /entries?year=yyyy`.

#### Scenario: A populated year
- **WHEN** a visitor opens `/y/2024`
- **THEN** the page states the 2024 total, names the first and last slip with dates and permalinks, names the busiest month, ranks the five most frequent names, shows the longest misspelling and the photo ratio, and links to the 2024 ledger filter

#### Scenario: Photo-only year
- **WHEN** every entry in a year is a photo entry with an empty `word`
- **THEN** the page still renders its totals and photo ratio and states that no misspelling was long enough to name, rather than showing an empty longest-misspelling figure

### Requirement: Deterministic figures
Every figure SHALL be computed on read from the `entries` table with no stored aggregate, and ties SHALL be broken so the same data always produces the same page: the busiest month takes the earliest month, the person ranking takes count descending then name ascending, and the longest misspelling takes rune length descending then the earliest `created_at` then the lowest id.

#### Scenario: Tied months
- **WHEN** March and July each hold the same number of entries and that number is the year's maximum
- **THEN** the busiest month is reported as March

#### Scenario: Repeated request
- **WHEN** the same year is requested twice with no change to the ledger
- **THEN** both responses carry the same body and the same ETag

### Requirement: Missing years return not-found
`GET /y/{yyyy}` SHALL return the existing not-found page with status 404 when a year holds no live entries or when the path segment is not a four-digit year.

#### Scenario: Year of only trashed entries
- **WHEN** every entry dated 2019 is trashed and a visitor opens `/y/2019`
- **THEN** the response is 404 with the not-found page, not a recap of zeroes

#### Scenario: Malformed year
- **WHEN** a visitor opens `/y/24` or `/y/abcd`
- **THEN** the response is 404 with the not-found page

#### Scenario: Year with no entries at all
- **WHEN** a visitor opens `/y/9999`
- **THEN** the response is 404 with the not-found page

### Requirement: Recap caching
The recap SHALL send an `ETag` derived from its computed figures and `Cache-Control: public, max-age=300`, SHALL answer a matching `If-None-Match` with 304 and no body, and SHALL send no `ETag` on a 404.

#### Scenario: Conditional request
- **WHEN** a client repeats a request with the ETag the page returned
- **THEN** the response is 304 with no body

#### Scenario: Edited entry changes the tag
- **WHEN** an entry in the year has its correction edited and the page is requested again with the old ETag
- **THEN** the response is 200 with a different ETag and the updated figures

### Requirement: Trashed entries are excluded from the recap
The recap SHALL count and rank only entries whose `deleted_at` is null, and a year whose remaining live count is zero SHALL return 404.

#### Scenario: Trash the only entry of a year
- **WHEN** the single live entry of a year is trashed
- **THEN** the year's recap returns 404 and its figures are gone from the totals, the months, the ranking and the longest misspelling

### Requirement: Recap discoverability
The recap SHALL emit a canonical URL, Open Graph and Twitter metadata, SHALL appear in the sitemap for every year with at least one live entry, and SHALL be linked from the matching year in the `/stats` by-year chart.

#### Scenario: Metadata
- **WHEN** `/y/2024` is rendered with `PUBLIC_BASE_URL` set
- **THEN** the canonical, `og:url`, `og:title`, `og:description` and Twitter tags use that base and describe the 2024 recap

#### Scenario: Sitemap
- **WHEN** `/sitemap.xml` is requested and 2024 has live entries while 2023 has none
- **THEN** `/y/2024` is listed and `/y/2023` is not

#### Scenario: Link from the statistics page
- **WHEN** a visitor views `/stats` and a year in the by-year chart has entries
- **THEN** that year is a link to `/y/{yyyy}` and a year with a zero count is not a link

### Requirement: Recap accessibility
The month chart and the person ranking SHALL each expose a visually hidden table alternative with a caption, header row and one row per value, matching the alternative that `/stats` provides, and the recap SHALL be fully readable with JavaScript disabled.

#### Scenario: Table alternative
- **WHEN** the recap HTML is inspected
- **THEN** the month figure is `aria-hidden` and is followed by a `table.visually-hidden` captioned for the year, and the person ranking has the same structure

#### Scenario: No JavaScript
- **WHEN** the recap is opened with scripting disabled
- **THEN** every figure and link is present because the page is rendered on the server

#### Scenario: Links are not hidden
- **WHEN** the year links are rendered on `/stats`
- **THEN** they sit outside the chart's `aria-hidden` figure so they are reachable by keyboard and assistive technology
