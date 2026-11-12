package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
)

func TestPersonPageCanonicalisesCase(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	makeEntry(t, st, "een", "one", "Marieke", "2021-01-02")
	makeEntry(t, st, "twee", "two", "marieke", "2022-03-04")
	makeEntry(t, st, "drie", "three", "MARIEKE", "2020-05-06")
	makeEntry(t, st, "vier", "four", "Someone Else", "2023-01-01")

	status, _, body := fetch(t, srv, "/p/marieke", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	for _, want := range []string{"een", "twee", "drie"} {
		if !strings.Contains(body, ">"+want+"<") {
			t.Errorf("person page is missing %q", want)
		}
	}
	if strings.Contains(body, ">vier<") {
		t.Error("person page leaked another person's slip")
	}
	if !strings.Contains(body, "3 slips carry this name") {
		t.Errorf("person page does not state the count: %s", body)
	}
	if !strings.Contains(body, "06 May 2020") || !strings.Contains(body, "04 Mar 2022") {
		t.Errorf("person page does not show the first and last slip dates: %s", body)
	}
	// The newest spelling is the canonical one, whatever case was requested.
	if !strings.Contains(body, `<link rel="canonical" href="`+srv.URL+`/p/marieke">`) {
		t.Errorf("canonical is not the newest spelling: %s", body)
	}
	if !strings.Contains(body, `<meta name="robots" content="noindex,follow">`) {
		t.Error("person page is indexable")
	}
	if !strings.Contains(body, `<link rel="alternate" type="application/atom+xml" title="The vanDyke Corrections Ledger" href="/feed.xml">`) {
		t.Error("person page dropped the site feed autodiscovery link")
	}
	if !strings.Contains(body, `<link rel="alternate" type="application/atom+xml" title="Slips by marieke" href="`+srv.URL+`/p/marieke/feed.xml">`) {
		t.Errorf("person page has no person feed autodiscovery link: %s", body)
	}

	_, _, upper := fetch(t, srv, "/p/MARIEKE", nil)
	if !strings.Contains(upper, `<link rel="canonical" href="`+srv.URL+`/p/marieke">`) {
		t.Error("upper-case request does not canonicalise to one document")
	}
	if strings.Contains(upper, `href="`+srv.URL+`/p/MARIEKE`) {
		t.Error("case variant leaked into a canonical URL")
	}
}

func TestPersonPageScopeSurvivesEveryControl(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	makeEntry(t, st, "een", "one", "Marieke", "2021-01-02")
	makeEntry(t, st, "twee", "two", "Marieke", "2022-03-04")
	makeEntry(t, st, "vier", "four", "Someone Else", "2023-01-01")
	for i := 0; i < 35; i++ {
		makeEntry(t, st, fmt.Sprintf("extra%02d", i), "filler", "Marieke", "")
	}

	_, _, body := fetch(t, srv, "/p/Marieke", nil)
	for _, want := range []string{
		`action="/p/Marieke"`,
		`hx-get="/p/Marieke"`,
		`href="/p/Marieke?`,
		`hx-get="/p/Marieke?`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("person page control does not target the person path: missing %q", want)
		}
	}
	if strings.Contains(body, `<select name="person"`) {
		t.Error("person page still renders the person select")
	}
	if !strings.Contains(body, `class="person-chip">Marieke`) {
		t.Error("person page has no locked person chip")
	}

	// A person parameter is ignored: the path wins.
	_, _, ignored := fetch(t, srv, "/p/Marieke?person=Someone%20Else&per_page=100", nil)
	if strings.Contains(ignored, ">vier<") {
		t.Error("person query parameter widened the scope")
	}
	if !strings.Contains(ignored, ">een<") {
		t.Error("path scope was lost")
	}

	// Sorting keeps the person.
	_, _, sorted := fetch(t, srv, "/p/Marieke?sort=meaning&order=desc&per_page=100", nil)
	if strings.Contains(sorted, ">vier<") || !strings.Contains(sorted, ">een<") {
		t.Error("sorting dropped or widened the person scope")
	}

	// Searching keeps the person.
	_, _, searched := fetch(t, srv, "/p/Marieke?q=een&per_page=100", nil)
	if !strings.Contains(searched, ">een<") || strings.Contains(searched, ">twee<") {
		t.Error("search did not stay inside the person")
	}

	// The pager links back to the person path and page 2 keeps the scope.
	if !strings.Contains(body, `page=2`) {
		t.Fatal("person page has no second page")
	}
	_, _, page2 := fetch(t, srv, "/p/Marieke?page=2&per_page=30", nil)
	if strings.Contains(page2, ">vier<") {
		t.Error("second person page included another person's slip")
	}
	if !strings.Contains(page2, `href="/p/Marieke?`) {
		t.Error("second person page controls lost the person path")
	}

	// HTMX gets only the scoped ledger fragment.
	_, _, fragment := fetch(t, srv, "/p/Marieke?sort=word", map[string]string{"HX-Request": "true"})
	if strings.Contains(fragment, "<!doctype html>") || strings.Contains(fragment, "<html") {
		t.Error("HTMX response is a full document")
	}
	if !strings.Contains(fragment, `id="ledger"`) {
		t.Error("HTMX response is not the ledger fragment")
	}
}

