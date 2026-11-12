package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

// clearLedger removes every entry, using the same trash-then-purge lifecycle an
// administrator would, so browse tests start from a known corpus.
func clearLedger(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	ids, err := st.AllEntryIDs(ctx)
	if err != nil {
		t.Fatalf("AllEntryIDs: %v", err)
	}
	if len(ids) == 0 {
		return
	}
	if _, err := st.SoftDeleteEntries(ctx, ids); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.PurgeEntries(ctx, ids); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
}

func makePhotoEntry(t *testing.T, st *store.Store, word, meaning, person, day string) store.Entry {
	t.Helper()
	ctx := context.Background()
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(ctx, word, meaning, person, photo)
	if err != nil {
		t.Fatalf("CreateEntry photo %q: %v", word, err)
	}
	if day == "" {
		return entry
	}
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatalf("parse day %q: %v", day, err)
	}
	_, updated, err := st.UpdateEntry(ctx, store.EntryUpdate{
		ID: entry.ID, Word: word, Meaning: meaning, Person: person, CreatedAt: parsed.UTC(),
	})
	if err != nil {
		t.Fatalf("UpdateEntry photo %q: %v", word, err)
	}
	return updated
}

func TestWordsDefaultBucketAndRail(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	boeke := makeEntry(t, st, "boeke", "books", "", "2021-01-01")
	makeEntry(t, st, "fiets", "bicycle", "", "2021-02-01")
	makeEntry(t, st, "Feutenreus", "feature", "", "2020-01-01")
	newest := makeEntry(t, st, "feutenreus", "featured", "", "2022-01-01")
	// A trashed spelling never reaches its bucket or its count.
	trashed := makeEntry(t, st, "kwak", "quack", "", "2021-05-01")
	if _, err := st.SoftDeleteEntries(context.Background(), []int64{trashed.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	status, _, body := fetch(t, srv, "/words", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	// No # rows, so the default bucket is the first non-empty letter, B.
	if !strings.Contains(body, `<a class="word-letter is-active" href="/words?letter=B" aria-current="page">B`) {
		t.Fatalf("B is not the active bucket: %s", body)
	}
	if !strings.Contains(body, `href="/words?letter=F"`) {
		t.Error("rail has no F link")
	}
	if !strings.Contains(body, `<span class="word-letter is-empty" aria-disabled="true">Q`) {
		t.Error("empty Q is not inert")
	}
	if !strings.Contains(body, `<span class="word-letter is-empty" aria-disabled="true">K`) {
		t.Error("a bucket whose only word is trashed is not empty")
	}
	if !strings.Contains(body, `href="/e/`+fmt.Sprint(boeke.ID)+`"`) {
		t.Error("B bucket does not link to the word's permalink")
	}
	// The F bucket is not rendered in this document, and the grouped spelling
	// shown is the newest occurrence's.
	_, _, letterF := fetch(t, srv, "/words?letter=F", nil)
	if !strings.Contains(letterF, "/e/"+fmt.Sprint(newest.ID)) {
		t.Error("F bucket does not link to the newest occurrence")
	}
	if !strings.Contains(letterF, "feutenreus") || strings.Contains(letterF, "Feutenreus") {
		t.Errorf("grouped row does not show the newest spelling: %s", letterF)
	}
	if !strings.Contains(letterF, `title="Entries carrying this spelling">2×</span>`) {
		t.Error("grouped row count is not 2")
	}
	if !strings.Contains(letterF, `<a class="word-letter is-active" href="/words?letter=F" aria-current="page">F`) {
		t.Error("F is not marked active")
	}
}

func TestWordsNonLetterBucketAndExplicitLetter(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	digit := makeEntry(t, st, "3dprint", "3d printer", "", "2021-01-01")
	makeEntry(t, st, "feutenreus", "feature", "", "2021-01-02")

	_, _, body := fetch(t, srv, "/words", nil)
	if !strings.Contains(body, `<a class="word-letter is-active" href="/words?letter=%23" aria-current="page">#`) {
		t.Fatalf("# is not the default active bucket: %s", body)
	}
	if !strings.Contains(body, `href="/e/`+fmt.Sprint(digit.ID)+`"`) {
		t.Error("# bucket does not list the digit word")
	}

	// Lower case is accepted and normalised, and the pager/rail keep the
	// canonical upper-case spelling.
	_, _, lower := fetch(t, srv, "/words?letter=f", nil)
	if !strings.Contains(lower, "feutenreus") {
		t.Error("lower-case letter did not select F")
	}
	if !strings.Contains(lower, `<a class="word-letter is-active" href="/words?letter=F"`) {
		t.Error("active letter is not normalised to F")
	}
	for _, path := range []string{"/words?letter=Q", "/words?letter=1", "/words?letter=AA"} {
		status, _, page := fetch(t, srv, path, nil)
		if status != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, status)
		}
		if !strings.Contains(page, "Letter not found") {
			t.Errorf("%s did not render the not-found page", path)
		}
	}
}

func TestWordsPaginationKeepsLetter(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	const total = wordsPerPage + 5
	for i := 0; i < total; i++ {
		makeEntry(t, st, fmt.Sprintf("slip%03d", i), "correction", "", "")
	}

	status, _, first := fetch(t, srv, "/words?letter=S", nil)
	if status != http.StatusOK {
		t.Fatalf("first page status = %d", status)
	}
	if got := strings.Count(first, `class="word-row"`); got != wordsPerPage {
		t.Errorf("first page rows = %d, want %d", got, wordsPerPage)
	}
	if !strings.Contains(first, `aria-current="page">1<`) {
		t.Error("pager does not mark page 1")
	}
	if !strings.Contains(first, `href="/words?letter=S&amp;page=2"`) && !strings.Contains(first, `href="/words?letter=S&page=2"`) {
		t.Error("pager has no page 2 link carrying the letter")
	}

	status, _, second := fetch(t, srv, "/words?letter=S&page=2", nil)
	if status != http.StatusOK {
		t.Fatalf("second page status = %d", status)
	}
	if got := strings.Count(second, `class="word-row"`); got != total-wordsPerPage {
		t.Errorf("second page rows = %d, want %d", got, total-wordsPerPage)
	}
	if !strings.Contains(second, `rel="prev"`) || !strings.Contains(second, `aria-current="page">2<`) {
		t.Error("second page has no prev link or page marker")
	}
}

func TestWordsPhotoOnlyNoteAndEmptyIndex(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	makeEntry(t, st, "kast", "kassasysteem", "", "2021-01-01")
	photoOnly := makePhotoEntry(t, st, "", "", "", "")

	_, _, body := fetch(t, srv, "/words", nil)
	if !strings.Contains(body, "One slip has only a photograph") {
		t.Errorf("photo-only note missing: %s", body)
	}
	if !strings.Contains(body, `href="/photos"`) {
		t.Error("photo-only note does not link to /photos")
	}

	// Without a worded entry the index states that it is empty and still shows
	// the rail and the note.
	ids, err := st.AllEntryIDs(context.Background())
	if err != nil {
		t.Fatalf("AllEntryIDs: %v", err)
	}
	for _, id := range ids {
		entry, err := st.GetEntry(context.Background(), id)
		if err != nil {
			t.Fatalf("GetEntry: %v", err)
		}
		if entry.ID != photoOnly.ID {
			if _, err := st.SoftDeleteEntries(context.Background(), []int64{id}); err != nil {
				t.Fatalf("SoftDeleteEntries: %v", err)
			}
		}
	}
	_, _, empty := fetch(t, srv, "/words", nil)
	if !strings.Contains(empty, "The index is empty") {
		t.Errorf("empty index message missing: %s", empty)
	}
	if !strings.Contains(empty, `class="word-rail"`) {
		t.Error("empty index dropped the rail")
	}
	if !strings.Contains(empty, "One slip has only a photograph") {
		t.Error("empty index dropped the photo-only note")
	}

	// No photo-only entry left either: the note disappears.
	if _, err := st.SoftDeleteEntries(context.Background(), []int64{photoOnly.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries photo-only: %v", err)
	}
	_, _, noNote := fetch(t, srv, "/words", nil)
	if strings.Contains(noNote, "only a photograph") {
		t.Error("photo-only note rendered with no photo-only slips")
	}
}

func TestWordsCanonicalAndRobots(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)
	makeEntry(t, st, "boeke", "books", "", "2021-01-01")

	for _, tc := range []struct {
		path       string
		noindex    bool
		wantStatus int
	}{
		{path: "/words", noindex: false, wantStatus: http.StatusOK},
		{path: "/words?letter=B", noindex: true, wantStatus: http.StatusOK},
		{path: "/words?letter=B&page=1", noindex: true, wantStatus: http.StatusOK},
		{path: "/words?page=1", noindex: true, wantStatus: http.StatusOK},
	} {
		status, _, body := fetch(t, srv, tc.path, nil)
		if status != tc.wantStatus {
			t.Errorf("%s status = %d, want %d", tc.path, status, tc.wantStatus)
			continue
		}
		if !strings.Contains(body, `<link rel="canonical" href="`+srv.URL+`/words">`) {
			t.Errorf("%s canonical is not /words", tc.path)
		}
		hasNoindex := strings.Contains(body, `<meta name="robots" content="noindex,follow">`)
		if hasNoindex != tc.noindex {
			t.Errorf("%s noindex = %v, want %v", tc.path, hasNoindex, tc.noindex)
		}
	}
}
