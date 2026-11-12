package httpapi

import (
	"context"
	"net/http"
	"testing"

	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

func TestRandomEntryAPI(t *testing.T) {
	srv, st := newTestAPI(t, ratelimit.New(1000, 100))

	var entry store.Entry
	resp := getJSON(t, srv.URL+"/api/v1/random", &entry)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if entry.ID <= 0 || entry.Word == "" || entry.Meaning == "" || entry.CreatedAt.IsZero() {
		t.Fatalf("random entry is incomplete: %+v", entry)
	}

	resp = getJSON(t, srv.URL+"/api/v1/random?person=Nobody", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unmatched draw status = %d, want 404", resp.StatusCode)
	}
	if errBody := decodeError(t, resp); errBody.Error.Code != "not_found" {
		t.Errorf("error code = %q, want not_found", errBody.Error.Code)
	}

	// The id path keeps its documented contract: a non-numeric segment is a
	// validation error, never a second way to ask for a random entry.
	resp = getJSON(t, srv.URL+"/api/v1/entries/random", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("entries/random status = %d, want 400", resp.StatusCode)
	}
	if errBody := decodeError(t, resp); errBody.Error.Code != "invalid_id" {
		t.Errorf("error code = %q, want invalid_id", errBody.Error.Code)
	}

	// The photo filter narrows the draw to the only photographed row.
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	created, err := st.CreateEntry(context.Background(), "fotoproef", "photo probe", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	for i := 0; i < 10; i++ {
		var drawn store.Entry
		resp := getJSON(t, srv.URL+"/api/v1/random?photo=1", &drawn)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("photo draw status = %d", resp.StatusCode)
		}
		if drawn.ID != created.ID || drawn.PhotoURL == "" {
			t.Fatalf("photo draw = %+v, want entry %d with a photo_url", drawn, created.ID)
		}
	}
}