func TestPersonPageNotFound(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)
	makeEntry(t, st, "een", "one", "Marieke", "2021-01-02")

	for _, path := range []string{"/p/Nobody", "/p/%20", "/p/Nobody/feed.xml"} {
		status, _, body := fetch(t, srv, path, nil)
		if status != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, status)
		}
		if path != "/p/%20" && !strings.Contains(body, "Person not found") {
			t.Errorf("%s did not render the not-found page", path)
		}
	}

	// /p/ never reaches the handler: a wildcard segment is non-empty.
	status, _, _ := fetch(t, srv, "/p/", nil)
	if status != http.StatusNotFound {
		t.Errorf("/p/ status = %d, want 404", status)
	}
}

func TestPersonPageEncodedNames(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	makeEntry(t, st, "een", "one", "M. van Dijke", "2021-01-02")
	makeEntry(t, st, "twee", "two", "A/B", "2021-01-03")

	status, _, body := fetch(t, srv, "/p/M.%20van%20Dijke", nil)
	if status != http.StatusOK {
		t.Fatalf("spaced name status = %d", status)
	}
	if !strings.Contains(body, `<link rel="canonical" href="`+srv.URL+`/p/M.%20van%20Dijke">`) {
		t.Error("spaced name canonical is not the encoded path")
	}

	// %2F stays inside one wildcard segment and resolves to the stored name.
	status, _, slash := fetch(t, srv, "/p/A%2FB", nil)
	if status != http.StatusOK {
		t.Fatalf("slashed name status = %d", status)
	}
	if !strings.Contains(slash, "twee") {
		t.Error("slashed name page did not resolve")
	}
	if !strings.Contains(slash, `<link rel="canonical" href="`+srv.URL+`/p/A%2FB">`) {
		t.Errorf("slashed name canonical is not the encoded path: %s", slash)
	}
	status, _, slashedFeed := fetch(t, srv, "/p/A%2FB/feed.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("slashed name feed status = %d", status)
	}
	if got := parseFeed(t, slashedFeed).ID; got != srv.URL+"/p/A%2FB/feed.xml" {
		t.Errorf("slashed name feed id = %q", got)
	}

	// A hand-built /p/a/b is two segments and never matches the person route.
	status, _, _ = fetch(t, srv, "/p/A/B", nil)
	if status != http.StatusNotFound {
		t.Errorf("hand-built slash path status = %d, want 404", status)
	}
}

