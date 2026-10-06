package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestAPI(t *testing.T, limiter *ratelimit.Limiter) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mux := http.NewServeMux()
	New(st, limiter, false, discardLogger()).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func getJSON(t *testing.T, url string, out any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
	return resp
}

func decodeError(t *testing.T, resp *http.Response) httpx.ErrorBody {
	t.Helper()
	var body httpx.ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1000, 100))

	var body map[string]string
	resp := getJSON(t, srv.URL+"/healthz", &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v, want status ok", body)
	}
}

func TestListEntries(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1000, 100))

	var page store.Page
	resp := getJSON(t, srv.URL+"/api/v1/entries?per_page=5", &page)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if page.Total != 100 || page.Pages != 20 || len(page.Entries) != 5 {
		t.Fatalf("page metadata = total %d pages %d items %d, want 100/20/5", page.Total, page.Pages, len(page.Entries))
	}
	for i := 1; i < len(page.Entries); i++ {
		if page.Entries[i].CreatedAt.After(page.Entries[i-1].CreatedAt) {
			t.Fatalf("items are not sorted newest first: %v then %v", page.Entries[i-1].CreatedAt, page.Entries[i].CreatedAt)
		}
	}
	if page.Entries[0].Word == "" || page.Entries[0].Meaning == "" {
		t.Fatalf("entry fields are empty: %+v", page.Entries[0])
	}

	var ascending store.Page
	getJSON(t, srv.URL+"/api/v1/entries?sort=word&order=asc&per_page=100", &ascending)
	if ascending.Entries[0].Word == "" {
		t.Fatal("ascending listing returned no first word")
	}
	if strings.ToLower(ascending.Entries[0].Word) > strings.ToLower(ascending.Entries[1].Word) {
		t.Fatalf("word ordering broken: %q > %q", ascending.Entries[0].Word, ascending.Entries[1].Word)
	}
}

func TestGetEntry(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1000, 100))

	var page store.Page
	getJSON(t, srv.URL+"/api/v1/entries?per_page=1", &page)
	first := page.Entries[0]

	var entry store.Entry
	resp := getJSON(t, srv.URL+"/api/v1/entries/"+itoa(first.ID), &entry)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if entry != first {
		t.Fatalf("entry = %+v, want %+v", entry, first)
	}

	resp = getJSON(t, srv.URL+"/api/v1/entries/abc", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d, want 400", resp.StatusCode)
	}
	if body := decodeError(t, resp); body.Error.Code != "invalid_id" {
		t.Fatalf("invalid id code = %q, want invalid_id", body.Error.Code)
	}

	resp = getJSON(t, srv.URL+"/api/v1/entries/999999", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing entry status = %d, want 404", resp.StatusCode)
	}
	if body := decodeError(t, resp); body.Error.Code != "not_found" {
		t.Fatalf("missing entry code = %q, want not_found", body.Error.Code)
	}
}

func TestCreateEntry(t *testing.T) {
	srv, st := newTestAPI(t, ratelimit.New(1000, 100))

	payload := `{"word":"  trein vertraging  ","meaning":"  train delay  "}`
	resp, err := http.Post(srv.URL+"/api/v1/entries", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var entry store.Entry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entry.Word != "trein vertraging" || entry.Meaning != "train delay" {
		t.Fatalf("entry = %+v, want trimmed values", entry)
	}
	if want := "/api/v1/entries/" + itoa(entry.ID); resp.Header.Get("Location") != want {
		t.Fatalf("Location = %q, want %q", resp.Header.Get("Location"), want)
	}

	count, err := st.CountEntries(t.Context())
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 101 {
		t.Fatalf("count = %d, want 101", count)
	}
}

func TestCreateEntryValidation(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1000, 100))

	cases := []struct {
		name    string
		payload string
		status  int
		code    string
	}{
		{"missing word", `{"word":"","meaning":"x"}`, http.StatusUnprocessableEntity, "validation_error"},
		{"whitespace meaning", `{"word":"x","meaning":"   "}`, http.StatusUnprocessableEntity, "validation_error"},
		{"malformed json", `{"word":`, http.StatusBadRequest, "invalid_json"},
		{"wrong shape", `[1,2,3]`, http.StatusBadRequest, "invalid_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/api/v1/entries", "application/json", strings.NewReader(tc.payload))
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if body := decodeError(t, resp); body.Error.Code != tc.code {
				t.Fatalf("code = %q, want %q", body.Error.Code, tc.code)
			}
		})
	}
}

func TestCreateEntryOversizedBody(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1000, 100))

	big := `{"word":"` + strings.Repeat("a", maxBodyBytes+1) + `","meaning":"x"}`
	resp, err := http.Post(srv.URL+"/api/v1/entries", "application/json", bytes.NewReader([]byte(big)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for oversized body", resp.StatusCode)
	}
}

func TestRateLimit(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1, 1))

	post := func() *http.Response {
		resp, err := http.Post(srv.URL+"/api/v1/entries", "application/json",
			strings.NewReader(`{"word":"w","meaning":"m"}`))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		return resp
	}

	first := post()
	defer first.Body.Close()
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", first.StatusCode)
	}

	second := post()
	defer second.Body.Close()
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.StatusCode)
	}
	if second.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	if body := decodeError(t, second); body.Error.Code != "rate_limited" {
		t.Fatalf("code = %q, want rate_limited", body.Error.Code)
	}
}

func TestStats(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1000, 100))

	var stats store.Stats
	resp := getJSON(t, srv.URL+"/api/v1/stats", &stats)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if stats.Total != 100 || stats.UniqueWords == 0 {
		t.Fatalf("stats = %+v, want 100 seeded entries", stats)
	}
	if stats.FirstEntryAt == nil || stats.LastEntryAt == nil {
		t.Fatalf("stats timestamps missing: %+v", stats)
	}
}

func TestSecurityHeadersAppliedByMiddleware(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "mw.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	New(st, ratelimit.New(1000, 100), false, discardLogger()).Register(mux)
	handler := httpx.SecurityHeaders("default-src 'self'")(mux)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Errorf("CSP = %q", got)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
