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
	s, err := New(ctx, st, env, config.Webhook{})
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

	reloaded, err := New(ctx, st, env, config.Webhook{})
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

func TestWebhookEnvFallbackOverrideAndSecretLifecycle(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "webhook-settings.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	envWebhook := config.Webhook{
		URL:    "https://env.example.com/hook",
		Secret: "env-secret",
	}
	s, err := New(ctx, st, config.Umami{}, envWebhook)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := s.CurrentWebhook(); got != envWebhook {
		t.Fatalf("CurrentWebhook = %+v, want %+v", got, envWebhook)
	}

	if err := s.SaveWebhook(ctx, config.Webhook{URL: "https://db.example.com/hook"}, false); err != nil {
		t.Fatalf("SaveWebhook: %v", err)
	}
	got := s.CurrentWebhook()
	if got.URL != "https://db.example.com/hook" {
		t.Fatalf("override URL = %q", got.URL)
	}
	if got.Secret != "env-secret" {
		t.Fatalf("blank secret should keep the effective env secret, got %q", got.Secret)
	}

	if err := s.SaveWebhook(ctx, config.Webhook{URL: "https://db.example.com/hook", Secret: "stored-secret"}, false); err != nil {
		t.Fatalf("SaveWebhook secret: %v", err)
	}
	if s.CurrentWebhook().Secret != "stored-secret" {
		t.Fatalf("stored secret not applied: %+v", s.CurrentWebhook())
	}

	// A blank secret with a clear request removes the stored one and falls
	// back to the environment secret.
	if err := s.SaveWebhook(ctx, config.Webhook{URL: "https://db.example.com/hook"}, true); err != nil {
		t.Fatalf("SaveWebhook clear: %v", err)
	}
	if s.CurrentWebhook().Secret != "env-secret" {
		t.Fatalf("cleared secret = %q, want env fallback", s.CurrentWebhook().Secret)
	}

	if err := s.SaveWebhook(ctx, config.Webhook{URL: "ftp://example.com/hook"}, false); err == nil {
		t.Fatal("non-http webhook URL accepted")
	}
	if err := s.SaveWebhook(ctx, config.Webhook{URL: "not-a-url"}, false); err == nil {
		t.Fatal("relative webhook URL accepted")
	}

	effective := s.Current()
	if effective != (config.Umami{}) {
		t.Fatalf("webhook saves disturbed Umami settings: %+v", effective)
	}
}
