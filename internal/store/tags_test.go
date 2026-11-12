package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestEntryTagsMigrationCreatesTablesIndexesAndSeeds(t *testing.T) {
	st := newTestStore(t)

	for _, table := range []string{"tags", "entry_tags"} {
		var name string
		if err := st.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&name); err != nil {
			t.Fatalf("table %s: %v", table, err)
		}
	}
	for _, index := range []string{"idx_tags_name", "idx_tags_slug", "idx_entry_tags_tag"} {
		var name string
		if err := st.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index,
		).Scan(&name); err != nil {
			t.Fatalf("index %s: %v", index, err)
		}
	}

	want := map[string]string{
		"typo":        "#8c3b3b",
		"misheard":    "#3b5a8c",
		"autocorrect": "#3b7a57",
		"name":        "#8c6b3b",
		"dialect":     "#6b3b8c",
	}
	tags, err := st.ListTags(context.Background())
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != len(want) {
		t.Fatalf("seeded tags = %d, want %d", len(tags), len(want))
	}
	for _, tag := range tags {
		color, ok := want[tag.Name]
		if !ok {
			t.Fatalf("unexpected seed tag %q", tag.Name)
		}
		if tag.Slug != tag.Name {
			t.Errorf("seed %q slug = %q", tag.Name, tag.Slug)
		}
		if tag.Color != color {
			t.Errorf("seed %q colour = %q, want %q", tag.Name, tag.Color, color)
		}
		if tag.LiveCount != 0 {
			t.Errorf("seed %q has %d live assignments, want 0", tag.Name, tag.LiveCount)
		}
	}
	var assignments int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM entry_tags`).Scan(&assignments); err != nil {
		t.Fatalf("count assignments: %v", err)
	}
	if assignments != 0 {
		t.Fatalf("migration tagged %d entries, want 0", assignments)
	}
}

func TestMigrationIsRecordedOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "twice.db")

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = second.Close() }()

	var applied int
	if err := second.db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, "014_entry_tags.sql",
	).Scan(&applied); err != nil {
		t.Fatalf("count migration: %v", err)
	}
	if applied != 1 {
		t.Fatalf("014 recorded %d times, want 1", applied)
	}
	var tags int
	if err := second.db.QueryRow(`SELECT COUNT(*) FROM tags`).Scan(&tags); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if tags != 5 {
		t.Fatalf("tags after re-open = %d, want the five seeds only", tags)
	}
}

func TestTagSlugifyAndValidation(t *testing.T) {
	slugs := []struct {
		name string
		want string
	}{
		{"Ty-po", "ty-po"},
		{"typo", "typo"},
		{"  Ty  po  ", "ty-po"},
		{"TY PO", "ty-po"},
		{"Autocorrect", "autocorrect"},
		{"SELF-correction", "self-correction"},
		{"!!!", ""},
		{"--", ""},
		{"café", "caf"},
	}
	for _, tc := range slugs {
		if got := slugifyTag(tc.name); got != tc.want {
			t.Errorf("slugifyTag(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}

	valid := []struct{ name, color string }{
		{"typo", "#8c3b3b"},
		{"Spelling slip", "#AB12CD"},
		{"!!!", "#8c3b3b"},
	}
	for _, tc := range valid {
		err := ValidateTag(tc.name, tc.color)
		if tc.name == "!!!" {
			if err == nil {
				t.Errorf("ValidateTag(%q) accepted an empty slug", tc.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("ValidateTag(%q, %q) = %v", tc.name, tc.color, err)
		}
	}
	invalid := []struct{ name, color string }{
		{"", "#8c3b3b"},
		{"12345678901234567890123456789012345678901", "#8c3b3b"},
		{"typo", "red"},
		{"typo", "#ff00"},
		{"typo", "#gggggg"},
		{"typo", ""},
	}
	for _, tc := range invalid {
		if err := ValidateTag(tc.name, tc.color); err == nil {
			t.Errorf("ValidateTag(%q, %q) accepted an invalid value", tc.name, tc.color)
		}
	}
}

func TestTagCRUDAndStableSlug(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if _, err := st.CreateTag(ctx, "Typo", "#8c3b3b"); !errors.Is(err, ErrTagExists) {
		t.Fatalf("duplicate name error = %v, want ErrTagExists", err)
	}

	created, err := st.CreateTag(ctx, "  Brand new  ", "#3B5A8C")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if created.Name != "Brand new" || created.Slug != "brand-new" || created.Color != "#3b5a8c" {
		t.Fatalf("created tag = %+v", created)
	}

	renamed, err := st.UpdateTag(ctx, created.ID, "Spelling slip", "#3b5a8c")
	if err != nil {
		t.Fatalf("UpdateTag rename: %v", err)
	}
	if renamed.Name != "Spelling slip" || renamed.Slug != "brand-new" {
		t.Fatalf("renamed tag = %+v, want the original slug", renamed)
	}

	recoloured, err := st.UpdateTag(ctx, created.ID, "Spelling slip", "#112233")
	if err != nil {
		t.Fatalf("UpdateTag recolour: %v", err)
	}
	if recoloured.Color != "#112233" || recoloured.Slug != "brand-new" {
		t.Fatalf("recoloured tag = %+v", recoloured)
	}

	bySlug, err := st.GetTagBySlug(ctx, "BRAND-NEW")
	if err != nil || bySlug.ID != created.ID {
		t.Fatalf("GetTagBySlug = %+v (%v)", bySlug, err)
	}
	if _, err := st.GetTagBySlug(ctx, "no-such-tag"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown slug error = %v, want ErrNotFound", err)
	}
	if _, err := st.GetTag(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id error = %v, want ErrNotFound", err)
	}
	if _, err := st.UpdateTag(ctx, 999999, "gone", "#112233"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update unknown id error = %v, want ErrNotFound", err)
	}

	if _, err := st.DeleteTag(ctx, created.ID); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if _, err := st.DeleteTag(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete error = %v, want ErrNotFound", err)
	}
}

func TestDeleteTagCascadesAssignments(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	entry, err := st.CreateEntry(ctx, "geplakt", "plakband", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	tag, err := st.CreateTag(ctx, "temporary", "#123456")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := st.SetEntryTags(ctx, entry.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetEntryTags: %v", err)
	}
	removed, err := st.DeleteTag(ctx, tag.ID)
	if err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed assignments = %d, want 1", removed)
	}
	tags, err := st.EntryTags(ctx, entry.ID)
	if err != nil {
		t.Fatalf("EntryTags: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("entry still carries %d tags", len(tags))
	}
	if _, err := st.GetEntry(ctx, entry.ID); err != nil {
		t.Fatalf("deleting a tag removed the entry: %v", err)
	}
}

func TestPurgeEntryCascadesAssignments(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	entry, err := st.CreateEntry(ctx, "weggegooid", "weg", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	tag, err := st.GetTagBySlug(ctx, "typo")
	if err != nil {
		t.Fatalf("GetTagBySlug: %v", err)
	}
	if err := st.SetEntryTags(ctx, entry.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetEntryTags: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.PurgeEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
	var rows int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM entry_tags WHERE entry_id = ?`, entry.ID).Scan(&rows); err != nil {
		t.Fatalf("count assignments: %v", err)
	}
	if rows != 0 {
		t.Fatalf("purged entry left %d assignments", rows)
	}
}

