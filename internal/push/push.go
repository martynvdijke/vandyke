// Package push delivers notifications to Android devices through Firebase
// Cloud Messaging (HTTP v1) and to browsers through standards Web Push with
// VAPID.
//
// The package performs no retries: callers decide when a failure is worth
// another attempt. Result.Permanent distinguishes "this registration is gone
// and must be deleted" from an ordinary, usually transient, error.
package push

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// fcmScope is the only OAuth2 scope FCM HTTP v1 needs.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

const (
	fcmEndpointFormat = "https://fcm.googleapis.com/v1/projects/%s/messages:send"
	fcmTokenURI       = "https://oauth2.googleapis.com/token"
	defaultPushTTL    = 86400
)

// Notification is a single push message. The same value is rendered by both
// transports; each maps it to its own wire format.
type Notification struct {
	Title string
	Body  string
	URL   string // click-through URL
	Tag   string // collapses/replaces on web; also sent as data on Android
	Icon  string
	Data  map[string]string
}

// Result reports the outcome of a delivery attempt to one target.
type Result struct {
	Target    string // FCM token or web-push endpoint
	Err       error
	Permanent bool // true => caller must delete the registration (404/410/UNREGISTERED)
}

// OK reports whether the delivery succeeded.
func (r Result) OK() bool { return r.Err == nil }

// FCMConfig holds the service-account fields needed to mint OAuth2 tokens.
type FCMConfig struct {
	ProjectID   string
	ClientEmail string
	PrivateKey  string // PEM service-account private key
}

// FCM sends notifications through Firebase Cloud Messaging HTTP v1.
type FCM struct {
	projectID string
	tokens    oauth2.TokenSource
	client    *http.Client
	endpoint  string
}

// NewFCM builds an FCM sender that mints OAuth2 access tokens from the
// service-account JSON fields. It returns an error if the credentials cannot
// be parsed. httpClient may be nil (http.DefaultClient).
func NewFCM(cfg FCMConfig, httpClient *http.Client) (*FCM, error) {
	if cfg.ProjectID == "" {
		return nil, errors.New("push: FCM project ID is required")
	}
	if cfg.ClientEmail == "" || cfg.PrivateKey == "" {
		return nil, errors.New("push: FCM client email and private key are required")
	}

	payload, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   cfg.ProjectID,
		"private_key":  cfg.PrivateKey,
		"client_email": cfg.ClientEmail,
		"token_uri":    fcmTokenURI,
	})
	if err != nil {
		return nil, fmt.Errorf("push: marshal FCM credentials: %w", err)
	}

	// google.CredentialsFromJSON is lazy about the key material, so validate
	// the PEM here: a malformed key must fail at construction, not at the
	// first send.
	if err := validateServiceAccountKey(cfg.PrivateKey); err != nil {
		return nil, err
	}

	creds, err := google.CredentialsFromJSON(context.Background(), payload, fcmScope)
	if err != nil {
		return nil, fmt.Errorf("push: parse FCM credentials: %w", err)
	}

	projectID := cfg.ProjectID
	if projectID == "" {
		projectID = creds.ProjectID
	}

	return NewFCMWithTokenSource(projectID, creds.TokenSource, httpClient, ""), nil
}

// NewFCMWithTokenSource is the test seam: same behaviour but with an injected
// token source, so tests never call Google. An empty endpoint selects the real
// FCM API for projectID.
func NewFCMWithTokenSource(projectID string, ts oauth2.TokenSource, httpClient *http.Client, endpoint string) *FCM {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if endpoint == "" {
		endpoint = fmt.Sprintf(fcmEndpointFormat, projectID)
	}
	return &FCM{
		projectID: projectID,
		// ReuseTokenSource caches the token and refreshes it on expiry; it is
		// safe for concurrent use, which lets Send be called from many
		// goroutines.
		tokens:   oauth2.ReuseTokenSource(nil, ts),
		client:   httpClient,
		endpoint: endpoint,
	}
}

// validateServiceAccountKey checks that the PEM block holds an RSA private key,
// which is the only key type Google service accounts use.
func validateServiceAccountKey(pemKey string) error {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return errors.New("push: FCM private key is not valid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("push: parse FCM private key: %w", err)
	}
	if _, ok := key.(*rsa.PrivateKey); !ok {
		return errors.New("push: FCM private key is not an RSA key")
	}
	return nil
}

// fcmNotification is the cross-platform part of the message. Icon and tag are
// carried in android.notification instead, where Android actually reads them.
type fcmNotification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

