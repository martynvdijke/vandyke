## ADDED Requirements

### Requirement: Tag storage and case-insensitive identity
The system SHALL store tags in a `tags` table with `id`, `name`, `slug`, `color` and `created_at`, and assignments in an `entry_tags` table keyed by `(entry_id, tag_id)` with `ON DELETE CASCADE` on both foreign keys. `tags.name` and `tags.slug` SHALL each be unique under `COLLATE NOCASE`, and `entry_tags` SHALL be indexed by `(tag_id, entry_id)` in addition to its primary key.

#### Scenario: Duplicate name in another case
- **WHEN** an admin creates a tag named `Typo` while a tag named `typo` exists
- **THEN** the create is rejected as a duplicate and no row is written

#### Scenario: Deleting a tag removes its assignments
- **WHEN** a tag with assignments on several entries is deleted
- **THEN** its `entry_tags` rows are removed in the same transaction and the entries themselves are unchanged

#### Scenario: Purging an entry removes its assignments
- **WHEN** a trashed entry is permanently purged
- **THEN** its `entry_tags` rows cascade away with the `entries` row

### Requirement: Tag validation and stable slug
The system SHALL derive a lowercase slug from the tag name at creation, SHALL keep that slug unchanged when the name is renamed, and SHALL reject a name longer than 40 runes, a colour that is not `#RRGGBB`, or a name that yields an empty slug.

#### Scenario: Renaming keeps the slug
- **WHEN** an admin renames the tag `typo` to `spelling slip`
- **THEN** the name changes, the slug stays `typo`, and existing `/tags/typo` links still resolve

#### Scenario: Invalid colour
- **WHEN** an admin submits the colour `red` or `#ff00`
- **THEN** the form is re-rendered with 422, an inline error, and no stored change

#### Scenario: Name of punctuation only
- **WHEN** an admin submits a tag name of `!!!`
- **THEN** the create is rejected because the derived slug would be empty

### Requirement: Seed vocabulary
The migration SHALL insert the tags `typo`, `misheard`, `autocorrect`, `name` and `dialect` with fixed colours and no entry assignments, exactly once.

#### Scenario: First boot after the migration
- **WHEN** the database is migrated from `012` to `014`
- **THEN** the five seed tags exist with zero assignments and no entry is tagged

#### Scenario: Seed tags behave like any other tag
- **WHEN** an admin renames or deletes a seed tag and the process restarts
- **THEN** the rename is kept or the deleted tag stays absent, because the seed is not re-applied

### Requirement: Admin tag management
The system SHALL provide `GET /admin/tags`, restricted to an authenticated admin, listing every tag with its live entry count and its total assignment count, a create form, a rename and colour form per tag, and a delete form whose confirmation states how many entries lose the tag.

#### Scenario: List and counts
- **WHEN** an admin opens `/admin/tags` and one entry carrying `typo` is trashed
- **THEN** the row for `typo` shows a live count that excludes the trashed entry and a total count that includes it

#### Scenario: Create and recolour
- **WHEN** an admin creates `misheard` with colour `#3b5a8c` and later changes only the colour
- **THEN** the tag is listed with that colour and its slug is unchanged

#### Scenario: Delete is owner-only
- **WHEN** a signed-in editor posts to `/admin/tags/{id}/delete`
- **THEN** the request is rejected, the tag and its assignments remain, and no audit row for a deletion is written

#### Scenario: Delete confirmation names the damage
- **WHEN** an owner opens the delete control for a tag carried by 12 entries
- **THEN** the confirmation states that 12 entries lose the tag

### Requirement: Admin assignment on the entry edit form
The system SHALL render every tag as a checkbox in the tag fieldset of `GET /admin/entries/{id}/edit`, SHALL treat the submitted checkboxes as the complete set for that entry, and SHALL clear all tags when none is submitted.

#### Scenario: Assign two tags
- **WHEN** an admin saves an entry with `typo` and `misheard` checked
- **THEN** the entry carries exactly those two tags and the ledger row shows two chips

#### Scenario: Clear every tag
- **WHEN** an admin saves an entry with no tag checked
- **THEN** every assignment for that entry is removed and none is created

#### Scenario: Tags survive the trash
- **WHEN** a tagged entry is trashed and later restored
- **THEN** its assignments are still present after the restore

#### Scenario: Public creation stays untagged
- **WHEN** an entry is created through `POST /entries` or `POST /api/v1/entries`
- **THEN** the entry is stored with no tags

