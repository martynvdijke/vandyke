package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	return New(Options{
		Version: "1.9.1",
		DBPath:  t.TempDir() + "/vandyke.db",
		Start:   time.Now().Add(-60 * time.Second),
		Snapshot: func(context.Context) (DBSnapshot, error) {
			return DBSnapshot{Total: 12, Live: 10, Trashed: 2, WithPhoto: 1, PendingDeliveries: 3}, nil
		},
	})
}

func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	var b strings.Builder
	if err := r.Write(context.Background(), &b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return b.String()
}

func TestExpositionShape(t *testing.T) {
	r := testRegistry(t)
	r.SetQueueDepth(func() int { return 7 })
	r.IncEntryAddition("created")
	r.IncEntryAddition("duplicate")
	r.IncEntryAddition("not-a-real-outcome")
	r.IncRateLimited()
	r.IncWebhookDropped()
	r.ObserveWebhookDelivery("delivered")
	r.ObserveWebhookAttempt("success")
	r.ObserveHTTP("GET", "GET /", 200, 12*time.Millisecond)

	body := scrape(t, r)
	for _, want := range []string{
		"# HELP vandyke_build_info",
		"# TYPE vandyke_build_info gauge",
		`vandyke_build_info{version="1.9.1",go_version="`,
		"# TYPE vandyke_entries_total gauge",
		"vandyke_entries_total 12",
		"vandyke_entries_live 10",
		"vandyke_entries_trashed 2",
		"vandyke_entries_with_photo 1",
		`vandyke_webhook_pending_deliveries 3`,
		"vandyke_db_up 1",
		"vandyke_webhook_queue_depth 7",
		"vandyke_webhook_dropped_total 1",
		`vandyke_webhook_deliveries_total{result="delivered"} 1`,
		`vandyke_webhook_deliveries_total{result="failed"} 0`,
		`vandyke_webhook_deliveries_total{result="cancelled"} 0`,
		`vandyke_webhook_attempts_total{result="success"} 1`,
		`vandyke_webhook_attempts_total{result="retry"} 0`,
		`vandyke_webhook_attempts_total{result="permanent_failure"} 0`,
		`vandyke_entry_additions_total{outcome="created"} 1`,
		`vandyke_entry_additions_total{outcome="duplicate"} 1`,
		`vandyke_entry_additions_total{outcome="invalid"} 0`,
		`vandyke_entry_additions_total{outcome="rate_limited"} 0`,
		`vandyke_entry_additions_total{outcome="error"} 0`,
		"vandyke_rate_limited_requests_total 1",
		`vandyke_http_requests_total{method="GET",route="GET /",status="200"} 1`,
		"# TYPE vandyke_http_request_duration_seconds histogram",
		`vandyke_http_request_duration_seconds_bucket{method="GET",route="GET /",le="+Inf"} 1`,
		`vandyke_http_request_duration_seconds_count{method="GET",route="GET /"} 1`,
		"vandyke_process_start_time_seconds ",
		"vandyke_process_uptime_seconds ",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("exposition missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "# EOF") {
		t.Fatal("exposition unexpectedly contains an EOF marker")
	}
	if strings.Contains(body, "1.9.1 1\n2026-") {
		t.Fatal("exposition unexpectedly contains a timestamp")
	}
	if strings.Contains(body, "not-a-real-outcome") {
		t.Fatal("an unbounded outcome became a label value")
	}
	// Deterministic ordering: the series order is stable between scrapes even
	// though the uptime value advances.
	if again := scrape(t, r); strings.Join(seriesNames(again), "\n") != strings.Join(seriesNames(body), "\n") {
		t.Fatal("two scrapes of the same state ordered their series differently")
	}
}

func seriesNames(body string) []string {
	var names []string
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	return names
}

func TestDBSnapshotFailureOmitsGauges(t *testing.T) {
	r := New(Options{
		Version: "test",
		Start:   time.Now(),
		Snapshot: func(context.Context) (DBSnapshot, error) {
			return DBSnapshot{}, errors.New("database is gone")
		},
	})
	body := scrape(t, r)
	if !strings.Contains(body, "vandyke_db_up 0") {
		t.Fatalf("db_up is not 0:\n%s", body)
	}
	for _, omitted := range []string{
		"vandyke_entries_total",
		"vandyke_entries_live",
		"vandyke_entries_trashed",
		"vandyke_entries_with_photo",
		"vandyke_webhook_pending_deliveries",
	} {
		if strings.Contains(body, omitted) {
			t.Fatalf("%s was emitted despite a failed snapshot", omitted)
		}
	}
	if !strings.Contains(body, "vandyke_process_uptime_seconds") {
		t.Fatal("process metrics disappeared with the database")
	}
}

func TestMiddlewareUsesRoutePatternNotPath(t *testing.T) {
	r := New(Options{Start: time.Now()})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /e/{id}", func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(Middleware(r)(mux))
	defer server.Close()

	resp, err := http.Get(server.URL + "/e/123")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	resp, err = http.Get(server.URL + "/no/such/path")
	if err != nil {
		t.Fatalf("GET unmatched: %v", err)
	}
	_ = resp.Body.Close()

	body := scrape(t, r)
	if !strings.Contains(body, `route="GET /e/{id}"`) {
		t.Fatalf("route pattern missing:\n%s", body)
	}
	if strings.Contains(body, "/e/123") || strings.Contains(body, `route="123"`) {
		t.Fatalf("raw path leaked into a label:\n%s", body)
	}
	if !strings.Contains(body, `route="unmatched"`) {
		t.Fatalf("unmatched request missing:\n%s", body)
	}
	if !strings.Contains(body, `status="404"`) {
		t.Fatalf("unmatched status missing:\n%s", body)
	}
}

func TestHistogramBucketsAreCumulative(t *testing.T) {
	r := New(Options{Start: time.Now()})
	r.ObserveHTTP("GET", "GET /x", 200, 30*time.Millisecond)
	body := scrape(t, r)
	for _, want := range []string{
		`le="0.005"} 0`,
		`le="0.01"} 0`,
		`le="0.025"} 0`,
		`le="0.05"} 1`,
		`le="0.1"} 1`,
		`le="10"} 1`,
		`le="+Inf"} 1`,
		`vandyke_http_request_duration_seconds_count{method="GET",route="GET /x"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("histogram missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `vandyke_http_request_duration_seconds_sum{method="GET",route="GET /x"} 0.03`) {
		t.Fatalf("histogram sum missing:\n%s", body)
	}
}

func TestLabelEscaping(t *testing.T) {
	r := New(Options{Start: time.Now()})
	r.ObserveHTTP("GET", `GET /a"b\c`, 200, time.Millisecond)
	body := scrape(t, r)
	if !strings.Contains(body, `route="GET /a\"b\\c"`) {
		t.Fatalf("label was not escaped:\n%s", body)
	}
}

func TestNilRegistryIsSafe(t *testing.T) {
	var r *Registry
	r.IncEntryAddition("created")
	r.IncRateLimited()
	r.IncWebhookDropped()
	r.ObserveWebhookAttempt("success")
	r.ObserveWebhookDelivery("delivered")
	r.ObserveHTTP("GET", "GET /", 200, time.Second)
	r.SetQueueDepth(func() int { return 0 })
}
