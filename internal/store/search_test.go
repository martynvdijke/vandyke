package store

import (
	"context"
	"testing"
	"time"
)

func TestSearchIndexFollowsPerson(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	entry, err := st.CreateEntry(ctx, "naam", "a slip with a name", "M. van Dijke", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	page, err := st.ListEntries(ctx, ListParams{Query: "Dijke", PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != entry.ID {
		t.Fatalf("person-only search = %+v, want entry %d", page, entry.ID)
	}

	if _, err := st.UpdateEntry(ctx, EntryUpdate{
		ID: entry.ID, Word: "naam", Meaning: "a slip with a name", Person: "M. van Dijk",
	}); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "Dijke", PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries after rename: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("old person still matches after rename: %+v", page)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "Dijk", PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries new person: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != entry.ID {
		t.Fatalf("new person search = %+v, want entry %d", page, entry.ID)
	}

	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "Dijk", PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries after trash: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("trashed entry still matches by person: %+v", page)
	}

	if _, err := st.RestoreEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("RestoreEntries: %v", err)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "Dijk", PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries after restore: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("restored entry not reindexed: %+v", page)
	}
}

func TestSearchDateRangeIsInclusive(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	first := insertRaw(t, st, "eerste", "first", "2024-01-01T00:00:00Z")
	middle := insertRaw(t, st, "midden", "middle", "2024-01-15T12:00:00Z")
	last := insertRaw(t, st, "laatste", "last", "2024-01-31T00:00:00Z")
	february := insertRaw(t, st, "februari", "february", "2024-02-01T00:00:00Z")
	june := insertRaw(t, st, "juni", "june", "2024-06-01T00:00:00Z")
	nextYear := insertRaw(t, st, "volgend", "next year", "2025-01-01T00:00:00Z")

	date := func(value string) time.Time {
		t.Helper()
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			t.Fatalf("parse %q: %v", value, err)
		}
		return parsed
	}

	t.Run("both boundaries included", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{From: date("2024-01-01"), To: date("2024-01-31"), PerPage: 10})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Total != 3 {
			t.Fatalf("range total = %d, want 3 (%+v)", page.Total, page)
		}
		got := map[int64]bool{}
		for _, entry := range page.Entries {
			got[entry.ID] = true
		}
		for _, id := range []int64{first, middle, last} {
			if !got[id] {
				t.Errorf("entry %d missing from inclusive range", id)
			}
		}
		for _, id := range []int64{february, june, nextYear} {
			if got[id] {
				t.Errorf("entry %d outside the range was returned", id)
			}
		}
	})

	t.Run("lower boundary only", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{From: date("2024-01-02"), PerPage: 10})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Total != 5 {
			t.Fatalf("open-ended total = %d, want 5", page.Total)
		}
		for _, entry := range page.Entries {
			if entry.ID == first {
				t.Fatal("open-ended range returned the entry before From")
			}
		}
	})

	t.Run("upper boundary only", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{To: date("2024-01-30"), PerPage: 10})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Total != 2 {
			t.Fatalf("upper-bound total = %d, want 2", page.Total)
		}
	})

	t.Run("intersects with year", func(t *testing.T) {
		page, err := st.ListEntries(ctx, ListParams{Year: 2024, From: date("2024-06-01"), PerPage: 10})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Total != 1 || page.Entries[0].ID != june {
			t.Fatalf("year and From intersection = %+v, want only %d", page, june)
		}
	})
}

func TestRebuildSearchIndexRepopulatesPersonAndSkipsTrash(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	live, err := st.CreateEntry(ctx, "levend", "still here", "Live Person", nil)
	if err != nil {
		t.Fatalf("CreateEntry live: %v", err)
	}
	trashed, err := st.CreateEntry(ctx, "weg", "removed", "Ghost Person", nil)
	if err != nil {
		t.Fatalf("CreateEntry trashed: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{trashed.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	// Simulate a drifted index: drop every row, then rebuild.
	if _, err := st.db.Exec(`DELETE FROM entries_fts`); err != nil {
		t.Fatalf("clear index: %v", err)
	}
	if err := st.RebuildSearchIndex(ctx); err != nil {
		t.Fatalf("RebuildSearchIndex: %v", err)
	}

	page, err := st.ListEntries(ctx, ListParams{Query: "Live", PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries live person: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != live.ID {
		t.Fatalf("rebuild did not index the live person: %+v", page)
	}

	var trashedRows int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM entries_fts WHERE rowid = ?`, trashed.ID).Scan(&trashedRows); err != nil {
		t.Fatalf("count trashed index rows: %v", err)
	}
	if trashedRows != 0 {
		t.Fatalf("rebuild indexed %d rows for a trashed entry, want 0", trashedRows)
	}
	page, err = st.ListEntries(ctx, ListParams{Query: "Ghost", PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntries trashed person: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("trashed person searchable after rebuild: %+v", page)
	}
}

func TestSearchPhraseExclusionAndQuotesThroughFTS(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	phraseOnly, err := st.CreateEntry(ctx, "eerste", "de feature van Dijke", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	excluded, err := st.CreateEntry(ctx, "tweede", "feature feutenreus", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	page, err := st.ListEntries(ctx, ListParams{Query: `"van Dijke"`, PerPage: 10})
	if err != nil {
		t.Fatalf("phrase search: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != phraseOnly.ID {
		t.Fatalf("phrase search = %+v, want only %d", page, phraseOnly.ID)
	}

	page, err = st.ListEntries(ctx, ListParams{Query: "feature -feutenreus", PerPage: 10})
	if err != nil {
		t.Fatalf("exclusion search: %v", err)
	}
	if page.Total != 1 || page.Entries[0].ID != phraseOnly.ID {
		t.Fatalf("exclusion search = %+v, want only %d", page, phraseOnly.ID)
	}

	// A quote inside a term must not break the FTS expression.
	if _, err := st.ListEntries(ctx, ListParams{Query: `feature" OR`, PerPage: 10}); err != nil {
		t.Fatalf("quoted search: %v", err)
	}
	_ = excluded
}
