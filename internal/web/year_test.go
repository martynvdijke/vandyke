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

func TestYearReviewPage(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	ctx := context.Background()

	first := makeEntry(t, st, "kast", "kas", "Anna", "2031-01-05")
	makeEntry(t, st, "feature", "euten", "Bob", "2031-03-02")
	makeEntry(t, st, "boeke", "boeken", "Anna", "2031-03-19")
	longest := makeEntry(t, st, "langsteverschrijving", "longest", "Bob", "2031-07-04")
	makeEntry(t, st, "kort", "short", "Bob", "2031-07-20")

	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	last, err := st.CreateEntry(ctx, "", "", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry photo: %v", err)
	}
	_, last, err = st.UpdateEntry(ctx, store.EntryUpdate{
		ID: last.ID, CreatedAt: time.Date(2031, 12, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("UpdateEntry photo: %v", err)
	}

	status, headers, body := fetch(t, srv, "/y/2031", nil)
	if status != http.StatusOK {
		t.Fatalf("year status = %d", status)
	}
	if headers.Get("ETag") == "" {
		t.Fatal("year page has no ETag")
	}
	if got := headers.Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("cache control = %q", got)
	}
	for _, want := range []string{
		"2031 in review",
		">6<",
		"March",
		"Anna",
		"Bob",
		">" + longest.Word + "<",
		">17<",
		`href="/entries?year=2031"`,
		`href="/e/` + strconv.FormatInt(first.ID, 10) + `"`,
		`href="/e/` + strconv.FormatInt(last.ID, 10) + `"`,
		`<link rel="canonical" href="` + srv.URL + `/y/2031">`,
		`<meta property="og:url" content="` + srv.URL + `/y/2031">`,
		`<meta name="twitter:card" content="summary">`,
		"Entries per month in 2031",
		"Slips per person in 2031",
		`class="visually-hidden"`,
		`aria-hidden="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("year page missing %q", want)
		}
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Fatal("year page could not render a value")
	}

	// The recap is deterministic: the same request yields the same ETag.
	_, repeatHeaders, repeatBody := fetch(t, srv, "/y/2031", nil)
	if repeatHeaders.Get("ETag") != headers.Get("ETag") {
		t.Fatal("repeat request changed the ETag")
	}
	if repeatBody != body {
		t.Fatal("repeat request changed the body")
	}
}

func TestYearReviewMissingAndMalformed(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	ctx := context.Background()

	entry := makeEntry(t, st, "vergeten", "gone", "", "2033-02-02")
	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	for _, path := range []string{"/y/1999", "/y/2033", "/y/24", "/y/abcd", "/y/9999"} {
		status, headers, body := fetch(t, srv, path, nil)
		if status != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, status)
		}
		if headers.Get("ETag") != "" {
			t.Errorf("%s 404 carries an ETag", path)
		}
		if !strings.Contains(body, "Year not found") {
			t.Errorf("%s did not render the not-found page: %.200s", path, body)
		}
	}
}

func TestYearReviewConditionalAndEditInvalidation(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	ctx := context.Background()

	entry := makeEntry(t, st, "eerst", "first", "", "2034-06-01")
	_, headers, _ := fetch(t, srv, "/y/2034", nil)
	etag := headers.Get("ETag")
	if etag == "" {
		t.Fatal("year page has no ETag")
	}

	status, conditionalHeaders, body := fetch(t, srv, "/y/2034", map[string]string{"If-None-Match": etag})
	if status != http.StatusNotModified {
		t.Fatalf("conditional year status = %d, want 304", status)
	}
	if body != "" {
		t.Fatalf("304 returned %d body bytes", len(body))
	}
	if conditionalHeaders.Get("ETag") != etag {
		t.Fatal("304 lost the ETag")
	}

	if _, _, err := st.UpdateEntry(ctx, store.EntryUpdate{
		ID: entry.ID, Word: "gewijzigd", Meaning: "changed", CreatedAt: entry.CreatedAt,
	}); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	status, changedHeaders, changedBody := fetch(t, srv, "/y/2034", map[string]string{"If-None-Match": etag})
	if status != http.StatusOK {
		t.Fatalf("edited year status = %d, want 200", status)
	}
	if changedHeaders.Get("ETag") == etag {
		t.Fatal("editing an entry did not change the year ETag")
	}
	if !strings.Contains(changedBody, "gewijzigd") {
		t.Fatal("edited year did not show the new word")
	}
}

func TestStatsLinksOnlyYearsWithEntries(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	makeEntry(t, st, "toekomstig", "future", "", "2035-04-04")

	status, _, body := fetch(t, srv, "/stats", nil)
	if status != http.StatusOK {
		t.Fatalf("stats status = %d", status)
	}
	if !strings.Contains(body, `aria-label="Year in review"`) {
		t.Fatal("stats page has no year-in-review navigation")
	}
	if !strings.Contains(body, `href="/y/2035"`) {
		t.Fatal("stats page does not link a year with entries")
	}
	if strings.Contains(body, `href="/y/2030"`) {
		t.Fatal("stats page linked a zero-count gap year")
	}

	// The gap bars themselves carry no link.
	bars := buildYearBars([]store.YearCount{{Year: 2019, Count: 2}, {Year: 2021, Count: 1}}, 2026)
	if len(bars) != 3 {
		t.Fatalf("bars = %d", len(bars))
	}
	if bars[0].Href != "/y/2019" || bars[2].Href != "/y/2021" {
		t.Fatalf("entry-year hrefs = %q/%q", bars[0].Href, bars[2].Href)
	}
	if bars[1].Href != "" {
		t.Fatalf("gap bar has href %q, want none", bars[1].Href)
	}
}
