## ADDED Requirements

### Requirement: Crawler directives
The server SHALL serve `GET /robots.txt` as `text/plain` with a rule for all user agents that disallows `/admin` and a `Sitemap` line naming the absolute sitemap URL. The absolute URL SHALL come from `PUBLIC_BASE_URL` when configured and otherwise from the request host, matching the permalink canonical builder.

#### Scenario: Robots file is served
- **WHEN** a client requests `/robots.txt`
- **THEN** the response is 200 with a `text/plain` content type, contains `Disallow: /admin`, and contains a `Sitemap:` line ending in `/sitemap.xml`

#### Scenario: Configured base URL wins
- **WHEN** `PUBLIC_BASE_URL` is `https://vandyke.example.com` and a client requests `/robots.txt`
- **THEN** the `Sitemap` line reads `https://vandyke.example.com/sitemap.xml`

### Requirement: Sitemap lists public pages and permalinks
The server SHALL serve `GET /sitemap.xml` as `application/xml` containing the home page, `/stats`, and one entry for every ledger entry at `/e/{id}`. Every location SHALL be an absolute URL derived the same way as the robots sitemap line.

#### Scenario: Full sitemap
- **WHEN** a client requests `/sitemap.xml` and the ledger holds three entries
- **THEN** the document contains five `loc` elements: the home page, `/stats` and the three permalinks

#### Scenario: Sitemap after a deletion
- **WHEN** an entry is deleted and `/sitemap.xml` is requested again
- **THEN** the deleted entry's permalink no longer appears

#### Scenario: Empty ledger
- **WHEN** the ledger holds no entries
- **THEN** the sitemap still contains the home page and `/stats` and remains well-formed XML

### Requirement: Web manifest
The server SHALL serve `GET /site.webmanifest` as `application/manifest+json` with a name, a short name, a start URL of `/`, a display mode, `theme_color` and `background_color` of `#f4eee1`, and icons referencing the existing `favicon.svg` and `apple-touch-icon.png` assets.

#### Scenario: Manifest fields
- **WHEN** a client requests `/site.webmanifest`
- **THEN** the JSON object contains `name`, `short_name`, `start_url`, `display`, `theme_color`, `background_color` and an `icons` array of two entries

#### Scenario: Icons resolve
- **WHEN** the manifest's icon `src` values are requested from `/static/`
- **THEN** each returns 200 with an image content type

### Requirement: Manifest is linked from every page
Every HTML document served by the web and admin packages SHALL include a `link` element with `rel="manifest"` pointing at `/site.webmanifest` in its head.

#### Scenario: Manifest link on each page
- **WHEN** `/`, `/entries`, `/stats`, `/e/{id}` and `/admin/login` are requested, and `/admin` is requested with a signed-in session
- **THEN** each rendered document head contains `rel="manifest" href="/site.webmanifest"`
