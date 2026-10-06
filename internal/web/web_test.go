package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestWeb(t *testing.T, umami config.Umami, limiter *ratelimit.Limiter) *httptest.Server {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	w, err := New(st, limiter, umami, false, discardLogger())
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	mux := http.NewServeMux()
	w.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
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
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index page does not contain %q", want)
		}
	}
	if !strings.Contains(body, ">100<") {
		t.Errorf("index page does not show the seeded entry count of 100")
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
		t.Fatal("pager not rendered for the seeded 100 entries")
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
