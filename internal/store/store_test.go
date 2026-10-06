package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "vandyke-test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func clearEntries(t *testing.T, st *Store) {
	t.Helper()
	if _, err := st.db.Exec(`DELETE FROM entries`); err != nil {
		t.Fatalf("clear entries: %v", err)
	}
}

func insertRaw(t *testing.T, st *Store, word, meaning, createdAt string) int64 {
	t.Helper()
	var id int64
	err := st.db.QueryRow(
		`INSERT INTO entries (word, meaning, created_at) VALUES (?, ?, ?) RETURNING id`,
		word, meaning, createdAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert raw entry: %v", err)
	}
	return id
}

func TestSeedSnapshotImported(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	count, err := st.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 100 {
		t.Fatalf("seeded entry count = %d, want 100", count)
	}

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 100 {
		t.Errorf("stats total = %d, want 100", stats.Total)
	}
	if stats.FirstEntryAt == nil || stats.LastEntryAt == nil {
		t.Fatalf("stats timestamps missing: %+v", stats)
	}
	if got := stats.FirstEntryAt.Format(time.RFC3339); got != "2017-10-02T00:00:00Z" {
		t.Errorf("first entry = %s, want 2017-10-02T00:00:00Z", got)
	}
	if got := stats.LastEntryAt.Format(time.RFC3339); got != "2026-03-13T22:07:21Z" {
		t.Errorf("last entry = %s, want 2026-03-13T22:07:21Z", got)
	}

	first, err := st.GetEntry(ctx, 1)
	if err != nil {
		t.Fatalf("GetEntry(1): %v", err)
	}
	if first.Word != "kast systeem" || first.Meaning != "kassasysteem" {
		t.Errorf("entry 1 = %q -> %q, want %q -> %q", first.Word, first.Meaning, "kast systeem", "kassasysteem")
	}

	page, err := st.ListEntries(ctx, ListParams{Query: "kassasysteem", PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries search: %v", err)
	}
	if page.Total == 0 {
		t.Errorf("search for known seed correction returned no results")
	}
}

func TestSeedOnlyImportedOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "once.db")

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	if _, err := first.CreateEntry(ctx, "nieuw", "nieuw correct"); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	defer second.Close()

	count, err := second.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 101 {
		t.Fatalf("count after reopen = %d, want 101 (no reseed)", count)
	}
}

func TestListEntriesSearchSortPagination(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	ids := map[string]int64{}
	ids["alpha"] = insertRaw(t, st, "alpha", "first", "2020-01-01T00:00:00Z")
	ids["beta"] = insertRaw(t, st, "beta", "second needle", "2020-01-02T00:00:00Z")
	ids["gamma"] = insertRaw(t, st, "gamma", "third", "2020-01-03T00:00:00Z")
	ids["delta"] = insertRaw(t, st, "delta", "fourth", "2020-01-04T00:00:00Z")
	ids["epsilon"] = insertRaw(t, st, "epsilon", "fifth", "2020-01-05T00:00:00Z")
	ids["percent"] = insertRaw(t, st, "100% sure", "fully certain", "2020-01-06T00:00:00Z")

	t.Run("default ordering is newest first", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Total != 6 || page.Page != 1 || page.PerPage != DefaultPerPage || page.Pages != 1 {
			t.Fatalf("unexpected page metadata: %+v", page)
		}
		if page.Entries[0].Word != "100% sure" {
			t.Errorf("first entry = %q, want newest (%q)", page.Entries[0].Word, "100% sure")
		}
	})

	t.Run("case-insensitive search", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{Query: "NEEDLE"})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Total != 1 || page.Entries[0].ID != ids["beta"] {
			t.Fatalf("search result = %+v, want only beta", page)
		}
	})

	t.Run("like wildcards are escaped", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{Query: "%"})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Total != 1 || page.Entries[0].Word != "100% sure" {
			t.Fatalf("literal %% search = %+v, want only 100%% sure", page)
		}
	})

	t.Run("sort word ascending page two", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{Sort: "word", Order: "asc", Page: 2, PerPage: 2})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Pages != 3 || page.Page != 2 {
			t.Fatalf("pagination metadata = %+v", page)
		}
		if got := []string{page.Entries[0].Word, page.Entries[1].Word}; got[0] != "beta" || got[1] != "delta" {
			t.Fatalf("page two = %v, want [beta delta]", got)
		}
	})

	t.Run("sort id ascending", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{Sort: "id", Order: "asc", PerPage: 10})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Entries[0].ID != ids["alpha"] || page.Entries[5].ID != ids["percent"] {
			t.Fatalf("id ordering = %d..%d", page.Entries[0].ID, page.Entries[5].ID)
		}
	})

	t.Run("page beyond the end is clamped", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{Page: 99, PerPage: 5})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Page != 2 {
			t.Fatalf("clamped page = %d, want 2", page.Page)
		}
	})

	t.Run("per page is clamped", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{PerPage: 5000})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.PerPage != MaxPerPage {
			t.Fatalf("per page = %d, want %d", page.PerPage, MaxPerPage)
		}
	})
}

