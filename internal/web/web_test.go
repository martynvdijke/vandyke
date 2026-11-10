package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/searchquery"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestWeb(t *testing.T, umami config.Umami, limiter *ratelimit.Limiter) *httptest.Server {
	t.Helper()
	srv, _ := newTestWebWithStore(t, umami, limiter)
	return srv
}

func newTestWebWithStore(t *testing.T, umami config.Umami, limiter *ratelimit.Limiter) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	w, err := New(st, limiter, func() config.Umami { return umami }, func() config.Webhook { return config.Webhook{} }, false, "", discardLogger())
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	mux := http.NewServeMux()
	w.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func fetch(t *testing.T, srv *httptest.Server, path string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header, string(body)
}

func postForm(t *testing.T, srv *httptest.Server, form url.Values, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/entries", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /entries: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header, string(body)
}

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatalf("decode tiny png: %v", err)
	}
	return data
}

func postMultipart(t *testing.T, srv *httptest.Server, fields map[string]string, photo []byte, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := mw.WriteField(key, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	if photo != nil {
		part, err := mw.CreateFormFile("photo", "slip.png")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(photo); err != nil {
			t.Fatalf("write photo: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/entries", &body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /entries multipart: %v", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header, string(responseBody)
}

func TestIndexRendersLedgerAndStats(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, _, body := fetch(t, srv, "/", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	for _, want := range []string{
		`<section id="ledger"`,
		"kast systeem",
		"kassasysteem",
		"entries in the ledger",
		`hx-get="/entries"`,
		`id="add-dialog"`,
		`id="info-dialog"`,
		`href="/admin/login"`,
		`href="/stats"`,
		"The ledger in numbers",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index page does not contain %q", want)
		}
	}
	if !strings.Contains(body, ">78<") {
		t.Errorf("index page does not show the seeded entry count of 78")
	}
}

func TestStatsPage(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, _, body := fetch(t, srv, "/stats", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	for _, want := range []string{
		"The ledger in numbers",
		"slips in the ledger",
		"unique misspellings",
		"carry a photograph",
		"Slips by year",
		"Most frequent slips",
		"From the first slip to the last",
		`href="/stats"`,
		"2017",
		"2026",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stats page does not contain %q", want)
		}
	}
	if !strings.Contains(body, ">78<") {
		t.Error("stats page does not show the seeded entry count of 78")
	}
	if strings.Contains(body, "The ledger is still blank") {
		t.Error("stats page shows the empty state for a seeded ledger")
	}
}

func TestStatsPageEmptyLedger(t *testing.T) {
	d := buildStatsPage(store.Stats{}, config.Umami{}, time.Date(2026, time.November, 8, 0, 0, 0, 0, time.UTC))
	if !d.Empty {
		t.Fatal("empty store.Stats should produce the empty state")
	}
	if len(d.Years) != 0 || len(d.Persons) != 0 {
		t.Fatalf("empty ledger grew bars: %d years, %d persons", len(d.Years), len(d.Persons))
	}
	if d.PhotoPct != 0 || d.SpanText != "" {
		t.Fatalf("empty ledger has derived figures: pct=%d span=%q", d.PhotoPct, d.SpanText)
	}
}

func TestStatsYearBarsFillGaps(t *testing.T) {
	bars := buildYearBars([]store.YearCount{{Year: 2019, Count: 2}, {Year: 2021, Count: 1}}, 2026)
	if len(bars) != 3 {
		t.Fatalf("bars = %d, want a bar for 2019, 2020 and 2021", len(bars))
	}
	if bars[1].Year != 2020 || bars[1].Count != 0 || bars[1].Height != 0 {
		t.Fatalf("gap year = %+v, want an empty 2020 bar", bars[1])
	}
	if bars[0].Height != 100 || bars[2].Height != 50 {
		t.Fatalf("heights = %d/%d, want 100/50", bars[0].Height, bars[2].Height)
	}
}

func TestHumanSpan(t *testing.T) {
	first := time.Date(2017, time.October, 2, 0, 0, 0, 0, time.UTC)
	last := time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)
	if got := humanSpan(first, last); got != "9 years and 1 month" {
		t.Fatalf("humanSpan = %q", got)
	}
}

func TestLedgerPartial(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, _, body := fetch(t, srv, "/entries", map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), `<section id="ledger"`) {
		t.Fatalf("partial does not start with the ledger section: %.120s", body)
	}
	if strings.Contains(body, "<!doctype html>") {
		t.Fatal("partial contains a full HTML document")
	}
	if strings.Contains(body, `id="search-form"`) {
		t.Fatal("partial should not re-render the toolbar")
	}
}

func TestLedgerSearchAndEmptyState(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	_, _, body := fetch(t, srv, "/entries?q=kassasysteem", nil)
	if !strings.Contains(body, "kast systeem") {
		t.Fatal("search result does not contain the matching entry")
	}

	_, _, empty := fetch(t, srv, "/entries?q=zzz-nothing-matches", nil)
	if !strings.Contains(empty, "Nothing found") {
		t.Fatal("empty state not rendered")
	}
}

func TestLedgerSorting(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	_, _, body := fetch(t, srv, "/entries?sort=word&order=asc&per_page=100", nil)
	if !strings.Contains(body, "sorted by A to Z") {
		t.Fatal("sort label not rendered")
	}
	first := strings.Index(body, "agonda der laanco")
	last := strings.Index(body, "Marjolijn")
	if first == -1 || last == -1 || first > last {
		t.Fatalf("word ordering looks wrong (agonda at %d, Marjolijn at %d)", first, last)
	}
}

func TestPaginationLinks(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	_, _, body := fetch(t, srv, "/", nil)
	if !strings.Contains(body, `class="pager"`) {
		t.Fatal("pager not rendered for the seeded 78 entries")
	}
	if !strings.Contains(body, "Next →") {
		t.Fatal("next link missing on page one")
	}

	status, _, page2 := fetch(t, srv, "/entries?page=2", nil)
	if status != http.StatusOK {
		t.Fatalf("page two status = %d, want 200", status)
	}
	if !strings.Contains(page2, "aria-current=\"page\"") {
		t.Fatal("page two is not marked as current")
	}
}

func TestCreateEntryHTMX(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	form := url.Values{"word": {"nieuw woord"}, "meaning": {"nieuwe correctie"}}
	status, headers, body := postForm(t, srv, form, map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	trigger := headers.Get("HX-Trigger-After-Swap")
	if !strings.Contains(trigger, "entryAdded") || !strings.Contains(trigger, "nieuw woord") {
		t.Fatalf("HX-Trigger-After-Swap = %q, want entryAdded with the word", trigger)
	}
	if !strings.Contains(body, "nieuw woord") {
		t.Fatal("re-rendered ledger does not contain the new entry")
	}
	if headers.Get("HX-Retarget") != "" {
		t.Fatalf("unexpected HX-Retarget = %q", headers.Get("HX-Retarget"))
	}

	_, _, search := fetch(t, srv, "/entries?q=nieuw+woord", nil)
	if !strings.Contains(search, "nieuw woord") {
		t.Fatal("new entry is not searchable")
	}
}

func TestCreateEntryHTMXDuplicate(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	form := url.Values{"word": {"dubbel"}, "meaning": {"dubbel"}}
	if status, _, _ := postForm(t, srv, form, map[string]string{"HX-Request": "true"}); status != http.StatusOK {
		t.Fatalf("first status = %d, want 200", status)
	}

	status, headers, body := postForm(t, srv, url.Values{"word": {"DUBBEL"}, "meaning": {"Dubbel"}},
		map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("duplicate status = %d, want 200", status)
	}
	if got := headers.Get("HX-Retarget"); got != "#form-error" {
		t.Fatalf("HX-Retarget = %q, want #form-error", got)
	}
	if !strings.Contains(body, "already in the ledger") {
		t.Fatalf("error body = %q, want duplicate message", body)
	}
}

func TestCreateEntryHTMXValidation(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, headers, body := postForm(t, srv, url.Values{"word": {""}, "meaning": {"x"}}, map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := headers.Get("HX-Retarget"); got != "#form-error" {
		t.Fatalf("HX-Retarget = %q, want #form-error", got)
	}
	if got := headers.Get("HX-Reswap"); got != "innerHTML" {
		t.Fatalf("HX-Reswap = %q, want innerHTML", got)
	}
	if !strings.Contains(body, "Word is required") {
		t.Fatalf("error body = %q, want capitalized validation message", body)
	}
	if strings.Contains(body, "<section") {
		t.Fatal("validation response should only contain the error fragment")
	}
}

func TestCreateEntryWithoutJavaScript(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, headers, _ := postForm(t, srv, url.Values{"word": {"geen js"}, "meaning": {"no javascript"}}, nil)
	if status != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", status)
	}
	if got := headers.Get("Location"); got != "/?added=1" {
		t.Fatalf("Location = %q, want /?added=1", got)
	}

	_, _, body := fetch(t, srv, "/?added=1", nil)
	if !strings.Contains(body, "toast-server") {
		t.Fatal("added toast not rendered on the redirect target")
	}

	status, _, errorPage := postForm(t, srv, url.Values{"word": {""}, "meaning": {"x"}}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("no-js validation status = %d, want 422", status)
	}
	if !strings.Contains(errorPage, "Word is required") || !strings.Contains(errorPage, `id="add-dialog"`) {
		t.Fatal("full error page with the add dialog not rendered")
	}
}

func TestCreateEntryWithPhoto(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, headers, body := postMultipart(t, srv,
		map[string]string{"word": "foto", "meaning": "photo"}, tinyPNG(t),
		map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	trigger := headers.Get("HX-Trigger-After-Swap")
	if !strings.Contains(trigger, `"photo":true`) {
		t.Fatalf("HX-Trigger-After-Swap = %q, want photo flag", trigger)
	}
	if !strings.Contains(body, "entry-photo") || !strings.Contains(body, "/api/v1/entries/") {
		t.Fatal("re-rendered ledger does not show the photo")
	}
}

func TestCreatePhotoOnlyEntry(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, headers, body := postMultipart(t, srv, nil, tinyPNG(t), map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := headers.Get("HX-Retarget"); got != "" {
		t.Fatalf("unexpected HX-Retarget = %q", got)
	}
	if !strings.Contains(body, "photo entry") {
		t.Fatal("photo-only entry is not labelled in the ledger")
	}
}

func TestCreateEntryWithInvalidPhoto(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, headers, body := postMultipart(t, srv,
		map[string]string{"word": "x", "meaning": "y"}, []byte("just text"),
		map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := headers.Get("HX-Retarget"); got != "#form-error" {
		t.Fatalf("HX-Retarget = %q, want #form-error", got)
	}
	if !strings.Contains(body, "Photo must be") {
		t.Fatalf("error body = %q, want photo guidance", body)
	}

	// The form still validates text when no photo is attached.
	status, _, body = postMultipart(t, srv, map[string]string{"word": "x"}, nil,
		map[string]string{"HX-Request": "true"})
	if status != http.StatusOK || !strings.Contains(body, "Meaning is required") {
		t.Fatalf("status = %d body = %q, want missing meaning", status, body)
	}
}

func TestRateLimitedHTMX(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1, 1))

	form := url.Values{"word": {"eerste"}, "meaning": {"first"}}
	status, _, _ := postForm(t, srv, form, map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("first status = %d, want 200", status)
	}

	status, headers, body := postForm(t, srv, url.Values{"word": {"tweede"}, "meaning": {"second"}}, map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("second status = %d, want 200 (htmx error swap)", status)
	}
	if headers.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	if got := headers.Get("HX-Retarget"); got != "#form-error" {
		t.Fatalf("HX-Retarget = %q, want #form-error", got)
	}
	if !strings.Contains(body, "Too many entries") {
		t.Fatalf("body = %q, want rate limit message", body)
	}
}

func TestUmamiScriptInjection(t *testing.T) {
	without := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))
	_, _, body := fetch(t, without, "/", nil)
	if strings.Contains(body, "umami") {
		t.Fatal("umami script rendered without configuration")
	}

	with := newTestWeb(t, config.Umami{
		URL:       "https://umami.example.com/script.js",
		WebsiteID: "website-123",
		Domains:   "vandyke.thor.edu",
	}, ratelimit.New(1000, 100))
	_, _, body = fetch(t, with, "/", nil)
	for _, want := range []string{
		`src="https://umami.example.com/script.js"`,
		`data-website-id="website-123"`,
		`data-domains="vandyke.thor.edu"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("umami snippet missing %q", want)
		}
	}
}

func TestStaticAssetsAreCacheable(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, headers, body := fetch(t, srv, "/static/css/app.css", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := headers.Get("Cache-Control"); !strings.Contains(got, "max-age=86400") {
		t.Fatalf("Cache-Control = %q, want max-age", got)
	}
	if !strings.Contains(body, ":root") {
		t.Fatal("stylesheet body looks wrong")
	}

	status, _, fontCSS := fetch(t, srv, "/static/fonts/fonts.css", nil)
	if status != http.StatusOK || !strings.Contains(fontCSS, "@font-face") {
		t.Fatalf("fonts.css status %d, contains @font-face: %v", status, strings.Contains(fontCSS, "@font-face"))
	}
}

func TestPermalinkPage(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), "perm", "link", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	id := strconv.FormatInt(entry.ID, 10)

	status, _, body := fetch(t, srv, "/e/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	for _, want := range []string{
		`property="og:title"`,
		`property="og:url"`,
		`rel="canonical"`,
		`href="` + srv.URL + `/e/` + id + `"`,
		`data-copy-link="` + srv.URL + `/e/` + id + `"`,
		"Copy link",
		"Back to the ledger",
		"perm",
		"link",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("permalink does not contain %q", want)
		}
	}

	status, _, missing := fetch(t, srv, "/e/999999", nil)
	if status != http.StatusNotFound {
		t.Fatalf("missing permalink status = %d, want 404", status)
	}
	if !strings.Contains(missing, "Entry not found") {
		t.Fatal("styled not-found page not rendered")
	}
	status, _, _ = fetch(t, srv, "/e/not-a-number", nil)
	if status != http.StatusNotFound {
		t.Fatalf("invalid permalink status = %d, want 404", status)
	}
}

func TestPermalinkPhotoOpenGraph(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "foto", "photo", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	id := strconv.FormatInt(entry.ID, 10)

	status, _, body := fetch(t, srv, "/e/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	wantImage := srv.URL + "/api/v1/entries/" + id + "/photo?size=display"
	if !strings.Contains(body, `property="og:image" content="`+wantImage+`"`) {
		t.Fatalf("og:image missing %q in %s", wantImage, body)
	}
	if !strings.Contains(body, `name="twitter:card" content="summary_large_image"`) {
		t.Fatal("twitter large-image card not rendered")
	}
}

func TestLedgerPermalinksAndPhotoSizes(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "foto", "photo", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	id := strconv.FormatInt(entry.ID, 10)

	_, _, body := fetch(t, srv, "/entries?per_page=100", nil)
	if !strings.Contains(body, `href="/e/`+id+`"`) {
		t.Fatal("entry number is not linked to its permalink")
	}
	if !strings.Contains(body, `src="`+entry.PhotoURL+`?size=thumb"`) {
		t.Fatal("ledger thumbnail does not request the thumb variant")
	}
	if !strings.Contains(body, `href="`+entry.PhotoURL+`?size=display"`) {
		t.Fatal("ledger photo link does not request the display variant")
	}
}

func TestLedgerFilters(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "filterfoto", "photo", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	id := strconv.FormatInt(entry.ID, 10)

	_, _, all := fetch(t, srv, "/", nil)
	for _, want := range []string{`name="photo"`, `name="year"`, `value="1"`} {
		if !strings.Contains(all, want) {
			t.Errorf("filter control missing %q", want)
		}
	}

	// The filter assertions read the ledger fragment so the shared footer copy
	// (which mentions "kast systeem") cannot mask a regression.
	status, _, photos := fetch(t, srv, "/entries?photo=1&per_page=100", map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("photo filter status = %d", status)
	}
	if !strings.Contains(photos, "filterfoto") || !strings.Contains(photos, "with photos") {
		t.Fatal("photo filter did not return the photo entry")
	}
	if strings.Contains(photos, "kast systeem") {
		t.Fatal("photo filter returned a text-only entry")
	}
	if !strings.Contains(photos, `href="/e/`+id+`"`) {
		t.Fatal("photo filter lost the permalink")
	}

	_, _, year := fetch(t, srv, "/entries?year=2026&per_page=100", map[string]string{"HX-Request": "true"})
	if !strings.Contains(year, "from 2026") {
		t.Fatal("year filter label not rendered")
	}
	if strings.Contains(year, "kast systeem") {
		t.Fatal("year filter returned an entry from another year")
	}
}

func TestPhotoInputAllowsCameraCapture(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))
	_, _, body := fetch(t, srv, "/", nil)
	if !strings.Contains(body, `capture="environment"`) {
		t.Fatal("photo input is missing mobile camera capture")
	}
}

func TestPermalinkTrashedEntryNotFound(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	entry, err := st.CreateEntry(t.Context(), "verdwenen", "gone", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	path := "/e/" + strconv.FormatInt(entry.ID, 10)
	if status, _, body := fetch(t, srv, path, nil); status != http.StatusOK || !strings.Contains(body, "verdwenen") {
		t.Fatalf("live permalink = %d, want 200 with entry", status)
	}

	if _, err := st.SoftDeleteEntries(t.Context(), []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	status, _, body := fetch(t, srv, path, nil)
	if status != http.StatusNotFound {
		t.Fatalf("trashed permalink status = %d, want 404", status)
	}
	if !strings.Contains(body, "Entry not found") {
		t.Fatalf("trashed permalink did not render the not-found page")
	}
	if strings.Contains(body, "verdwenen") {
		t.Fatal("trashed permalink leaked the entry content")
	}

	status, _, _ = fetch(t, srv, "/entries?q=verdwenen", nil)
	if status != http.StatusOK {
		t.Fatalf("ledger status = %d, want 200", status)
	}
	status, _, ledgerBody := fetch(t, srv, "/", nil)
	if status != http.StatusOK {
		t.Fatalf("index status = %d, want 200", status)
	}
	if strings.Contains(ledgerBody, "verdwenen") {
		t.Fatal("trashed entry still rendered on the index")
	}
}

func newTestWebWithBaseURL(t *testing.T, baseURL string) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "web-base.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w, err := New(st, ratelimit.New(1000, 100), func() config.Umami { return config.Umami{} }, func() config.Webhook { return config.Webhook{} }, false, baseURL, discardLogger())
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	mux := http.NewServeMux()
	w.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func TestLedgerDocumentAndFragment(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))

	status, headers, doc := fetch(t, srv, "/entries?sort=word&order=asc", nil)
	if status != http.StatusOK {
		t.Fatalf("document status = %d, want 200", status)
	}
	if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("document content type = %q", got)
	}
	for _, want := range []string{
		"<html", "<head", `href="/static/css/app.css"`, `class="masthead"`,
		`id="search-form"`, `id="ledger"`, `id="add-dialog"`, `id="info-dialog"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("standalone /entries is missing %q", want)
		}
	}
	if !strings.Contains(doc, `href="/entries?`) {
		t.Error("standalone /entries does not keep absolute sort/pager links")
	}

	status, _, frag := fetch(t, srv, "/entries?sort=word&order=asc", map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("fragment status = %d, want 200", status)
	}
	if strings.Contains(frag, "<html") {
		t.Fatal("HTMX response wrapped the fragment in a document")
	}
	if !strings.HasPrefix(strings.TrimSpace(frag), "<section") {
		t.Fatalf("HTMX response is not the bare ledger section: %.60q", frag)
	}
	if !strings.Contains(frag, `id="ledger"`) {
		t.Error("HTMX response is missing the ledger section")
	}
}

func TestDialogTriggersResolveOnMastheadPages(t *testing.T) {
	srv := newTestWeb(t, config.Umami{}, ratelimit.New(1000, 100))
	pattern := regexp.MustCompile(`data-dialog-open="([^"]+)"`)

	for _, path := range []string{"/", "/entries", "/stats"} {
		status, _, body := fetch(t, srv, path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, status)
		}
		for _, id := range []string{"add-dialog", "info-dialog"} {
			if !strings.Contains(body, `id="`+id+`"`) {
				t.Errorf("%s does not render #%s", path, id)
			}
		}
		matches := pattern.FindAllStringSubmatch(body, -1)
		if len(matches) == 0 {
			t.Fatalf("%s has no dialog triggers", path)
		}
		for _, match := range matches {
			target := strings.TrimPrefix(match[1], "#")
			if target == "" || !strings.Contains(body, `id="`+target+`"`) {
				t.Errorf("%s: dialog trigger %q has no matching dialog", path, match[1])
			}
		}
	}
}

