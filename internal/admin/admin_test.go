package admin

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/settings"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/web"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

var csrfPattern = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func newAdminTestServer(t *testing.T, cfg config.Config) (*httptest.Server, *http.Client, *store.Store) {
	t.Helper()
	srv, client, st, _ := newAdminTestServerWithEngine(t, cfg)
	return srv, client, st
}

func newAdminTestServerWithEngine(t *testing.T, cfg config.Config) (*httptest.Server, *http.Client, *store.Store, *webhook.Engine) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	stg, err := settings.New(t.Context(), st, cfg.Umami, cfg.Webhook, cfg.Notifications)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := webhook.NewEngine(st, stg.CurrentWebhook, "", nil, logger)
	engine.Start()
	t.Cleanup(engine.Close)
	a, err := New(t.Context(), st, stg, cfg, engine, nil, logger)
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	w, err := web.New(st, ratelimit.New(1000, 100), stg.Current, engine, nil, false, "", nil, logger)
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
	return srv, client, st, engine
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

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatalf("decode tiny png: %v", err)
	}
	return data
}

func TestAdminOriginalPhotoDownload(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, client)

	png := tinyPNG(t)
	photo, err := store.NewPhoto(png)
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "original", "photo", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	originalURL := srv.URL + "/admin/entries/" + itoa(entry.ID) + "/photo/original"

	anon := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := anon.Get(originalURL)
	if err != nil {
		t.Fatalf("anonymous GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/login") {
		t.Fatalf("anonymous original = %d %q, want login redirect", resp.StatusCode, resp.Header.Get("Location"))
	}

	authResp, body := get(t, client, originalURL)
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("original status = %d", authResp.StatusCode)
	}
	if got := authResp.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "entry-") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if body != string(png) {
		t.Fatal("original download did not return the uploaded bytes")
	}

	resp, _ = get(t, client, srv.URL+"/admin/entries/999999/photo/original")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing original status = %d, want 404", resp.StatusCode)
	}
}

func TestAdminFiltersRender(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, client)

	png := tinyPNG(t)
	photo, err := store.NewPhoto(png)
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if _, err := st.CreateEntry(t.Context(), "filter", "photo", "", photo); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	resp, body := get(t, client, srv.URL+"/admin?photo=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, want := range []string{`name="photo" value="1" checked`, `name="year"`, "with photos"} {
		if !strings.Contains(body, want) {
			t.Errorf("admin filter missing %q", want)
		}
	}
}

