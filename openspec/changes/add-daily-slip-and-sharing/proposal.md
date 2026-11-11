## Why

The ledger has no notion of a featured entry. `GET /{$}` renders the hero, the statistics block, the five newest entries from `recentEntries` (`internal/web/web.go`) and then the whole ledger, so every visitor sees the same newest-first list and nothing on the page is chosen for the day. There is no route, store method or setting that chooses an entry for a date: `ListEntries` only supports `sort`, `order` and filters, and no query uses `ORDER BY RANDOM()`. A reader who opens the site on two days sees the same newest rows, and the four TRMNL plugins can only mirror those rows.

Sharing is limited to a clipboard button. `internal/web/templates/permalink.html` renders a `<button data-copy-link>` handled by `internal/web/static/js/app.js`, with no `navigator.share` call. A text-only entry emits `twitter:card: summary` and no `og:image`, because `handlePermalink` only sets `OGImage` when the entry has a photo. `GET /entries` and `/?q=...` responses carry no robots directive, so every filter, sort and page permutation is indexable, and `/stats` has no canonical link and no structured data. The head advertises the Atom feed and the manifest, but there is no OpenSearch description document, so a browser cannot add the ledger as a search provider.

## What Changes

- Add a deterministic slip of the day: the entry is chosen by a stable hash of the local calendar date over the ascending live entry ids, then pinned in the existing `settings` table under `daily_slip.<YYYY-MM-DD>` so the whole day serves one id without a new table.
- Serve the slip server-side on the home page as a section that is rendered only when the request carries no listing parameter, and hide it client-side when a visitor starts typing in the search box.
- Add `GET /api/v1/slip` returning `{"date": "...", "entry": {...}}` with a strong ETag, `Cache-Control: public, max-age=60` and 304 on `If-None-Match`, following `internal/web/feed.go`.
- Show a "Slip of the day" badge on the permalink of the pinned entry while keeping `/e/{id}` as the canonical URL.
- Add a fifth TRMNL plugin `vandyke-slip` that reads `/api/v1/slip`, packaged and pushed beside the existing four.
- Make the permalink share control call `navigator.share` when the browser supports it, fall back to the existing clipboard copy, show a visible success state and announce the result through the existing polite live region.
- Add JSON-LD to permalinks (`DefinedTerm` in a `DefinedTermSet`) and to `/stats` (`Dataset`).
- Add canonical links and a robots policy: `/`, `/stats` and `/e/{id}` stay indexable and self-canonical, while `/entries` and every filtered or paginated listing URL become `noindex,follow`.
- Add a committed 1200x630 PNG fallback preview image so every permalink emits `og:image` and `twitter:image` with `summary_large_image`, including text-only entries.
- Serve `GET /opensearch.xml` and add the `<link rel="search">` discovery tag to every page head, with the URL template `{base}/?q={searchTerms}`.

### Non-goals

- Push notifications, e-mail digests or any scheduled job; the process still has no scheduler.
- Per-user, per-session or per-request personalisation of the slip.
- An editorial calendar, an admin override of the day's pick, or a table of past slips.
- Runtime generation of Open Graph card images.
- Third-party share SDKs, analytics of share events beyond the existing Umami `track` call, AMP, or i18n and `hreflang`.
- An OpenSearch suggestions endpoint.

## Capabilities

### New Capabilities

- `ledger-daily-slip`: A deterministic, pinned slip of the day with a home-page section, a JSON endpoint, a permalink badge and a TRMNL plugin.
- `ledger-sharing`: Web Share API and clipboard sharing, JSON-LD structured data, canonical and robots hygiene, a static social preview image and OpenSearch discovery.

## Impact

- `internal/slip/slip.go`: new resolver that computes the day's pick, pins it and returns the entry.
- `internal/store/store.go`: new single-key setting accessors `GetSetting` and `SetSettingIfAbsent`; `AllEntryIDs` and `GetEntry` are reused unchanged, and no migration is added.
- `internal/web/web.go`: slip section in `buildPage`, the badge in `handlePermalink`, the `GET /opensearch.xml` route, the robots and canonical decisions, and the fallback preview URL.
- `internal/web/templates/base.html`, `permalink.html`, `stats.html`: slip section, badge, canonical, robots, JSON-LD and social tags.
- `internal/web/templates/slip.html`: new server-side slip section included from `base.html`.
- `internal/httpapi/api.go`: `GET /api/v1/slip`.
- `internal/web/static/js/app.js`: share-then-copy handler and the search-time hide of the slip section; `internal/web/web_test.go` asserts the current `data-copy-link` markup at line 550 and must move to the new attributes.
- `internal/web/static/images/og-default.png`: new committed 1200x630 preview image; `internal/web/static/css/app.css` for the slip section and badge.
- `trmnl/plugins/vandyke-slip/` (`.trmnlp.yml`, two icons, `src/settings.yml`, `src/transform.py`, four Liquid views), `trmnl/README.md`, `trmnl/tests/test_vandyke_transform.py` and the packaging assertion in `.github/workflows/trmnl.yaml`.
- `README.md`, `docs/superpowers/specs/2026-11-11-daily-slip-and-sharing-design.md` and `docs/index.md`.
