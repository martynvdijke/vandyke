package httpapi

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/notify"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/realtime"
	"vandyke.thor.edu/vandyke/internal/settings"
	"vandyke.thor.edu/vandyke/internal/store"
)

func newPushTestAPI(t *testing.T) (*httptest.Server, *store.Store, *settings.Store, *realtime.Hub) {
	t.Helper()
	ctx := t.Context()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "push.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	stg, err := settings.New(ctx, st, config.Umami{}, config.Webhook{}, config.Notifications{})
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	hub := realtime.NewHub()
	notifier := notify.New(st, stg, hub, "https://ledger.example", discardLogger())

	mux := http.NewServeMux()
	New(st, ratelimit.New(1000, 100), false, "", testEngine(t, st, config.Webhook{}),
		notifier, nil, "", discardLogger()).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st, stg, hub
}

func TestPushConfigHidesSecrets(t *testing.T) {
	ctx := t.Context()
	srv, _, stg, _ := newPushTestAPI(t)

	if err := stg.SaveNotifications(ctx, config.Notifications{
		Enabled:          true,
		Realtime:         true,
		WebPush:          true,
		Android:          true,
		EntryCreated:     true,
		VAPIDSubject:     "mailto:test@example.com",
		VAPIDPublicKey:   "PUBLIC-VAPID-KEY",
		VAPIDPrivateKey:  "SUPERSECRET-VAPID-PRIVATE",
		FCMProjectID:     "proj-123",
		FCMClientEmail:   "svc@example.com",
		FCMPrivateKey:    "SUPERSECRET-FCM-PRIVATE",
		FirebaseAPIKey:   "firebase-api-key",
		FirebaseAppID:    "firebase-app-id",
		FirebaseSenderID: "sender-123",
	}, false, false); err != nil {
		t.Fatalf("SaveNotifications: %v", err)
	}

	resp, err := http.Get(srv.URL + "/api/v1/push/config")
	if err != nil {
		t.Fatalf("GET config: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	for _, secret := range []string{"SUPERSECRET-VAPID-PRIVATE", "SUPERSECRET-FCM-PRIVATE", "svc@example.com"} {
		if strings.Contains(body, secret) {
			t.Fatalf("config body leaked %q: %s", secret, body)
		}
	}

	var got struct {
		Enabled        bool   `json:"enabled"`
		Realtime       bool   `json:"realtime"`
		WebPush        bool   `json:"web_push"`
		Android        bool   `json:"android"`
		VAPIDPublicKey string `json:"vapid_public_key"`
		Firebase       struct {
			APIKey            string `json:"api_key"`
			AppID             string `json:"app_id"`
			MessagingSenderID string `json:"messaging_sender_id"`
			ProjectID         string `json:"project_id"`
		} `json:"firebase"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !got.Enabled || !got.Realtime || !got.WebPush || !got.Android {
		t.Fatalf("enabled flags = %+v", got)
	}
	if got.VAPIDPublicKey != "PUBLIC-VAPID-KEY" {
		t.Fatalf("vapid_public_key = %q", got.VAPIDPublicKey)
	}
	if got.Firebase.APIKey != "firebase-api-key" ||
		got.Firebase.AppID != "firebase-app-id" ||
		got.Firebase.MessagingSenderID != "sender-123" ||
		got.Firebase.ProjectID != "proj-123" {
		t.Fatalf("firebase = %+v", got.Firebase)
	}
}

func TestPushDeviceUpsertAndInvalidPlatform(t *testing.T) {
	ctx := t.Context()
	srv, st, _, _ := newPushTestAPI(t)

	body := `{"token":"fcm-token-1","platform":"android","device_name":"Pixel","app_version":"1.2.3"}`
	resp, err := http.Post(srv.URL+"/api/v1/push/devices", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST device: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d (%s), want 201", resp.StatusCode, raw)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID < 1 {
		t.Fatalf("id = %d, want positive", created.ID)
	}

	devices, err := st.ListActivePushDevices(ctx)
	if err != nil {
		t.Fatalf("ListActivePushDevices: %v", err)
	}
	if len(devices) != 1 || devices[0].Token != "fcm-token-1" || devices[0].DeviceName != "Pixel" {
		t.Fatalf("devices = %+v", devices)
	}

	bad := `{"token":"fcm-token-2","platform":"ios"}`
	resp2, err := http.Post(srv.URL+"/api/v1/push/devices", "application/json", strings.NewReader(bad))
	if err != nil {
		t.Fatalf("POST invalid device: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a non-android platform", resp2.StatusCode)
	}
}

func TestWebPushSubscriptionUpsertAndDelete(t *testing.T) {
	ctx := t.Context()
	srv, st, _, _ := newPushTestAPI(t)

	body := `{"endpoint":"https://push.example/sub/1","keys":{"p256dh":"pkey","auth":"akey"}}`
	resp, err := http.Post(srv.URL+"/api/v1/push/subscriptions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST subscription: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d (%s), want 201", resp.StatusCode, raw)
	}

	subs, err := st.ListActiveWebPushSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListActiveWebPushSubscriptions: %v", err)
	}
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example/sub/1" || subs[0].P256DH != "pkey" {
		t.Fatalf("subs = %+v", subs)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/push/subscriptions",
		strings.NewReader(`{"endpoint":"https://push.example/sub/1"}`))
	if err != nil {
		t.Fatalf("build DELETE: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	del, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE subscription: %v", err)
	}
	defer func() { _ = del.Body.Close() }()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", del.StatusCode)
	}
	subs, err = st.ListActiveWebPushSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListActiveWebPushSubscriptions after delete: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("subs after delete = %+v, want empty", subs)
	}
}

func TestEventsStreamsPublishedEvent(t *testing.T) {
	srv, _, _, hub := newPushTestAPI(t)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	type result struct {
		frame string
		err   error
	}
	frames := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var frame strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if frame.Len() > 0 {
					frames <- result{frame: frame.String()}
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
		frames <- result{err: scanner.Err()}
	}()

	// The handler registers its subscriber after writing the retry hint; a
	// short pause keeps the publish from racing the registration.
	time.Sleep(50 * time.Millisecond)
	hub.Publish(realtime.Event{Name: "entry.created", Data: []byte(`{"id":1}`)})

	select {
	case got := <-frames:
		if got.err != nil {
			t.Fatalf("read stream: %v", got.err)
		}
		if !strings.Contains(got.frame, "event: entry.created") || !strings.Contains(got.frame, `data: {"id":1}`) {
			t.Fatalf("frame = %q", got.frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an SSE frame")
	}
}

func TestPushConfigDisabledWhenMasterOff(t *testing.T) {
	srv, _, _, _ := newPushTestAPI(t)
	resp, err := http.Get(srv.URL + "/api/v1/push/config")
	if err != nil {
		t.Fatalf("GET config: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var got struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enabled {
		t.Fatal("enabled = true, want false when the master switch is off")
	}
}
