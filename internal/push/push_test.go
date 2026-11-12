package push

import (
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"
)

func staticToken(token string) oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
}

func TestFCMSendBuildsRequest(t *testing.T) {
	var (
		mu       sync.Mutex
		paths    []string
		auths    []string
		bodies   [][]byte
		requests int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests++
		paths = append(paths, r.URL.Path)
		auths = append(auths, r.Header.Get("Authorization"))
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/p/messages/1"}`))
	}))
	defer srv.Close()

	fcm := NewFCMWithTokenSource("demo-project", staticToken("test-token"), srv.Client(), srv.URL)

	n := Notification{
		Title: "New entry",
		Body:  "Alice posted",
		URL:   "https://example.test/entries/1",
		Tag:   "entry-1",
		Icon:  "/icons/app.png",
		Data:  map[string]string{"entry_id": "1"},
	}
	results := fcm.Send(context.Background(), []string{"token-a", "token-b"}, n)

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for i, res := range results {
		if res.Err != nil {
			t.Fatalf("result %d: unexpected error: %v", i, res.Err)
		}
		if res.Permanent {
			t.Fatalf("result %d: unexpectedly permanent", i)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Fatalf("server saw %d requests, want 2", requests)
	}
	if paths[0] != "/" {
		t.Errorf("path = %q, want %q", paths[0], "/")
	}
	if auths[0] != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", auths[0], "Bearer test-token")
	}

	var body struct {
		Message struct {
			Token        string `json:"token"`
			Notification struct {
				Title string `json:"title"`
				Body  string `json:"body"`
			} `json:"notification"`
			Data    map[string]string `json:"data"`
			Android struct {
				Priority     string `json:"priority"`
				Notification struct {
					Icon string `json:"icon"`
					Tag  string `json:"tag"`
				} `json:"notification"`
			} `json:"android"`
		} `json:"message"`
	}
	if err := json.Unmarshal(bodies[0], &body); err != nil {
		t.Fatalf("decode body: %v (%s)", err, bodies[0])
	}
	if body.Message.Token != "token-a" {
		t.Errorf("token = %q, want token-a", body.Message.Token)
	}
	if body.Message.Notification.Title != "New entry" {
		t.Errorf("title = %q", body.Message.Notification.Title)
	}
	if body.Message.Notification.Body != "Alice posted" {
		t.Errorf("body = %q", body.Message.Notification.Body)
	}
	if got := body.Message.Data["url"]; got != "https://example.test/entries/1" {
		t.Errorf("data.url = %q", got)
	}
	if got := body.Message.Data["tag"]; got != "entry-1" {
		t.Errorf("data.tag = %q", got)
	}
	if got := body.Message.Data["entry_id"]; got != "1" {
		t.Errorf("data.entry_id = %q", got)
	}
	if body.Message.Android.Priority != "high" {
		t.Errorf("android.priority = %q, want high", body.Message.Android.Priority)
	}
	// A relative icon resolves against the click-through URL.
	if got := body.Message.Android.Notification.Icon; got != "https://example.test/icons/app.png" {
		t.Errorf("android.notification.icon = %q", got)
	}
	if body.Message.Android.Notification.Tag != "entry-1" {
		t.Errorf("android.notification.tag = %q", body.Message.Android.Notification.Tag)
	}

	if string(bodies[0]) == "" || !strings.Contains(string(bodies[1]), "token-b") {
		t.Errorf("second request body missing token-b: %s", bodies[1])
	}
}