func TestListTagsCountsExcludeTrash(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	tag, err := st.GetTagBySlug(ctx, "misheard")
	if err != nil {
		t.Fatalf("GetTagBySlug: %v", err)
	}
	first, err := st.CreateEntry(ctx, "eerste", "first", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry first: %v", err)
	}
	second, err := st.CreateEntry(ctx, "tweede", "second", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry second: %v", err)
	}
	for _, id := range []int64{first.ID, second.ID} {
		if err := st.SetEntryTags(ctx, id, []int64{tag.ID}); err != nil {
			t.Fatalf("SetEntryTags %d: %v", id, err)
		}
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{second.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	live, err := st.ListTags(ctx)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	for _, tag := range live {
		if tag.Name == "misheard" {
			if tag.LiveCount != 1 {
				t.Fatalf("live count = %d, want 1", tag.LiveCount)
			}
			if tag.TotalCount != 0 {
				t.Fatalf("ListTags populated TotalCount = %d, want 0", tag.TotalCount)
			}
		}
	}

	admin, err := st.ListAdminTags(ctx)
	if err != nil {
		t.Fatalf("ListAdminTags: %v", err)
	}
	for _, tag := range admin {
		if tag.Name == "misheard" {
			if tag.LiveCount != 1 || tag.TotalCount != 2 {
				t.Fatalf("admin counts = live %d total %d, want 1/2", tag.LiveCount, tag.TotalCount)
			}
		}
	}
	if got := tagCount(t, live, "misheard"); got != 1 {
		t.Fatalf("live count helper = %d", got)
	}
}

func tagCount(t *testing.T, tags []Tag, name string) int {
	t.Helper()
	for _, tag := range tags {
		if tag.Name == name {
			return tag.LiveCount
		}
	}
	t.Fatalf("tag %q missing", name)
	return 0
}

func TestTagFilterIntersectsEveryFilter(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	typo, err := st.GetTagBySlug(ctx, "typo")
	if err != nil {
		t.Fatalf("GetTagBySlug typo: %v", err)
	}
	name, err := st.GetTagBySlug(ctx, "name")
	if err != nil {
		t.Fatalf("GetTagBySlug name: %v", err)
	}

	a := insertRaw(t, st, "feature euten", "first slip", "2024-03-01T00:00:00Z")
	b := insertRaw(t, st, "feature", "older slip", "2023-05-01T00:00:00Z")
	c := insertRaw(t, st, "andere", "other slip", "2024-04-01T00:00:00Z")
	d := insertRaw(t, st, "feature", "name slip", "2024-05-01T00:00:00Z")
	for id, tagID := range map[int64]int64{a: typo.ID, b: typo.ID, c: typo.ID, d: name.ID} {
		if err := st.SetEntryTags(ctx, id, []int64{tagID}); err != nil {
			t.Fatalf("SetEntryTags %d: %v", id, err)
		}
	}
	// c carries both tags.
	if err := st.SetEntryTags(ctx, c, []int64{typo.ID, name.ID}); err != nil {
		t.Fatalf("SetEntryTags both: %v", err)
	}

	cases := []struct {
		name   string
		params ListParams
		want   int
	}{
		{"tag only", ListParams{Tags: []string{"typo"}, PerPage: 100}, 3},
		{"tag and query", ListParams{Tags: []string{"typo"}, Query: "feature", PerPage: 100}, 2},
		{"tag and year", ListParams{Tags: []string{"typo"}, Year: 2024, PerPage: 100}, 2},
		{"two tags intersect", ListParams{Tags: []string{"typo", "name"}, PerPage: 100}, 1},
		{"unknown slug", ListParams{Tags: []string{"does-not-exist"}, PerPage: 100}, 0},
		{"tag and person nobody", ListParams{Tags: []string{"typo"}, Person: "nobody", PerPage: 100}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := st.ListEntries(ctx, tc.params)
			if err != nil {
				t.Fatalf("ListEntries: %v", err)
			}
			if page.Total != tc.want {
				t.Fatalf("total = %d, want %d", page.Total, tc.want)
			}
		})
	}

	// An empty tag value and a repeated tag are normalised away.
	page, err := st.ListEntries(ctx, ListParams{Tags: []string{"", "TYPO", "typo"}, PerPage: 100})
	if err != nil {
		t.Fatalf("ListEntries normalise: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("normalised tag total = %d, want 3", page.Total)
	}

	// A trashed entry matches neither the filter nor the live count.
	if _, err := st.SoftDeleteEntries(ctx, []int64{a}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	page, err = st.ListEntries(ctx, ListParams{Tags: []string{"typo"}, PerPage: 100})
	if err != nil {
		t.Fatalf("ListEntries after trash: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("tag total after trash = %d, want 2", page.Total)
	}
	for _, entry := range page.Entries {
		if entry.ID == a {
			t.Fatal("trashed entry matched the tag filter")
		}
	}
	tags, err := st.ListTags(ctx)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if got := tagCount(t, tags, "typo"); got != 2 {
		t.Fatalf("typo live count = %d, want 2", got)
	}
}

func TestSetEntryTagsDeduplicatesAndIgnoresUnknown(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	entry, err := st.CreateEntry(ctx, "dubbel", "double", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	typo, err := st.GetTagBySlug(ctx, "typo")
	if err != nil {
		t.Fatalf("GetTagBySlug: %v", err)
	}
	if err := st.SetEntryTags(ctx, entry.ID, []int64{typo.ID, typo.ID, 999999}); err != nil {
		t.Fatalf("SetEntryTags: %v", err)
	}
	tags, err := st.EntryTags(ctx, entry.ID)
	if err != nil {
		t.Fatalf("EntryTags: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != typo.ID {
		t.Fatalf("tags = %+v, want only typo", tags)
	}

	if err := st.SetEntryTags(ctx, entry.ID, nil); err != nil {
		t.Fatalf("SetEntryTags clear: %v", err)
	}
	tags, err = st.EntryTags(ctx, entry.ID)
	if err != nil {
		t.Fatalf("EntryTags clear: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("tags after clear = %+v", tags)
	}
	if err := st.SetEntryTags(ctx, 999999, []int64{typo.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown entry error = %v, want ErrNotFound", err)
	}
}

func TestTagsByEntryIsOneBatch(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	typo, _ := st.GetTagBySlug(ctx, "typo")
	name, _ := st.GetTagBySlug(ctx, "name")
	first, _ := st.CreateEntry(ctx, "een", "one", "", nil)
	second, _ := st.CreateEntry(ctx, "twee", "two", "", nil)
	if err := st.SetEntryTags(ctx, first.ID, []int64{typo.ID, name.ID}); err != nil {
		t.Fatalf("SetEntryTags first: %v", err)
	}
	if err := st.SetEntryTags(ctx, second.ID, []int64{typo.ID}); err != nil {
		t.Fatalf("SetEntryTags second: %v", err)
	}

	byEntry, err := st.TagsByEntry(ctx, []int64{first.ID, second.ID})
	if err != nil {
		t.Fatalf("TagsByEntry: %v", err)
	}
	if len(byEntry[first.ID]) != 2 || len(byEntry[second.ID]) != 1 {
		t.Fatalf("batch tags = %+v", byEntry)
	}
	if byEntry[first.ID][0].Name != "name" || byEntry[first.ID][1].Name != "typo" {
		t.Fatalf("tags are not ordered by name: %+v", byEntry[first.ID])
	}
	empty, err := st.TagsByEntry(ctx, nil)
	if err != nil {
		t.Fatalf("TagsByEntry nil: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("TagsByEntry nil = %+v", empty)
	}
}
