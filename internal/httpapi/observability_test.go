package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/metrics"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

func newObservabilityServer(t *testing.T, registry *metrics.Registry, token string) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "obs.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	engine := webhook.NewEngine(st, nil, "", nil, discardLogger())
	t.Cleanup(engine.Close)
	mux := http.NewServeMux()
	New(st, ratelimit.New(1000, 100), false, "", engine, registry, token, discardLogger()).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func TestReadyz(t *testing.T) {
	srv, st := newObservabilityServer(t, nil, "")
	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ready" || body.Checks["database"] != "ok" || body.Checks["migrations"] != "ok" {
		t.Fatalf("ready body = %+v", body)
	}

	// A closed database reports the database check and no error text.
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	resp, err = http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz closed: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("closed readyz status = %d, want 503", resp.StatusCode)
	}
	if !strings.Contains(string(raw), `"database":"error"`) {
		t.Fatalf("closed readyz body = %s", raw)
	}
	for _, leaked := range []string{"obs.db", "sql:", "vandyke"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("readyz body leaked %q: %s", leaked, raw)
		}
	}
}

func TestReadyzMissingMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrations.db")
	st, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	engine := webhook.NewEngine(st, nil, "", nil, discardLogger())
	defer engine.Close()
	mux := http.NewServeMux()
	New(st, ratelimit.New(1000, 100), false, "", engine, nil, "", discardLogger()).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	if _, err := raw.ExecContext(context.Background(),
		`DELETE FROM schema_migrations WHERE version = '013_webhook_endpoints.sql'`); err != nil {
		_ = raw.Close()
		t.Fatalf("delete migration: %v", err)
	}
	_ = raw.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing-migration readyz = %d, want 503", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"migrations":"error"`) || !strings.Contains(string(body), `"database":"ok"`) {
		t.Fatalf("missing-migration body = %s", body)
	}
	if !strings.Contains(string(body), `"status":"unavailable"`) {
		t.Fatalf("missing-migration status = %s", body)
	}
}

func TestMetricsPublicWithoutToken(t *testing.T) {
	registry := metrics.New(metrics.Options{Version: "9.9.9", Start: time.Now().Add(-time.Minute)})
	srv, _ := newObservabilityServer(t, registry, "")

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Content-Type"), metrics.ContentType; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `vandyke_build_info{version="9.9.9"`) {
		t.Fatalf("metrics body = %.400s", body)
	}
}

func TestMetricsToken(t *testing.T) {
	registry := metrics.New(metrics.Options{Version: "9.9.9", Start: time.Now()})
	srv, _ := newObservabilityServer(t, registry, "scrape-token")

	for _, tc := range []struct {
		name   string
		header string
		want   int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer nope", http.StatusUnauthorized},
		{"wrong scheme", "Basic scrape-token", http.StatusUnauthorized},
		{"correct", "Bearer scrape-token", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/metrics", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			if tc.want == http.StatusUnauthorized && len(body) != 0 {
				t.Fatalf("401 carried a metric body: %.200s", body)
			}
			if tc.want == http.StatusOK && !strings.Contains(string(body), "vandyke_process_uptime_seconds") {
				t.Fatalf("authorized scrape has no metrics")
			}
		})
	}

	// Readiness is never gated by the metrics token.
	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readyz with a token configured = %d, want 200", resp.StatusCode)
	}
}

func TestAdditionOutcomeMetrics(t *testing.T) {
	registry := metrics.New(metrics.Options{Version: "test", Start: time.Now()})
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "outcomes.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	engine := webhook.NewEngine(st, nil, "", nil, discardLogger())
	t.Cleanup(engine.Close)
	mux := http.NewServeMux()
	// A burst of five lets the first five POSTs through; the sixth is denied.
	New(st, ratelimit.New(100, 5), false, "", engine, registry, "", discardLogger()).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	post := func(body string) int {
		resp, err := http.Post(srv.URL+"/api/v1/entries", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := post(`{"word":"","meaning":""}`); got != http.StatusUnprocessableEntity {
		t.Fatalf("invalid create = %d", got)
	}
	if got := post(`{"word":"outcome","meaning":"metric"}`); got != http.StatusCreated {
		t.Fatalf("create = %d", got)
	}
	if got := post(`{"word":"outcome","meaning":"metric"}`); got != http.StatusConflict {
		t.Fatalf("duplicate create = %d", got)
	}
	if got := post(`{"word":"outcome2","meaning":"metric"}`); got != http.StatusCreated {
		t.Fatalf("second create = %d", got)
	}
	if got := post(`{"word":"outcome3","meaning":"metric"}`); got != http.StatusCreated {
		t.Fatalf("third create = %d", got)
	}
	if got := post(`{"word":"outcome4","meaning":"metric"}`); got != http.StatusTooManyRequests {
		t.Fatalf("rate-limited create = %d", got)
	}

	var b strings.Builder
	if err := registry.Write(t.Context(), &b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body := b.String()
	for _, want := range []string{
		`vandyke_entry_additions_total{outcome="created"} 3`,
		`vandyke_entry_additions_total{outcome="duplicate"} 1`,
		`vandyke_entry_additions_total{outcome="invalid"} 1`,
		`vandyke_entry_additions_total{outcome="rate_limited"} 1`,
		`vandyke_entry_additions_total{outcome="error"} 0`,
		`vandyke_rate_limited_requests_total 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
}