func TestNormalizeListParams(t *testing.T) {
	cases := []struct {
		name string
		in   ListParams
		want ListParams
	}{
		{"defaults", ListParams{}, ListParams{Sort: "created_at", Order: "desc", Page: 1, PerPage: 30}},
		{"unknown sort", ListParams{Sort: "drop table", Order: "sideways", Page: -3, PerPage: -1}, ListParams{Sort: "created_at", Order: "desc", Page: 1, PerPage: 30}},
		{"word defaults asc", ListParams{Sort: "word"}, ListParams{Sort: "word", Order: "asc", Page: 1, PerPage: 30}},
		{"explicit order kept", ListParams{Sort: "word", Order: "desc"}, ListParams{Sort: "word", Order: "desc", Page: 1, PerPage: 30}},
		{"per page capped", ListParams{PerPage: 9999}, ListParams{Sort: "created_at", Order: "desc", Page: 1, PerPage: MaxPerPage}},
		{"query trimmed", ListParams{Query: "  boeke  "}, ListParams{Query: "boeke", Sort: "created_at", Order: "desc", Page: 1, PerPage: 30}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeListParams(tc.in); got != tc.want {
				t.Errorf("NormalizeListParams(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCreateEntryValidationAndTrimming(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	invalid := []struct{ word, meaning string }{
		{"", "meaning"},
		{"   ", "meaning"},
		{"word", ""},
		{"word", "   "},
		{strings.Repeat("x", MaxWordLen+1), "meaning"},
		{"word", strings.Repeat("y", MaxMeaningLen+1)},
	}
	for _, tc := range invalid {
		if _, err := st.CreateEntry(ctx, tc.word, tc.meaning); err == nil {
			t.Errorf("CreateEntry(%q, %q) succeeded, want validation error", tc.word, tc.meaning)
		}
	}

	entry, err := st.CreateEntry(ctx, "  nieuw woord  ", "  nieuwe correctie  ")
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if entry.Word != "nieuw woord" || entry.Meaning != "nieuwe correctie" {
		t.Fatalf("entry = %q -> %q, want trimmed values", entry.Word, entry.Meaning)
	}
	if entry.ID < 1 {
		t.Fatalf("entry id = %d, want positive", entry.ID)
	}
	if elapsed := time.Since(entry.CreatedAt); elapsed < 0 || elapsed > time.Minute {
		t.Fatalf("created_at = %v, not close to now", entry.CreatedAt)
	}

	if _, err := st.GetEntry(ctx, entry.ID+1000); err != ErrNotFound {
		t.Fatalf("GetEntry(missing) error = %v, want ErrNotFound", err)
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	now := time.Now().UTC()
	insertRaw(t, st, "Foo", "one", now.Format(time.RFC3339))
	insertRaw(t, st, "foo", "two", now.AddDate(0, 0, -40).Format(time.RFC3339))
	insertRaw(t, st, "bar", "three", now.AddDate(0, 0, -10).Format(time.RFC3339))

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 3 {
		t.Errorf("total = %d, want 3", stats.Total)
	}
	if stats.UniqueWords != 2 {
		t.Errorf("unique words = %d, want 2 (case-insensitive)", stats.UniqueWords)
	}
	if stats.EntriesLast30d != 2 {
		t.Errorf("entries last 30 days = %d, want 2", stats.EntriesLast30d)
	}
	if stats.FirstEntryAt == nil || !stats.FirstEntryAt.Equal(now.AddDate(0, 0, -40).Truncate(time.Second)) {
		t.Errorf("first entry = %v, want the 40-day-old row", stats.FirstEntryAt)
	}
	if stats.LastEntryAt == nil || !stats.LastEntryAt.Equal(now.Truncate(time.Second)) {
		t.Errorf("last entry = %v, want now", stats.LastEntryAt)
	}
}

func TestStatsOnEmptyLedger(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 0 || stats.UniqueWords != 0 || stats.EntriesLast30d != 0 {
		t.Errorf("empty stats = %+v, want zeroes", stats)
	}
	if stats.FirstEntryAt != nil || stats.LastEntryAt != nil {
		t.Errorf("empty stats timestamps = %v / %v, want nil", stats.FirstEntryAt, stats.LastEntryAt)
	}
}
