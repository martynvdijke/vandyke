package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

func jsonKeys(t *testing.T, body []byte) []string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertKeys(t *testing.T, body []byte, want ...string) {
	t.Helper()
	sort.Strings(want)
	got := jsonKeys(t, body)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keys = %v, want %v", got, want)
	}
}

func TestCreatedPayloadIsFrozenAtSevenKeys(t *testing.T) {
	payload := BuildPayload(testEntry(), "https://ledger.example/e/123")
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertKeys(t, body, "event", "id", "word", "meaning", "person", "created_at", "url")

	var decoded Payload
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Event != EventEntryCreated || decoded.ID != 123 || decoded.Person != "M. van Dijke" ||
		decoded.CreatedAt != "2024-05-06T07:08:09Z" || decoded.URL != "https://ledger.example/e/123" {
		t.Fatalf("payload = %+v", decoded)
	}
}

func TestNewEventPayloadFields(t *testing.T) {
	deleted := time.Date(2024, 5, 7, 0, 0, 0, 0, time.UTC)
	trashed := testEntry()
	trashed.DeletedAt = &deleted

	deletedBody, err := json.Marshal(buildPayload(Change{Event: EventEntryDeleted, Entry: trashed},
		"https://ledger.example", time.Date(2024, 5, 8, 9, 10, 11, 0, time.UTC)))
	if err != nil {
		t.Fatalf("marshal deleted: %v", err)
	}
	assertKeys(t, deletedBody, "event", "id", "word", "meaning", "person", "created_at", "url", "occurred_at", "deleted_at")

	restoredBody, err := json.Marshal(buildPayload(Change{Event: EventEntryRestored, Entry: testEntry()},
		"https://ledger.example", time.Now()))
	if err != nil {
		t.Fatalf("marshal restored: %v", err)
	}
	assertKeys(t, restoredBody, "event", "id", "word", "meaning", "person", "created_at", "url", "occurred_at")

	updatedBody, err := json.Marshal(buildPayload(Change{
		Event: EventEntryUpdated,
		Entry: testEntry(),
		Changes: map[string]FieldChange{
			"meaning": {From: "old", To: "new"},
			"photo":   {From: nil, To: "abcdef123456"},
		},
	}, "https://ledger.example", time.Now()))
	if err != nil {
		t.Fatalf("marshal updated: %v", err)
	}
	assertKeys(t, updatedBody, "event", "id", "word", "meaning", "person", "created_at", "url", "occurred_at", "changes")

	// Editing a trashed entry adds deleted_at so a receiver can tell it apart
	// from a live edit.
	trashedUpdated := testEntry()
	trashedUpdated.DeletedAt = &deleted
	trashedBody, err := json.Marshal(buildPayload(Change{
		Event: EventEntryUpdated, Entry: trashedUpdated,
		Changes: map[string]FieldChange{"meaning": {From: "a", To: "b"}},
	}, "https://ledger.example", time.Now()))
	if err != nil {
		t.Fatalf("marshal trashed update: %v", err)
	}
	assertKeys(t, trashedBody, "event", "id", "word", "meaning", "person", "created_at", "url", "occurred_at", "deleted_at", "changes")
}

