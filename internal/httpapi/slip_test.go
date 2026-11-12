package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/slip"
	"vandyke.thor.edu/vandyke/internal/store"
)

type slipBody struct {
	Date  string      `json:"date"`
	Entry store.Entry `json:"entry"`
}

func purgeAll(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	ids, err := st.AllEntryIDs(ctx)
	if err != nil {
		t.Fatalf("AllEntryIDs: %v", err)
	}
	if len(ids) == 0 {
		return
	}
	if _, err := st.SoftDeleteEntries(ctx, ids); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.PurgeEntries(ctx, ids); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
}

func TestSlipEndpoint(t *testing.T) {
	srv, st := newTestAPI(t, ratelimit.New(1000, 100))
	want, err := slip.New(st).Today(context.Background())
	if err != nil {
		t.Fatalf("slip.Today: %v", err)
	}

	resp, err := http.Get(srv.URL + "/api/v1/slip")
	if err != nil {
		t.Fatalf("GET /api/v1/slip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content type = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control = %q, want public, max-age=60", got)
	}
	wantETag := `"slip-` + want.Date + `-` + strconv.FormatInt(want.Entry.ID, 10) + `"`
	if got := resp.Header.Get("ETag"); got != wantETag {
		t.Errorf("ETag = %q, want %q", got, wantETag)
	}

	var body slipBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Date != want.Date {
		t.Errorf("date = %q, want %q", body.Date, want.Date)
	}
	if body.Entry.ID != want.Entry.ID {
		t.Errorf("entry id = %d, want %d", body.Entry.ID, want.Entry.ID)
	}
	if body.Entry.Word == "" && body.Entry.PhotoURL == "" {
		t.Errorf("entry looks empty: %+v", body.Entry)
	}
}

func TestSlipEndpointConditional(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1000, 100))

	resp, err := http.Get(srv.URL + "/api/v1/slip")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	etag := resp.Header.Get("ETag")
	_ = resp.Body.Close()
	if etag == "" {
		t.Fatal("first response carried no ETag")
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/slip", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("If-None-Match", etag)
	conditional, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	defer conditional.Body.Close()
	if conditional.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", conditional.StatusCode)
	}
	remainder, err := io.ReadAll(conditional.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(remainder) != 0 {
		t.Fatalf("304 carried a body: %q", remainder)
	}
	if got := conditional.Header.Get("ETag"); got != etag {
		t.Errorf("304 ETag = %q, want %q", got, etag)
	}
}

func TestSlipEndpointStaleTagGetsFreshBody(t *testing.T) {
	srv, _ := newTestAPI(t, ratelimit.New(1000, 100))

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/slip", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("If-None-Match", `"slip-1999-01-01-1"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("ETag"); got == `"slip-1999-01-01-1"` || got == "" {
		t.Fatalf("stale tag was echoed or missing: %q", got)
	}
}

func TestSlipEndpointEmptyLedger(t *testing.T) {
	srv, st := newTestAPI(t, ratelimit.New(1000, 100))
	purgeAll(t, st)

	resp, err := http.Get(srv.URL + "/api/v1/slip")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	body := decodeError(t, resp)
	if body.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", body.Error.Code)
	}
	if body.Error.Message != "the ledger has no entry for today" {
		t.Errorf("message = %q", body.Error.Message)
	}
}
