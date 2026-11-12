package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

// makeEntry creates an entry and moves it to a fixed UTC date.
func makeEntry(t *testing.T, st *store.Store, word, meaning, person, day string) store.Entry {
	t.Helper()
	ctx := context.Background()
	entry, err := st.CreateEntry(ctx, word, meaning, person, nil)
	if err != nil {
		t.Fatalf("CreateEntry %q: %v", word, err)
	}
	if day == "" {
		return entry
	}
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatalf("parse %q: %v", day, err)
	}
	_, updated, err := st.UpdateEntry(ctx, store.EntryUpdate{
		ID: entry.ID, Word: word, Meaning: meaning, Person: person, CreatedAt: parsed.UTC(),
	})
	if err != nil {
		t.Fatalf("UpdateEntry %q: %v", word, err)
	}
	return updated
}

func tagIDFor(t *testing.T, st *store.Store, slug string) int64 {
	t.Helper()
	tag, err := st.GetTagBySlug(context.Background(), slug)
	if err != nil {
		t.Fatalf("GetTagBySlug %q: %v", slug, err)
	}
	return tag.ID
}

func TestTagIndexShowsLiveCounts(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	ctx := context.Background()

	live := makeEntry(t, st, "levend", "live", "", "2024-01-02")
	trashed := makeEntry(t, st, "verdwenen", "trashed", "", "2024-01-03")
	typo := tagIDFor(t, st, "typo")
	if err := st.SetEntryTags(ctx, live.ID, []int64{typo}); err != nil {
		t.Fatalf("SetEntryTags live: %v", err)
	}
	if err := st.SetEntryTags(ctx, trashed.ID, []int64{typo}); err != nil {
		t.Fatalf("SetEntryTags trashed: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{trashed.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	status, _, body := fetch(t, srv, "/tags", nil)
	if status != http.StatusOK {
		t.Fatalf("tags index status = %d", status)
	}
	for _, want := range []string{
		`href="/tags/typo"`,
		`href="/tags/dialect"`,
		`<span class="tag-index-count">1 entry</span>`,
		`<span class="tag-index-count">0 entries</span>`,
		`<link rel="canonical" href="` + srv.URL + `/tags">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tags index missing %q", want)
		}
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Fatal("tag index could not render a colour")
	}
}

func TestTagPageRendersFilteredLedgerAndCanonical(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	ctx := context.Background()

	entry := makeEntry(t, st, "getagd", "tagged", "", "2024-02-03")
	trashed := makeEntry(t, st, "alleen-weg", "trashed", "", "2024-02-04")
	if err := st.SetEntryTags(ctx, entry.ID, []int64{tagIDFor(t, st, "misheard")}); err != nil {
		t.Fatalf("SetEntryTags: %v", err)
	}
	if err := st.SetEntryTags(ctx, trashed.ID, []int64{tagIDFor(t, st, "dialect")}); err != nil {
		t.Fatalf("SetEntryTags trashed: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{trashed.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	status, _, body := fetch(t, srv, "/tags/misheard", nil)
	if status != http.StatusOK {
		t.Fatalf("tag page status = %d", status)
	}
	for _, want := range []string{
		"getagd",
		`href="/entries?tag=misheard"`,
		`<link rel="canonical" href="` + srv.URL + `/tags/misheard">`,
		"1 entry carry this tag",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tag page missing %q", want)
		}
	}
	if strings.Contains(body, `content="noindex,follow"`) {
		t.Error("canonical tag page must stay indexable")
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Fatal("tag page could not render a colour")
	}

	// A tag whose only entry is trashed stays a 200 with the empty state.
	status, _, body = fetch(t, srv, "/tags/dialect", nil)
	if status != http.StatusOK {
		t.Fatalf("trashed tag page status = %d, want 200", status)
	}
	if !strings.Contains(body, "No live entry carries this tag right now.") ||
		!strings.Contains(body, "Nothing found") {
		t.Fatalf("trashed tag page lost the empty state: %.500s", body)
	}

	status, _, body = fetch(t, srv, "/tags/no-such-tag", nil)
	if status != http.StatusNotFound || !strings.Contains(body, "Tag not found") {
		t.Fatalf("unknown tag = %d, want 404: %.300s", status, body)
	}
}

func TestLedgerTagFilterChipsAndPersistence(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	ctx := context.Background()

	tagged := makeEntry(t, st, "getagd", "tagged", "", "2024-03-01")
	untagged := makeEntry(t, st, "losse", "untagged", "", "2024-03-02")
	byName := makeEntry(t, st, "naamval", "name", "", "2023-04-01")
	if err := st.SetEntryTags(ctx, tagged.ID, []int64{tagIDFor(t, st, "typo")}); err != nil {
		t.Fatalf("SetEntryTags tagged: %v", err)
	}
	if err := st.SetEntryTags(ctx, byName.ID, []int64{tagIDFor(t, st, "name")}); err != nil {
		t.Fatalf("SetEntryTags name: %v", err)
	}

	status, _, body := fetch(t, srv, "/entries?tag=typo&per_page=100", nil)
	if status != http.StatusOK {
		t.Fatalf("tag filter status = %d", status)
	}
	if !strings.Contains(body, "getagd") {
		t.Fatal("tag filter lost the tagged entry")
	}
	if strings.Contains(body, "losse") || strings.Contains(body, "naamval") {
		t.Fatal("tag filter returned an entry with another tag")
	}
	if !strings.Contains(body, `<input type="hidden" name="tag" value="typo">`) {
		t.Fatal("toolbar does not carry the tag as a hidden field")
	}
	if !strings.Contains(body, `href="/entries?`) || !strings.Contains(body, "tag=typo") {
		t.Fatal("sort or pager link dropped the tag")
	}
	if !strings.Contains(body, `href="/entries?tag=typo"`) {
		t.Fatal("ledger row has no tag chip linking to the filter")
	}

	// The HTMX fragment path keeps the filter too.
	status, _, fragment := fetch(t, srv, "/entries?tag=typo&per_page=100", map[string]string{"HX-Request": "true"})
	if status != http.StatusOK || !strings.Contains(fragment, "getagd") || strings.Contains(fragment, "losse") {
		t.Fatalf("HTMX tag fragment = %d: %.300s", status, fragment)
	}

	// The home page's shared toolbar carries the tag when the filter is in the
	// home URL, so a search submitted from there keeps it.
	status, _, home := fetch(t, srv, "/?tag=typo", nil)
	if status != http.StatusOK || !strings.Contains(home, `<input type="hidden" name="tag" value="typo">`) ||
		!strings.Contains(home, "getagd") {
		t.Fatalf("home toolbar lost the tag (%d)", status)
	}

	// Tag intersects with q and year.
	_, _, combined := fetch(t, srv, "/entries?tag=typo&q=getagd&year=2024&per_page=100", nil)
	if !strings.Contains(combined, "getagd") || strings.Contains(combined, "naamval") {
		t.Fatal("tag did not intersect with q and year")
	}
	status, _, empty := fetch(t, srv, "/entries?tag=typo&year=2023", nil)
	if status != http.StatusOK || strings.Contains(empty, "getagd") {
		t.Fatalf("tag plus wrong year = %d", status)
	}
	status, _, unknown := fetch(t, srv, "/entries?tag=does-not-exist", nil)
	if status != http.StatusOK || strings.Contains(unknown, "getagd") {
		t.Fatalf("unknown tag filter = %d", status)
	}

	// The permalink shows the same chips.
	status, _, permalink := fetch(t, srv, "/e/"+strconv.FormatInt(tagged.ID, 10), nil)
	if status != http.StatusOK || !strings.Contains(permalink, `href="/entries?tag=typo"`) {
		t.Fatalf("permalink has no tag chip (%d)", status)
	}

	// Trashing the entry removes it from the filter, the tag page and the count.
	if _, err := st.SoftDeleteEntries(ctx, []int64{tagged.ID, untagged.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	_, _, filtered := fetch(t, srv, "/entries?tag=typo&per_page=100", nil)
	if strings.Contains(filtered, "getagd") {
		t.Fatal("trashed entry still matches the tag filter")
	}
	_, _, tagPage := fetch(t, srv, "/tags/typo", nil)
	if strings.Contains(tagPage, "getagd") || !strings.Contains(tagPage, "No live entry carries this tag right now.") {
		t.Fatal("trashed entry still appears on the tag page")
	}
}

func TestCanonicalAndRobotsBehaviour(t *testing.T) {
	srv, _ := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	for _, path := range []string{"/entries", "/entries?tag=typo", "/entries?q=kast&tag=typo"} {
		_, _, body := fetch(t, srv, path, nil)
		if !strings.Contains(body, `<meta name="robots" content="noindex,follow">`) {
			t.Errorf("%s is missing noindex,follow", path)
		}
		if strings.Contains(body, `rel="canonical"`) {
			t.Errorf("%s declares a canonical while noindex", path)
		}
	}

	_, _, tagPage := fetch(t, srv, "/tags/typo", nil)
	if !strings.Contains(tagPage, `<link rel="canonical" href="`+srv.URL+`/tags/typo">`) {
		t.Fatal("tag page has no self canonical")
	}
	if strings.Contains(tagPage, "noindex") {
		t.Fatal("tag page is noindex")
	}

	_, _, index := fetch(t, srv, "/tags", nil)
	if !strings.Contains(index, `<link rel="canonical" href="`+srv.URL+`/tags">`) {
		t.Fatal("tag index has no self canonical")
	}
}

func TestTaggedFeedFilterAndETag(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	ctx := context.Background()

	tagged := makeEntry(t, st, "geautomatiseerd", "autocorrected", "", "2024-05-01")
	makeEntry(t, st, "gewoon", "plain", "", "2024-05-02")
	if err := st.SetEntryTags(ctx, tagged.ID, []int64{tagIDFor(t, st, "autocorrect")}); err != nil {
		t.Fatalf("SetEntryTags: %v", err)
	}

	status, headers, body := fetch(t, srv, "/feed.xml?tag=autocorrect", nil)
	if status != http.StatusOK {
		t.Fatalf("tagged feed status = %d", status)
	}
	feed := parseFeed(t, body)
	if len(feed.Entries) != 1 || feed.Entries[0].Title != "geautomatiseerd" {
		t.Fatalf("tagged feed entries = %+v", feed.Entries)
	}
	self := srv.URL + "/feed.xml?tag=autocorrect"
	if feed.ID != self || feed.Link.Href != self {
		t.Fatalf("tagged feed id/self = %q/%q, want %q", feed.ID, feed.Link.Href, self)
	}
	taggedETag := headers.Get("ETag")
	if taggedETag == "" {
		t.Fatal("tagged feed has no ETag")
	}

	_, plainHeaders, plainBody := fetch(t, srv, "/feed.xml", nil)
	if plainHeaders.Get("ETag") == taggedETag {
		t.Fatal("tagged and untagged feeds share an ETag")
	}
	if !strings.Contains(plainBody, "geautomatiseerd") {
		t.Fatal("untagged feed lost the tagged entry")
	}

	// Two empty tag views still cache separately: the slug is in the tag.
	_, typoHeaders, typoBody := fetch(t, srv, "/feed.xml?tag=typo", nil)
	_, dialectHeaders, _ := fetch(t, srv, "/feed.xml?tag=dialect", nil)
	if typoHeaders.Get("ETag") == dialectHeaders.Get("ETag") {
		t.Fatal("different empty tag feeds share an ETag")
	}
	if feed := parseFeed(t, typoBody); len(feed.Entries) != 0 {
		t.Fatalf("empty tag feed has %d entries", len(feed.Entries))
	}

	status, headers, body = fetch(t, srv, "/feed.xml?tag=autocorrect", map[string]string{"If-None-Match": taggedETag})
	if status != http.StatusNotModified || body != "" {
		t.Fatalf("conditional tagged feed = %d, body %d bytes", status, len(body))
	}
	if headers.Get("ETag") != taggedETag {
		t.Fatal("304 lost the ETag")
	}

	// Trashing the only tagged entry changes the tag view.
	if _, err := st.SoftDeleteEntries(ctx, []int64{tagged.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	_, trashedHeaders, trashedBody := fetch(t, srv, "/feed.xml?tag=autocorrect", nil)
	if trashedHeaders.Get("ETag") == taggedETag {
		t.Fatal("ETag did not change after the tagged entry was trashed")
	}
	if feed := parseFeed(t, trashedBody); len(feed.Entries) != 0 {
		t.Fatalf("trashed entry still in the tagged feed: %+v", feed.Entries)
	}
}

func TestTagSitemapEntries(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	ctx := context.Background()

	live := makeEntry(t, st, "toekomst", "future", "", "2031-01-02")
	dead := makeEntry(t, st, "verleden", "past", "", "2032-01-03")
	if err := st.SetEntryTags(ctx, live.ID, []int64{tagIDFor(t, st, "typo")}); err != nil {
		t.Fatalf("SetEntryTags live: %v", err)
	}
	if err := st.SetEntryTags(ctx, dead.ID, []int64{tagIDFor(t, st, "dialect")}); err != nil {
		t.Fatalf("SetEntryTags dead: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{dead.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	status, _, sitemap := fetch(t, srv, "/sitemap.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("sitemap status = %d", status)
	}
	for _, loc := range []string{srv.URL + "/tags", srv.URL + "/tags/typo", srv.URL + "/y/2031"} {
		if !strings.Contains(sitemap, "<loc>"+loc+"</loc>") {
			t.Errorf("sitemap is missing %s", loc)
		}
	}
	for _, loc := range []string{srv.URL + "/tags/dialect", srv.URL + "/y/2032"} {
		if strings.Contains(sitemap, "<loc>"+loc+"</loc>") {
			t.Errorf("sitemap lists an empty view: %s", loc)
		}
	}
	if strings.Contains(sitemap, "entries?tag=") {
		t.Error("sitemap lists a query-string filter URL")
	}
}
