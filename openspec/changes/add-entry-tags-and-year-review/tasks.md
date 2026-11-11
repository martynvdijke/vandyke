## 1. Schema

- [ ] 1.1 Add `internal/store/migrations/014_entry_tags.sql` creating `tags(id, name, slug, color, created_at)` and `entry_tags(entry_id, tag_id, created_at)`, with `PRIMARY KEY (entry_id, tag_id)`, both foreign keys `ON DELETE CASCADE`, and the connection's default `created_at` timestamp format.
- [ ] 1.2 Add `CREATE UNIQUE INDEX idx_tags_name ON tags (name COLLATE NOCASE)` and `CREATE UNIQUE INDEX idx_tags_slug ON tags (slug COLLATE NOCASE)`.
- [ ] 1.3 Add `CREATE INDEX idx_entry_tags_tag ON entry_tags (tag_id, entry_id)` for tag filtering and counting.
- [ ] 1.4 Seed `typo`, `misheard`, `autocorrect`, `name` and `dialect` in the same migration with their fixed colours and no assignments.
- [ ] 1.5 Add a migration test that applies all migrations to an empty database, confirms both tables and all three indexes exist, confirms the five seeds are present, and confirms that applying the migration file twice is not possible through `schema_migrations`.
- [ ] 1.6 Add a store test that deletes a tag and asserts its `entry_tags` rows are gone, and one that purges a trashed entry and asserts its join rows are gone.

## 2. Store: tag records

- [ ] 2.1 Add `MaxTagNameLen` and `ValidateTag(name, color string)` covering a trim, the 40-rune limit, `#RRGGBB` and a non-empty slug.
- [ ] 2.2 Add `slugifyTag(name string) string` (lowercase ASCII letters and digits, other runs collapsed to `-`, trimmed) and assert the same slug for `Ty-po` and `typo`.
- [ ] 2.3 Add `type Tag struct` with `ID`, `Name`, `Slug`, `Color`, `CreatedAt`, plus `LiveCount` and `TotalCount` for the admin view.
- [ ] 2.4 Add `ListTags` returning every tag with its live count (`entries.deleted_at IS NULL`) and `ListAdminTags` returning both counts.
- [ ] 2.5 Add `GetTagBySlug` returning `ErrNotFound` for an unknown slug, and `GetTag` by id for the admin routes.
- [ ] 2.6 Add `CreateTag`, returning a duplicate-name error mapped to a form error, and `UpdateTag` for name and colour with the slug left untouched.
- [ ] 2.7 Add `DeleteTag` returning the number of join rows removed in the same transaction.
- [ ] 2.8 Add `EntryTags(ctx, entryID)` and `TagsByEntry(ctx, ids)` with a single batch query, and `SetEntryTags(ctx, entryID, tagIDs)` replacing the set in one transaction with id deduplication.

## 3. Store: tag filter

- [ ] 3.1 Add `Tags []string` to `ListParams` and normalise empty values away in `NormalizeListParams`.
- [ ] 3.2 Add one `EXISTS` clause per tag to `ledgerFilter`, joined with `AND`, so tags intersect with `q`, `from`/`to`, `year`, `photo` and `person`.
- [ ] 3.3 Add store tests for tag-only filtering, tag plus `q`, tag plus `year`, two tags intersecting, an unknown slug returning an empty page, and a trashed entry matching neither a tag filter nor a tag count.

## 4. Store: year review

- [ ] 4.1 Add `type YearReview struct` holding the year, total, first and last entry, twelve month counts, busiest month and count, top persons, longest entry, photo count and photo percentage input.
- [ ] 4.2 Add `YearReview(ctx, year)` returning `ErrNotFound` when the year holds no live entries, computing the aggregate, the month buckets and the top five persons with the documented tie-breakers.
- [ ] 4.3 Fetch the first and last entry and the longest misspelling through the same visibility rules, excluding empty `word` values from the longest-misspelling figure.
- [ ] 4.4 Add store tests for an empty year, a year with only trashed entries, the busiest-month tie, the top-persons tie, the longest-word tie and the photo count.

## 5. Admin: tag management

- [ ] 5.1 Add `internal/admin/tags.go` with handlers for the list, create, update and delete actions.
- [ ] 5.2 Register `GET /admin/tags` behind `requireAuth`, `POST /admin/tags` and `POST /admin/tags/{id}` behind `withCSRF`, and `POST /admin/tags/{id}/delete` behind `withOwnerCSRF`.
- [ ] 5.3 Add `internal/admin/templates/tags.html` listing every tag with its live and total counts, a create form, a per-row rename and colour form, and a delete form whose confirmation names the total entry count.
- [ ] 5.4 Reject an invalid name or colour with 422 and an inline error that preserves the submitted values.
- [ ] 5.5 Record `tag.create`, `tag.rename`, `tag.color` and `tag.delete` through `auditAs`, with the tag id as subject and the changed values as detail.
- [ ] 5.6 Add a `Tags` link to the admin navigation in `internal/admin/templates/base.html`.
- [ ] 5.7 Add handler tests for create, rename, recolour, delete, the owner-only delete, the missing-CSRF rejection and the invalid-colour rejection.

## 6. Admin: entry tags on the edit form

