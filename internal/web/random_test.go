package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
)

func fetchNoRedirect(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(body)
}

func TestRandomRedirectsToLivePermalinks(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	live := makeEntry(t, st, "levend", "live", "", "2024-01-01")
	photo := makePhotoEntry(t, st, "foto", "photo", "Marieke", "2024-02-01")
	trashed := makeEntry(t, st, "weg", "trashed", "", "2024-03-01")
	if _, err := st.SoftDeleteEntries(context.Background(), []int64{trashed.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	allowed := map[string]bool{
		"/e/" + strconv.FormatInt(live.ID, 10):  true,
		"/e/" + strconv.FormatInt(photo.ID, 10): true,
	}
	for i := 0; i < 20; i++ {
		resp, body := fetchNoRedirect(t, srv, "/random")
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", resp.StatusCode)
		}
		location := resp.Header.Get("Location")
		if !allowed[location] {
			t.Fatalf("Location = %q, want one of %v", location, allowed)
		}
		if location == "/e/"+strconv.FormatInt(trashed.ID, 10) {
			t.Fatal("random redirect pointed at a trashed entry")
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
		if got := resp.Header.Get("X-Robots-Tag"); got != "noindex" {
			t.Errorf("X-Robots-Tag = %q", got)
		}
		if body != "" && !strings.Contains(body, location) {
			t.Errorf("303 body does not point at %q: %q", location, body)
		}
	}

	// The photo filter narrows the draw to the only photographed entry.
	for i := 0; i < 10; i++ {
		resp, _ := fetchNoRedirect(t, srv, "/random?photo=1")
		if got := resp.Header.Get("Location"); got != "/e/"+strconv.FormatInt(photo.ID, 10) {
			t.Fatalf("photo draw = %q, want the photographed entry", got)
		}
	}

	// The person filter matches case-insensitively.
	for i := 0; i < 10; i++ {
		resp, _ := fetchNoRedirect(t, srv, "/random?person=marieke")
		if got := resp.Header.Get("Location"); got != "/e/"+strconv.FormatInt(photo.ID, 10) {
			t.Fatalf("person draw = %q, want Marieke's entry", got)
		}
	}

	resp, body := fetchNoRedirect(t, srv, "/random?person=Nobody")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unmatched draw status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(body, "No slip to draw") {
		t.Error("unmatched draw did not render the not-found page")
	}

	// /e/random stays inside the numeric permalink namespace.
	status, _, _ := fetch(t, srv, "/e/random", nil)
	if status != http.StatusNotFound {
		t.Errorf("/e/random status = %d, want 404", status)
	}
}

func TestRandomOnEmptyLedger(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	resp, body := fetchNoRedirect(t, srv, "/random")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(body, "No slip to draw") {
		t.Error("empty ledger did not render the not-found page")
	}
	if !strings.Contains(body, `href="/"`) {
		// The shared not-found page still offers a way back.
		t.Error("not-found page has no way back")
	}
}

func TestBrowseNavigationAndSitemap(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)
	entry := makeEntry(t, st, "kast", "kassasysteem", "Marieke", "2024-01-01")

	paths := map[string]int{
		"/":                                     2, // shared masthead and footer
		"/stats":                                2,
		"/words":                                2,
		"/photos":                               2,
		"/e/" + strconv.FormatInt(entry.ID, 10): 1, // the standalone permalink header
	}
	for path, want := range paths {
		status, _, body := fetch(t, srv, path, nil)
		if status != http.StatusOK {
			t.Errorf("%s status = %d", path, status)
			continue
		}
		if got := strings.Count(body, `href="/words"`); got < want {
			t.Errorf("%s has %d /words links, want at least %d", path, got, want)
		}
		if got := strings.Count(body, `href="/photos"`); got < want {
			t.Errorf("%s has %d /photos links, want at least %d", path, got, want)
		}
	}

	status, _, sitemap := fetch(t, srv, "/sitemap.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("sitemap status = %d", status)
	}
	for _, loc := range []string{srv.URL + "/words", srv.URL + "/photos", srv.URL + "/stats"} {
		if !strings.Contains(sitemap, "<loc>"+loc+"</loc>") {
			t.Errorf("sitemap is missing %q", loc)
		}
	}
	for _, absent := range []string{srv.URL + "/random", srv.URL + "/p/"} {
		if strings.Contains(sitemap, "<loc>"+absent) {
			t.Errorf("sitemap advertises %q", absent)
		}
	}
}
