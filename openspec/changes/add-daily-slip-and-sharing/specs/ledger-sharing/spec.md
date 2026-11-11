## ADDED Requirements

### Requirement: Share or copy a permalink
The system SHALL render the permalink share control as an anchor whose `href` is the canonical permalink, SHALL call `navigator.share` on activation when the browser provides it, SHALL fall back to the existing clipboard copy when the browser does not provide it or the share call fails for a reason other than user cancellation, and SHALL report success in the button label and through a polite live region.

#### Scenario: Browser supports the share sheet
- **WHEN** a visitor activates the control in a browser where `navigator.share` is a function
- **THEN** the browser is asked to share the canonical URL with the entry title and text, and no clipboard call is made

#### Scenario: The visitor cancels the share sheet
- **WHEN** the share promise rejects with `AbortError`
- **THEN** nothing is copied, no success state is shown and no error is announced

#### Scenario: Share is unavailable
- **WHEN** the control is activated in a browser without `navigator.share`
- **THEN** the canonical URL is written with `navigator.clipboard.writeText`, or through the existing `document.execCommand` fallback when that is unavailable

#### Scenario: Copy succeeds
- **WHEN** the clipboard write resolves
- **THEN** the label reads `Link copied` for a short period and `Link copied.` is appended to the `role="status"` live region

#### Scenario: Copy fails
- **WHEN** every copy path fails
- **THEN** the recovery message tells the visitor to select the address bar instead, and no success state is shown

#### Scenario: JavaScript is disabled
- **WHEN** the page is loaded without JavaScript
- **THEN** the control is still an anchor whose `href` is the canonical permalink, so the browser's link menu can copy or open it

### Requirement: Entry structured data
The system SHALL emit a JSON-LD document on every permalink using schema.org `DefinedTerm`, with the misspelling as `name`, the correction as `description`, the canonical permalink as `url`, the entry id as `identifier`, the creation time as `dateCreated`, and a `DefinedTermSet` naming the ledger as `inDefinedTermSet`.

#### Scenario: Text entry
- **WHEN** a permalink for an entry with a word and a correction is served
- **THEN** the page contains one `application/ld+json` block whose `@type` is `DefinedTerm`, whose `name` is the word, whose `description` is the correction and whose `url` equals the canonical link

#### Scenario: Photo-only entry
- **WHEN** a permalink for an entry without a word or a correction is served
- **THEN** the block uses `Photo entry #<id>` as `name`, omits `description` and includes the absolute photo URL as `image`

#### Scenario: The document cannot break the page
- **WHEN** the entry text contains `<`, `>` or `&`
- **THEN** the encoded document escapes those characters and no `</script>` sequence reaches the response

### Requirement: Statistics structured data
The system SHALL emit a JSON-LD document on `GET /stats` using schema.org `Dataset`, with a name, a description, the canonical statistics URL, `isAccessibleForFree` set to true and a `DataDownload` whose `contentUrl` is `/api/v1/stats`.

#### Scenario: A populated ledger
- **WHEN** `/stats` is served for a ledger with at least one entry
- **THEN** the document declares `temporalCoverage` from the first entry date to the last entry date and lists the measured values as `variableMeasured`

#### Scenario: An empty ledger
- **WHEN** `/stats` is served for a ledger with no entry
- **THEN** the document omits `temporalCoverage` and still carries the name, description and URL

### Requirement: Canonical links
The system SHALL emit a `<link rel="canonical">` with an absolute URL on `/`, on `/stats` and on `/e/{id}`, built from `PUBLIC_BASE_URL` when it is set and from the request otherwise.

#### Scenario: Home page canonical
- **WHEN** `/` is served with no listing parameter
- **THEN** the canonical link points at the absolute root URL, including when the request carried `?add=1` or `?info=1`

#### Scenario: Statistics canonical
- **WHEN** `/stats` is served
- **THEN** the canonical link points at the absolute `/stats` URL