func TestPersonFeedContentETagAndLimits(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	oldest := makeEntry(t, st, "een", "one", "Marieke", "2020-01-01")
	newest := makeEntry(t, st, "drie", "three", "Marieke", "2022-01-01")
	middle := makeEntry(t, st, "twee", "two", "marieke", "2021-01-01")
	trashed := makeEntry(t, st, "weg", "gone", "Marieke", "2023-01-01")
	makeEntry(t, st, "ander", "other", "Someone Else", "2024-01-01")
	ctx := context.Background()
	if _, err := st.SoftDeleteEntries(ctx, []int64{trashed.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	status, headers, body := fetch(t, srv, "/p/marieke/feed.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "application/atom+xml") {
		t.Errorf("content type = %q", got)
	}
	if got := headers.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("cache control = %q", got)
	}
	feed := parseFeed(t, body)
	self := srv.URL + "/p/Marieke/feed.xml"
	if feed.ID != self || feed.Link.Href != self {
		t.Errorf("feed id/self = %q/%q, want %q", feed.ID, feed.Link.Href, self)
	}
	if feed.Author.Name != "Marieke" {
		t.Errorf("feed author = %q, want Marieke", feed.Author.Name)
	}
	if len(feed.Entries) != 3 {
		t.Fatalf("entries = %d, want 3 (%+v)", len(feed.Entries), feed.Entries)
	}
	wantOrder := []string{newest.Word, middle.Word, oldest.Word}
	seen := make(map[string]bool)
	for i, entry := range feed.Entries {
		if entry.Title != wantOrder[i] {
			t.Errorf("entry %d title = %q, want %q", i, entry.Title, wantOrder[i])
		}
		seen[entry.Title] = true
		if entry.Author == nil || entry.Author.Name == "" {
			t.Errorf("entry %q has no author", entry.Title)
		}
	}
	if seen[trashed.Word] {
		t.Error("trashed entry is in the person feed")
	}

	etag := headers.Get("ETag")
	if etag == "" {
		t.Fatal("person feed has no ETag")
	}
	status, _, conditional := fetch(t, srv, "/p/marieke/feed.xml", map[string]string{"If-None-Match": etag})
	if status != http.StatusNotModified || conditional != "" {
		t.Fatalf("conditional feed = %d, body %d bytes", status, len(conditional))
	}

	// Two persons never share a feed id or an ETag.
	_, otherHeaders, otherBody := fetch(t, srv, "/p/Someone%20Else/feed.xml", nil)
	other := parseFeed(t, otherBody)
	if other.ID == feed.ID {
		t.Error("two persons share a feed id")
	}
	if otherHeaders.Get("ETag") == etag {
		t.Error("two persons share an ETag")
	}

	if _, err := st.SoftDeleteEntries(ctx, []int64{oldest.ID, newest.ID, middle.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if status, _, _ := fetch(t, srv, "/p/Marieke/feed.xml", nil); status != http.StatusNotFound {
		t.Errorf("all-trashed person feed status = %d, want 404", status)
	}
}

func TestPersonFeedLimitClamp(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	const total = 105
	for i := 0; i < total; i++ {
		makeEntry(t, st, fmt.Sprintf("slip%03d", i), "correction", "Marieke", "")
	}

	_, _, body := fetch(t, srv, "/p/Marieke/feed.xml?limit=5000", nil)
	if got := len(parseFeed(t, body).Entries); got != 100 {
		t.Errorf("limit=5000 returned %d entries, want the 100 cap", got)
	}
	_, _, body = fetch(t, srv, "/p/Marieke/feed.xml", nil)
	if got := len(parseFeed(t, body).Entries); got != 50 {
		t.Errorf("default limit returned %d entries, want 50", got)
	}
	_, _, body = fetch(t, srv, "/p/Marieke/feed.xml?limit=2", nil)
	if got := len(parseFeed(t, body).Entries); got != 2 {
		t.Errorf("limit=2 returned %d entries", got)
	}
}
