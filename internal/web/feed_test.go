package web

import (
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

type parsedAuthor struct {
	Name string `xml:"name"`
}

type parsedLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type parsedEntry struct {
	ID      string        `xml:"id"`
	Title   string        `xml:"title"`
	Summary string        `xml:"summary"`
	Updated string        `xml:"updated"`
	Link    parsedLink    `xml:"link"`
	Author  *parsedAuthor `xml:"author"`
}

type parsedFeed struct {
	XMLName xml.Name      `xml:"feed"`
	Xmlns   string        `xml:"xmlns,attr"`
	ID      string        `xml:"id"`
	Title   string        `xml:"title"`
	Updated string        `xml:"updated"`
	Author  parsedAuthor  `xml:"author"`
	Link    parsedLink    `xml:"link"`
	Entries []parsedEntry `xml:"entry"`
}

func parseFeed(t *testing.T, body string) parsedFeed {
	t.Helper()
	var feed parsedFeed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatalf("feed is not well-formed XML: %v\n%.400s", err, body)
	}
	if feed.Xmlns != "http://www.w3.org/2005/Atom" {
		t.Fatalf("xmlns = %q, want the Atom namespace", feed.Xmlns)
	}
	return feed
}

func TestFeedMapsEntriesAndEscapesText(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	escaped, err := st.CreateEntry(t.Context(),
		`kast & "systeem" <x>`, `kassasysteem & zo`, "A & B <C>", nil)
	if err != nil {
		t.Fatalf("CreateEntry escaped: %v", err)
	}
	newest, err := st.CreateEntry(t.Context(), "nieuwste", "newest slip", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry newest: %v", err)
	}
	// Make the unattributed entry unambiguously newest.
	if _, err := st.UpdateEntry(t.Context(), store.EntryUpdate{
		ID: newest.ID, Word: "nieuwste", Meaning: "newest slip",
		CreatedAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpdateEntry newest: %v", err)
	}

	status, headers, body := fetch(t, srv, "/feed.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("feed status = %d", status)
	}
	if got := headers.Get("Content-Type"); got != "application/atom+xml; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(body, "&amp;") || !strings.Contains(body, "&lt;x&gt;") {
		t.Fatalf("user text was not XML-escaped: %.600s", body)
	}

	feed := parseFeed(t, body)
	if feed.ID != srv.URL+"/feed.xml" {
		t.Fatalf("feed id = %q, want %q", feed.ID, srv.URL+"/feed.xml")
	}
	if feed.Author.Name == "" {
		t.Fatal("feed-level author missing")
	}
	if len(feed.Entries) == 0 {
		t.Fatal("feed has no entries")
	}

	first := feed.Entries[0]
	if first.ID != srv.URL+"/e/"+strconv.FormatInt(newest.ID, 10) {
		t.Fatalf("first entry id = %q", first.ID)
	}
	if first.Title != "nieuwste" || first.Summary != "newest slip" {
		t.Fatalf("first entry = %+v", first)
	}
	if first.Author != nil {
		t.Fatalf("unattributed entry has an author: %+v", first.Author)
	}
	if first.Link.Href != first.ID {
		t.Fatalf("entry link = %q, want id %q", first.Link.Href, first.ID)
	}
	if first.Updated != "2030-01-02T03:04:05Z" {
		t.Fatalf("entry updated = %q", first.Updated)
	}

	var found *parsedEntry
	for i := range feed.Entries {
		if feed.Entries[i].Title == `kast & "systeem" <x>` {
			found = &feed.Entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("escaped entry missing from feed: %+v", feed.Entries)
	}
	if found.Summary != "kassasysteem & zo" {
		t.Fatalf("escaped summary round-trip = %q", found.Summary)
	}
	if found.Author == nil || found.Author.Name != "A & B <C>" {
		t.Fatalf("person author = %+v", found.Author)
	}
	if found.ID != srv.URL+"/e/"+strconv.FormatInt(escaped.ID, 10) {
		t.Fatalf("escaped entry id = %q", found.ID)
	}
}

func TestFeedOrderingAndLimit(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	newest, err := st.CreateEntry(t.Context(), "allernieuwste", "the newest", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.UpdateEntry(t.Context(), store.EntryUpdate{
		ID: newest.ID, Word: "allernieuwste", Meaning: "the newest",
		CreatedAt: time.Date(2031, 6, 7, 8, 9, 10, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}

	// The seeded ledger has 78 entries; add enough to exercise the 100 cap.
	for i := 0; i < 30; i++ {
		if _, err := st.CreateEntry(t.Context(), "cap-"+strconv.Itoa(i), "cap filler "+strconv.Itoa(i), "", nil); err != nil {
			t.Fatalf("CreateEntry filler: %v", err)
		}
	}

	_, _, body := fetch(t, srv, "/feed.xml", nil)
	feed := parseFeed(t, body)
	if feed.Entries[0].Title != "allernieuwste" {
		t.Fatalf("feed is not newest first: first = %q", feed.Entries[0].Title)
	}
	for i := 1; i < len(feed.Entries); i++ {
		prev, err := time.Parse(time.RFC3339, feed.Entries[i-1].Updated)
		if err != nil {
			t.Fatalf("parse updated: %v", err)
		}
		cur, err := time.Parse(time.RFC3339, feed.Entries[i].Updated)
		if err != nil {
			t.Fatalf("parse updated: %v", err)
		}
		if cur.After(prev) {
			t.Fatalf("entry %d is newer than its predecessor", i)
		}
	}

	tests := []struct {
		name string
		path string
		want int
	}{
		{name: "default", path: "/feed.xml", want: 50},
		{name: "small", path: "/feed.xml?limit=10", want: 10},
		{name: "capped", path: "/feed.xml?limit=5000", want: 100},
		{name: "non numeric", path: "/feed.xml?limit=all", want: 50},
		{name: "sub one", path: "/feed.xml?limit=0", want: 50},
		{name: "negative", path: "/feed.xml?limit=-3", want: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, body := fetch(t, srv, tt.path, nil)
			feed := parseFeed(t, body)
			if len(feed.Entries) != tt.want {
				t.Fatalf("%s entries = %d, want %d", tt.path, len(feed.Entries), tt.want)
			}
		})
	}
}

func TestFeedETagAndConditionalRequest(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	status, headers, _ := fetch(t, srv, "/feed.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("feed status = %d", status)
	}
	tag := headers.Get("ETag")
	if tag == "" {
		t.Fatal("feed has no ETag")
	}
	if got := headers.Get("Cache-Control"); got != "public, max-age=60" {
		t.Fatalf("cache control = %q", got)
	}

	status, _, body := fetch(t, srv, "/feed.xml", map[string]string{"If-None-Match": tag})
	if status != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", status)
	}
	if body != "" {
		t.Fatalf("304 carried a body: %q", body)
	}

	entry, err := st.CreateEntry(t.Context(), "etag-nieuw", "invalidates the tag", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	status, headers, body = fetch(t, srv, "/feed.xml", map[string]string{"If-None-Match": tag})
	if status != http.StatusOK {
		t.Fatalf("status after create = %d, want 200", status)
	}
	if headers.Get("ETag") == tag {
		t.Fatal("ETag did not change after a new entry")
	}
	if !strings.Contains(body, "/e/"+strconv.FormatInt(entry.ID, 10)) {
		t.Fatal("new entry missing from the refreshed feed")
	}
}

func TestFeedExcludesTrashedEntries(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	entry, err := st.CreateEntry(t.Context(), "weggegooid", "trashed slip", "Ghost", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.SoftDeleteEntries(t.Context(), []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	_, _, body := fetch(t, srv, "/feed.xml?limit=100", nil)
	if strings.Contains(body, "weggegooid") || strings.Contains(body, "/e/"+strconv.FormatInt(entry.ID, 10)) {
		t.Fatal("trashed entry appeared in the feed")
	}
	feed := parseFeed(t, body)
	for _, item := range feed.Entries {
		if item.Title == "weggegooid" {
			t.Fatal("trashed entry parsed from feed")
		}
	}
}

func TestFeedAutodiscoveryLinks(t *testing.T) {
	srv, _ := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	want := `<link rel="alternate" type="application/atom+xml" title="The vanDyke Corrections Ledger" href="/feed.xml">`
	for _, path := range []string{"/", "/stats", "/e/1"} {
		status, _, body := fetch(t, srv, path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d", path, status)
		}
		if !strings.Contains(body, want) {
			t.Errorf("%s head is missing the Atom autodiscovery link", path)
		}
	}
}

func TestFeedEmptyLedgerIsValidAtom(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	page, err := st.ListEntries(t.Context(), store.ListParams{PerPage: 100})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	for _, entry := range page.Entries {
		// Purge only removes trashed rows, so trash each entry first.
		if _, err := st.SoftDeleteEntries(t.Context(), []int64{entry.ID}); err != nil {
			t.Fatalf("SoftDeleteEntries: %v", err)
		}
		if _, err := st.PurgeEntries(t.Context(), []int64{entry.ID}); err != nil {
			t.Fatalf("PurgeEntries: %v", err)
		}
	}

	status, _, body := fetch(t, srv, "/feed.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("empty feed status = %d", status)
	}
	feed := parseFeed(t, body)
	if len(feed.Entries) != 0 {
		t.Fatalf("empty ledger feed has %d entries", len(feed.Entries))
	}
	if feed.ID != srv.URL+"/feed.xml" || feed.Updated == "" {
		t.Fatalf("empty feed header = %+v", feed)
	}
}