### Requirement: Audit trail for tag mutations
The system SHALL record an audit row through the existing `auditAs` helper for every tag create, rename, colour change and delete, and for every change to an entry's tag set, using the tag id or entry id as subject.

#### Scenario: Tag creation is audited
- **WHEN** an admin creates a tag
- **THEN** an audit row with action `tag.create` records the actor, the new tag id and its name

#### Scenario: Assignment change is audited
- **WHEN** an entry's tags change from `typo` to `typo,misheard`
- **THEN** an audit row with action `entry.tags` records the entry id and the resulting slugs

### Requirement: Public tag filter composes with existing filters
`GET /entries` and `GET /api/v1/entries` SHALL accept one or more `tag` parameters naming tag slugs and SHALL return only entries carrying every named tag, intersected with `q`, `from`, `to`, `year`, `photo` and `person`.

#### Scenario: Tag with a query and a year
- **WHEN** a client requests `?tag=misheard&q=feature&year=2024`
- **THEN** only entries that carry `misheard`, match the query and fall in 2024 are returned

#### Scenario: Two tags intersect
- **WHEN** a client requests `?tag=typo&tag=name`
- **THEN** only entries carrying both tags are returned

#### Scenario: Unknown tag
- **WHEN** a client requests `?tag=does-not-exist`
- **THEN** the response is 200 with an empty result, not an error

#### Scenario: Web filter view
- **WHEN** the ledger is loaded with `?tag=typo` and the visitor changes the sort order
- **THEN** the new URL still carries `tag=typo` and the filtered result is preserved

### Requirement: Tag chips on public entries
The ledger row and the permalink SHALL show one chip per tag of the entry, each linking to the filtered ledger for that tag.

#### Scenario: Row chips
- **WHEN** the ledger page renders an entry carrying `misheard` and `dialect`
- **THEN** the row shows both chips and each links to `/entries?tag=<slug>`

#### Scenario: Permalink chips
- **WHEN** a tagged entry's `/e/{id}` page is rendered
- **THEN** the page shows the entry's tags with the same links

### Requirement: Tag index and canonical tag pages
The system SHALL provide `GET /tags` listing every tag with its live count, and `GET /tags/{slug}` rendering the ledger filtered to one tag with its own canonical URL. An unknown slug SHALL return 404.

#### Scenario: Index with counts
- **WHEN** a visitor opens `/tags`
- **THEN** every tag is listed with the number of live entries that carry it, and a tag with no live entry is shown with zero

#### Scenario: Canonical tag page
- **WHEN** a visitor opens `/tags/typo`
- **THEN** the page lists the entries carrying `typo`, paginates, and names `/tags/typo` as its canonical URL

#### Scenario: Tag page for a tag with only trashed entries
- **WHEN** every entry carrying a tag is trashed and a visitor opens that tag's page
- **THEN** the response is 200 with the empty state

#### Scenario: Unknown slug
- **WHEN** a visitor opens `/tags/no-such-tag`
- **THEN** the response is 404 with the existing not-found page

### Requirement: Tagged feed
`GET /feed.xml` SHALL accept `tag=<slug>` and return only entries carrying that tag, with the tag reflected in the feed id, the self link and the ETag.

#### Scenario: Feed for one tag
- **WHEN** a client requests `/feed.xml?tag=autocorrect`
- **THEN** every item is an entry carrying `autocorrect` and the feed's self link names the same URL

#### Scenario: Unknown tag in the feed
- **WHEN** a client requests `/feed.xml?tag=no-such-tag`
- **THEN** the response is 200 with a valid feed that has no items

### Requirement: Tag sitemap entries
The sitemap SHALL include `/tags` and one `/tags/{slug}` URL for every tag with at least one live entry, built from `PUBLIC_BASE_URL` when set, and SHALL omit zero-count tags and query-string filter URLs.

#### Scenario: Tags in the sitemap
- **WHEN** `GET /sitemap.xml` is requested and `typo` has live entries while `dialect` has only trashed ones
- **THEN** `/tags` and `/tags/typo` are listed and `/tags/dialect` is not

### Requirement: Trashed entries never count
The system SHALL exclude entries with a non-null `deleted_at` from tag counts, tag filter results, tag pages, the tagged feed and the sitemap.

#### Scenario: Trash lowers the count
- **WHEN** an entry carrying `misheard` is trashed
- **THEN** the `misheard` live count drops by one, the entry is absent from `/tags/misheard`, `/entries?tag=misheard`, `/feed.xml?tag=misheard`, and it is not restored until the entry is restored