type fcmAndroidNotification struct {
	Icon string `json:"icon,omitempty"`
	Tag  string `json:"tag,omitempty"`
}

type fcmAndroid struct {
	Priority     string                  `json:"priority,omitempty"`
	Notification *fcmAndroidNotification `json:"notification,omitempty"`
}

type fcmMessageBody struct {
	Token        string            `json:"token"`
	Notification *fcmNotification  `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	Android      *fcmAndroid       `json:"android,omitempty"`
}

type fcmRequest struct {
	Message fcmMessageBody `json:"message"`
}

// Send delivers n to each token, sequentially, and returns one Result per
// token in input order. It never retries.
func (f *FCM) Send(ctx context.Context, tokens []string, n Notification) []Result {
	results := make([]Result, 0, len(tokens))
	for _, token := range tokens {
		results = append(results, f.sendOne(ctx, token, n))
	}
	return results
}

func (f *FCM) sendOne(ctx context.Context, token string, n Notification) Result {
	res := Result{Target: token}
	if token == "" {
		res.Err = errors.New("push: empty FCM token")
		return res
	}

	body, err := json.Marshal(fcmRequest{Message: buildFCMMessage(token, n)})
	if err != nil {
		res.Err = errors.New("push: marshal FCM request: " + err.Error())
		return res
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(body))
	if err != nil {
		res.Err = errors.New("push: build FCM request: " + err.Error())
		return res
	}
	req.Header.Set("Content-Type", "application/json")

	tok, err := f.tokens.Token()
	if err != nil {
		res.Err = errors.New("push: FCM access token unavailable")
		return res
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	resp, err := f.client.Do(req)
	if err != nil {
		res.Err = sanitizeErr(fmt.Errorf("push: FCM request failed: %w", err), token)
		return res
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	permanent, msg := classifyFCM(resp.StatusCode, raw)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return res
	}
	msg = strings.ReplaceAll(msg, token, "[token]")
	res.Err = fmt.Errorf("push: FCM returned %d: %s", resp.StatusCode, msg)
	res.Permanent = permanent
	return res
}

func buildFCMMessage(token string, n Notification) fcmMessageBody {
	data := make(map[string]string, len(n.Data)+2)
	for k, v := range n.Data {
		data[k] = v
	}
	// url is always present so the Flutter client can route a tap even when
	// the notification carries no explicit data payload.
	data["url"] = n.URL
	if n.Tag != "" {
		data["tag"] = n.Tag
	}

	msg := fcmMessageBody{
		Token: token,
		Notification: &fcmNotification{
			Title: n.Title,
			Body:  n.Body,
		},
		Data: data,
		Android: &fcmAndroid{
			Priority: "high",
		},
	}

	if n.Icon != "" || n.Tag != "" {
		msg.Android.Notification = &fcmAndroidNotification{
			Icon: absoluteIcon(n.Icon, n.URL),
			Tag:  n.Tag,
		}
	}

	return msg
}

// absoluteIcon resolves a relative icon path against the notification's URL so
// Android can load it. An absolute icon, or one with no usable base URL, is
// passed through unchanged.
func absoluteIcon(icon, clickURL string) string {
	if icon == "" {
		return ""
	}
	if u, err := url.Parse(icon); err == nil && u.IsAbs() {
		return icon
	}
	base, err := url.Parse(clickURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return icon
	}
	ref, err := url.Parse(icon)
	if err != nil {
		return icon
	}
	return base.ResolveReference(ref).String()
}

type fcmErrorEnvelope struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// classifyFCM maps an FCM response to (permanent, readable message).
func classifyFCM(status int, body []byte) (permanent bool, message string) {
	message = http.StatusText(status)
	if message == "" {
		message = "unexpected response"
	}

	var env fcmErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		message = env.Error.Message
	}

	if status < 200 || status >= 300 {
		switch {
		case status == http.StatusNotFound:
			return true, message
		case env.Error.Status == "NOT_FOUND" || env.Error.Status == "UNREGISTERED":
			return true, message
		case env.Error.Status == "INVALID_ARGUMENT" &&
			strings.Contains(strings.ToLower(message), "registration token"):
			return true, message
		}
	}
	return false, message
}

func sanitizeErr(err error, token string) error {
	if token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "[token]"))
}

// WebPushConfig holds the VAPID credentials used to sign push messages.
type WebPushConfig struct {
	Subject    string // mailto: or https: VAPID subject
	PublicKey  string // base64url
	PrivateKey string // base64url
}

// Subscription is a browser push subscription.
type Subscription struct {
	Endpoint string
	P256DH   string
	Auth     string
}

// WebPush sends notifications to browsers with the Web Push protocol.
type WebPush struct {
	subject    string
	publicKey  string
	privateKey string
	client     *http.Client
	ttl        int
}

// NewWebPush validates the VAPID key pair and builds a sender. httpClient may
// be nil, in which case the library's default client is used.
func NewWebPush(cfg WebPushConfig, httpClient *http.Client) (*WebPush, error) {
	if cfg.Subject == "" {
		return nil, errors.New("push: VAPID subject is required")
	}
	if cfg.PublicKey == "" || cfg.PrivateKey == "" {
		return nil, errors.New("push: VAPID public and private keys are required")
	}

	priv, err := decodeBase64URL(cfg.PrivateKey)
	if err != nil || len(priv) == 0 || len(priv) > 32 {
		return nil, errors.New("push: VAPID private key is not a valid P-256 scalar")
	}
	pub, err := decodeBase64URL(cfg.PublicKey)
	if err != nil || len(pub) != 65 {
		return nil, errors.New("push: VAPID public key is not an uncompressed P-256 point")
	}
	if x, y := elliptic.Unmarshal(elliptic.P256(), pub); x == nil || y == nil {
		return nil, errors.New("push: VAPID public key is not on the P-256 curve")
	}

	return &WebPush{
		subject:    cfg.Subject,
		publicKey:  cfg.PublicKey,
		privateKey: cfg.PrivateKey,
		client:     httpClient,
		ttl:        defaultPushTTL,
	}, nil
}

// webPayload is the JSON document a service worker receives.
type webPayload struct {
	Title string            `json:"title"`
	Body  string            `json:"body"`
	URL   string            `json:"url"`
	Tag   string            `json:"tag,omitempty"`
	Icon  string            `json:"icon,omitempty"`
	Data  map[string]string `json:"data,omitempty"`
}

// MarshalPayload renders a Notification as the JSON payload of a Web Push
// message. It is exported so callers (and tests) can inspect exactly what a
// service worker will receive.
func MarshalPayload(n Notification) ([]byte, error) {
	return json.Marshal(webPayload{
		Title: n.Title,
		Body:  n.Body,
		URL:   n.URL,
		Tag:   n.Tag,
		Icon:  n.Icon,
		Data:  n.Data,
	})
}

// Send delivers n to each subscription and returns one Result per subscription
// in input order. Status 404/410 marks the subscription Permanent.
func (w *WebPush) Send(ctx context.Context, subs []Subscription, n Notification) []Result {
	payload, err := MarshalPayload(n)
	results := make([]Result, 0, len(subs))
	if err != nil {
		for _, sub := range subs {
			results = append(results, Result{
				Target: sub.Endpoint,
				Err:    errors.New("push: marshal web push payload: " + err.Error()),
			})
		}
		return results
	}

	for _, sub := range subs {
		results = append(results, w.sendOne(ctx, sub, payload))
	}
	return results
}

func (w *WebPush) sendOne(ctx context.Context, sub Subscription, payload []byte) Result {
	res := Result{Target: sub.Endpoint}
	if sub.Endpoint == "" {
		res.Err = errors.New("push: empty web push endpoint")
		return res
	}
	if sub.P256DH == "" || sub.Auth == "" {
		res.Err = errors.New("push: web push subscription is missing keys")
		res.Permanent = true
		return res
	}

	opts := &webpush.Options{
		Subscriber:      w.subject,
		VAPIDPublicKey:  w.publicKey,
		VAPIDPrivateKey: w.privateKey,
		TTL:             w.ttl,
	}
	if w.client != nil {
		opts.HTTPClient = w.client
	}

	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256DH,
			Auth:   sub.Auth,
		},
	}, opts)
	if err != nil {
		res.Err = fmt.Errorf("push: web push request failed: %w", err)
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain so the connection can be reused.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return res
	}

	permanent := resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	res.Err = fmt.Errorf("push: web push returned %d: %s", resp.StatusCode, msg)
	res.Permanent = permanent
	return res
}

// GenerateVAPIDKeys returns base64url-encoded public and private VAPID keys
// suitable for saving in admin settings. The private key is the 32-byte P-256
// scalar; the public key is the uncompressed point.
func GenerateVAPIDKeys() (publicKey, privateKey string, err error) {
	// The webpush library generates exactly this pair (private, public), so it
	// keeps the encoding consistent with what its own signer expects.
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", fmt.Errorf("push: generate VAPID keys: %w", err)
	}
	return pub, priv, nil
}

func decodeBase64URL(s string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}