- [ ] 6.1 Add `Tags []tagOption` to `editData` and populate it in `handleEdit`, marking the entry's current tags.
- [ ] 6.2 Render a tag fieldset of checkboxes named `tags` in `internal/admin/templates/edit.html`, with a link to `/admin/tags` for creating one.
- [ ] 6.3 Call `SetEntryTags` in `handleUpdate` after a successful `UpdateEntry`, reading `r.PostForm["tags"]`.
- [ ] 6.4 Include the sorted resulting slugs in the `entry.tags` audit detail, including the empty case.
- [ ] 6.5 Add handler tests for adding a tag, clearing every tag, assigning an unknown tag id and confirming that a tag survives a trash-and-restore cycle.

## 7. Public: tag filter and chips

- [ ] 7.1 Parse every `tag` value in `parseListParams` (`internal/web/web.go`) and in `handleListEntries` (`internal/httpapi/api.go`) into `ListParams.Tags`.
- [ ] 7.2 Carry the tags in `ledgerQuery`, include them in `values()` and emit hidden `tag` inputs in the `ledger-toolbar` form so sort, pager and filter submissions keep the tag.
- [ ] 7.3 Load tags for the visible page with `TagsByEntry` and render chips on each row of the `ledger` partial, linking to `/entries?tag=<slug>`.
- [ ] 7.4 Render the entry's tags on the permalink page.
- [ ] 7.5 Add the tag filter to the toolbar's hidden state on the home page and confirm the `HX-Request` fragment path still returns the filtered rows.
- [ ] 7.6 Add web and API tests for `?tag=`, `?tag=` combined with `q` and `year`, an unknown tag returning an empty 200, and a trashed entry being absent from a tag result.

## 8. Public: tag pages, feed and sitemap

- [ ] 8.1 Add `GET /tags` behind the existing web handler set, rendering every tag with its live count and an empty state from a new `internal/web/templates/tags.html`.
- [ ] 8.2 Add `GET /tags/{slug}` rendering the toolbar and ledger for one tag from a new `internal/web/templates/tag.html`, returning 404 for an unknown slug and 200 with the empty state for a tag whose entries are all trashed.
- [ ] 8.3 Emit `<link rel="canonical">` on `/tags/{slug}` from a new `pageData.CanonicalURL` rendered conditionally in `page-head`; leave `/entries` in every form, including `?tag=`, `noindex,follow` with no canonical link.
- [ ] 8.4 Add the `tag` parameter to `handleFeed`, filtering through `ListParams.Tags`, and include the tag in the feed id, the self link and the ETag.
- [ ] 8.5 Add `/tags` and every tag with at least one live entry to `handleSitemap`, and leave zero-count tags and `/entries?tag=` out.
- [ ] 8.6 Add a `Tags` link to the public footer navigation.
- [ ] 8.7 Add tests for the tag index counts, the tag page, the unknown slug 404, the feed filter and ETag, the sitemap entries, and the absence of a trashed entry from all four.

## 9. Public: year recap

- [ ] 9.1 Add `GET /y/{year}` to the router and a handler that parses a four-digit year, calls `YearReview` and renders the existing not-found page with 404 on `ErrNotFound` or a malformed segment.
- [ ] 9.2 Add `internal/web/templates/year.html` rendering totals, the first and last entry with permalinks, the busiest month, the twelve-month chart, the top five persons, the longest misspelling, the photo ratio and a link to `/entries?year=<year>`.
- [ ] 9.3 Emit canonical, Open Graph and Twitter metadata from the year view model, with every absolute URL built by `absoluteURL`.
- [ ] 9.4 Emit the ETag hash and `Cache-Control: public, max-age=300`, answer a matching `If-None-Match` with 304 and no body, and send no ETag on the 404.
- [ ] 9.5 Add the month chart figure and the top-persons ranking each with a sibling `table.visually-hidden` caption and header row, matching `stats.html`.
- [ ] 9.6 Add the years with at least one live entry to `handleSitemap`.
- [ ] 9.7 Add handler tests for a populated year, a year with only trashed entries, a malformed year, the 304, the changed ETag after an edit, and the sitemap entry.

## 10. Stats link and styling

- [ ] 10.1 Add `Href` to `yearBar`, set it only when the bucket count is above zero, and render a `nav aria-label="Year in review"` of year links under the by-year figure in `stats.html`, outside the `aria-hidden` chart.
- [ ] 10.2 Add tag chip and recap table styling to `internal/web/static/css/app.css` without changing the existing person chip.
- [ ] 10.3 Add a test that the stats page links the years that have entries and does not link a gap year.

## 11. Docs

- [ ] 11.1 Add `docs/superpowers/specs/2026-11-11-entry-tags-and-year-review-design.md` describing the schema, the filter composition, the canonical tag URLs, the recap and its caching.
- [ ] 11.2 Link the new entry from `docs/index.md`.
- [ ] 11.3 Document `/admin/tags`, `?tag=`, `/tags`, `/tags/{slug}`, `/feed.xml?tag=` and `/y/{yyyy}` in `README.md`.

## 12. Testing

- [ ] 12.1 Run `go test ./internal/store ./internal/admin ./internal/httpapi ./internal/web`.
- [ ] 12.2 With a running instance, confirm `/tags` counts, `/tags/{slug}`, `?tag=` on the ledger and the API, and `/feed.xml?tag=` agree with each other.
- [ ] 12.3 Trash a tagged entry and confirm it leaves the tag count, the tag page, the feed, the sitemap and the year recap, and that restoring it brings them back.
- [ ] 12.4 Open `/y/{yyyy}` for a populated year, confirm every figure matches `/stats` and the filtered ledger, and confirm the page renders without JavaScript.
- [ ] 12.5 Run `go vet ./...` and `go test ./...`.