func TestPublicDocumentsExposeSkipLinkMainAndManifest(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), "structure", "landmark", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	mainPattern := regexp.MustCompile(`<main\b`)
	paths := []string{"/", "/entries", "/stats", "/e/" + strconv.FormatInt(entry.ID, 10)}

	for _, path := range paths {
		status, _, body := fetch(t, srv, path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, status)
		}
		if got := len(mainPattern.FindAllString(body, -1)); got != 1 {
			t.Errorf("%s has %d main elements, want 1", path, got)
		}
		if !strings.Contains(body, `id="content"`) {
			t.Errorf("%s main is missing id=content", path)
		}
		skip := strings.Index(body, `class="skip-link"`)
		main := strings.Index(body, "<main")
		if skip < 0 {
			t.Errorf("%s has no skip link", path)
			continue
		}
		if main < 0 || skip > main {
			t.Errorf("%s skip link does not precede main", path)
		}
		if !strings.Contains(body, `rel="manifest" href="/site.webmanifest"`) {
			t.Errorf("%s is missing the manifest link", path)
		}
	}
}

func TestStatsChartsExposeTableAlternatives(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	if _, err := st.CreateEntry(t.Context(), "grafiek", "chart", "Aaa", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	status, _, body := fetch(t, srv, "/stats", nil)
	if status != http.StatusOK {
		t.Fatalf("stats status = %d, want 200", status)
	}
	yearRows := strings.Count(body, `class="year-row"`)
	yearBars := strings.Count(body, `class="bar-col`)
	if yearRows == 0 || yearRows != yearBars {
		t.Errorf("year table rows = %d, bars = %d; want equal and non-zero", yearRows, yearBars)
	}
	personRows := strings.Count(body, `class="person-row"`)
	personBars := strings.Count(body, `class="person-bar"`)
	if personRows == 0 || personRows != personBars {
		t.Errorf("person table rows = %d, bars = %d; want equal and non-zero", personRows, personBars)
	}
	if !regexp.MustCompile(`<ol class="bars[^>]*aria-hidden="true"`).MatchString(body) {
		t.Error("year bar list is not marked aria-hidden")
	}
	if !strings.Contains(body, `<ol class="person-bars" aria-hidden="true">`) {
		t.Error("person bar list is not marked aria-hidden")
	}
	if !strings.Contains(body, "<caption>Entries per year</caption>") || !strings.Contains(body, "<caption>Slips per person</caption>") {
		t.Error("chart tables are missing captions")
	}
}

func TestDiscoverabilityRoutes(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), "sitemap", "entry", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	permalink := "/e/" + strconv.FormatInt(entry.ID, 10)

	status, headers, robots := fetch(t, srv, "/robots.txt", nil)
	if status != http.StatusOK {
		t.Fatalf("robots status = %d", status)
	}
	if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("robots content type = %q", got)
	}
	if !strings.Contains(robots, "Disallow: /admin") {
		t.Error("robots has no /admin disallow")
	}
	if !strings.Contains(robots, "Sitemap: "+srv.URL+"/sitemap.xml") {
		t.Errorf("robots sitemap line is not absolute: %q", robots)
	}

	status, headers, sitemap := fetch(t, srv, "/sitemap.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("sitemap status = %d", status)
	}
	if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "application/xml") {
		t.Fatalf("sitemap content type = %q", got)
	}
	if !strings.HasPrefix(sitemap, "<?xml") {
		t.Fatal("sitemap lacks the XML declaration")
	}
	for _, loc := range []string{srv.URL + "/", srv.URL + "/stats", srv.URL + permalink} {
		if !strings.Contains(sitemap, "<loc>"+loc+"</loc>") {
			t.Errorf("sitemap is missing <loc>%s</loc>", loc)
		}
	}

	status, headers, rawManifest := fetch(t, srv, "/site.webmanifest", nil)
	if status != http.StatusOK {
		t.Fatalf("manifest status = %d", status)
	}
	if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "application/manifest+json") {
		t.Fatalf("manifest content type = %q", got)
	}
	var manifest struct {
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		StartURL        string `json:"start_url"`
		Display         string `json:"display"`
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
		Icons           []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
			Type  string `json:"type"`
		} `json:"icons"`
	}
	if err := json.Unmarshal([]byte(rawManifest), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Name == "" || manifest.ShortName == "" || manifest.StartURL != "/" || manifest.Display == "" {
		t.Fatalf("manifest core fields incomplete: %+v", manifest)
	}
	if manifest.ThemeColor != "#f4eee1" || manifest.BackgroundColor != "#f4eee1" {
		t.Fatalf("manifest colours = %q/%q, want #f4eee1", manifest.ThemeColor, manifest.BackgroundColor)
	}
	if len(manifest.Icons) != 2 {
		t.Fatalf("manifest icons = %d, want 2", len(manifest.Icons))
	}
	for _, icon := range manifest.Icons {
		status, headers, _ := fetch(t, srv, icon.Src, nil)
		if status != http.StatusOK {
			t.Errorf("icon %s status = %d", icon.Src, status)
		}
		if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "image/") {
			t.Errorf("icon %s content type = %q", icon.Src, got)
		}
	}

	// A deleted (soft-deleted) entry leaves the sitemap.
	if _, err := st.SoftDeleteEntries(t.Context(), []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	_, _, sitemap = fetch(t, srv, "/sitemap.xml", nil)
	if strings.Contains(sitemap, srv.URL+permalink) {
		t.Error("sitemap still lists a deleted entry")
	}
}

