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
	s, err := New(ctx, st, env, config.Webhook{}, config.Notifications{})
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

	reloaded, err := New(ctx, st, env, config.Webhook{}, config.Notifications{})
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
	s, err := New(ctx, st, config.Umami{}, envWebhook, config.Notifications{})
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

func newNotificationSettings(t *testing.T, env config.Notifications) *Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s, err := New(ctx, st, config.Umami{}, config.Webhook{}, env)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNotificationsDefaultsWhenNothingStored(t *testing.T) {
	s := newNotificationSettings(t, config.Notifications{})

	got := s.CurrentNotifications()
	if got.Enabled {
		t.Fatalf("Enabled = true, want false by default")
	}
	if !got.Realtime || !got.WebPush || !got.Android || !got.EntryCreated {
		t.Fatalf("boolean defaults = %+v, want true", got)
	}
	if got.TitleTemplate != "New entry added" {
		t.Fatalf("TitleTemplate = %q", got.TitleTemplate)
	}
	if got.BodyTemplate != `{person} added “{word}” — {meaning}` {
		t.Fatalf("BodyTemplate = %q", got.BodyTemplate)
	}
	if got.ClickURL != `{base}/e/{id}` {
		t.Fatalf("ClickURL = %q", got.ClickURL)
	}
}

func TestNotificationsSaveLoadRoundTripAndSecretsHidden(t *testing.T) {
	ctx := context.Background()
	s := newNotificationSettings(t, config.Notifications{})

	in := config.Notifications{
		Enabled:          true,
		Realtime:         false,
		WebPush:          true,
		Android:          false,
		EntryCreated:     false,
		TitleTemplate:    "  New word  ",
		BodyTemplate:     "  {person}: {word}  ",
		ClickURL:         "  /e/{id}  ",
		VAPIDSubject:     "  mailto:ops@example.com  ",
		VAPIDPublicKey:   "  public-key  ",
		VAPIDPrivateKey:  "  vapid-private  ",
		FCMProjectID:     "  project  ",
		FCMClientEmail:   "  fcm@example.com  ",
		FCMPrivateKey:    "  fcm-private  ",
		FirebaseAPIKey:   "  api-key  ",
		FirebaseAppID:    "  app-id  ",
		FirebaseSenderID: "  sender-id  ",
	}
	if err := s.SaveNotifications(ctx, in, false, false); err != nil {
		t.Fatalf("SaveNotifications: %v", err)
	}

	got := s.CurrentNotifications()
	want := in
	want.TitleTemplate = "New word"
	want.BodyTemplate = "{person}: {word}"
	want.ClickURL = "/e/{id}"
	want.VAPIDSubject = "mailto:ops@example.com"
	want.VAPIDPublicKey = "public-key"
	want.VAPIDPrivateKey = "vapid-private"
	want.FCMProjectID = "project"
	want.FCMClientEmail = "fcm@example.com"
	want.FCMPrivateKey = "fcm-private"
	want.FirebaseAPIKey = "api-key"
	want.FirebaseAppID = "app-id"
	want.FirebaseSenderID = "sender-id"
	if got != want {
		t.Fatalf("CurrentNotifications = %+v, want %+v", got, want)
	}

	overrides, err := s.OverridesNotifications(ctx)
	if err != nil {
		t.Fatalf("OverridesNotifications: %v", err)
	}
	if overrides.VAPIDPrivateKey != "" || overrides.FCMPrivateKey != "" {
		t.Fatalf("secrets leaked in overrides: %+v", overrides)
	}
	wantOverrides := want
	wantOverrides.VAPIDPrivateKey = ""
	wantOverrides.FCMPrivateKey = ""
	if overrides != wantOverrides {
		t.Fatalf("overrides = %+v, want %+v", overrides, wantOverrides)
	}

	// Settings survive a reload from the database.
	reloaded, err := New(ctx, s.store, config.Umami{}, config.Webhook{}, config.Notifications{})
	if err != nil {
		t.Fatalf("New reloaded: %v", err)
	}
	if reloaded.CurrentNotifications() != want {
		t.Fatalf("reloaded = %+v, want %+v", reloaded.CurrentNotifications(), want)
	}
}

func TestNotificationsPrivateKeyLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newNotificationSettings(t, config.Notifications{})

	base := config.Notifications{ClickURL: "/e/{id}", VAPIDPrivateKey: "vapid-secret", FCMPrivateKey: "fcm-secret"}
	if err := s.SaveNotifications(ctx, base, false, false); err != nil {
		t.Fatalf("SaveNotifications: %v", err)
	}

	// A blank private key preserves the stored one.
	blank := config.Notifications{ClickURL: "/e/{id}"}
	if err := s.SaveNotifications(ctx, blank, false, false); err != nil {
		t.Fatalf("SaveNotifications blank: %v", err)
	}
	if got := s.CurrentNotifications(); got.VAPIDPrivateKey != "vapid-secret" || got.FCMPrivateKey != "fcm-secret" {
		t.Fatalf("blank private keys did not preserve storage: %+v", got)
	}

	// The clear flags remove them.
	if err := s.SaveNotifications(ctx, blank, true, true); err != nil {
		t.Fatalf("SaveNotifications clear: %v", err)
	}
	if got := s.CurrentNotifications(); got.VAPIDPrivateKey != "" || got.FCMPrivateKey != "" {
		t.Fatalf("clear flags did not remove private keys: %+v", got)
	}
}

func TestNotificationsInvalidClickURLRejected(t *testing.T) {
	ctx := context.Background()
	s := newNotificationSettings(t, config.Notifications{})

	for _, bad := range []string{
		"https://example.com/no-placeholder",
		"relative/e/{id}",
		"ftp://example.com/e/{id}",
	} {
		if err := s.SaveNotifications(ctx, config.Notifications{ClickURL: bad}, false, false); err == nil {
			t.Fatalf("invalid click URL %q accepted", bad)
		}
	}
	for _, good := range []string{
		"/e/{id}",
		"https://example.com/e/{id}",
	} {
		if err := s.SaveNotifications(ctx, config.Notifications{ClickURL: good}, false, false); err != nil {
			t.Fatalf("valid click URL %q rejected: %v", good, err)
		}
	}
}

func TestNotificationsEnvFallbackAndStoredDefaults(t *testing.T) {
	ctx := context.Background()
	env := config.Notifications{
		Enabled:       true,
		Realtime:      true,
		WebPush:       true,
		Android:       true,
		EntryCreated:  true,
		TitleTemplate: "Env title",
		BodyTemplate:  "Env body",
		ClickURL:      "/env/{id}",
	}
	s := newNotificationSettings(t, env)

	got := s.CurrentNotifications()
	if !got.Enabled || got.TitleTemplate != "Env title" || got.BodyTemplate != "Env body" || got.ClickURL != "/env/{id}" {
		t.Fatalf("env fallback = %+v", got)
	}

	// Storing an empty title/body/click URL must not bake in defaults; it lets
	// the env fallback apply again.
	if err := s.SaveNotifications(ctx, config.Notifications{Enabled: true}, false, false); err != nil {
		t.Fatalf("SaveNotifications: %v", err)
	}
	got = s.CurrentNotifications()
	if got.TitleTemplate != "Env title" || got.BodyTemplate != "Env body" || got.ClickURL != "/env/{id}" {
		t.Fatalf("blank values did not fall back to env: %+v", got)
	}

	overrides, err := s.OverridesNotifications(ctx)
	if err != nil {
		t.Fatalf("OverridesNotifications: %v", err)
	}
	if overrides.TitleTemplate != "" || overrides.BodyTemplate != "" || overrides.ClickURL != "" {
		t.Fatalf("defaults were baked into storage: %+v", overrides)
	}

	// Booleans round-trip through storage and override both env and defaults.
	if err := s.SaveNotifications(ctx, config.Notifications{
		Enabled: true, Realtime: false, WebPush: false, Android: true, EntryCreated: false,
	}, false, false); err != nil {
		t.Fatalf("SaveNotifications booleans: %v", err)
	}
	got = s.CurrentNotifications()
	if !got.Enabled || got.Realtime || got.WebPush || !got.Android || got.EntryCreated {
		t.Fatalf("stored booleans = %+v", got)
	}
	overrides, err = s.OverridesNotifications(ctx)
	if err != nil {
		t.Fatalf("OverridesNotifications booleans: %v", err)
	}
	if !overrides.Enabled || overrides.Realtime || overrides.WebPush || !overrides.Android || overrides.EntryCreated {
		t.Fatalf("override booleans = %+v", overrides)
	}
}
