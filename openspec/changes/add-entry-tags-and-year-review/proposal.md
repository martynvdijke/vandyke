## Why

An entry carries `word`, `meaning`, an optional `person` and a `created_at`, and the only grouping the app can express is a filter: `ledgerFilter` (`internal/store/store.go`) supports `q`, `year`, `from`/`to`, `photo` and an exact `person`, and `Stats` charts entries by year and by person. Nothing records what kind of slip an entry is, so distinctions the committee already makes in conversation ("misheard", "typo", "autocorrect", "name", "dialect") cannot be written down, counted or linked to. Separately, a year is only a bar in `/stats`: there is no page that says what a year looked like, and no URL to share for it.

The `entries` table has no join table (migrations `001`–`012`), `007_dedupe_entries.sql` makes `(word COLLATE NOCASE, meaning COLLATE NOCASE)` unique, and `entries_fts` is a trigram index over `word`, `meaning` and `person` — so tags are new storage, not new search text.

## What Changes

- Add migration `014_entry_tags.sql` with a `tags` table (`name`, `slug`, `color`, `created_at`), a `entry_tags` join table keyed by `(entry_id, tag_id)` with cascading deletes, a reverse index for filtering and counting, and case-insensitive unique indexes on `name` and `slug`.
- Seed five tags in that migration (`typo`, `misheard`, `autocorrect`, `name`, `dialect`) with no assignments; the migration ledger makes the seed one-time, so a later rename or delete is permanent.
- Add store methods to list tags with live counts, create, rename, recolour and delete a tag, read the tags of one entry and replace the tags of one entry.
- Add `ListParams.Tags` and one `EXISTS` clause per requested tag in `ledgerFilter`, so `?tag=` intersects with `q`, `from`/`to`, `year`, `photo` and `person`, and repeated `tag` values intersect with each other.
- Add tag assignment to the existing admin entry edit form and a new admin tag page at `GET /admin/tags` with create, rename, colour and delete, where delete states how many entries lose the tag.
- Record audit rows for every tag mutation, following the existing `auditAs` action names.
- Add `?tag=` to `GET /entries` and `GET /api/v1/entries`, tag chips on ledger rows and permalinks, a `GET /tags` index with per-tag counts, canonical `GET /tags/{slug}` pages, `GET /feed.xml?tag=`, and per-tag sitemap entries.
- Add `GET /y/{yyyy}`: a server-rendered recap of one year with totals, first and last entry, busiest month, a twelve-month chart, top persons, the longest misspelling, the photo ratio and a link into `GET /entries?year=`.
- Link each year in the `/stats` by-year chart to its recap, and include years with entries in the sitemap.
- Keep trashed entries out of every new count, filter, feed, sitemap entry and recap.

### Non-goals

- Public tag creation or public assignment of tags to entries.
- Tag hierarchies, aliases or merging two tags into one.
- ML or rules-based classification of existing entries.
- Per-tag permissions or per-tag moderation.
- Changing the `(word, meaning)` uniqueness rule, or deriving "repeated slip" counts from tags.
- Bulk tag assignment from the admin entries list.
- Tags in the JSON entry payload returned by `GET /api/v1/entries` and `GET /api/v1/entries/{id}`.
- User-generated recaps, email digests, runtime image generation for share cards, per-month recap pages, and stored aggregate tables for a year.

## Capabilities

### New Capabilities

- `entry-tags`: A tag vocabulary, admin management and assignment, and public tag filtering, chips, index pages, feed filter and sitemap entries.
- `year-in-review`: A generated, cacheable recap page for one calendar year of the ledger.

### Modified Capabilities

- `ledger-search`: The list filters now combine by intersection with a `tag` filter that stays outside the `q` grammar.

## Impact

- New migration `internal/store/migrations/014_entry_tags.sql`.
- `internal/store/store.go`: `Tag`, `TagCounts`, `ListTags`, `ListAdminTags`, `GetTagBySlug`, `CreateTag`, `UpdateTag`, `DeleteTag`, `EntryTags`, `TagsByEntry`, `SetEntryTags`, `ValidateTag`, `ListParams.Tags`, the tag clause in `ledgerFilter`, and `YearReview`.
- `internal/httpapi/api.go`: `tag` parsing on `handleListEntries`.
- `internal/web/web.go`: `/tags`, `/tags/{slug}` and `/y/{year}` routes, tag plumbing in `buildLedger`/`parseListParams`, `ledgerQuery.Tags`, `CanonicalURL` for standalone pages, `yearBar.Href`, and years plus tags in `handleSitemap`.
- `internal/web/feed.go`: tag filter, tag in the feed id and self link, tag in the ETag.
- `internal/web/templates/tags.html`, `tag.html`, `year.html` (new), plus `base.html` (hidden tag inputs, canonical field), `partials.html` (row chips), `permalink.html` (chips) and `stats.html` (recap links).
- `internal/web/static/css/app.css`: tag chip styling.
- `internal/admin/admin.go`: tag routes and the edit-form tag submission.
- `internal/admin/tags.go` and `internal/admin/templates/tags.html` (new), `internal/admin/entries.go` and `internal/admin/templates/edit.html` (tag fieldset), `internal/admin/templates/base.html` (navigation).
- `README.md` endpoint and admin rows, a new `docs/superpowers/specs/2026-11-11-entry-tags-and-year-review-design.md`, and a link from `docs/index.md`.