func TestDiscoverabilityRoutesCarrySecurityHeaders(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "web-secure.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w, err := New(st, ratelimit.New(1000, 100), func() config.Umami { return config.Umami{} }, func() config.Webhook { return config.Webhook{} }, false, "", discardLogger())
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	mux := http.NewServeMux()
	w.Register(mux)
	srv := httptest.NewServer(httpx.SecurityHeadersFunc(func(*http.Request) string {
		return "default-src 'self'"
	})(mux))
	t.Cleanup(srv.Close)

	for _, path := range []string{"/robots.txt", "/sitemap.xml", "/site.webmanifest"} {
		_, headers, _ := fetch(t, srv, path, nil)
		if got := headers.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s X-Frame-Options = %q, want DENY", path, got)
		}
		if got := headers.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
			t.Errorf("%s CSP = %q", path, got)
		}
	}
}

func TestDiscoverabilityHonoursPublicBaseURL(t *testing.T) {
	srv, _ := newTestWebWithBaseURL(t, "https://vandyke.example.com")

	_, _, robots := fetch(t, srv, "/robots.txt", nil)
	if !strings.Contains(robots, "Sitemap: https://vandyke.example.com/sitemap.xml") {
		t.Errorf("robots ignores PUBLIC_BASE_URL: %q", robots)
	}
	_, _, sitemap := fetch(t, srv, "/sitemap.xml", nil)
	for _, loc := range []string{"https://vandyke.example.com/", "https://vandyke.example.com/stats"} {
		if !strings.Contains(sitemap, "<loc>"+loc+"</loc>") {
			t.Errorf("sitemap ignores PUBLIC_BASE_URL, missing %q", loc)
		}
	}
}