func TestGenericRenderSendsEnvelope(t *testing.T) {
	body, err := Render(store.WebhookPresetGeneric, BuildPayload(testEntry(), "https://ledger.example/e/123"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Event != EventEntryCreated || payload.Word != "feuten" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestDiscordPresetShape(t *testing.T) {
	body, err := Render(store.WebhookPresetDiscord, BuildPayload(testEntry(), "https://ledger.example/e/123"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var decoded struct {
		Content string `json:"content"`
		Embeds  []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			URL         string `json:"url"`
			Timestamp   string `json:"timestamp"`
		} `json:"embeds"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(decoded.Content, EventEntryCreated) {
		t.Fatalf("content = %q, want the event name", decoded.Content)
	}
	if len(decoded.Embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(decoded.Embeds))
	}
	embed := decoded.Embeds[0]
	if embed.Title != "feuten" || embed.Description != "feature" ||
		embed.URL != "https://ledger.example/e/123" || embed.Timestamp != "2024-05-06T07:08:09Z" {
		t.Fatalf("embed = %+v", embed)
	}
	if decoded.AllowedMentions.Parse == nil || len(decoded.AllowedMentions.Parse) != 0 {
		t.Fatalf("allowed_mentions.parse = %#v, want an empty array", decoded.AllowedMentions.Parse)
	}
}

func TestDiscordPhotoOnlyFallbacks(t *testing.T) {
	entry := testEntry()
	entry.Word = ""
	entry.Meaning = ""
	body, err := Render(store.WebhookPresetDiscord, BuildPayload(entry, "https://ledger.example/e/123"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var decoded struct {
		Embeds []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"embeds"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Embeds[0].Title == "" || decoded.Embeds[0].Description == "" {
		t.Fatalf("empty fallbacks: %+v", decoded.Embeds[0])
	}
	if !strings.Contains(decoded.Embeds[0].Title, "123") {
		t.Fatalf("title = %q, want the entry id", decoded.Embeds[0].Title)
	}
}

func TestDiscordMentionsCannotNotify(t *testing.T) {
	entry := testEntry()
	entry.Word = "@everyone"
	entry.Meaning = "<@&12345>"
	body, err := Render(store.WebhookPresetDiscord, BuildPayload(entry, "https://ledger.example/e/1"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(decoded["allowed_mentions"]) != `{"parse":[]}` {
		t.Fatalf("allowed_mentions = %s", decoded["allowed_mentions"])
	}
}

func TestSlackPresetShapeAndEscaping(t *testing.T) {
	entry := testEntry()
	entry.Word = "<!channel>"
	entry.Meaning = "a & b"
	body, err := Render(store.WebhookPresetSlack, BuildPayload(entry, "https://ledger.example/e/123"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var decoded struct {
		Text   string `json:"text"`
		Blocks []struct {
			Type string `json:"type"`
			Text struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"text"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Text == "" || !strings.Contains(decoded.Text, EventEntryCreated) {
		t.Fatalf("text = %q", decoded.Text)
	}
	if len(decoded.Blocks) != 1 || decoded.Blocks[0].Type != "section" || decoded.Blocks[0].Text.Type != "mrkdwn" {
		t.Fatalf("blocks = %+v", decoded.Blocks)
	}
	block := decoded.Blocks[0].Text.Text
	if strings.Contains(block, "<!channel>") {
		t.Fatalf("mrkdwn still contains an active mention: %q", block)
	}
	if !strings.Contains(block, "&lt;!channel&gt;") || !strings.Contains(block, "a &amp; b") {
		t.Fatalf("escaped block = %q", block)
	}
}

func TestSlackPhotoOnlyFallbacks(t *testing.T) {
	entry := testEntry()
	entry.Word = ""
	entry.Meaning = ""
	body, err := Render(store.WebhookPresetSlack, BuildPayload(entry, "https://ledger.example/e/123"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(body), "123") || !strings.Contains(string(body), "https://ledger.example/e/123") {
		t.Fatalf("fallback body = %s", body)
	}
}

func TestSignatureCoversRenderedDiscordBody(t *testing.T) {
	body, err := Render(store.WebhookPresetDiscord, BuildPayload(testEntry(), "https://ledger.example/e/123"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	signature := Signature("topsecret", body)
	mac := hmac.New(sha256.New, []byte("topsecret"))
	_, _ = mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if signature != want {
		t.Fatalf("signature = %q, want %q", signature, want)
	}
	if !strings.HasPrefix(signature, "sha256=") || signature != strings.ToLower(signature) {
		t.Fatalf("signature shape = %q", signature)
	}
}

func TestRenderUnknownPreset(t *testing.T) {
	if _, err := Render("teams", Payload{}); err == nil {
		t.Fatal("Render accepted an unknown preset")
	}
}

func TestBackoffBounds(t *testing.T) {
	zero := func() float64 { return 0 }
	one := func() float64 { return 1 }
	for attempt := 1; attempt <= 12; attempt++ {
		limit := 5 * time.Second
		for i := 1; i < attempt; i++ {
			if limit >= time.Hour {
				break
			}
			limit *= 2
		}
		if limit > time.Hour {
			limit = time.Hour
		}
		if got := Backoff(attempt, zero); got != 0 {
			t.Fatalf("Backoff(%d, 0) = %v, want 0", attempt, got)
		}
		if got := Backoff(attempt, one); got != limit {
			t.Fatalf("Backoff(%d, 1) = %v, want %v", attempt, got, limit)
		}
		if got := Backoff(attempt, func() float64 { return 0.5 }); got != limit/2 {
			t.Fatalf("Backoff(%d, 0.5) = %v, want %v", attempt, got, limit/2)
		}
	}
	if Backoff(0, one) != 5*time.Second {
		t.Fatalf("Backoff(0) = %v, want the base delay", Backoff(0, one))
	}
}

func TestIsAddressAllowed(t *testing.T) {
	tests := []struct {
		address      string
		allowPrivate bool
		want         bool
	}{
		{"8.8.8.8", false, true},
		{"8.8.8.8", true, true},
		{"127.0.0.1", false, false},
		{"127.0.0.1", true, true},
		{"10.0.0.5", false, false},
		{"10.0.0.5", true, true},
		{"192.168.1.20", false, false},
		{"192.168.1.20", true, true},
		{"172.16.0.1", false, false},
		{"100.64.0.1", false, false},
		{"100.64.0.1", true, true},
		{"169.254.169.254", false, false},
		{"169.254.169.254", true, false},
		{"0.0.0.0", true, false},
		{"224.0.0.1", true, false},
		{"::1", false, false},
		{"::1", true, true},
		{"fc00::1", false, false},
		{"fc00::1", true, true},
		{"fe80::1", true, false},
		{"::ffff:10.0.0.5", false, false},
		{"::ffff:10.0.0.5", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.address, func(t *testing.T) {
			ip := parseIP(t, tc.address)
			if got := IsAddressAllowed(ip, tc.allowPrivate); got != tc.want {
				t.Fatalf("IsAddressAllowed(%s, %v) = %v, want %v", tc.address, tc.allowPrivate, got, tc.want)
			}
		})
	}
}

func parseIP(t *testing.T, raw string) net.IP {
	t.Helper()
	ip := net.ParseIP(raw)
	if ip == nil {
		t.Fatalf("parse IP %q", raw)
	}
	return ip
}

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		allowPrivate bool
		wantErr      bool
	}{
		{"file", "file:///etc/passwd", false, true},
		{"userinfo", "https://user:pass@example.com/hook", false, true},
		{"no host", "https:///hook", false, true},
		{"loopback denied", "http://127.0.0.1:8080/hook", false, true},
		{"loopback allowed", "http://127.0.0.1:8080/hook", true, false},
		{"private denied", "http://10.0.0.5/hook", false, true},
		{"private allowed", "http://10.0.0.5/hook", true, false},
		{"link local always denied", "http://169.254.169.254/latest/meta-data", true, true},
		{"hostname resolving private", "http://localhost:8080/hook", false, true},
		{"hostname resolving private allowed", "http://localhost:8080/hook", true, false},
		{"public host", "https://example.com/hook", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTarget(context.Background(), tc.raw, tc.allowPrivate)
			if tc.wantErr && err == nil {
				t.Fatal("ValidateTarget accepted a denied target")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateTarget = %v", err)
			}
		})
	}
}

// fakeMetrics records engine counters without the exposition registry.
type fakeMetrics struct {
	mu         sync.Mutex
	dropped    int
	deliveries []string
	attempts   []string
}

func (f *fakeMetrics) IncWebhookDropped() {
	f.mu.Lock()
	f.dropped++
	f.mu.Unlock()
}

func (f *fakeMetrics) ObserveWebhookAttempt(result string) {
	f.mu.Lock()
	f.attempts = append(f.attempts, result)
	f.mu.Unlock()
}

func (f *fakeMetrics) ObserveWebhookDelivery(result string) {
	f.mu.Lock()
	f.deliveries = append(f.deliveries, result)
	f.mu.Unlock()
}

func (f *fakeMetrics) snapshot() (int, []string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dropped, append([]string(nil), f.deliveries...), append([]string(nil), f.attempts...)
}

func newEngine(t *testing.T, st *store.Store, legacy config.Webhook, metrics Metrics) *Engine {
	t.Helper()
	engine := NewEngine(st, func() config.Webhook { return legacy }, "", metrics, discardLogger())
	engine.jitter = func() float64 { return 0 }
	engine.Start()
	t.Cleanup(engine.Close)
	return engine
}

func waitFor(t *testing.T, timeout time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestEngineLegacyDeliversSignedCreatedPayload(t *testing.T) {
	st := newTestStore(t)
	body := make(chan []byte, 1)
	header := make(chan http.Header, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body <- data
		header <- r.Header.Clone()
		rw.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	engine := newEngine(t, st, config.Webhook{URL: receiver.URL, Secret: "topsecret"}, nil)
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})

	select {
	case data := <-body:
		assertKeys(t, data, "event", "id", "word", "meaning", "person", "created_at", "url")
		var payload Payload
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if payload.Event != EventEntryCreated || payload.URL != "https://ledger.example/e/123" {
			t.Fatalf("payload = %+v", payload)
		}
		h := <-header
		if h.Get("User-Agent") != userAgent || h.Get("Content-Type") != "application/json" {
			t.Fatalf("headers = %v", h)
		}
		mac := hmac.New(sha256.New, []byte("topsecret"))
		_, _ = mac.Write(data)
		if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); h.Get("X-VanDyke-Signature") != want {
			t.Fatalf("signature = %q, want %q", h.Get("X-VanDyke-Signature"), want)
		}
		waitFor(t, 3*time.Second, "delivery row", func() bool {
			page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
			return err == nil && page.Total == 1 && page.Deliveries[0].Status == store.WebhookStatusDelivered &&
				page.Deliveries[0].EndpointID == nil
		})
	case <-time.After(3 * time.Second):
		t.Fatal("legacy target received no delivery")
	}
}

func TestEngineRetries500ThenDelivers(t *testing.T) {
	st := newTestStore(t)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	engine := newEngine(t, st, config.Webhook{URL: receiver.URL}, nil)
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})

	waitFor(t, 3*time.Second, "first failed attempt", func() bool {
		page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
		return err == nil && len(page.Deliveries) == 1 && page.Deliveries[0].Attempts == 1 &&
			page.Deliveries[0].Status == store.WebhookStatusPending
	})
	engine.claimDue()
	waitFor(t, 3*time.Second, "delivered retry", func() bool {
		page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
		return err == nil && len(page.Deliveries) == 1 && page.Deliveries[0].Status == store.WebhookStatusDelivered &&
			page.Deliveries[0].Attempts == 2 && page.Deliveries[0].LastStatus != nil &&
			*page.Deliveries[0].LastStatus == http.StatusNoContent && page.Deliveries[0].Payload == ""
	})
	if got := calls.Load(); got != 2 {
		t.Fatalf("receiver calls = %d, want 2", got)
	}
}

func TestEnginePermanent404IsTerminal(t *testing.T) {
	st := newTestStore(t)
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}))
	defer receiver.Close()

	engine := newEngine(t, st, config.Webhook{URL: receiver.URL}, nil)
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})

	waitFor(t, 3*time.Second, "permanent failure", func() bool {
		page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
		return err == nil && len(page.Deliveries) == 1 && page.Deliveries[0].Status == store.WebhookStatusFailed &&
			page.Deliveries[0].Attempts == 1 && page.Deliveries[0].NextAttemptAt == nil
	})
	// A subsequent claim finds nothing due.
	engine.claimDue()
	time.Sleep(100 * time.Millisecond)
	page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
	if err != nil || page.Deliveries[0].Attempts != 1 {
		t.Fatalf("permanent failure was retried: %+v (%v)", page.Deliveries, err)
	}
}

func TestEngineStopsAfterEightAttempts(t *testing.T) {
	st := newTestStore(t)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer receiver.Close()

	engine := newEngine(t, st, config.Webhook{URL: receiver.URL}, nil)
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})

	waitFor(t, 10*time.Second, "eight attempts", func() bool {
		page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
		if err != nil || len(page.Deliveries) != 1 {
			return false
		}
		delivery := page.Deliveries[0]
		if delivery.Status == store.WebhookStatusPending {
			engine.claimDue()
		}
		return delivery.Status == store.WebhookStatusFailed && delivery.Attempts == store.WebhookMaxAttempts
	})
	if got := calls.Load(); got != store.WebhookMaxAttempts {
		t.Fatalf("receiver calls = %d, want %d", got, store.WebhookMaxAttempts)
	}
}

func TestEngineDoesNotFollowRedirects(t *testing.T) {
	st := newTestStore(t)
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	var redirectHits atomic.Int32
	redirect := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		redirectHits.Add(1)
		http.Redirect(rw, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	engine := newEngine(t, st, config.Webhook{URL: redirect.URL}, nil)
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})

	waitFor(t, 3*time.Second, "permanent redirect failure", func() bool {
		page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
		return err == nil && len(page.Deliveries) == 1 && page.Deliveries[0].Status == store.WebhookStatusFailed &&
			page.Deliveries[0].LastStatus != nil && *page.Deliveries[0].LastStatus == http.StatusFound
	})
	if redirectHits.Load() != 1 {
		t.Fatalf("redirect receiver hits = %d, want 1", redirectHits.Load())
	}
	if targetHits.Load() != 0 {
		t.Fatalf("redirect target was followed %d times", targetHits.Load())
	}
}

func TestEngineQueueFullDropsAndCounts(t *testing.T) {
	st := newTestStore(t)
	recorder := &fakeMetrics{}
	engine := NewEngine(st, func() config.Webhook { return config.Webhook{} }, "", recorder, discardLogger())
	// Install an endpoint snapshot without starting workers so the queue fills.
	engine.snapshot.Store(&snapshot{
		endpoints:  []Endpoint{{ID: 1, Name: "one", URL: "https://example.com", Preset: store.WebhookPresetGeneric, Events: map[string]bool{EventEntryCreated: true}}},
		hasEnabled: true,
	})
	for i := 0; i < queueCapacity+5; i++ {
		engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})
	}
	if got := engine.QueueDepth(); got != queueCapacity {
		t.Fatalf("queue depth = %d, want %d", got, queueCapacity)
	}
	dropped, _, _ := recorder.snapshot()
	if dropped != 5 {
		t.Fatalf("dropped = %d, want 5", dropped)
	}
}

func TestEngineEmitDoesNotBlockOnSlowReceiver(t *testing.T) {
	st := newTestStore(t)
	release := make(chan struct{})
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		<-release
		rw.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()
	defer close(release)

	engine := newEngine(t, st, config.Webhook{URL: receiver.URL}, nil)
	start := time.Now()
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Emit blocked for %v", elapsed)
	}
}

func TestEngineDisabledEndpointCancelsPending(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	ep, err := st.CreateWebhookEndpoint(ctx, store.WebhookEndpoint{
		Name: "Disabled", URL: "https://example.com/hook", Secret: "", Enabled: true,
		Events: []string{store.WebhookEventCreated}, Preset: store.WebhookPresetGeneric,
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	now := time.Now().UTC()
	entryID := int64(1)
	delivery, err := st.CreateWebhookDelivery(ctx, store.WebhookDelivery{
		EndpointID: &ep.ID, Event: store.WebhookEventCreated, EntryID: &entryID,
		Status: store.WebhookStatusPending, NextAttemptAt: &now, Payload: `{"event":"entry.created","id":1}`,
	})
	if err != nil {
		t.Fatalf("create delivery: %v", err)
	}
	if _, err := st.SetWebhookEndpointEnabled(ctx, ep.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	engine := newEngine(t, st, config.Webhook{}, nil)
	engine.claimDue()
	waitFor(t, 2*time.Second, "cancelled delivery", func() bool {
		got, err := st.GetWebhookDelivery(ctx, delivery.ID)
		return err == nil && got.Status == store.WebhookStatusCancelled
	})
}

func TestEngineDiscardsJobForDeletedEndpoint(t *testing.T) {
	st := newTestStore(t)
	engine := newEngine(t, st, config.Webhook{}, nil)
	engine.handleJob(job{
		endpoint: Endpoint{ID: 999999, Name: "gone", URL: "https://example.com", Preset: store.WebhookPresetGeneric,
			Events: map[string]bool{EventEntryCreated: true}},
		change: Change{Event: EventEntryCreated, Entry: testEntry()},
		base:   "https://ledger.example",
	})
	page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
	if err != nil {
		t.Fatalf("ListWebhookDeliveries: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("deleted endpoint wrote %d delivery rows", page.Total)
	}
}

func TestEnginePersistedEndpointSuppressesLegacy(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	legacyHits := make(chan struct{}, 4)
	legacy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		legacyHits <- struct{}{}
		rw.WriteHeader(http.StatusOK)
	}))
	defer legacy.Close()
	persistedHits := make(chan struct{}, 4)
	persisted := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		persistedHits <- struct{}{}
		rw.WriteHeader(http.StatusOK)
	}))
	defer persisted.Close()

	engine := newEngine(t, st, config.Webhook{URL: legacy.URL}, nil)
	// Before a persisted endpoint exists the legacy target is active.
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})
	select {
	case <-legacyHits:
	case <-time.After(3 * time.Second):
		t.Fatal("legacy target did not receive the first event")
	}

	ep, err := st.CreateWebhookEndpoint(ctx, store.WebhookEndpoint{
		Name: "Persisted", URL: persisted.URL, Events: []string{store.WebhookEventCreated},
		Preset: store.WebhookPresetGeneric, AllowPrivate: true,
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})
	select {
	case <-persistedHits:
	case <-time.After(3 * time.Second):
		t.Fatal("persisted endpoint received no event")
	}
	select {
	case <-legacyHits:
		t.Fatal("legacy target was not suppressed by the persisted endpoint")
	case <-time.After(150 * time.Millisecond):
	}

	// Disabling the last persisted endpoint reactivates the legacy target.
	if _, err := st.SetWebhookEndpointEnabled(ctx, ep.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	engine.Emit(Change{Event: EventEntryCreated, Entry: testEntry(), BaseURL: "https://ledger.example"})
	select {
	case <-legacyHits:
	case <-time.After(3 * time.Second):
		t.Fatal("legacy target did not reactivate after the last endpoint was disabled")
	}
}

func TestEngineEditedSubscriptionTakesEffect(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	hits := make(chan struct{}, 4)
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		hits <- struct{}{}
		rw.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	ep, err := st.CreateWebhookEndpoint(ctx, store.WebhookEndpoint{
		Name: "Subscribe", URL: receiver.URL, Events: []string{store.WebhookEventCreated},
		Preset: store.WebhookPresetGeneric, AllowPrivate: true,
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	engine := newEngine(t, st, config.Webhook{}, nil)

	engine.Emit(Change{Event: EventEntryUpdated, Entry: testEntry(), BaseURL: "https://ledger.example"})
	select {
	case <-hits:
		t.Fatal("endpoint received an event it was not subscribed to")
	case <-time.After(150 * time.Millisecond):
	}

	ep.Events = []string{store.WebhookEventUpdated}
	if _, err := st.UpdateWebhookEndpoint(ctx, ep); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	engine.Emit(Change{Event: EventEntryUpdated, Entry: testEntry(), BaseURL: "https://ledger.example"})
	select {
	case <-hits:
	case <-time.After(3 * time.Second):
		t.Fatal("edited subscription did not take effect")
	}
}

func TestEngineFailedReloadKeepsSnapshot(t *testing.T) {
	st := newTestStore(t)
	legacy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer legacy.Close()
	engine := NewEngine(st, func() config.Webhook { return config.Webhook{URL: legacy.URL} }, "", nil, discardLogger())
	engine.Start()
	t.Cleanup(engine.Close)
	before := engine.snapshot.Load()
	if before == nil {
		t.Fatal("snapshot was not loaded at Start")
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := engine.Reload(context.Background()); err == nil {
		t.Fatal("Reload against a closed store returned no error")
	}
	if after := engine.snapshot.Load(); after != before {
		t.Fatal("a failed reload replaced the previous snapshot")
	}
}

func TestEngineSendTestUsesPresetAndSignature(t *testing.T) {
	st := newTestStore(t)
	body := make(chan []byte, 1)
	signature := make(chan string, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body <- data
		signature <- r.Header.Get("X-VanDyke-Signature")
		rw.WriteHeader(http.StatusAccepted)
	}))
	defer receiver.Close()

	engine := newEngine(t, st, config.Webhook{}, nil)
	endpoint := Endpoint{ID: 1, URL: receiver.URL, Secret: "s3cret", Preset: store.WebhookPresetDiscord, AllowPrivate: true}
	status, err := engine.SendTest(context.Background(), endpoint,
		BuildPayload(testEntry(), "https://ledger.example/e/123"))
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("SendTest = %d (%v)", status, err)
	}
	data := <-body
	var decoded struct {
		Embeds []json.RawMessage `json:"embeds"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Embeds) != 1 {
		t.Fatalf("discord body = %s (%v)", data, err)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	_, _ = mac.Write(data)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); <-signature != want {
		t.Fatalf("signature did not cover the rendered body")
	}
}

func TestEngineSendTestRejectsLinkLocal(t *testing.T) {
	st := newTestStore(t)
	engine := newEngine(t, st, config.Webhook{}, nil)
	status, err := engine.SendTest(context.Background(), Endpoint{
		URL: "http://169.254.169.254/latest/meta-data", Preset: store.WebhookPresetGeneric, AllowPrivate: true,
	}, Payload{})
	if err == nil || status != 0 {
		t.Fatalf("SendTest to a metadata address = %d (%v), want a target error", status, err)
	}
	if !strings.Contains(SanitiseError(err, "http://169.254.169.254/x"), "169.254.169.254") {
		t.Fatalf("sanitised error = %q", SanitiseError(err, "http://169.254.169.254/x"))
	}
}

func TestSanitiseErrorHidesPathAndToken(t *testing.T) {
	raw := "https://discord.com/api/webhooks/123/topsecret?wait=true"
	err := &urlErrorStub{}
	if got := SanitiseError(err, raw); got != "discord.com: request failed" {
		t.Fatalf("SanitiseError = %q", got)
	}
}

// urlErrorStub stands in for a transport error that may carry the full URL.
type urlErrorStub struct{}

func (*urlErrorStub) Error() string {
	return "https://discord.com/api/webhooks/123/topsecret?wait=true"
}

func TestRenderDoesNotLeakOnUnknownPreset(t *testing.T) {
	_, err := Render("unknown", Payload{URL: "https://discord.com/api/webhooks/1/token"})
	if err == nil || strings.Contains(err.Error(), "token") {
		t.Fatalf("render error = %v", err)
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "webhook.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