func TestSetupPageOffersOIDCWhenConfigured(t *testing.T) {
	// Only the names matter here: setup.html renders admin-head/admin-foot, which
	// use none of the ledger helpers, so variadic stubs are enough to parse.
	stub := func(...any) string { return "" }
	funcs := template.FuncMap{
		"entryNo": stub, "inc": stub, "dec": stub,
		"formatDate": stub, "formatDateTime": stub, "formatDateTimeP": stub,
	}
	tmpl, err := template.New("root").Funcs(funcs).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	render := func(oidc bool) string {
		t.Helper()
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "setup", setupPage{OIDC: oidc}); err != nil {
			t.Fatalf("render setup (oidc=%v): %v", oidc, err)
		}
		return buf.String()
	}
	if got := render(true); !strings.Contains(got, `href="/admin/oidc/start"`) {
		t.Errorf("setup page with OIDC lacks the provider button:\n%s", got)
	}
	if got := render(false); strings.Contains(got, "/admin/oidc/start") {
		t.Errorf("setup page without OIDC still offers the provider button:\n%s", got)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestAdminTrashRestorePurgeAndCap(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)

	first, err := st.CreateEntry(t.Context(), "trashword", "trash meaning", "Ada", nil)
	if err != nil {
		t.Fatalf("CreateEntry first: %v", err)
	}
	second, err := st.CreateEntry(t.Context(), "secondtrash", "second meaning", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry second: %v", err)
	}

	// The admin bulk delete now soft-deletes.
	resp := postForm(t, client, srv.URL+"/admin/entries/delete", url.Values{
		"csrf": {csrf}, "ids": {strconv.FormatInt(first.ID, 10)},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want 303", resp.StatusCode)
	}
	if _, err := st.GetEntry(t.Context(), first.ID); err == nil {
		t.Fatal("soft-deleted entry still visible to the public read path")
	}
	trashed, err := st.GetEntryIncludingDeleted(t.Context(), first.ID)
	if err != nil || trashed.DeletedAt == nil {
		t.Fatalf("entry not in trash after delete: %+v (%v)", trashed, err)
	}

	resp, body := get(t, client, srv.URL+"/admin/trash")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trash page status = %d", resp.StatusCode)
	}
	for _, want := range []string{"trashword", "trash meaning", "deleted", "Restore selected", "Purge selected"} {
		if !strings.Contains(body, want) {
			t.Errorf("trash page missing %q", want)
		}
	}

	// Missing CSRF is rejected before any restore happens.
	resp = postForm(t, client, srv.URL+"/admin/trash/restore", url.Values{
		"ids": {strconv.FormatInt(first.ID, 10)},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("restore without CSRF = %d, want 403", resp.StatusCode)
	}

	resp = postForm(t, client, srv.URL+"/admin/trash/restore", url.Values{
		"csrf": {csrf}, "ids": {strconv.FormatInt(first.ID, 10)},
	})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/trash?restored=1" {
		t.Fatalf("restore = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	restored, err := st.GetEntry(t.Context(), first.ID)
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("entry not restored: %+v (%v)", restored, err)
	}

	resp, body = get(t, client, srv.URL+"/admin/trash")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "The trash is empty") {
		t.Fatalf("trash page after restore = %d, empty state missing", resp.StatusCode)
	}

	// Trash both again, then exercise the 100-id cap.
	if _, err := st.SoftDeleteEntries(t.Context(), []int64{first.ID, second.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	tooMany := make([]string, 0, 101)
	for i := 1; i <= 101; i++ {
		tooMany = append(tooMany, strconv.Itoa(i))
	}
	resp = postForm(t, client, srv.URL+"/admin/trash/purge", url.Values{
		"csrf": {csrf}, "ids": tooMany,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("purge over cap = %d, want 400", resp.StatusCode)
	}
	if _, err := st.GetEntryIncludingDeleted(t.Context(), first.ID); err != nil {
		t.Fatalf("over-cap purge deleted rows: %v", err)
	}

	resp = postForm(t, client, srv.URL+"/admin/trash/purge", url.Values{
		"csrf": {csrf},
		"ids": []string{
			strconv.FormatInt(first.ID, 10),
			strconv.FormatInt(second.ID, 10),
		},
	})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/trash?purged=2" {
		t.Fatalf("purge = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	for _, id := range []int64{first.ID, second.ID} {
		if _, err := st.GetEntryIncludingDeleted(t.Context(), id); err == nil {
			t.Fatalf("entry %d survived purge", id)
		}
	}
}

func TestAdminCanInspectTrashedEntryAndPhoto(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, client)

	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "", "", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.SoftDeleteEntries(t.Context(), []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	resp, body := get(t, client, srv.URL+"/admin/entries/"+itoa(entry.ID)+"/edit")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trashed edit page status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "This entry is in the trash") || !strings.Contains(body, "Restore entry") {
		t.Fatal("edit page lacks the trash banner or restore control")
	}

	resp, _ = get(t, client, srv.URL+"/admin/entries/"+itoa(entry.ID)+"/photo/original")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trashed original photo status = %d, want 200", resp.StatusCode)
	}
}

func TestAdminDocumentsExposeStructureManifestAndFormAria(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, client)

	mainPattern := regexp.MustCompile(`<main\b`)
	anon := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	pages := []struct {
		path   string
		client *http.Client
	}{
		{"/admin/login", anon},
		{"/admin", client},
	}
	for _, page := range pages {
		resp, body := get(t, page.client, srv.URL+page.path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", page.path, resp.StatusCode)
		}
		if got := len(mainPattern.FindAllString(body, -1)); got != 1 {
			t.Errorf("%s has %d main elements, want 1", page.path, got)
		}
		if !strings.Contains(body, `id="content"`) {
			t.Errorf("%s main is missing id=content", page.path)
		}
		skip := strings.Index(body, `class="skip-link"`)
		main := strings.Index(body, "<main")
		if skip < 0 || main < 0 || skip > main {
			t.Errorf("%s skip link does not precede main", page.path)
		}
		if !strings.Contains(body, `rel="manifest" href="/site.webmanifest"`) {
			t.Errorf("%s is missing the manifest link", page.path)
		}
	}

	// A failed sign-in is announced through a role=alert region the form names.
	resp, err := anon.PostForm(srv.URL+"/admin/login", url.Values{
		"username": {"owner"}, "password": {"definitely wrong"},
	})
	if err != nil {
		t.Fatalf("failed login POST: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read login body: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("failed login status = %d, want 401", resp.StatusCode)
	}
	for _, want := range []string{`id="login-error"`, `role="alert"`, `aria-describedby="login-error"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("failed login page is missing %q", want)
		}
	}

	// Every hinted input on the edit form names its hint, and the remove-photo
	// checkbox keeps its visible label.
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "hints", "labels", "Ada", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	resp, editBody := get(t, client, srv.URL+"/admin/entries/"+itoa(entry.ID)+"/edit")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit status = %d, want 200", resp.StatusCode)
	}
	for _, hint := range []string{"edit-word-hint", "edit-meaning-hint", "edit-person-hint", "edit-date-hint", "edit-photo-hint"} {
		if !strings.Contains(editBody, `id="`+hint+`"`) {
			t.Errorf("edit form has no hint element #%s", hint)
		}
		if !strings.Contains(editBody, `aria-describedby="`+hint+`"`) {
			t.Errorf("edit form input does not reference #%s", hint)
		}
	}
	if !strings.Contains(editBody, `<input type="checkbox" name="remove_photo"`) || !strings.Contains(editBody, "Remove this photo") {
		t.Error("remove-photo checkbox lost its visible label")
	}
}

func TestAdminSearchRebuild(t *testing.T) {
	srv, client, _ := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)

	resp := postForm(t, client, srv.URL+"/entries", url.Values{
		"word": {"rebuildprobe"}, "meaning": {"index me"}, "person": {"Rebuild Person"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("add entry status = %d", resp.StatusCode)
	}

	_, settings := get(t, client, srv.URL+"/admin/settings")
	if !strings.Contains(settings, `action="/admin/search/rebuild"`) {
		t.Fatal("settings page has no rebuild form")
	}

	// Without a CSRF token the action is rejected before touching the index.
	resp = postForm(t, client, srv.URL+"/admin/search/rebuild", url.Values{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d, want 403", resp.StatusCode)
	}

	// Without a session the request is redirected to the login page.
	anonymous := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp = postForm(t, anonymous, srv.URL+"/admin/search/rebuild", url.Values{"csrf": {"anything"}})
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/login") {
		t.Fatalf("unauthenticated rebuild = %d %q, want login redirect", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp = postForm(t, client, srv.URL+"/admin/search/rebuild", url.Values{"csrf": {csrf}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/settings?rebuilt=1" {
		t.Fatalf("rebuild = %d %q, want settings confirmation", resp.StatusCode, resp.Header.Get("Location"))
	}

	_, confirmed := get(t, client, srv.URL+"/admin/settings?rebuilt=1")
	if !strings.Contains(confirmed, "Search index rebuilt") {
		t.Fatal("settings page does not confirm the rebuild")
	}

	// The rebuilt index still serves the person field to the public search.
	_, public := get(t, client, srv.URL+"/entries?q=Rebuild+Person")
	// Highlighting splits "rebuildprobe" around the matched term, so assert on
	// the unmarked correction text instead.
	if !strings.Contains(public, "index me") {
		t.Fatal("rebuilt index lost the person entry")
	}
}

func postFormRead(t *testing.T, client *http.Client, u string, values url.Values) (int, string) {
	t.Helper()
	resp, err := client.PostForm(u, values)
	if err != nil {
		t.Fatalf("POST %s: %v", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

func TestAdminWebhookSettingsAndTestFire(t *testing.T) {
	srv, client, _ := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)

	// Missing CSRF is rejected before any request is made.
	if status, _ := postFormRead(t, client, srv.URL+"/admin/settings/webhook/test", url.Values{}); status != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d, want 403", status)
	}

	// Without a session the action redirects to the login page.
	anonymous := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := anonymous.PostForm(srv.URL+"/admin/settings/webhook/test", url.Values{"csrf": {"anything"}})
	if err != nil {
		t.Fatalf("anonymous POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/login") {
		t.Fatalf("unauthenticated test-fire = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// No URL configured: a clear message and no outbound request.
	status, body := postFormRead(t, client, srv.URL+"/admin/settings/webhook/test", url.Values{"csrf": {csrf}})
	if status != http.StatusOK {
		t.Fatalf("no-url test-fire status = %d", status)
	}
	if !strings.Contains(body, "No legacy webhook configured") {
		t.Fatalf("no-url test-fire body missing the message: %.400s", body)
	}

	type delivery struct {
		body      []byte
		signature string
	}
	deliveries := make(chan delivery, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		deliveries <- delivery{body: payload, signature: r.Header.Get("X-VanDyke-Signature")}
		rw.WriteHeader(http.StatusAccepted)
	}))
	defer receiver.Close()

	status, _ = postFormRead(t, client, srv.URL+"/admin/settings", url.Values{
		"csrf":           {csrf},
		"settings_form":  {"webhook"},
		"webhook_url":    {receiver.URL},
		"webhook_secret": {"supersecret"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("save webhook status = %d", status)
	}

	_, settings := get(t, client, srv.URL+"/admin/settings")
	if strings.Contains(settings, "supersecret") {
		t.Fatal("stored webhook secret was rendered back into the page")
	}
	if !strings.Contains(settings, "A secret is configured") {
		t.Fatal("settings page does not report the configured secret")
	}
	if !strings.Contains(settings, receiver.URL) {
		t.Fatal("settings page does not show the effective webhook URL")
	}

	status, body = postFormRead(t, client, srv.URL+"/admin/settings/webhook/test", url.Values{"csrf": {csrf}})
	if status != http.StatusOK {
		t.Fatalf("test-fire status = %d", status)
	}
	if !strings.Contains(body, "HTTP 202") {
		t.Fatalf("test-fire did not report the receiver status: %.400s", body)
	}

	select {
	case got := <-deliveries:
		mac := hmac.New(sha256.New, []byte("supersecret"))
		_, _ = mac.Write(got.body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got.signature != want {
			t.Fatalf("test-fire signature = %q, want %q", got.signature, want)
		}
		var payload struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal(got.body, &payload); err != nil {
			t.Fatalf("decode test-fire payload: %v", err)
		}
		if payload.Event != "entry.created" {
			t.Fatalf("test-fire event = %q", payload.Event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("test-fire made no request")
	}

	// A failing receiver is reported as its HTTP status.
	failing := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	if status, _ := postFormRead(t, client, srv.URL+"/admin/settings", url.Values{
		"csrf":          {csrf},
		"settings_form": {"webhook"},
		"webhook_url":   {failing.URL},
	}); status != http.StatusSeeOther {
		t.Fatalf("save failing webhook status = %d", status)
	}
	status, body = postFormRead(t, client, srv.URL+"/admin/settings/webhook/test", url.Values{"csrf": {csrf}})
	if status != http.StatusOK || !strings.Contains(body, "HTTP 500") {
		t.Fatalf("failing test-fire = %d, body %.400s", status, body)
	}

	// Clearing removes the stored secret without touching the URL.
	if status, _ := postFormRead(t, client, srv.URL+"/admin/settings", url.Values{
		"csrf":                 {csrf},
		"settings_form":        {"webhook"},
		"webhook_url":          {failing.URL},
		"webhook_clear_secret": {"1"},
	}); status != http.StatusSeeOther {
		t.Fatalf("clear secret status = %d", status)
	}
	_, cleared := get(t, client, srv.URL+"/admin/settings")
	if strings.Contains(cleared, "A secret is configured") {
		t.Fatal("secret still reported as configured after clearing")
	}
	if !strings.Contains(cleared, failing.URL) {
		t.Fatal("clearing the secret removed the URL")
	}
}

func TestAdminNotificationSettingsSave(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)

	resp := postForm(t, client, srv.URL+"/admin/settings", url.Values{
		"csrf":                           {csrf},
		"settings_form":                  {"notifications"},
		"notifications_enabled":          {"1"},
		"notifications_realtime":         {"1"},
		"notifications_webpush":          {"1"},
		"notifications_entry_created":    {"1"},
		"notifications_title":            {"{person}: {word}"},
		"notifications_body":             {"{meaning} (#{id})"},
		"notifications_click_url":        {"/e/{id}"},
		"notifications_vapid_subject":    {"mailto:ops@example.com"},
		"notifications_vapid_public_key": {"test-public-key"},
		"notifications_fcm_project_id":   {"project-1"},
		"notifications_firebase_api_key": {"firebase-key"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save notifications = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/admin/settings?saved=1#notifications" {
		t.Fatalf("Location = %q", got)
	}

	values, err := st.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if values["notifications.enabled"] != "1" || values["notifications.entry_created"] != "1" {
		t.Fatalf("notification flags = %q/%q", values["notifications.enabled"], values["notifications.entry_created"])
	}
	if values["notifications.title"] != "{person}: {word}" {
		t.Fatalf("stored title = %q", values["notifications.title"])
	}
	if values["notifications.vapid.public_key"] != "test-public-key" {
		t.Fatalf("stored VAPID public key = %q", values["notifications.vapid.public_key"])
	}

	_, page := get(t, client, srv.URL+"/admin/settings")
	if !strings.Contains(page, `value="{person}: {word}"`) {
		t.Fatal("settings page does not show the saved title template")
	}
	if !strings.Contains(page, `name="notifications_enabled" value="1" checked`) {
		t.Fatal("settings page does not show the master toggle as checked")
	}
}

func TestAdminNotificationSettingsInvalidClickURL(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)

	status, body := postFormRead(t, client, srv.URL+"/admin/settings", url.Values{
		"csrf":                    {csrf},
		"settings_form":           {"notifications"},
		"notifications_title":     {"Never persisted"},
		"notifications_click_url": {"not-a-url"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid click URL status = %d, want 422", status)
	}
	if !strings.Contains(body, "must contain") {
		t.Fatalf("invalid click URL body lacks the validation error: %.300s", body)
	}

	values, err := st.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if values["notifications.title"] != "" {
		t.Fatalf("invalid save persisted title %q", values["notifications.title"])
	}
	if values["notifications.click_url"] != "" {
		t.Fatalf("invalid save persisted click URL %q", values["notifications.click_url"])
	}
}

func TestAdminAccountNotificationPrefsRoundTrip(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)

	admin, err := st.GetAdminByUsername(t.Context(), "owner")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}

	resp := postForm(t, client, srv.URL+"/admin/account/notifications", url.Values{
		"csrf":          {csrf},
		"entry_created": {"1"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save prefs = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/admin/account?flash=notifications-saved" {
		t.Fatalf("Location = %q", got)
	}

	prefs, err := st.GetAdminNotificationPrefs(t.Context(), admin.ID)
	if err != nil {
		t.Fatalf("GetAdminNotificationPrefs: %v", err)
	}
	if prefs.Realtime || prefs.WebPush || !prefs.EntryCreated {
		t.Fatalf("prefs after first save = %+v", prefs)
	}

	_, body := get(t, client, srv.URL+"/admin/account?flash=notifications-saved")
	if !strings.Contains(body, "Notification preferences saved.") {
		t.Fatal("account page does not confirm the saved preferences")
	}

	resp = postForm(t, client, srv.URL+"/admin/account/notifications", url.Values{
		"csrf":             {csrf},
		"realtime_enabled": {"1"},
		"web_push_enabled": {"1"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("second save prefs = %d, want 303", resp.StatusCode)
	}
	prefs, err = st.GetAdminNotificationPrefs(t.Context(), admin.ID)
	if err != nil {
		t.Fatalf("GetAdminNotificationPrefs: %v", err)
	}
	if !prefs.Realtime || !prefs.WebPush || prefs.EntryCreated {
		t.Fatalf("prefs after second save = %+v", prefs)
	}
}

func TestAdminVAPIDGenerate(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)

	resp := postForm(t, client, srv.URL+"/admin/settings/notifications/vapid", url.Values{
		"csrf": {csrf},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("generate VAPID = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/admin/settings?saved=1#notifications" {
		t.Fatalf("Location = %q", got)
	}

	values, err := st.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if values["notifications.vapid.public_key"] == "" {
		t.Fatal("generated VAPID public key was not stored")
	}
	if values["notifications.vapid.private_key"] == "" {
		t.Fatal("generated VAPID private key was not stored")
	}
	// The built-in click-URL default must not be pinned as an invalid override.
	if values["notifications.click_url"] != "" {
		t.Fatalf("click URL became an override: %q", values["notifications.click_url"])
	}
	if strings.Contains(values["notifications.vapid.private_key"], "BEGIN") {
		t.Fatal("stored VAPID private key looks like a malformed placeholder")
	}
}
