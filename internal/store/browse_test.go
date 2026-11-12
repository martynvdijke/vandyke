package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func insertRawPerson(t *testing.T, st *Store, word, meaning, person, createdAt string) int64 {
	t.Helper()
	var id int64
	err := st.db.QueryRow(
		`INSERT INTO entries (word, meaning, person, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
		word, meaning, person, createdAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert raw entry: %v", err)
	}
	if _, err := st.db.Exec(`INSERT INTO entries_fts (rowid, word, meaning, person) VALUES (?, ?, ?, ?)`, id, word, meaning, person); err != nil {
		t.Fatalf("index raw entry: %v", err)
	}
	return id
}

func makePhotoEntry(t *testing.T, st *Store, word, meaning, person string, at time.Time) Entry {
	t.Helper()
	ctx := context.Background()
	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(ctx, word, meaning, person, photo)
	if err != nil {
		t.Fatalf("CreateEntry %q: %v", word, err)
	}
	_, updated, err := st.UpdateEntry(ctx, EntryUpdate{
		ID: entry.ID, Word: word, Meaning: meaning, Person: person, CreatedAt: at,
	})
	if err != nil {
		t.Fatalf("UpdateEntry %q: %v", word, err)
	}
	return updated
}

func TestWordIndexGroupsCaseInsensitively(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	older := insertRawPerson(t, st, "Feutenreus", "feature", "", "2020-01-02T00:00:00Z")
	newer := insertRawPerson(t, st, "feutenreus", "featured", "", "2022-03-04T00:00:00Z")
	feature := insertRawPerson(t, st, "feature", "actual word", "", "2021-02-03T00:00:00Z")
	// NOCASE folds ASCII only, so an accented pair stays two groups.
	accentUpper := insertRawPerson(t, st, "Échec", "first", "", "2019-01-01T00:00:00Z")
	accentLower := insertRawPerson(t, st, "échec", "second", "", "2023-01-01T00:00:00Z")
	trashed := insertRawPerson(t, st, "kast", "kassasysteem", "", "2024-01-01T00:00:00Z")
	if _, err := st.SoftDeleteEntries(ctx, []int64{trashed}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	photoOnly, err := st.CreateEntry(ctx, "", "", "", mustPhoto(t))
	if err != nil {
		t.Fatalf("CreateEntry photo-only: %v", err)
	}
	_ = photoOnly

	index, err := st.WordIndex(ctx)
	if err != nil {
		t.Fatalf("WordIndex: %v", err)
	}
	if index.PhotoOnly != 1 {
		t.Errorf("photo-only count = %d, want 1", index.PhotoOnly)
	}
	if len(index.Words) != 4 {
		t.Fatalf("word groups = %d (%+v), want 4", len(index.Words), index.Words)
	}

	byWord := make(map[string]WordSummary, len(index.Words))
	for _, summary := range index.Words {
		byWord[summary.Word] = summary
	}
	if _, ok := byWord["kast"]; ok {
		t.Error("trashed spelling is in the index")
	}
	if _, ok := byWord[""]; ok {
		t.Error("empty spelling is an index row")
	}
	group, ok := byWord["feutenreus"]
	if !ok {
		t.Fatalf("grouped spelling missing, got %+v", index.Words)
	}
	if group.Count != 2 {
		t.Errorf("grouped count = %d, want 2", group.Count)
	}
	if group.NewestID != newer {
		t.Errorf("newest id = %d, want %d", group.NewestID, newer)
	}
	if !group.FirstAt.Equal(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first at = %v", group.FirstAt)
	}
	if !group.LastAt.Equal(time.Date(2022, 3, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("last at = %v", group.LastAt)
	}
	if got := byWord["feature"]; got.NewestID != feature {
		t.Errorf("feature newest id = %d, want %d", got.NewestID, feature)
	}
	if got := byWord["Échec"]; got.NewestID != accentUpper {
		t.Errorf("accented upper newest id = %d, want %d", got.NewestID, accentUpper)
	}
	if got := byWord["échec"]; got.NewestID != accentLower {
		t.Errorf("accented lower newest id = %d, want %d", got.NewestID, accentLower)
	}
	if older <= 0 {
		t.Fatal("unused older id")
	}
}

func TestWordIndexNewestTieBreaksOnID(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	sameAt := "2024-06-01T00:00:00Z"
	first := insertRawPerson(t, st, "kast", "one", "", sameAt)
	second := insertRawPerson(t, st, "Kast", "two", "", sameAt)

	index, err := st.WordIndex(ctx)
	if err != nil {
		t.Fatalf("WordIndex: %v", err)
	}
	if len(index.Words) != 1 {
		t.Fatalf("word groups = %d, want 1", len(index.Words))
	}
	if index.Words[0].NewestID != second || index.Words[0].Word != "Kast" {
		t.Errorf("newest = %+v, want id %d spelling Kast (first id %d)", index.Words[0], second, first)
	}
}

func TestPersonProfileCanonicalisesCase(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	insertRawPerson(t, st, "een", "one", "Marieke", "2021-01-02T00:00:00Z")
	insertRawPerson(t, st, "twee", "two", "marieke", "2023-04-05T00:00:00Z")
	insertRawPerson(t, st, "drie", "three", "MARIEKE", "2020-06-07T00:00:00Z")
	insertRawPerson(t, st, "vier", "four", "Someone Else", "2022-01-01T00:00:00Z")

	profile, err := st.PersonProfile(ctx, "  mArIeKe ")
	if err != nil {
		t.Fatalf("PersonProfile: %v", err)
	}
	if profile.Person != "marieke" {
		t.Errorf("canonical spelling = %q, want marieke (newest)", profile.Person)
	}
	if profile.Count != 3 {
		t.Errorf("count = %d, want 3", profile.Count)
	}
	if !profile.FirstAt.Equal(time.Date(2020, 6, 7, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first at = %v", profile.FirstAt)
	}
	if !profile.LastAt.Equal(time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("last at = %v", profile.LastAt)
	}

	for _, name := range []string{"Nobody", "", "   "} {
		if _, err := st.PersonProfile(ctx, name); !errors.Is(err, ErrNotFound) {
			t.Errorf("PersonProfile(%q) error = %v, want ErrNotFound", name, err)
		}
	}

	// Trashing every slip for a person removes the profile entirely.
	ids, err := st.AllEntryIDs(ctx)
	if err != nil {
		t.Fatalf("AllEntryIDs: %v", err)
	}
	var mariekeIDs []int64
	for _, id := range ids {
		entry, err := st.GetEntry(ctx, id)
		if err != nil {
			t.Fatalf("GetEntry(%d): %v", id, err)
		}
		if entry.Person != "" && entry.Person != "Someone Else" {
			mariekeIDs = append(mariekeIDs, id)
		}
	}
	if _, err := st.SoftDeleteEntries(ctx, mariekeIDs); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.PersonProfile(ctx, "Marieke"); !errors.Is(err, ErrNotFound) {
		t.Errorf("trashed person profile error = %v, want ErrNotFound", err)
	}
}

func TestRandomEntryFiltersAndExcludesTrashed(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	live := insertRawPerson(t, st, "levend", "live", "Marieke", "2024-01-01T00:00:00Z")
	insertRawPerson(t, st, "ander", "other", "Someone Else", "2024-01-02T00:00:00Z")
	trashed := insertRawPerson(t, st, "weg", "trashed", "Marieke", "2024-01-03T00:00:00Z")
	if _, err := st.SoftDeleteEntries(ctx, []int64{trashed}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	photo := makePhotoEntry(t, st, "foto", "photo", "Marieke", time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC))

	// With a single trashed and a single live row the draw is deterministic, but
	// repeat it so a future ordering regression cannot pass by luck.
	for i := 0; i < 25; i++ {
		entry, err := st.RandomEntry(ctx, "", nil)
		if err != nil {
			t.Fatalf("RandomEntry: %v", err)
		}
		if entry.ID == trashed {
			t.Fatalf("random draw returned the trashed entry %d", trashed)
		}
	}

	byPerson, err := st.RandomEntry(ctx, "marieke", nil)
	if err != nil {
		t.Fatalf("RandomEntry(person): %v", err)
	}
	if byPerson.Person != "Marieke" {
		t.Errorf("person draw = %q, want Marieke", byPerson.Person)
	}

	withPhoto, err := st.RandomEntry(ctx, "", boolPtr(true))
	if err != nil {
		t.Fatalf("RandomEntry(photo): %v", err)
	}
	if withPhoto.ID != photo.ID {
		t.Errorf("photo draw = %d, want %d", withPhoto.ID, photo.ID)
	}

	if _, err := st.RandomEntry(ctx, "Nobody", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown person draw error = %v, want ErrNotFound", err)
	}

	if _, err := st.SoftDeleteEntries(ctx, []int64{live, photo.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	remaining, err := st.AllEntryIDs(ctx)
	if err != nil {
		t.Fatalf("AllEntryIDs: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, remaining); err != nil {
		t.Fatalf("SoftDeleteEntries remaining: %v", err)
	}
	if _, err := st.RandomEntry(ctx, "", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty ledger draw error = %v, want ErrNotFound", err)
	}
}

func TestListPhotoEntriesCursorPaging(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	total := 30
	ids := make([]int64, 0, total)
	for i := 0; i < total; i++ {
		entry := makePhotoEntry(t, st, "foto"+strconv.Itoa(i), "photo", "", time.Date(2024, 1, 1, 0, 0, i, 0, time.UTC))
		ids = append(ids, entry.ID)
	}
	// Newest first: the last created row is the newest.
	expected := make([]int64, 0, total)
	for i := total - 1; i >= 0; i-- {
		expected = append(expected, ids[i])
	}

	first, err := st.ListPhotoEntries(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListPhotoEntries first: %v", err)
	}
	if first.Total != total {
		t.Errorf("total = %d, want %d", first.Total, total)
	}
	assertIDs(t, first.Entries, expected[:10])
	if first.Next == nil || first.Prev != nil {
		t.Fatalf("first batch next=%v prev=%v", first.Next, first.Prev)
	}

	second, err := st.ListPhotoEntries(ctx, first.Next.Token(), 10)
	if err != nil {
		t.Fatalf("ListPhotoEntries second: %v", err)
	}
	assertIDs(t, second.Entries, expected[10:20])
	if second.Next == nil || second.Prev != nil {
		t.Fatalf("second batch next=%v prev=%v, want next and no prev", second.Next, second.Prev)
	}

	third, err := st.ListPhotoEntries(ctx, second.Next.Token(), 10)
	if err != nil {
		t.Fatalf("ListPhotoEntries third: %v", err)
	}
	assertIDs(t, third.Entries, expected[20:])
	if third.Next != nil {
		t.Errorf("last batch still offers a cursor")
	}
	if third.Prev == nil {
		t.Fatal("third batch has no previous cursor")
	}
	previous, err := st.ListPhotoEntries(ctx, third.Prev.Token(), 10)
	if err != nil {
		t.Fatalf("ListPhotoEntries previous: %v", err)
	}
	assertIDs(t, previous.Entries, expected[10:20])

	if _, err := st.ListPhotoEntries(ctx, "not-a-cursor", 10); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("malformed cursor error = %v, want ErrInvalidCursor", err)
	}

	// Trashed and photo-less rows never enter the gallery or its total.
	if _, err := st.SoftDeleteEntries(ctx, []int64{ids[0]}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.CreateEntry(ctx, "zonder", "photo", "", nil); err != nil {
		t.Fatalf("CreateEntry without photo: %v", err)
	}
	refreshed, err := st.ListPhotoEntries(ctx, "", total)
	if err != nil {
		t.Fatalf("ListPhotoEntries refreshed: %v", err)
	}
	if refreshed.Total != total-1 {
		t.Errorf("total after trash = %d, want %d", refreshed.Total, total-1)
	}
	for _, entry := range refreshed.Entries {
		if entry.ID == ids[0] {
			t.Error("trashed entry still in the gallery")
		}
	}
}

func TestListPhotoEntriesSharedTimestampBoundary(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	shared := time.Date(2024, 5, 5, 12, 0, 0, 0, time.UTC)
	// 23 newer entries, then two rows with the same created_at, then six older.
	var ids []int64
	for i := 0; i < 23; i++ {
		ids = append(ids, makePhotoEntry(t, st, "nieuw"+strconv.Itoa(i), "new", "", shared.Add(time.Duration(i+1)*time.Hour)).ID)
	}
	lower := makePhotoEntry(t, st, "gelijk-laag", "same", "", shared).ID
	higher := makePhotoEntry(t, st, "gelijk-hoog", "same", "", shared).ID
	for i := 0; i < 6; i++ {
		ids = append(ids, makePhotoEntry(t, st, "oud"+strconv.Itoa(i), "old", "", shared.Add(-time.Duration(i+1)*time.Hour)).ID)
	}
	_ = ids

	first, err := st.ListPhotoEntries(ctx, "", 24)
	if err != nil {
		t.Fatalf("ListPhotoEntries first: %v", err)
	}
	second, err := st.ListPhotoEntries(ctx, first.Next.Token(), 24)
	if err != nil {
		t.Fatalf("ListPhotoEntries second: %v", err)
	}
	seen := make(map[int64]int)
	for _, entry := range append(first.Entries, second.Entries...) {
		seen[entry.ID]++
	}
	if seen[higher] != 1 || seen[lower] != 1 {
		t.Fatalf("shared-timestamp rows seen %d/%d times, want once each", seen[higher], seen[lower])
	}
	// The higher id sorts first when the timestamps tie.
	if len(first.Entries) < 24 || first.Entries[23].ID != higher {
		t.Errorf("first batch tail = %v, want shared higher id %d at position 24", entryIDs(first.Entries), higher)
	}
	if len(second.Entries) == 0 || second.Entries[0].ID != lower {
		t.Errorf("second batch head = %v, want shared lower id %d", entryIDs(second.Entries), lower)
	}
}

func TestParsePhotoCursor(t *testing.T) {
	at := time.Date(2024, 7, 8, 9, 10, 11, 0, time.UTC)
	cursor := PhotoCursor{CreatedAt: at, ID: 42}
	decoded, err := ParsePhotoCursor(cursor.Token())
	if err != nil {
		t.Fatalf("ParsePhotoCursor: %v", err)
	}
	if !decoded.CreatedAt.Equal(at) || decoded.ID != 42 {
		t.Errorf("decoded = %+v, want %+v", decoded, cursor)
	}
	for _, bad := range []string{"", "not-a-cursor", "Zm9v", "Zm9vX2Jhcg", "MjAyNC0wMS0wMVQwMDowMDowMFpfMA"} {
		if _, err := ParsePhotoCursor(bad); err == nil {
			t.Errorf("ParsePhotoCursor(%q) accepted a malformed token", bad)
		}
	}
}

func mustPhoto(t *testing.T) *Photo {
	t.Helper()
	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	return photo
}

func boolPtr(v bool) *bool { return &v }

func assertIDs(t *testing.T, entries []Entry, want []int64) {
	t.Helper()
	if len(entries) != len(want) {
		t.Fatalf("entries = %d, want %d (%v)", len(entries), len(want), entryIDs(entries))
	}
	for i, entry := range entries {
		if entry.ID != want[i] {
			t.Fatalf("entry %d id = %d, want %d (%v)", i, entry.ID, want[i], entryIDs(entries))
		}
	}
}

func entryIDs(entries []Entry) []int64 {
	ids := make([]int64, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}
