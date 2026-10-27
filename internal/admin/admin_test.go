package admin

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/settings"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/web"
)

var csrfPattern = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func newAdminTestServer(t *testing.T, cfg config.Config) (*httptest.Server, *http.Client, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	stg, err := settings.New(t.Context(), st, cfg.Umami)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := New(t.Context(), st, stg, cfg, logger)
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	w, err := web.New(st, ratelimit.New(1000, 100), stg.Current, false, logger)
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	mux := http.NewServeMux()
	a.Register(mux)
	w.Register(mux)
	handler := httpx.SecurityHeadersFunc(func(*http.Request) string {
		return "default-src 'self'; script-src 'self' " + stg.Current().Origin()
	})(mux)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return srv, client, st
}

func get(t *testing.T, client *http.Client, u string) (*http.Response, string) {
	t.Helper()
	resp, err := client.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(body)
}

func postForm(t *testing.T, client *http.Client, u string, values url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(u, values)
	if err != nil {
		t.Fatalf("POST %s: %v", u, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp
}

func csrfFrom(t *testing.T, body string) string {
	t.Helper()
	match := csrfPattern.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("csrf token missing from body")
	}
	return match[1]
}

func setupAdmin(t *testing.T, srv *httptest.Server, client *http.Client) string {
	t.Helper()
	resp, body := get(t, client, srv.URL+"/admin/setup")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "Create the first admin") {
		t.Fatalf("setup page missing heading")
	}
	resp = postForm(t, client, srv.URL+"/admin/setup", url.Values{
		"username": {"owner"},
		"password": {"correct horse battery"},
		"confirm":  {"correct horse battery"},
	})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin" {
		t.Fatalf("setup POST = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, body = get(t, client, srv.URL+"/admin")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Ledger entries") {
		t.Fatalf("admin GET = %d", resp.StatusCode)
	}
	return csrfFrom(t, body)
}

func TestSetupSessionEditDeleteAndSettings(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)

	// Public additions remain open.
	resp := postForm(t, client, srv.URL+"/entries", url.Values{
		"word": {"publiek"}, "meaning": {"public"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("public add status = %d", resp.StatusCode)
	}

	resp = postForm(t, client, srv.URL+"/admin/entries/1", url.Values{
		"csrf": {csrf}, "word": {"edited"}, "meaning": {"correction"}, "date": {"2017-10-03"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	entry, err := st.GetEntry(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Word != "edited" || entry.Meaning != "correction" || entry.CreatedAt.Format("2006-01-02") != "2017-10-03" {
		t.Fatalf("entry after update = %+v", entry)
	}

	resp = postForm(t, client, srv.URL+"/admin/entries/2", url.Values{
		"word": {"no csrf"}, "meaning": {"x"},
	})
	// Missing CSRF is rejected before the update.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("csrf rejection status = %d", resp.StatusCode)
	}

	resp = postForm(t, client, srv.URL+"/admin/entries/delete", url.Values{
		"csrf": {csrf}, "ids": {"2", "3"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	if _, err := st.GetEntry(t.Context(), 2); err == nil {
		t.Fatal("entry 2 still exists")
	}

	resp = postForm(t, client, srv.URL+"/admin/settings", url.Values{
		"csrf":             {csrf},
		"umami_url":        {"https://umami.example.com/script.js"},
		"umami_website_id": {"site-id"},
		"umami_domains":    {"vandyke.example.com"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("settings status = %d", resp.StatusCode)
	}
	resp, body := get(t, client, srv.URL+"/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public page status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "umami.example.com/script.js") {
		t.Fatal("tracker not injected after settings save")
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "https://umami.example.com") {
		t.Fatalf("CSP missing Umami origin: %q", csp)
	}

	resp = postForm(t, client, srv.URL+"/admin/logout", url.Values{"csrf": {csrf}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	resp, _ = get(t, client, srv.URL+"/admin")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/login") {
		t.Fatalf("after logout = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestLoginAndSetupToken(t *testing.T) {
	srv, client, _ := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
		AdminSetupToken: "setup-secret",
	})
	resp := postForm(t, client, srv.URL+"/admin/setup", url.Values{
		"username": {"owner"}, "password": {"correct horse battery"}, "confirm": {"correct horse battery"},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("setup without token = %d, want 403", resp.StatusCode)
	}
	resp = postForm(t, client, srv.URL+"/admin/setup", url.Values{
		"token": {"setup-secret"}, "username": {"owner"},
		"password": {"correct horse battery"}, "confirm": {"correct horse battery"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup with token = %d", resp.StatusCode)
	}
	csrf := csrfFrom(t, mustGet(t, client, srv.URL+"/admin"))

	resp = postForm(t, client, srv.URL+"/admin/logout", url.Values{"csrf": {csrf}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	resp = postForm(t, client, srv.URL+"/admin/login", url.Values{
		"username": {"owner"}, "password": {"wrong"},
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong login = %d, want 401", resp.StatusCode)
	}
	resp = postForm(t, client, srv.URL+"/admin/login", url.Values{
		"username": {"owner"}, "password": {"correct horse battery"},
	})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin" {
		t.Fatalf("login = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestOIDCDisabledAndSetupCloses(t *testing.T) {
	srv, client, _ := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	resp, _ := get(t, client, srv.URL+"/admin/oidc/start")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("oidc start = %d, want 404", resp.StatusCode)
	}
	setupAdmin(t, srv, client)
	resp, _ = get(t, client, srv.URL+"/admin/setup")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("setup after first admin = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func mustGet(t *testing.T, client *http.Client, u string) string {
	t.Helper()
	resp, body := get(t, client, u)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", u, resp.StatusCode)
	}
	return body
}
