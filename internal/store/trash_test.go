package store

import (
	"context"
	"errors"
	"testing"
)

func TestMigrationAddsDeletedAtColumnAndIndex(t *testing.T) {
	st := newTestStore(t)

	var (
		name    string
		ctype   string
		notnull int
	)
	found := false
	rows, err := st.db.Query(`PRAGMA table_info(entries)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	for rows.Next() {
		var (
			cid  int
			dflt any
			pk   int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			t.Fatalf("scan table_info: %v", err)
		}
		if name == "deleted_at" {
			found = true
			if ctype != "TEXT" {
				t.Errorf("deleted_at type = %q, want TEXT", ctype)
			}
			if notnull != 0 {
				t.Errorf("deleted_at notnull = %d, want nullable", notnull)
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate table_info: %v", err)
	}
	rows.Close()
	if !found {
		t.Fatal("entries has no deleted_at column")
	}

	var count int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_entries_deleted_at'`,
	).Scan(&count); err != nil {
		t.Fatalf("query index: %v", err)
	}
	if count != 1 {
		t.Fatalf("idx_entries_deleted_at count = %d, want 1", count)
	}
}

func TestTrashHidesEntriesFromReadPathsAndRestores(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	first := insertRaw(t, st, "feuten", "feature", "2020-04-01T00:00:00Z")
	if _, err := st.db.Exec(`UPDATE entries SET person = 'Alice' WHERE id = ?`, first); err != nil {
		t.Fatalf("set person: %v", err)
	}
	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	second, err := st.CreateEntry(ctx, "fotos", "photos", "Bob", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.db.Exec(`UPDATE entries SET created_at = '2021-04-01T00:00:00Z' WHERE id = ?`, second.ID); err != nil {
		t.Fatalf("set created_at: %v", err)
	}
	insertRaw(t, st, "derde", "third", "2022-04-01T00:00:00Z")

	before, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if before.Total != 3 || before.WithPhoto != 1 {
		t.Fatalf("stats before = total %d photos %d, want 3/1", before.Total, before.WithPhoto)
	}

	n, err := st.SoftDeleteEntries(ctx, []int64{second.ID, second.ID, 0})
	if err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if n != 1 {
		t.Fatalf("soft deleted = %d, want 1", n)
	}

	if _, err := st.GetEntry(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetEntry(trashed) = %v, want ErrNotFound", err)
	}
	trashed, err := st.GetEntryIncludingDeleted(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetEntryIncludingDeleted: %v", err)
	}
	if trashed.DeletedAt == nil {
		t.Fatal("GetEntryIncludingDeleted returned no DeletedAt")
	}

	if got, err := st.CountEntries(ctx); err != nil || got != 2 {
		t.Fatalf("CountEntries after trash = %d (%v), want 2", got, err)
	}
	page, err := st.ListEntries(ctx, ListParams{PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("list total after trash = %d, want 2", page.Total)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "fotos"})
	if err != nil {
		t.Fatalf("ListEntries search: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("FTS search still matches trashed row: %+v", page)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "euten"})
	if err != nil {
		t.Fatalf("ListEntries partial search: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("partial search matches = %d, want 1", page.Total)
	}

	after, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if after.Total != 2 || after.WithPhoto != 0 || after.UniqueWords != 2 {
		t.Fatalf("stats after = %+v, want total 2, photos 0, unique 2", after)
	}
	for _, yc := range after.ByYear {
		if yc.Year == 2021 {
			t.Fatalf("stats by-year still counts 2021: %+v", after.ByYear)
		}
	}
	for _, pc := range after.TopPersons {
		if pc.Person == "Bob" {
			t.Fatalf("stats top persons still counts Bob: %+v", after.TopPersons)
		}
	}

	years, err := st.EntryYears(ctx)
	if err != nil {
		t.Fatalf("EntryYears: %v", err)
	}
	for _, year := range years {
		if year == 2021 {
			t.Fatalf("years still contains 2021: %v", years)
		}
	}
	persons, err := st.Persons(ctx)
	if err != nil {
		t.Fatalf("Persons: %v", err)
	}
	if len(persons) != 1 || persons[0] != "Alice" {
		t.Fatalf("persons after trash = %v, want [Alice]", persons)
	}
	counts, err := st.PersonCounts(ctx)
	if err != nil {
		t.Fatalf("PersonCounts: %v", err)
	}
	if len(counts) != 1 || counts[0].Person != "Alice" {
		t.Fatalf("person counts after trash = %+v, want Alice only", counts)
	}

	list, err := st.ListTrashedEntries(ctx)
	if err != nil {
		t.Fatalf("ListTrashedEntries: %v", err)
	}
	if len(list) != 1 || list[0].ID != second.ID || list[0].DeletedAt == nil {
		t.Fatalf("trash list = %+v, want entry %d with DeletedAt", list, second.ID)
	}

	// A trashed entry keeps its photo for inspection and restore.
	if _, err := st.GetEntryPhoto(ctx, second.ID); err != nil {
		t.Fatalf("photo after trash = %v, want retained", err)
	}

	restored, err := st.RestoreEntries(ctx, []int64{second.ID})
	if err != nil {
		t.Fatalf("RestoreEntries: %v", err)
	}
	if restored != 1 {
		t.Fatalf("restored = %d, want 1", restored)
	}
	live, err := st.GetEntry(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetEntry after restore: %v", err)
	}
	if live.DeletedAt != nil {
		t.Fatalf("restored entry DeletedAt = %v, want nil", live.DeletedAt)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "fotos"})
	if err != nil {
		t.Fatalf("ListEntries after restore: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("search after restore = %d, want 1", page.Total)
	}
	restoredStats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats after restore: %v", err)
	}
	if restoredStats.Total != 3 || restoredStats.WithPhoto != 1 {
		t.Fatalf("stats after restore = %+v, want total 3, photos 1", restoredStats)
	}
	persons, err = st.Persons(ctx)
	if err != nil {
		t.Fatalf("Persons after restore: %v", err)
	}
	if len(persons) != 2 || persons[0] != "Alice" || persons[1] != "Bob" {
		t.Fatalf("persons after restore = %v, want [Alice Bob]", persons)
	}

	// Restore is idempotent: a live id changes nothing.
	again, err := st.RestoreEntries(ctx, []int64{second.ID})
	if err != nil {
		t.Fatalf("RestoreEntries(live): %v", err)
	}
	if again != 0 {
		t.Fatalf("restored live id = %d, want 0", again)
	}
	if _, err := st.GetEntryPhoto(ctx, second.ID); err != nil {
		t.Fatalf("photo after restore = %v, want served", err)
	}
}

func TestPurgeDeletesIndexAndReleasesIdentity(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(ctx, "fotos", "photos", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	// Purging a live id is a no-op.
	var liveID int64
	if err := st.db.QueryRow(`INSERT INTO entries (word, meaning) VALUES ('live', 'kept') RETURNING id`).Scan(&liveID); err != nil {
		t.Fatalf("insert live entry: %v", err)
	}
	if n, err := st.PurgeEntries(ctx, []int64{liveID}); err != nil || n != 0 {
		t.Fatalf("PurgeEntries(live) = %d (%v), want 0", n, err)
	}
	if _, err := st.GetEntry(ctx, liveID); err != nil {
		t.Fatalf("live entry disappeared: %v", err)
	}

	n, err := st.PurgeEntries(ctx, []int64{entry.ID, entry.ID})
	if err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
	if n != 1 {
		t.Fatalf("purged = %d, want 1", n)
	}
	if _, err := st.GetEntryIncludingDeleted(ctx, entry.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purged entry still readable: %v", err)
	}
	if _, err := st.GetEntryPhoto(ctx, entry.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("photo after purge = %v, want ErrNotFound", err)
	}
	var ftsRows int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM entries_fts WHERE rowid = ?`, entry.ID).Scan(&ftsRows); err != nil {
		t.Fatalf("count fts row: %v", err)
	}
	if ftsRows != 0 {
		t.Fatalf("fts rows after purge = %d, want 0", ftsRows)
	}
	list, err := st.ListTrashedEntries(ctx)
	if err != nil {
		t.Fatalf("ListTrashedEntries: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("trash after purge = %+v, want empty", list)
	}

	// The (word, meaning) identity is free again once the row is purged.
	replacement, err := st.CreateEntry(ctx, "fotos", "photos", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry after purge: %v", err)
	}
	if replacement.ID == entry.ID {
		t.Fatal("replacement reused the purged id")
	}
}

func TestTrashedEntryStillReservesUniqueIdentity(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	entry, err := st.CreateEntry(ctx, "dubbel", "double", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.CreateEntry(ctx, "DUBBEL", "Double", "", nil); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("create against trashed row = %v, want ErrDuplicate", err)
	}
	if _, err := st.PurgeEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
	if _, err := st.CreateEntry(ctx, "DUBBEL", "Double", "", nil); err != nil {
		t.Fatalf("create after purge = %v, want success", err)
	}
}