func TestLedgerHighlightsIncludedTermsOnly(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	if _, err := st.CreateEntry(t.Context(), "highlightprobe", "the highlightprobe correction", "Alice", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	hx := map[string]string{"HX-Request": "true"}

	_, _, body := fetch(t, srv, "/entries?q=highlightprobe&per_page=100", hx)
	if got := strings.Count(body, "<mark>highlightprobe</mark>"); got != 2 {
		t.Fatalf("marked occurrences = %d, want 2 in %s", got, body)
	}

	// An excluded term is parsed out of Include, so the renderer cannot mark it.
	parsed := searchquery.Parse("feature -feutenreus")
	rendered := string(markMatches("feature and feutenreus together", parsed.Include))
	if strings.Contains(rendered, "<mark>feutenreus</mark>") {
		t.Fatalf("excluded term was marked: %s", rendered)
	}
	if !strings.Contains(rendered, "<mark>feature</mark>") {
		t.Fatalf("included term lost its mark: %s", rendered)
	}

	_, _, off := fetch(t, srv, "/entries?q=highlightprobe&highlight=0&per_page=100", hx)
	if strings.Contains(off, "<mark>") {
		t.Fatalf("highlight=0 still rendered marks: %s", off)
	}
	if !strings.Contains(off, "highlightprobe") {
		t.Fatal("highlight=0 dropped the entry text")
	}
	if !strings.Contains(off, "highlight=0") {
		t.Fatal("highlight=0 was not persisted in generated links")
	}
}

func TestLedgerHighlightEscapesQueryMarkup(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	if _, err := st.CreateEntry(t.Context(), "alert<script>probe", "a slip with markup", "", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	_, _, body := fetch(t, srv, "/entries?q="+url.QueryEscape(`"<script>"`)+"&per_page=100",
		map[string]string{"HX-Request": "true"})
	if strings.Contains(body, "<script") {
		t.Fatalf("query markup became a script element: %s", body)
	}
	if !strings.Contains(body, "<mark>&lt;script&gt;</mark>") {
		t.Fatalf("matched markup was not escaped inside the mark: %s", body)
	}
}

func TestLedgerDateRangeFilter(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	entry, err := st.CreateEntry(t.Context(), "datumprobe", "a dated slip", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	created, err := time.Parse("2006-01-02", "1992-03-04")
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	if _, err := st.UpdateEntry(t.Context(), store.EntryUpdate{
		ID: entry.ID, Word: "datumprobe", Meaning: "a dated slip", CreatedAt: created,
	}); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}

	hx := map[string]string{"HX-Request": "true"}
	_, _, body := fetch(t, srv, "/entries?from=1992-03-01&to=1992-03-31&per_page=100", hx)
	if !strings.Contains(body, "datumprobe") {
		t.Fatal("in-range entry missing")
	}
	if !strings.Contains(body, "from 1992-03-01") || !strings.Contains(body, "to 1992-03-31") {
		t.Fatalf("active range not shown in the meta line: %s", body)
	}

	_, _, outside := fetch(t, srv, "/entries?from=1992-04-01&to=1992-04-30&per_page=100", hx)
	if strings.Contains(outside, "datumprobe") {
		t.Fatal("out-of-range entry returned")
	}

	// A malformed bound is ignored rather than failing the page.
	if status, _, _ := fetch(t, srv, "/entries?from=not-a-date", hx); status != http.StatusOK {
		t.Fatalf("malformed web date status = %d, want 200", status)
	}
}

func TestWebhookDispatchDoesNotBlockCreate(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "web-webhook.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	started := make(chan struct{})
	release := make(chan struct{})
	bodies := make(chan []byte, 1)
	var once sync.Once
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
		once.Do(func() { close(started) })
		<-release
		rw.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()
	defer close(release)

	w, err := New(st, ratelimit.New(1000, 100),
		func() config.Umami { return config.Umami{} },
		func() config.Webhook { return config.Webhook{URL: receiver.URL, Secret: "s3cret"} },
		false, "", discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mux := http.NewServeMux()
	w.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	type result struct {
		status int
		err    error
	}
	done := make(chan result, 1)
	go func() {
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.PostForm(srv.URL+"/entries", url.Values{
			"word": {"webhookprobe"}, "meaning": {"payload probe"}, "person": {"Alice"},
		})
		if err != nil {
			done <- result{err: err}
			return
		}
		_ = resp.Body.Close()
		done <- result{status: resp.StatusCode}
	}()

	// The receiver is still blocked, so a synchronous dispatch would hang here.
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("create: %v", got.err)
		}
		if got.status != http.StatusSeeOther {
			t.Fatalf("create status = %d, want 303", got.status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("entry creation blocked on the webhook receiver")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook receiver was never contacted")
	}

	select {
	case body := <-bodies:
		var payload webhook.Payload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.Event != "entry.created" || payload.Word != "webhookprobe" || payload.Person != "Alice" {
			t.Fatalf("payload = %+v", payload)
		}
		if payload.ID < 1 || !strings.HasSuffix(payload.URL, "/e/"+strconv.FormatInt(payload.ID, 10)) {
			t.Fatalf("payload url = %q for id %d", payload.URL, payload.ID)
		}
		if !strings.HasPrefix(payload.URL, srv.URL) {
			t.Fatalf("payload url = %q, want the request host %q", payload.URL, srv.URL)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no payload received")
	}
}

type capturedLog struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *capturedLog) Enabled(context.Context, slog.Level) bool { return true }

func (c *capturedLog) Handle(_ context.Context, record slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, record.Clone())
	return nil
}

func (c *capturedLog) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capturedLog) WithGroup(string) slog.Handler      { return c }

func (c *capturedLog) warnings() []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []slog.Record
	for _, record := range c.records {
		if record.Level >= slog.LevelWarn {
			out = append(out, record)
		}
	}
	return out
}

func TestWebhookFailureDoesNotAffectCreate(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "web-webhook-fail.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer receiver.Close()

	logs := &capturedLog{}
	w, err := New(st, ratelimit.New(1000, 100),
		func() config.Umami { return config.Umami{} },
		func() config.Webhook { return config.Webhook{URL: receiver.URL} },
		false, "", slog.New(logs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mux := http.NewServeMux()
	w.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(srv.URL+"/entries", url.Values{
		"word": {"mislukt"}, "meaning": {"failed delivery"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d, want 303", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(logs.warnings()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("webhook failure was not logged at warn level")
}
