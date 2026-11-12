package store

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatalf("decode tiny png: %v", err)
	}
	return data
}

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
	if _, err := st.db.Exec(`INSERT INTO entries_fts (rowid, word, meaning) VALUES (?, ?, ?)`, id, word, meaning); err != nil {
		t.Fatalf("index raw entry: %v", err)
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
	if count != 78 {
		t.Fatalf("seeded entry count = %d, want 78", count)
	}

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 78 {
		t.Errorf("stats total = %d, want 78", stats.Total)
	}
	if stats.FirstEntryAt == nil || stats.LastEntryAt == nil {
		t.Fatalf("stats timestamps missing: %+v", stats)
	}
	if got := stats.FirstEntryAt.Format(time.RFC3339); got != "2017-10-02T00:00:00Z" {
		t.Errorf("first entry = %s, want 2017-10-02T00:00:00Z", got)
	}
	if got := stats.LastEntryAt.Format(time.RFC3339); got != "2026-03-13T22:07:13Z" {
		t.Errorf("last entry = %s, want 2026-03-13T22:07:13Z", got)
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

func TestSearchFiltersYearsAndIndexUpdates(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	typoID := insertRaw(t, st, "feutenreus", "feature", "2020-04-01T00:00:00Z")
	otherID := insertRaw(t, st, "boeke", "books", "2021-04-01T00:00:00Z")
	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	photoEntry, err := st.CreateEntry(ctx, "foto", "", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry photo: %v", err)
	}

	page, err := st.ListEntries(ctx, ListParams{Query: "euten"})
	if err != nil {
		t.Fatalf("ListEntries partial search: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != typoID {
		t.Fatalf("partial search = %+v, want feutenreus", page)
	}

	page, err = st.ListEntries(ctx, ListParams{Year: 2020})
	if err != nil {
		t.Fatalf("ListEntries year: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != typoID {
		t.Fatalf("year filter = %+v, want 2020 only", page)
	}

	hasPhoto := true
	page, err = st.ListEntries(ctx, ListParams{HasPhoto: &hasPhoto})
	if err != nil {
		t.Fatalf("ListEntries photo: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != photoEntry.ID {
		t.Fatalf("photo filter = %+v, want photo entry", page)
	}

	noPhoto := false
	page, err = st.ListEntries(ctx, ListParams{HasPhoto: &noPhoto})
	if err != nil {
		t.Fatalf("ListEntries no photo: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("no-photo filter total = %d, want 2", page.Total)
	}

	years, err := st.EntryYears(ctx)
	if err != nil {
		t.Fatalf("EntryYears: %v", err)
	}
	if len(years) < 3 || years[len(years)-1] != 2020 {
		t.Fatalf("years = %v, want 2020 included", years)
	}

	if _, _, err := st.UpdateEntry(ctx, EntryUpdate{ID: typoID, Word: "nieuw", Meaning: "new"}); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "euten"})
	if err != nil {
		t.Fatalf("ListEntries old term: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("old search term still matches after update: %+v", page)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "nieuw"})
	if err != nil {
		t.Fatalf("ListEntries new term: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("new search term does not match after update: %+v", page)
	}

	if _, err := st.SoftDeleteEntries(ctx, []int64{typoID, otherID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "nieuw"})
	if err != nil {
		t.Fatalf("ListEntries after delete: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("search still matches deleted entry: %+v", page)
	}
}

func TestSeedOnlyImportedOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "once.db")

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	if _, err := first.CreateEntry(ctx, "nieuw", "nieuw correct", "", nil); err != nil {
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
	if count != 79 {
		t.Fatalf("count after reopen = %d, want 79 (no reseed)", count)
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
			if got := NormalizeListParams(tc.in); !reflect.DeepEqual(got, tc.want) {
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
		if _, err := st.CreateEntry(ctx, tc.word, tc.meaning, "", nil); err == nil {
			t.Errorf("CreateEntry(%q, %q) succeeded, want validation error", tc.word, tc.meaning)
		}
	}

	entry, err := st.CreateEntry(ctx, "  nieuw woord  ", "  nieuwe correctie  ", "", nil)
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

func TestCreateEntryRejectsDuplicates(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	if _, err := st.CreateEntry(ctx, "Dubbel", "dubbel", "", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.CreateEntry(ctx, "  dubbel  ", "DUBBEL", "", nil); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate CreateEntry error = %v, want ErrDuplicate", err)
	}

	other, err := st.CreateEntry(ctx, "ander", "anders", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry other: %v", err)
	}
	if _, _, err := st.UpdateEntry(ctx, EntryUpdate{ID: other.ID, Word: "dubbel", Meaning: "DUBBEL"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate UpdateEntry error = %v, want ErrDuplicate", err)
	}

	count, err := st.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestCreatePhotoEntry(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if photo.ContentType != "image/png" || photo.Size == 0 || photo.SHA256 == "" {
		t.Fatalf("photo metadata = %+v", photo)
	}

	entry, err := st.CreateEntry(ctx, "  foto  ", "  photo  ", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry with photo: %v", err)
	}
	if entry.PhotoURL != "/api/v1/entries/"+itoa(entry.ID)+"/photo" {
		t.Fatalf("photo_url = %q", entry.PhotoURL)
	}

	stored, err := st.GetEntryPhoto(ctx, entry.ID)
	if err != nil {
		t.Fatalf("GetEntryPhoto: %v", err)
	}
	if string(stored.Data) != string(photo.Data) || stored.ContentType != "image/png" {
		t.Fatalf("stored photo = %+v", stored)
	}

	page, err := st.ListEntries(ctx, ListParams{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].PhotoURL == "" {
		t.Fatalf("listed entries = %+v, want photo_url set", page.Entries)
	}

	if _, err := st.GetEntryPhoto(ctx, entry.ID+1000); err != ErrNotFound {
		t.Fatalf("GetEntryPhoto(missing) error = %v, want ErrNotFound", err)
	}
}

func TestPhotoOnlyEntry(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(ctx, "", "   ", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry(photo only): %v", err)
	}
	if entry.Word != "" || entry.Meaning != "" || entry.PhotoURL == "" {
		t.Fatalf("entry = %+v, want empty text and a photo", entry)
	}

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 1 || stats.UniqueWords != 0 {
		t.Fatalf("stats = %+v, want one photo entry and zero unique words", stats)
	}
}

func TestNewPhotoValidation(t *testing.T) {
	if _, err := NewPhoto(nil); err == nil {
		t.Fatal("NewPhoto(nil) succeeded, want error")
	}
	if _, err := NewPhoto([]byte("not an image")); err == nil {
		t.Fatal("NewPhoto(text) succeeded, want error")
	}
	oversized := make([]byte, MaxPhotoBytes+1)
	copy(oversized, []byte("\x89PNG\r\n\x1a\n"))
	if _, err := NewPhoto(oversized); err == nil {
		t.Fatal("NewPhoto(oversized) succeeded, want error")
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	now := time.Now().UTC()
	insertRaw(t, st, "Foo", "one", now.Format(time.RFC3339))
	insertRaw(t, st, "foo", "two", now.AddDate(0, 0, -40).Format(time.RFC3339))
	insertRaw(t, st, "bar", "three", now.AddDate(0, 0, -10).Format(time.RFC3339))
	// A photo-only entry has no misspelling and must not inflate unique words.
	insertRaw(t, st, "", "", now.AddDate(0, 0, -5).Format(time.RFC3339))

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 4 {
		t.Errorf("total = %d, want 4", stats.Total)
	}
	if stats.UniqueWords != 2 {
		t.Errorf("unique words = %d, want 2 (case-insensitive)", stats.UniqueWords)
	}
	if stats.EntriesLast30d != 3 {
		t.Errorf("entries last 30 days = %d, want 3", stats.EntriesLast30d)
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

func insertAttributed(t *testing.T, st *Store, word, meaning, person, createdAt string) int64 {
	t.Helper()
	id := insertRaw(t, st, word, meaning, createdAt)
	if _, err := st.db.Exec(`UPDATE entries SET person = ? WHERE id = ?`, person, id); err != nil {
		t.Fatalf("attribute entry: %v", err)
	}
	return id
}

func attachRawPhoto(t *testing.T, st *Store, entryID int64) {
	t.Helper()
	_, err := st.db.Exec(
		`INSERT INTO entry_photos (entry_id, content_type, data, sha256, size) VALUES (?, ?, ?, ?, ?)`,
		entryID, "image/png", tinyPNG(t), "raw-test-sha256", 1,
	)
	if err != nil {
		t.Fatalf("attach photo: %v", err)
	}
}

func TestStatsAggregates(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	first := insertAttributed(t, st, "alpha", "a", "Alice", "2024-03-04T10:00:00Z")
	insertAttributed(t, st, "beta", "b", "alice", "2024-08-09T10:00:00Z")
	third := insertAttributed(t, st, "gamma", "c", "ALICE", "2025-01-02T10:00:00Z")
	insertAttributed(t, st, "delta", "d", "Bob", "2026-05-06T10:00:00Z")
	insertAttributed(t, st, "epsilon", "e", "", "2026-05-07T10:00:00Z")
	insertAttributed(t, st, "zeta", "f", "", "2026-05-08T10:00:00Z")
	attachRawPhoto(t, st, first)
	attachRawPhoto(t, st, third)

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.WithPhoto != 2 {
		t.Errorf("with photo = %d, want 2", stats.WithPhoto)
	}
	wantYears := []YearCount{{Year: 2024, Count: 2}, {Year: 2025, Count: 1}, {Year: 2026, Count: 3}}
	if len(stats.ByYear) != len(wantYears) {
		t.Fatalf("by year = %+v, want %+v", stats.ByYear, wantYears)
	}
	for i, want := range wantYears {
		if stats.ByYear[i] != want {
			t.Errorf("by year[%d] = %+v, want %+v", i, stats.ByYear[i], want)
		}
	}
	wantPersons := []PersonCount{{Person: "Alice", Count: 3}, {Person: "Bob", Count: 1}}
	if len(stats.TopPersons) != len(wantPersons) {
		t.Fatalf("top persons = %+v, want %+v", stats.TopPersons, wantPersons)
	}
	for i, want := range wantPersons {
		if stats.TopPersons[i] != want {
			t.Errorf("top persons[%d] = %+v, want %+v", i, stats.TopPersons[i], want)
		}
	}

	// The ranking keeps only the eight busiest names.
	for i := 0; i < 10; i++ {
		insertAttributed(t, st, "extra-"+strconv.Itoa(i), "x", "Person "+strconv.Itoa(i), "2026-06-0"+strconv.Itoa(i%9+1)+"T10:00:00Z")
	}
	stats, err = st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats after extras: %v", err)
	}
	if len(stats.TopPersons) != 8 {
		t.Fatalf("top persons length = %d, want 8 with LIMIT 8", len(stats.TopPersons))
	}
	if stats.TopPersons[0] != (PersonCount{Person: "Alice", Count: 3}) {
		t.Errorf("leading person = %+v, want Alice with 3", stats.TopPersons[0])
	}
}

func TestAdminAndSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	count, err := st.AdminCount(ctx)
	if err != nil || count != 0 {
		t.Fatalf("AdminCount = %d, %v; want 0", count, err)
	}
	admin, err := st.CreateFirstAdmin(ctx, "Owner", "hash")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	if admin.Username != "owner" || !admin.HasPassword() {
		t.Fatalf("admin = %+v", admin)
	}
	if _, err := st.CreateFirstAdmin(ctx, "other", "hash"); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("second CreateFirstAdmin error = %v, want ErrAdminExists", err)
	}
	got, err := st.GetAdminByUsername(ctx, "OWNER")
	if err != nil || got.ID != admin.ID {
		t.Fatalf("GetAdminByUsername = %+v, %v", got, err)
	}
	if err := st.TouchAdmin(ctx, admin.ID, time.Now()); err != nil {
		t.Fatalf("TouchAdmin: %v", err)
	}

	now := time.Now().UTC()
	session := Session{
		TokenHash: "token-hash",
		AdminID:   admin.ID,
		CSRFToken: "csrf",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := st.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	info, err := st.GetSession(ctx, "token-hash")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if info.Admin.ID != admin.ID || info.Session.CSRFToken != "csrf" {
		t.Fatalf("session info = %+v", info)
	}
	if err := st.DeleteSession(ctx, "token-hash"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := st.GetSession(ctx, "token-hash"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession after delete = %v, want ErrSessionNotFound", err)
	}
}

func TestOIDCAdminUpsert(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	// A fresh instance has no admin; the first OIDC identity to sign in creates one.
	if count, err := st.AdminCount(ctx); err != nil || count != 0 {
		t.Fatalf("AdminCount before first OIDC login = %d, %v; want 0", count, err)
	}

	first, err := st.UpsertOIDCAdmin(ctx, "https://id.example.com", "sub-1", "a@example.com", "Alice")
	if err != nil {
		t.Fatalf("UpsertOIDCAdmin: %v", err)
	}
	second, err := st.UpsertOIDCAdmin(ctx, "https://id.example.com", "sub-1", "b@example.com", "Alice B")
	if err != nil {
		t.Fatalf("UpsertOIDCAdmin again: %v", err)
	}
	if first.ID != second.ID || second.Email != "b@example.com" || second.DisplayName != "Alice B" {
		t.Fatalf("upsert did not update identity: first=%+v second=%+v", first, second)
	}

	if count, err := st.AdminCount(ctx); err != nil || count != 1 {
		t.Fatalf("AdminCount after first OIDC login = %d, %v; want 1", count, err)
	}
	if first.HasPassword() {
		t.Errorf("OIDC admin should not have a local password")
	}
	if first.OIDCIssuer != "https://id.example.com" || first.OIDCSubject != "sub-1" {
		t.Errorf("OIDC identity not stored: %+v", first)
	}
}

func TestUpdateEntryFieldsAndPhoto(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	entry, err := st.CreateEntry(ctx, "wrong", "right", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	when := time.Date(2019, 4, 5, 12, 30, 0, 0, time.UTC)
	_, updated, err := st.UpdateEntry(ctx, EntryUpdate{
		ID: entry.ID, Word: " wrong2 ", Meaning: " right2 ", CreatedAt: when,
	})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if updated.Word != "wrong2" || updated.Meaning != "right2" || !updated.CreatedAt.Equal(when) {
		t.Fatalf("updated = %+v", updated)
	}

	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	_, updated, err = st.UpdateEntry(ctx, EntryUpdate{ID: entry.ID, Photo: photo})
	if err != nil {
		t.Fatalf("UpdateEntry photo: %v", err)
	}
	if updated.PhotoURL == "" {
		t.Fatal("photo_url missing after update")
	}
	if _, err := st.GetEntryPhoto(ctx, entry.ID); err != nil {
		t.Fatalf("GetEntryPhoto: %v", err)
	}

	_, updated, err = st.UpdateEntry(ctx, EntryUpdate{ID: entry.ID, Word: "wrong2", Meaning: "right2", RemovePhoto: true})
	if err != nil {
		t.Fatalf("UpdateEntry remove photo: %v", err)
	}
	if updated.PhotoURL != "" {
		t.Fatalf("photo_url = %q after removal", updated.PhotoURL)
	}
}

func TestEntryPersonCreateFilterUpdateAndList(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	alice, err := st.CreateEntry(ctx, "alice-word", "alice-meaning", "  Alice  ", nil)
	if err != nil {
		t.Fatalf("CreateEntry alice: %v", err)
	}
	if alice.Person != "Alice" {
		t.Fatalf("person = %q, want trimmed Alice", alice.Person)
	}
	if _, err := st.CreateEntry(ctx, "bob-word", "bob-meaning", "Bob", nil); err != nil {
		t.Fatalf("CreateEntry bob: %v", err)
	}
	if _, err := st.CreateEntry(ctx, "anon-word", "anon-meaning", "", nil); err != nil {
		t.Fatalf("CreateEntry anonymous: %v", err)
	}
	if _, err := st.CreateEntry(ctx, "long-word", "long-meaning", strings.Repeat("x", MaxPersonLen+1), nil); err == nil {
		t.Fatal("CreateEntry with oversized person succeeded, want validation error")
	}

	stored, err := st.GetEntry(ctx, alice.ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if stored.Person != "Alice" {
		t.Fatalf("GetEntry person = %q, want Alice", stored.Person)
	}

	page, err := st.ListEntries(ctx, ListParams{Person: " alice "})
	if err != nil {
		t.Fatalf("ListEntries person: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != alice.ID {
		t.Fatalf("person filter = %+v, want only Alice entry", page)
	}

	_, updated, err := st.UpdateEntry(ctx, EntryUpdate{
		ID: alice.ID, Word: "alice-word", Meaning: "alice-meaning", Person: "  Carol  ",
	})
	if err != nil {
		t.Fatalf("UpdateEntry person: %v", err)
	}
	if updated.Person != "Carol" {
		t.Fatalf("updated person = %q, want Carol", updated.Person)
	}
	page, err = st.ListEntries(ctx, ListParams{Person: "carol"})
	if err != nil {
		t.Fatalf("ListEntries updated person: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != alice.ID {
		t.Fatalf("updated person filter = %+v, want only Carol entry", page)
	}

	persons, err := st.Persons(ctx)
	if err != nil {
		t.Fatalf("Persons: %v", err)
	}
	want := []string{"Bob", "Carol"}
	if len(persons) != len(want) || persons[0] != want[0] || persons[1] != want[1] {
		t.Fatalf("persons = %v, want %v", persons, want)
	}
}

func TestSoftDeleteEntriesRetainPhotoAndSettings(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(ctx, "", "", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	removed, err := st.SoftDeleteEntries(ctx, []int64{entry.ID, entry.ID, 0})
	if err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != entry.ID {
		t.Fatalf("soft deleted = %+v, want exactly the requested entry", removed)
	}
	// A trashed entry keeps its photo until it is purged.
	if _, err := st.GetEntryPhoto(ctx, entry.ID); err != nil {
		t.Fatalf("photo after soft delete = %v, want retained", err)
	}
	if _, err := st.GetEntry(ctx, entry.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetEntry after soft delete = %v, want ErrNotFound", err)
	}
	if _, err := st.PurgeEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
	if _, err := st.GetEntryPhoto(ctx, entry.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("photo after purge = %v, want ErrNotFound", err)
	}

	if err := st.SetSettings(ctx, map[string]string{"umami.url": "https://u.example/script.js"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	values, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if values["umami.url"] != "https://u.example/script.js" {
		t.Fatalf("settings = %v", values)
	}
}