func TestFCMStatusClassification(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantErr   bool
		permanent bool
	}{
		{
			name:   "ok",
			status: http.StatusOK,
			body:   `{"name":"projects/p/messages/1"}`,
		},
		{
			name:      "not found",
			status:    http.StatusNotFound,
			body:      `{"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND"}}`,
			wantErr:   true,
			permanent: true,
		},
		{
			name:      "unregistered",
			status:    http.StatusBadRequest,
			body:      `{"error":{"code":400,"message":"Token is unregistered","status":"UNREGISTERED"}}`,
			wantErr:   true,
			permanent: true,
		},
		{
			name:      "invalid argument registration token",
			status:    http.StatusBadRequest,
			body:      `{"error":{"code":400,"message":"The registration token is not a valid FCM registration token","status":"INVALID_ARGUMENT"}}`,
			wantErr:   true,
			permanent: true,
		},
		{
			name:    "rate limited",
			status:  http.StatusTooManyRequests,
			body:    `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`,
			wantErr: true,
		},
		{
			name:    "server error",
			status:  http.StatusInternalServerError,
			body:    `{"error":{"code":500,"message":"Internal error","status":"INTERNAL"}}`,
			wantErr: true,
		},
		{
			name:    "unavailable",
			status:  http.StatusServiceUnavailable,
			body:    ``,
			wantErr: true,
		},
		{
			name:    "bad request without token hint",
			status:  http.StatusBadRequest,
			body:    `{"error":{"code":400,"message":"Invalid JSON payload","status":"INVALID_ARGUMENT"}}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			fcm := NewFCMWithTokenSource("p", staticToken("tok"), srv.Client(), srv.URL)
			const secretToken = "super-secret-device-token"
			results := fcm.Send(context.Background(), []string{secretToken, "second-token"}, Notification{Title: "t"})

			if len(results) != 2 {
				t.Fatalf("got %d results, want 2", len(results))
			}
			for i, res := range results {
				if tc.wantErr && res.Err == nil {
					t.Fatalf("result %d: expected error", i)
				}
				if !tc.wantErr && res.Err != nil {
					t.Fatalf("result %d: unexpected error: %v", i, res.Err)
				}
				if res.Permanent != tc.permanent {
					t.Errorf("result %d: Permanent = %v, want %v", i, res.Permanent, tc.permanent)
				}
				if res.Err != nil {
					if strings.Contains(res.Err.Error(), secretToken) {
						t.Errorf("result %d: error leaks token: %v", i, res.Err)
					}
					if strings.Contains(res.Err.Error(), "second-token") {
						t.Errorf("result %d: error leaks token: %v", i, res.Err)
					}
				}
				if res.Target != []string{secretToken, "second-token"}[i] {
					t.Errorf("result %d: Target = %q, want input order", i, res.Target)
				}
			}
		})
	}
}

func TestFCMRejectsMissingCredentials(t *testing.T) {
	if _, err := NewFCM(FCMConfig{}, nil); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := NewFCM(FCMConfig{ProjectID: "p"}, nil); err == nil {
		t.Fatal("expected error for missing email/key")
	}
	if _, err := NewFCM(FCMConfig{ProjectID: "p", ClientEmail: "x@y", PrivateKey: "not-a-pem"}, nil); err == nil {
		t.Fatal("expected error for unparsable private key")
	}
}

func TestGenerateVAPIDKeysAndNewWebPush(t *testing.T) {
	pub, priv, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys: %v", err)
	}
	if pub == "" || priv == "" {
		t.Fatal("GenerateVAPIDKeys returned empty key")
	}
	if pub == priv {
		t.Fatal("public and private keys are identical")
	}
	if _, err := base64.RawURLEncoding.DecodeString(pub); err != nil {
		t.Errorf("public key is not base64url: %v", err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(priv); err != nil {
		t.Errorf("private key is not base64url: %v", err)
	}

	pub2, _, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys (second): %v", err)
	}
	if pub == pub2 {
		t.Error("two generated public keys are identical")
	}

	wp, err := NewWebPush(WebPushConfig{
		Subject:    "mailto:admin@example.test",
		PublicKey:  pub,
		PrivateKey: priv,
	}, nil)
	if err != nil {
		t.Fatalf("NewWebPush: %v", err)
	}
	if wp == nil {
		t.Fatal("NewWebPush returned nil")
	}

	if _, err := NewWebPush(WebPushConfig{Subject: "mailto:a@b"}, nil); err == nil {
		t.Fatal("expected error for missing keys")
	}
	if _, err := NewWebPush(WebPushConfig{PublicKey: pub, PrivateKey: priv}, nil); err == nil {
		t.Fatal("expected error for missing subject")
	}
}

func TestMarshalPayload(t *testing.T) {
	raw, err := MarshalPayload(Notification{
		Title: "Hello",
		Body:  "World",
		URL:   "https://example.test/x",
		Tag:   "t1",
		Icon:  "/i.png",
		Data:  map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	for key, want := range map[string]string{
		"title": "Hello",
		"body":  "World",
		"url":   "https://example.test/x",
		"tag":   "t1",
		"icon":  "/i.png",
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %q", key, got[key], want)
		}
	}
	data, ok := got["data"].(map[string]any)
	if !ok || data["k"] != "v" {
		t.Errorf("data = %v, want {k:v}", got["data"])
	}
}

// testSubscriptionKeys builds a browser-style subscription with a real P-256
// key pair so the webpush library can encrypt to it.
func testSubscriptionKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pub := elliptic.Marshal(elliptic.P256(), x, y)
	authBytes := make([]byte, 16)
	if _, err := rand.Read(authBytes); err != nil {
		t.Fatalf("generate auth: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(pub), base64.RawURLEncoding.EncodeToString(authBytes)
}

func TestWebPushStatusClassification(t *testing.T) {
	pub, priv, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys: %v", err)
	}
	wp, err := NewWebPush(WebPushConfig{
		Subject:    "mailto:admin@example.test",
		PublicKey:  pub,
		PrivateKey: priv,
	}, nil)
	if err != nil {
		t.Fatalf("NewWebPush: %v", err)
	}

	tests := []struct {
		name      string
		status    int
		wantErr   bool
		permanent bool
	}{
		{name: "created", status: http.StatusCreated},
		{name: "gone", status: http.StatusGone, wantErr: true, permanent: true},
		{name: "not found", status: http.StatusNotFound, wantErr: true, permanent: true},
		{name: "server error", status: http.StatusInternalServerError, wantErr: true},
		{name: "unauthorized", status: http.StatusUnauthorized, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "vapid ") {
					t.Errorf("missing VAPID Authorization header, got %q", got)
				}
				if got := r.Header.Get("Ttl"); got != "86400" {
					t.Errorf("TTL header = %q, want 86400", got)
				}
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			wp.client = srv.Client()

			p256dh, auth := testSubscriptionKeys(t)
			sub := Subscription{Endpoint: srv.URL + "/push/abc", P256DH: p256dh, Auth: auth}
			results := wp.Send(context.Background(), []Subscription{sub}, Notification{
				Title: "hi", Body: "there", URL: "https://example.test/",
			})
			if len(results) != 1 {
				t.Fatalf("got %d results, want 1", len(results))
			}
			res := results[0]
			if tc.wantErr && res.Err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && res.Err != nil {
				t.Fatalf("unexpected error: %v", res.Err)
			}
			if res.Permanent != tc.permanent {
				t.Errorf("Permanent = %v, want %v", res.Permanent, tc.permanent)
			}
			if !res.OK() && res.Err == nil {
				t.Error("OK() disagrees with Err")
			}
		})
	}
}

func TestWebPushMultipleResultsOrder(t *testing.T) {
	pub, priv, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys: %v", err)
	}
	wp, err := NewWebPush(WebPushConfig{Subject: "mailto:a@b.test", PublicKey: pub, PrivateKey: priv}, nil)
	if err != nil {
		t.Fatalf("NewWebPush: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	wp.client = srv.Client()

	p256dh, auth := testSubscriptionKeys(t)
	subs := []Subscription{
		{Endpoint: srv.URL + "/a", P256DH: p256dh, Auth: auth},
		{Endpoint: srv.URL + "/b", P256DH: p256dh, Auth: auth},
		{Endpoint: srv.URL + "/c", P256DH: p256dh, Auth: auth},
	}
	results := wp.Send(context.Background(), subs, Notification{Title: "x"})
	if len(results) != len(subs) {
		t.Fatalf("got %d results, want %d", len(results), len(subs))
	}
	for i, res := range results {
		if res.Target != subs[i].Endpoint {
			t.Errorf("result %d Target = %q, want %q", i, res.Target, subs[i].Endpoint)
		}
		if res.Err != nil {
			t.Errorf("result %d: %v", i, res.Err)
		}
	}

	// A subscription without keys is a permanent failure, not a crash.
	bad := wp.Send(context.Background(), []Subscription{{Endpoint: srv.URL + "/d"}}, Notification{Title: "x"})
	if len(bad) != 1 || bad[0].Err == nil || !bad[0].Permanent {
		t.Errorf("missing-keys result = %+v, want permanent error", bad)
	}
}
