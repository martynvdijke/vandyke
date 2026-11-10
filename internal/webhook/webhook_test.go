package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testEntry() store.Entry {
	return store.Entry{
		ID:        123,
		Word:      "feuten",
		Meaning:   "feature",
		Person:    "M. van Dijke",
		CreatedAt: time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC),
	}
}

func TestDispatchSendsSignedPayload(t *testing.T) {
	type received struct {
		body      []byte
		header    http.Header
		method    string
		mediaType string
	}
	got := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{body: body, header: r.Header.Clone(), method: r.Method, mediaType: r.Header.Get("Content-Type")}
		rw.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := New(func() config.Webhook {
		return config.Webhook{URL: srv.URL, Secret: "topsecret"}
	}, discardLogger())

	entry := testEntry()
	d.Dispatch(BuildPayload(entry, "https://ledger.example/e/123"))

	select {
	case r := <-got:
		if r.method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.method)
		}
		if r.mediaType != "application/json" {
			t.Fatalf("content type = %q", r.mediaType)
		}
		var payload Payload
		if err := json.Unmarshal(r.body, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		want := Payload{
			Event:     EventEntryCreated,
			ID:        entry.ID,
			Word:      entry.Word,
			Meaning:   entry.Meaning,
			Person:    entry.Person,
			CreatedAt: "2024-05-06T07:08:09Z",
			URL:       "https://ledger.example/e/123",
		}
		if payload != want {
			t.Fatalf("payload = %+v, want %+v", payload, want)
		}
		if payload.Event != "entry.created" {
			t.Fatalf("event = %q", payload.Event)
		}
		signature := r.header.Get("X-VanDyke-Signature")
		mac := hmac.New(sha256.New, []byte("topsecret"))
		_, _ = mac.Write(r.body)
		wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if signature != wantSignature {
			t.Fatalf("signature = %q, want %q", signature, wantSignature)
		}
		if got := r.header.Get("User-Agent"); got != userAgent {
			t.Fatalf("user agent = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no delivery received")
	}
}

func TestDispatchOmitsSignatureWithoutSecret(t *testing.T) {
	headers := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		rw.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := New(func() config.Webhook { return config.Webhook{URL: srv.URL} }, discardLogger())
	d.Dispatch(BuildPayload(testEntry(), "https://ledger.example/e/123"))

	select {
	case h := <-headers:
		if got := h.Get("X-VanDyke-Signature"); got != "" {
			t.Fatalf("unsigned delivery carried %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no delivery received")
	}
}

func TestDispatchWithoutURLMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := New(func() config.Webhook { return config.Webhook{} }, discardLogger())
	d.Dispatch(BuildPayload(testEntry(), "https://ledger.example/e/123"))

	time.Sleep(150 * time.Millisecond)
	if n := hits.Load(); n != 0 {
		t.Fatalf("disabled dispatcher made %d requests", n)
	}
}

func TestSendReportsStatusAndTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := New(func() config.Webhook { return config.Webhook{URL: srv.URL} }, discardLogger())
	status, err := d.Send(context.Background(), BuildPayload(testEntry(), "https://ledger.example/e/123"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}

	srv.Close()
	if _, err := d.Send(context.Background(), BuildPayload(testEntry(), "https://ledger.example/e/123")); err == nil {
		t.Fatal("Send against a closed receiver returned no error")
	}
}

func TestSendWithoutURLReportsNotConfigured(t *testing.T) {
	d := New(func() config.Webhook { return config.Webhook{} }, discardLogger())
	if _, err := d.Send(context.Background(), Payload{}); err != ErrNotConfigured {
		t.Fatalf("Send error = %v, want ErrNotConfigured", err)
	}
}
