package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/push"
	"vandyke.thor.edu/vandyke/internal/realtime"
	"vandyke.thor.edu/vandyke/internal/settings"
	"vandyke.thor.edu/vandyke/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestNotifier(t *testing.T) (*Notifier, *settings.Store, *store.Store) {
	t.Helper()
	ctx := t.Context()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	stg, err := settings.New(ctx, st, config.Umami{}, config.Webhook{}, config.Notifications{})
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	hub := realtime.NewHub()
	n := New(st, stg, hub, "https://fallback.example", discardLogger())
	return n, stg, st
}

func sampleEntry() store.Entry {
	return store.Entry{
		ID:        42,
		Word:      "definately",
		Meaning:   "definitely",
		Person:    "Alice",
		CreatedAt: time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC),
	}
}

func TestEntryCreatedDisabledSendsNothing(t *testing.T) {
	ctx := t.Context()
	n, stg, st := newTestNotifier(t)

	var requests atomic.Int64
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer receiver.Close()

	publicKey, privateKey, err := push.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys: %v", err)
	}
	if err := stg.SaveNotifications(ctx, config.Notifications{
		Enabled:         true,
		EntryCreated:    false,
		WebPush:         true,
		VAPIDSubject:    "mailto:test@example.com",
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
	}, false, false); err != nil {
		t.Fatalf("SaveNotifications: %v", err)
	}
	if _, err := st.UpsertWebPushSubscription(ctx, store.WebPushSubscription{
		Endpoint: receiver.URL,
		P256DH:   "p256dh",
		Auth:     "auth",
	}); err != nil {
		t.Fatalf("UpsertWebPushSubscription: %v", err)
	}

	n.EntryCreated(ctx, sampleEntry(), "https://req.example")
	time.Sleep(150 * time.Millisecond)

	if got := requests.Load(); got != 0 {
		t.Fatalf("EntryCreated sent %d requests, want 0 when entry-created notifications are off", got)
	}
}

func TestEntryCreatedPublishesRealtimeEvent(t *testing.T) {
	ctx := t.Context()
	n, stg, _ := newTestNotifier(t)

	if err := stg.SaveNotifications(ctx, config.Notifications{
		Enabled:       true,
		EntryCreated:  true,
		Realtime:      true,
		WebPush:       false,
		Android:       false,
		TitleTemplate: "{person}: {word}",
		BodyTemplate:  "{meaning} (#{id})",
		ClickURL:      "/e/{id}",
	}, false, false); err != nil {
		t.Fatalf("SaveNotifications: %v", err)
	}

	srv := httptest.NewServer(n.Hub())
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	frames := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var frame strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if frame.Len() > 0 {
					frames <- frame.String()
					return
				}
				continue
			}
			if strings.HasPrefix(line, "retry:") {
				continue
			}
			frame.WriteString(line)
			frame.WriteByte('\n')
		}
	}()

	// Give the handler a moment to register the subscriber.
	time.Sleep(50 * time.Millisecond)
	entry := sampleEntry()
	n.EntryCreated(ctx, entry, "https://req.example")

	select {
	case frame := <-frames:
		if !strings.Contains(frame, "event: entry.created") {
			t.Fatalf("frame = %q, want event name", frame)
		}
		data := frame[strings.Index(frame, "data: ")+len("data: "):]
		data = strings.TrimSpace(data)
		var payload struct {
			ID        int64  `json:"id"`
			Word      string `json:"word"`
			Meaning   string `json:"meaning"`
			Person    string `json:"person"`
			URL       string `json:"url"`
			CreatedAt string `json:"created_at"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatalf("decode event data %q: %v", data, err)
		}
		if payload.ID != entry.ID || payload.Word != entry.Word || payload.Meaning != entry.Meaning || payload.Person != entry.Person {
			t.Fatalf("payload = %+v, want entry fields", payload)
		}
		if payload.URL != "https://req.example/e/42" {
			t.Fatalf("url = %q, want absolute request-scoped URL", payload.URL)
		}
		if payload.CreatedAt != "2026-11-12T10:30:00Z" {
			t.Fatalf("created_at = %q, want RFC3339 UTC", payload.CreatedAt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for realtime event")
	}
}

func TestRenderAndClickURL(t *testing.T) {
	entry := sampleEntry()
	base := "https://ledger.example"

	title := render("{person}: {word}", entry, base)
	if title != "Alice: definately" {
		t.Fatalf("title = %q", title)
	}
	body := render("{meaning} #{id}", entry, base)
	if body != "definitely #42" {
		t.Fatalf("body = %q", body)
	}
	if got := clickURL(render("{base}/e/{id}", entry, base), base, entry.ID); got != "https://ledger.example/e/42" {
		t.Fatalf("clickURL absolute = %q", got)
	}
	if got := clickURL("", base, entry.ID); got != "https://ledger.example/e/42" {
		t.Fatalf("clickURL empty = %q", got)
	}
	if got := clickURL("/word/definately", base, entry.ID); got != "https://ledger.example/word/definately" {
		t.Fatalf("clickURL relative = %q", got)
	}
	if got := clickURL("https://elsewhere.example/x", base, entry.ID); got != "https://elsewhere.example/x" {
		t.Fatalf("clickURL foreign absolute = %q", got)
	}
}

func TestWebPushAllowedHonoursAdminPrefs(t *testing.T) {
	ctx := t.Context()
	n, _, st := newTestNotifier(t)

	admin, err := st.CreateAdmin(ctx, "prefs-admin", "Prefs Admin", "hash", store.RoleOwner)
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	if !n.webPushAllowed(ctx, nil) {
		t.Fatal("anonymous subscription should be allowed")
	}
	if !n.webPushAllowed(ctx, &admin.ID) {
		t.Fatal("admin without stored prefs should default to allowed")
	}

	if err := st.SaveAdminNotificationPrefs(ctx, store.AdminNotificationPrefs{
		AdminID:      admin.ID,
		WebPush:      false,
		EntryCreated: true,
	}); err != nil {
		t.Fatalf("SaveAdminNotificationPrefs: %v", err)
	}
	if n.webPushAllowed(ctx, &admin.ID) {
		t.Fatal("admin with web push disabled should be skipped")
	}

	if err := st.SaveAdminNotificationPrefs(ctx, store.AdminNotificationPrefs{
		AdminID:      admin.ID,
		WebPush:      true,
		EntryCreated: false,
	}); err != nil {
		t.Fatalf("SaveAdminNotificationPrefs: %v", err)
	}
	if n.webPushAllowed(ctx, &admin.ID) {
		t.Fatal("admin with entry-created notifications disabled should be skipped")
	}
}

func TestTestSendErrorsWhenDisabled(t *testing.T) {
	n, _, _ := newTestNotifier(t)
	sent, failed, err := n.TestSend(context.Background())
	if err == nil {
		t.Fatalf("TestSend = (%d, %d, nil), want an error when notifications are disabled", sent, failed)
	}
}

func TestTestSendErrorsWithNoChannels(t *testing.T) {
	ctx := t.Context()
	n, stg, _ := newTestNotifier(t)
	if err := stg.SaveNotifications(ctx, config.Notifications{
		Enabled:      true,
		EntryCreated: true,
		WebPush:      false,
		Android:      false,
	}, false, false); err != nil {
		t.Fatalf("SaveNotifications: %v", err)
	}
	if _, _, err := n.TestSend(ctx); err == nil {
		t.Fatal("TestSend should error when no channel is configured")
	}
}
