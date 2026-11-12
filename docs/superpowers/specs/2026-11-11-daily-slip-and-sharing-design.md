# Daily slip and sharing design

Date: 2026-11-11

The ledger needed a featured entry and a shareable permalink. This change adds a
deterministic, pinned slip of the day, a fifth TRMNL plugin for it, Web Share
support on permalinks, structured data, a canonical/robots policy, a committed
social preview image and OpenSearch discovery. It is served by the existing
single binary: no scheduler, no new table, no runtime image generation and no
JavaScript build step.

## Slip of the day

### Candidate selection

The candidate is a hash of the date, not a random row. The resolver hashes the
byte string `vandyke-daily-slip\0<YYYY-MM-DD>` with FNV-1a 64 (`hash/fnv`,
standard library) and takes the remainder modulo the number of live entries as
an index into the ascending id list from `AllEntryIDs`. The same date and the
same id set therefore yield the same entry for every request, process and
visitor, which `ORDER BY RANDOM() LIMIT 1` would not. FNV-1a is sufficient: the
input is public, short and needs no collision resistance.

The list comes from `AllEntryIDs`, which already filters `deleted_at IS NULL`,
so a trashed row is never a candidate.

### The pin

The first resolution of a date computes the candidate, writes
`daily_slip.<YYYY-MM-DD>` with the decimal entry id using
`INSERT ... ON CONFLICT(key) DO NOTHING`, and reads the row back. Two
simultaneous first requests both insert, exactly one wins and both serve the
winner, so a day cannot open on two different entries. The `settings` table
already exists (migration `004_settings.sql`); runtime state lives beside the
`umami.*` and `webhook.*` configuration, and the settings loader ignores keys it
does not know. No migration is added.

The pinned id is served for the rest of the day when it resolves through
`GetEntry`. The pin is overwritten only when that lookup reports
`store.ErrNotFound` — the entry was trashed or purged. Adding an entry,
restoring a different one or purging something else leaves the day's pick
untouched. A second, unreadable stored value is treated as absent and replaced.

Every write prunes all other `daily_slip.%` rows, so the table holds at most one
daily pin. A corrupt or non-positive value is overwritten by a fresh candidate.

### The day

The day is the server's local calendar date: `time.Now().In(time.Local)`
formatted as `2006-01-02`. The process time zone is deployment configuration via
`TZ`; a container without `TZ` uses UTC. The database is SQLite on one host, so
two instances in different zones are not a supported topology. `ForDay` takes
the date string, so tests never depend on the wall clock.

### Absence

`AllEntryIDs` returning no ids reports a distinct `slip.ErrNoSlip`. The home
page omits the section entirely, and `GET /api/v1/slip` answers `404` with the
standard error envelope and `the ledger has no entry for today`. A `204` would
make the TRMNL transform indistinguishable from a transport failure.

### Surfaces

- `GET /` renders the section server-side only when the request carries none of
  `q`, `person`, `year`, `from`, `to`, `photo`, `page`, `per_page`, `sort` or
  `order`. `app.js` hides the loaded section while a search term or filter is
  active and shows it again when the toolbar returns to its default state. The
  HTMX `ledger` fragment never contains it.
- `GET /api/v1/slip` returns `{"date": "...", "entry": {...}}` with the strong
  ETag `"slip-<date>-<id>"`, `Cache-Control: public, max-age=60` and a bodyless
  `304` for a matching `If-None-Match`. The tag changes at midnight or on a
  repin and at no other time.
- `GET /e/{id}` shows a "Slip of the day" badge when the entry is the pinned
  one. The canonical URL stays `/e/{id}`, and no `/slip` route competes with it.
- `trmnl/plugins/vandyke-slip` polls `/api/v1/slip` hourly.

## Sharing

### Web Share with a clipboard fallback

The copy control is a real anchor (`<a href="{canonical}" data-share-url
data-share-title data-share-text>`). With JavaScript it calls `navigator.share`
when available, treats an `AbortError` rejection as a user cancel (no copy, no
toast) and falls back to the clipboard for every other outcome. The clipboard
path uses `navigator.clipboard.writeText`, then `document.execCommand`. Success
sets the label to `Link copied` for two seconds and appends `Link copied.` to
the existing `#toasts` `role="status"` region; failure keeps the recovery
message pointing at the address bar. Without JavaScript the anchor is an
ordinary link to the canonical permalink.

### Structured data

Permalinks emit one JSON-LD `DefinedTerm`: the misspelling is `name`, the
correction is `description`, the canonical permalink is `url`, the id is
`identifier`, the creation time is `dateCreated`, and `inDefinedTermSet` names
the ledger. A photo-only entry uses `Photo entry #<id>` and adds `image`.
`/stats` emits a `Dataset` with `name`, `description`, the canonical URL,
`isAccessibleForFree`, `temporalCoverage` (omitted on an empty ledger),
`variableMeasured` and a `DataDownload` for `/api/v1/stats`.

The documents are Go values marshalled with `encoding/json`, which escapes `<`,
`>`, `&` by default, then injected with `template.JS`. A `</script>` sequence in
entry text therefore cannot break the page. Templates never build JSON by hand.

## Canonical and robots matrix

| Surface | Robots | Canonical |
| --- | --- | --- |
| `/` with no listing parameter | `index,follow` | absolute `/` |
| `/` with any of `q`, `person`, `year`, `from`, `to`, `photo`, `page`, `per_page`, `sort`, `order` | `noindex,follow` | none |
| `/entries` in every form | `noindex,follow` | none |
| `/stats` | `index,follow` | absolute `/stats` |
| `/e/{id}` | `index,follow` | absolute `/e/{id}` |

A canonical pointing elsewhere next to `noindex` sends conflicting signals, so
the noindex responses carry no canonical at all. `/entries` is excluded from
indexing because `/` is the canonical browsable listing. Sitemap membership is
unchanged: every live permalink remains discoverable through `/sitemap.xml`, the
home page's recent links and the ledger rows.

## Social preview

`internal/web/static/images/og-default.png` is a committed 1200x630 PNG,
embedded by the existing `//go:embed static` and served by the existing
`/static/` handler with its day-long cache header. Text-only entries use it as
`og:image` and `twitter:image` with `twitter:card: summary_large_image`;
photographed entries use their display photo. Only the fallback declares
`og:image:width` and `og:image:height`, because those dimensions are fixed and
known. Runtime card generation is an explicit non-goal: the binary has no
rasteriser and no font dependency, and adding one for a joke ledger is not worth
the image size. The home page and `/stats` use the same fallback and card type.

## OpenSearch

`GET /opensearch.xml` is an OpenSearch 1.1 description document built per
request from `PUBLIC_BASE_URL` (or the request). Its `Url` template is
`{base}/?q={searchTerms}`, the surface the toolbar already submits to and
`searchquery.Parse` already understands, including quoted phrases and `-`
exclusions. No suggestions URL is advertised. Every HTML head carries a
`<link rel="search" type="application/opensearchdescription+xml">` discovery
tag.