#### Scenario: Permalink canonical
- **WHEN** `/e/{id}` is served
- **THEN** the canonical link points at the absolute permalink and is unchanged from the current behaviour

### Requirement: Robots directives for filtered surfaces
The system SHALL emit `<meta name="robots" content="index,follow">` on `/` without a listing parameter, on `/stats` and on `/e/{id}`, and SHALL emit `<meta name="robots" content="noindex,follow">` on `/entries` in every form and on `/` when any of `q`, `person`, `year`, `from`, `to`, `photo`, `page`, `per_page`, `sort` or `order` is present, without adding a canonical link to those noindex responses.

#### Scenario: Search results
- **WHEN** a crawler requests `/entries?q=boeke`
- **THEN** the response carries `noindex,follow` and no canonical link

#### Scenario: A single filter
- **WHEN** a crawler requests `/entries?year=2026`, `/entries?person=Marieke`, `/entries?photo=1` or `/entries?page=2`
- **THEN** each response carries `noindex,follow` and no canonical link

#### Scenario: Home page search
- **WHEN** a crawler requests `/?q=boeke`
- **THEN** the response carries `noindex,follow` and the slip section is absent

#### Scenario: Unfiltered listing view
- **WHEN** a crawler requests `/entries` with no parameter
- **THEN** the response carries `noindex,follow`, because `/` is the canonical browsable listing

#### Scenario: Indexable pages
- **WHEN** a crawler requests `/`, `/stats` or `/e/{id}`
- **THEN** the response carries `index,follow` and a canonical link

#### Scenario: Permalinks stay discoverable
- **WHEN** `/entries` is excluded from indexing
- **THEN** every live permalink remains listed in `/sitemap.xml` and is linked from the home page

### Requirement: Social preview image for every entry
The system SHALL emit `og:image`, `og:image:alt`, `twitter:image` and `twitter:card` set to `summary_large_image` on every permalink, using the entry photo when one exists and the committed static asset `/static/images/og-default.png` otherwise, and SHALL NOT generate card images at request time.

#### Scenario: Text-only entry
- **WHEN** a permalink for an entry without a photo is served
- **THEN** `og:image` and `twitter:image` point at the absolute `/static/images/og-default.png` URL and the card type is `summary_large_image`

#### Scenario: Entry with a photo
- **WHEN** a permalink for an entry with a photo is served
- **THEN** `og:image` and `twitter:image` point at the absolute display photo URL and the card type is `summary_large_image`

#### Scenario: Static asset only
- **WHEN** an entry has no photo
- **THEN** the preview is the committed 1200x630 PNG served by the existing `/static/` handler, and no rasterisation, font or image encoding happens in the binary

#### Scenario: Index pages
- **WHEN** `/` or `/stats` is served
- **THEN** the page carries a title, description, canonical URL, the fallback image and the `summary_large_image` card type

### Requirement: OpenSearch discovery
The system SHALL serve `GET /opensearch.xml` as an OpenSearch 1.1 description document containing a short name, a description, UTF-8 input encoding and a `Url` element with `type="text/html"` and the template `{base}/?q={searchTerms}`, and SHALL advertise it from every HTML page head with a `<link rel="search" type="application/opensearchdescription+xml">` tag.

#### Scenario: Fetch the description document
- **WHEN** a client requests `GET /opensearch.xml`
- **THEN** the response has the OpenSearch description content type and contains a `Url` template ending in `/?q={searchTerms}` built from the public base URL

#### Scenario: Discover from a page
- **WHEN** a browser loads `/`, `/stats` or `/e/{id}`
- **THEN** the page head contains the `rel="search"` link pointing at `/opensearch.xml`

#### Scenario: The template matches the existing search grammar
- **WHEN** a browser submits a search term through the template
- **THEN** the resulting `/?q=...` request is parsed by `searchquery.Parse`, including quoted phrases and `-` exclusions, without a new parameter

#### Scenario: No suggestions endpoint
- **WHEN** the description document is served
- **THEN** it advertises no suggestions URL, because the ledger has no completion endpoint
