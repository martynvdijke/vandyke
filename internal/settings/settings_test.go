package settings

import (
	"context"
	"path/filepath"
	"testing"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

func TestEnvFallbackAndOverrides(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	env := config.Umami{
		URL:       "https://env.example.com/script.js",
		WebsiteID: "env-id",
		Domains:   "env.example.com",
	}
	s, err := New(ctx, st, env)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := s.Current(); got != env {
		t.Fatalf("Current = %+v, want %+v", got, env)
	}

	if err := s.Save(ctx, config.Umami{URL: "https://db.example.com/script.js"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := s.Current()
	if got.URL != "https://db.example.com/script.js" || got.WebsiteID != "env-id" || got.Domains != "env.example.com" {
		t.Fatalf("merged = %+v", got)
	}

	reloaded, err := New(ctx, st, env)
	if err != nil {
		t.Fatalf("New reloaded: %v", err)
	}
	if reloaded.Current().URL != "https://db.example.com/script.js" {
		t.Fatalf("reloaded = %+v", reloaded.Current())
	}

	if err := s.Save(ctx, config.Umami{URL: "not-a-url"}); err == nil {
		t.Fatal("invalid URL accepted")
	}
	if err := s.Save(ctx, config.Umami{URL: "ftp://example.com/script.js"}); err == nil {
		t.Fatal("non-http URL accepted")
	}
}
