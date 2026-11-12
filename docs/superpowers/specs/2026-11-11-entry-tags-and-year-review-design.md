# Entry tags and year in review

**Date:** 2026-11-11
**Status:** implemented
**Change:** `openspec/changes/add-entry-tags-and-year-review`

## Storage

Migration `014_entry_tags.sql` adds two tables. `tags` stores the curated
vocabulary: `id`, a case-insensitively unique `name` (`idx_tags_name`), an
immutable `slug` (`idx_tags_slug`), a `color` and `created_at`. `entry_tags`
is the join, keyed `(entry_id, tag_id)`, with both foreign keys
`ON DELETE CASCADE`; `idx_entry_tags_tag (tag_id, entry_id)` serves filtering
and counting, while the composite primary key serves entry-to-tag reads.

The migration inserts `typo`, `misheard`, `autocorrect`, `name` and `dialect`
with fixed muted colours and no assignments. The seed lives in the migration,
so `schema_migrations` applies it once: renaming or deleting a seed is permanent,
and no entry is classified automatically.

The slug is derived from the name at creation — lowercase ASCII letters and
digits kept, every other run collapsed to one hyphen, trimmed — and never
changes. Renaming changes `name` only, so `/tags/{slug}` and `?tag={slug}`
links survive. `ValidateTag` caps the name at 40 runes, rejects a name that
yields an empty slug and requires `#RRGGBB`. Tags stay out of `store.Entry`;
the JSON API shape is unchanged.

## Filter composition

`ListParams` gains `Tags []string`. `NormalizeListParams` trims each value,
drops empties and de-duplicates case-insensitively. `ledgerFilter` appends one
`EXISTS` clause per tag:

```sql
EXISTS (SELECT 1 FROM entry_tags et JOIN tags t ON t.id = et.tag_id
        WHERE et.entry_id = e.id AND t.slug = ? COLLATE NOCASE)
```

Every clause is joined with `AND`, so a tag intersects with `q`, `from`, `to`,
`year`, `photo` and `person`, and two `tag` values intersect with each other.
The clause starts from `e.deleted_at IS NULL`, so a trashed entry can never
match. Tags are a filter, not search text: `entries_fts` gains no column and
the `q` grammar is untouched.

`GET /entries` and `GET /api/v1/entries` parse every `tag` value. Unknown slugs
return `200` with an empty result on both filter views, exactly like
`person=nobody`.

## Public surfaces

- Ledger rows and `/e/{id}` permalinks render one chip per tag, each linking to
  `/entries?tag={slug}`. `TagsByEntry` loads every tag of the visible page in
  one query, so the ledger adds one statement regardless of page size.
- `GET /tags` lists the whole vocabulary with live counts, including zero: it
  is the taxonomy index and a fresh tag must be reachable.
- `GET /tags/{slug}` renders the ledger filtered to one tag through the
  ordinary toolbar and ledger partials, so `q`, dates, year, photo, person,
  sort, pagination and the HTMX fragment path all keep working. The tag rides
  in a hidden input and in the generated sort and pager URLs. An unknown slug
  is `404`; a tag whose entries are all trashed is `200` with the empty state.
- `GET /feed.xml?tag={slug}` filters the feed. The tag is folded into the feed
  id, the self link and the ETag (a hash of the tag list), so tagged feeds never
  share a cache entry and an empty tag feed still differs from another empty
  tag feed.
- `GET /sitemap.xml` lists `/tags`, every tag with at least one live entry, and
  every year with at least one live entry. Zero-count tags, empty years and
  query-string filter URLs are omitted.

## Canonicalisation

`page-head` gains a `CanonicalURL` field rendered only when set, and a `Robots`
field for the rare page that needs `noindex`. `/tags` and `/tags/{slug}` are
standalone documents and emit a self canonical. `/entries` in every form,
including `?tag=`, is `noindex,follow` with no canonical link: the tag page is
the indexable document and the filter view must not compete with it. The
sitemap never lists query-string URLs.

## Year in review

`GET /y/{yyyy}` is computed on read from the `entries` table and rendered on the
server with no JavaScript and no stored aggregates. `YearReview` returns the
live total, the first and last entry, twelve month buckets, the busiest month
and its count, the top five persons, the longest misspelling, and the photo
count and percentage.

The first and last entries come from `ListEntries` with `Year`, `PerPage: 1`
and `created_at asc`/`desc`, so visibility and ordering stay in one place. The
longest misspelling uses `ORDER BY length(word) DESC, created_at ASC, id ASC`
with `word <> ''`, so a photo-only year states that no misspelling was long
enough to name. Ties are deterministic: the earliest month wins, persons sort
by count then `person COLLATE NOCASE`, and the longest misspelling breaks ties
by earliest `created_at` then lowest id. The photo ratio uses the same rounded
integer formula as `/stats`.

A year with no live entries, or a path segment that is not four digits, returns
the ordinary not-found page with `404` and no ETag. A populated recap sends
`ETag: "year-{yyyy}-{first 16 hex of sha256 over the figures}"` and
`Cache-Control: public, max-age=300`; a matching `If-None-Match` answers `304`
with no body. Hashing the figures rather than an id means editing, trashing or
restoring an entry inside the year changes the tag.

The month chart and the person ranking follow the `/stats` accessibility
pattern: an `aria-hidden` figure plus a sibling `table.visually-hidden` with a
caption, header row and one row per value. The `/stats` by-year chart links only
years whose bucket count is above zero, because `/y/{yyyy}` would `404` for a
gap year; the links sit in a separate `nav aria-label="Year in review"` outside
the hidden chart. Canonical, Open Graph and Twitter URLs are built by
`absoluteURL`, so `PUBLIC_BASE_URL` wins behind a proxy.

## Admin

`GET /admin/tags` lists every tag with a live count and a total assignment
count, a create form and a per-row rename/recolour form. Create and update sit
behind CSRF; delete is owner-only behind CSRF because it destroys assignments
for every entry at once, and its confirmation states how many entries lose the
tag. Validation failures re-render with `422`, preserving the submitted values.
Every mutation writes an audit row through `auditAs`: `tag.create`,
`tag.rename`, `tag.color` and `tag.delete` with the tag id as subject, and
`entry.tags` with the entry id and the sorted resulting slugs.

The existing entry edit form gains a tag fieldset of checkboxes named `tags`.
The submitted set replaces the entry's assignments in one transaction; an empty
submission clears every tag, and unknown ids are ignored. Tags survive the trash
and disappear only when the entry is purged, when the join rows cascade.
