package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

func TestListEntryTagFilterIntersects(t *testing.T) {
	srv, st := newTestAPI(t, ratelimit.New(1000, 100))
	ctx := context.Background()

	typo, err := st.GetTagBySlug(ctx, "typo")
	if err != nil {
		t.Fatalf("GetTagBySlug: %v", err)
	}
	name, err := st.GetTagBySlug(ctx, "name")
	if err != nil {
		t.Fatalf("GetTagBySlug: %v", err)
	}

	create := func(word, day string, tags ...int64) store.Entry {
		t.Helper()
		created, err := st.CreateEntry(ctx, word, "correction", "", nil)
		if err != nil {
			t.Fatalf("CreateEntry %q: %v", word, err)
		}
		parsed, err := time.Parse("2006-01-02", day)
		if err != nil {
			t.Fatalf("parse %q: %v", day, err)
		}
		_, updated, err := st.UpdateEntry(ctx, store.EntryUpdate{
			ID: created.ID, Word: word, Meaning: "correction", CreatedAt: parsed.UTC(),
		})
		if err != nil {
			t.Fatalf("UpdateEntry %q: %v", word, err)
		}
		if err := st.SetEntryTags(ctx, updated.ID, tags); err != nil {
			t.Fatalf("SetEntryTags %q: %v", word, err)
		}
		return updated
	}

	current := create("taggedfeature", "2024-03-01", typo.ID)
	older := create("taggedolder", "2023-03-01", typo.ID)
	create("taggedother", "2024-04-01", name.ID)
	cases := []struct {
		query string
		want  int
	}{
		{"tag=typo&per_page=100", 2},
		{"tag=typo&q=taggedfeature&per_page=100", 1},
		{"tag=typo&year=2024&per_page=100", 1},
		{"tag=typo&tag=name&per_page=100", 0},
		{"tag=does-not-exist&per_page=100", 0},
		{"tag=typo&year=2024&to=2024-03-31&per_page=100", 1},
	}
	for _, tc := range cases {
		var page store.Page
		resp := getJSON(t, srv.URL+"/api/v1/entries?"+tc.query, &page)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", tc.query, resp.StatusCode)
		}
		if page.Total != tc.want {
			t.Fatalf("%s total = %d, want %d", tc.query, page.Total, tc.want)
		}
	}

	var page store.Page
	getJSON(t, srv.URL+"/api/v1/entries?tag=typo&per_page=100", &page)
	if len(page.Entries) != 2 {
		t.Fatalf("tag filtered entries = %d", len(page.Entries))
	}
	for _, entry := range page.Entries {
		if entry.ID != current.ID && entry.ID != older.ID {
			t.Fatalf("tag filter returned untagged entry %+v", entry)
		}
	}

	if _, err := st.SoftDeleteEntries(ctx, []int64{current.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	getJSON(t, srv.URL+"/api/v1/entries?tag=typo&per_page=100", &page)
	if page.Total != 1 || page.Entries[0].ID != older.ID {
		t.Fatalf("tag filter after trash = %+v", page)
	}
}
