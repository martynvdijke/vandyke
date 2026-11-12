// Package notify fans a newly created ledger entry out to the configured
// notification channels: the in-app realtime hub, Android devices through FCM,
// and browsers through Web Push.
//
// Delivery is best-effort and must never delay or fail the request that created
// the entry. EntryCreated therefore publishes the realtime event inline (it is
// non-blocking) and runs the push fan-out in a detached goroutine with its own
// deadline. Errors are logged, never returned.
package notify

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/push"
	"vandyke.thor.edu/vandyke/internal/realtime"
	"vandyke.thor.edu/vandyke/internal/settings"
	"vandyke.thor.edu/vandyke/internal/store"
)

const (
	// sendTimeout bounds one channel fan-out, including all network calls.
	sendTimeout = 30 * time.Second
	// fcmBatchSize is the largest number of tokens handed to one Send call.
	fcmBatchSize = 500

	testTitle = "Test notification"
	testBody  = "Notifications are working."
)

// Notifier renders and delivers entry notifications. It is safe for concurrent
// use; the sender instances are rebuilt only when the effective configuration
// changes.
type Notifier struct {
	store    *store.Store
	settings *settings.Store
	hub      *realtime.Hub
	baseURL  string
	logger   *slog.Logger

	mu       sync.Mutex
	fcm      *push.FCM
	fcmKey   string
	webPush  *push.WebPush
	webKey   string
	webBuilt bool
}

// New builds a Notifier. baseURL is the configured public origin used when a
// caller cannot provide a request-derived one; logger may be nil.
func New(st *store.Store, stg *settings.Store, hub *realtime.Hub, baseURL string, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		store:    st,
		settings: stg,
		hub:      hub,
		baseURL:  strings.TrimRight(baseURL, "/"),
		logger:   logger,
	}
}

// Hub returns the realtime hub this notifier publishes to. It may be nil.
func (n *Notifier) Hub() *realtime.Hub { return n.hub }

// Notifications returns the effective notification configuration. It exists so
// the public API can answer the client capability endpoint without holding its
// own settings store.
func (n *Notifier) Notifications() config.Notifications {
	if n == nil || n.settings == nil {
		return config.Notifications{}
	}
	return n.settings.CurrentNotifications()
}

// EntryCreated renders and sends the configured entry-created notification.
// requestBaseURL wins over the constructor base when both are set. It never
// returns an error: the push fan-out runs asynchronously and logs its outcome.
func (n *Notifier) EntryCreated(ctx context.Context, entry store.Entry, requestBaseURL string) {
	if n == nil || n.settings == nil {
		return
	}
	cfg := n.settings.CurrentNotifications()
	if !cfg.Enabled || !cfg.EntryCreated {
		return
	}

	base := n.resolveBase(requestBaseURL)
	title := render(cfg.TitleTemplate, entry, base)
	body := render(cfg.BodyTemplate, entry, base)
	click := clickURL(render(cfg.ClickURL, entry, base), base, entry.ID)

	if cfg.Realtime && n.hub != nil {
		n.publishRealtime(entry, click)
	}

	// Detach from the request cancellation: a client that disconnects after
	// the response must not abort delivery to other devices.
	detached := context.WithoutCancel(ctx)
	go n.fanOut(detached, cfg, entry, title, body, click)
}

// TestSend sends one test notification to every configured channel and active
// recipient. It returns the number of successful and failed deliveries. It
// errors only when notifications are disabled or no channel is configured.
func (n *Notifier) TestSend(ctx context.Context) (sent, failed int, err error) {
	if n == nil || n.settings == nil {
		return 0, 0, errors.New("notify: notifications are not configured")
	}
	cfg := n.settings.CurrentNotifications()
	if !cfg.Enabled {
		return 0, 0, errors.New("notify: notifications are disabled")
	}

	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	notification := push.Notification{Title: testTitle, Body: testBody}
	channels := 0

	if cfg.Android && fcmConfigured(cfg) {
		channels++
		ok, bad := n.sendAndroid(ctx, cfg, notification)
		sent += ok
		failed += bad
	}
	if cfg.WebPush && vapidConfigured(cfg) {
		channels++
		ok, bad := n.sendWebPush(ctx, cfg, notification)
		sent += ok
		failed += bad
	}

	if channels == 0 {
		return 0, 0, errors.New("notify: no notification channel is configured")
	}
	return sent, failed, nil
}

// resolveBase returns the trailing-slash-free origin for rendering. The request
// value wins; an empty one falls back to the constructor base.
func (n *Notifier) resolveBase(requestBaseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(requestBaseURL), "/")
	if base == "" {
		base = n.baseURL
	}
	return base
}

func (n *Notifier) publishRealtime(entry store.Entry, url string) {
	data, err := json.Marshal(map[string]any{
		"id":         entry.ID,
		"word":       entry.Word,
		"meaning":    entry.Meaning,
		"person":     entry.Person,
		"url":        url,
		"created_at": entry.CreatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		n.logger.Debug("notify: marshal realtime event", "error", err)
		return
	}
	n.hub.Publish(realtime.Event{Name: "entry.created", Data: data})
}

// fanOut delivers to the push channels under one deadline, isolating each
// channel so a failure in one does not prevent the other.
func (n *Notifier) fanOut(ctx context.Context, cfg config.Notifications, entry store.Entry, title, body, url string) {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	notification := push.Notification{
		Title: title,
		Body:  body,
		URL:   url,
		Tag:   "entry-" + strconv.FormatInt(entry.ID, 10),
		Data:  map[string]string{"entry_id": strconv.FormatInt(entry.ID, 10)},
	}

	if cfg.Android && fcmConfigured(cfg) {
		n.deliverAndroid(ctx, cfg, notification)
	}
	if cfg.WebPush && vapidConfigured(cfg) {
		n.deliverWebPush(ctx, cfg, notification)
	}
}

func (n *Notifier) deliverAndroid(ctx context.Context, cfg config.Notifications, notification push.Notification) {
	sender := n.fcmSender(cfg)
	if sender == nil {
		return
	}
	devices, err := n.store.ListActivePushDevices(ctx)
	if err != nil {
		n.logger.Error("notify: list push devices", "error", err)
		return
	}
	tokens := make([]string, 0, len(devices))
	for _, device := range devices {
		if token := strings.TrimSpace(device.Token); token != "" {
			tokens = append(tokens, token)
		}
	}

	sent, failed, disabled := 0, 0, 0
	for start := 0; start < len(tokens); start += fcmBatchSize {
		end := min(start+fcmBatchSize, len(tokens))
		results := sender.Send(ctx, tokens[start:end], notification)
		permanent := make([]string, 0)
		for _, result := range results {
			if result.OK() {
				sent++
				continue
			}
			failed++
			if result.Permanent {
				permanent = append(permanent, result.Target)
			} else {
				n.logger.Debug("notify: FCM send failed", "error", result.Err)
			}
		}
		if len(permanent) > 0 {
			if err := n.store.DisablePushDevices(ctx, permanent); err != nil {
				n.logger.Error("notify: disable push devices", "error", err)
			} else {
				disabled += len(permanent)
			}
		}
	}

	n.logger.Info("notify: fan-out", "channel", "android", "sent", sent, "failed", failed, "disabled", disabled)
}

func (n *Notifier) deliverWebPush(ctx context.Context, cfg config.Notifications, notification push.Notification) {
	sender := n.webPushSender(cfg)
	if sender == nil {
		return
	}
	stored, err := n.store.ListActiveWebPushSubscriptions(ctx)
	if err != nil {
		n.logger.Error("notify: list web push subscriptions", "error", err)
		return
	}

	subs := make([]push.Subscription, 0, len(stored))
	for _, sub := range stored {
		if !n.webPushAllowed(ctx, sub.AdminID) {
			continue
		}
		subs = append(subs, push.Subscription{
			Endpoint: sub.Endpoint,
			P256DH:   sub.P256DH,
			Auth:     sub.Auth,
		})
	}
	if len(subs) == 0 {
		n.logger.Info("notify: fan-out", "channel", "web_push", "sent", 0, "failed", 0, "disabled", 0)
		return
	}

	results := sender.Send(ctx, subs, notification)
	sent, failed := 0, 0
	permanent := make([]string, 0)
	for _, result := range results {
		if result.OK() {
			sent++
			continue
		}
		failed++
		if result.Permanent {
			permanent = append(permanent, result.Target)
		} else {
			n.logger.Debug("notify: web push send failed", "error", result.Err)
		}
	}

	disabled := 0
	if len(permanent) > 0 {
		if err := n.store.DisableWebPushSubscriptions(ctx, permanent); err != nil {
			n.logger.Error("notify: disable web push subscriptions", "error", err)
		} else {
			disabled = len(permanent)
		}
	}

	n.logger.Info("notify: fan-out", "channel", "web_push", "sent", sent, "failed", failed, "disabled", disabled)
}

// sendAndroid is the synchronous equivalent used by TestSend.
func (n *Notifier) sendAndroid(ctx context.Context, cfg config.Notifications, notification push.Notification) (sent, failed int) {
	sender := n.fcmSender(cfg)
	if sender == nil {
		return 0, 0
	}
	devices, err := n.store.ListActivePushDevices(ctx)
	if err != nil {
		n.logger.Error("notify: list push devices", "error", err)
		return 0, 0
	}
	tokens := make([]string, 0, len(devices))
	for _, device := range devices {
		if token := strings.TrimSpace(device.Token); token != "" {
			tokens = append(tokens, token)
		}
	}

	permanent := make([]string, 0)
	for start := 0; start < len(tokens); start += fcmBatchSize {
		end := min(start+fcmBatchSize, len(tokens))
		for _, result := range sender.Send(ctx, tokens[start:end], notification) {
			if result.OK() {
				sent++
				continue
			}
			failed++
			if result.Permanent {
				permanent = append(permanent, result.Target)
			}
		}
	}
	if len(permanent) > 0 {
		if err := n.store.DisablePushDevices(ctx, permanent); err != nil {
			n.logger.Error("notify: disable push devices", "error", err)
		}
	}
	return sent, failed
}

// sendWebPush is the synchronous equivalent used by TestSend.
func (n *Notifier) sendWebPush(ctx context.Context, cfg config.Notifications, notification push.Notification) (sent, failed int) {
	sender := n.webPushSender(cfg)
	if sender == nil {
		return 0, 0
	}
	stored, err := n.store.ListActiveWebPushSubscriptions(ctx)
	if err != nil {
		n.logger.Error("notify: list web push subscriptions", "error", err)
		return 0, 0
	}

	subs := make([]push.Subscription, 0, len(stored))
	for _, sub := range stored {
		if !n.webPushAllowed(ctx, sub.AdminID) {
			continue
		}
		subs = append(subs, push.Subscription{
			Endpoint: sub.Endpoint,
			P256DH:   sub.P256DH,
			Auth:     sub.Auth,
		})
	}
	if len(subs) == 0 {
		return 0, 0
	}

	permanent := make([]string, 0)
	for _, result := range sender.Send(ctx, subs, notification) {
		if result.OK() {
			sent++
			continue
		}
		failed++
		if result.Permanent {
			permanent = append(permanent, result.Target)
		}
	}
	if len(permanent) > 0 {
		if err := n.store.DisableWebPushSubscriptions(ctx, permanent); err != nil {
			n.logger.Error("notify: disable web push subscriptions", "error", err)
		}
	}
	return sent, failed
}

// webPushAllowed applies a subscribing administrator's own preferences. An
// anonymous subscription has no admin row and is always allowed. A lookup
// failure skips delivery rather than sending against an unknown preference.
func (n *Notifier) webPushAllowed(ctx context.Context, adminID *int64) bool {
	if adminID == nil {
		return true
	}
	prefs, err := n.store.GetAdminNotificationPrefs(ctx, *adminID)
	if err != nil {
		n.logger.Debug("notify: load admin notification prefs", "admin_id", *adminID, "error", err)
		return false
	}
	return prefs.WebPush && prefs.EntryCreated
}

// fcmSender returns a cached FCM sender, rebuilding it when the credentials
// change. A construction failure is logged and remembered for that
// configuration so it is not retried on every entry.
func (n *Notifier) fcmSender(cfg config.Notifications) *push.FCM {
	key := cfg.FCMProjectID + "|" + cfg.FCMClientEmail + "|" + hashSecret(cfg.FCMPrivateKey)

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fcmKey == key {
		return n.fcm
	}
	n.fcmKey = key
	n.fcm = nil
	sender, err := push.NewFCM(push.FCMConfig{
		ProjectID:   cfg.FCMProjectID,
		ClientEmail: cfg.FCMClientEmail,
		PrivateKey:  cfg.FCMPrivateKey,
	}, nil)
	if err != nil {
		n.logger.Error("notify: build FCM sender", "error", err)
		return nil
	}
	n.fcm = sender
	return sender
}

// webPushSender returns a cached Web Push sender, rebuilding it when the VAPID
// configuration changes.
func (n *Notifier) webPushSender(cfg config.Notifications) *push.WebPush {
	key := cfg.VAPIDSubject + "|" + cfg.VAPIDPublicKey + "|" + hashSecret(cfg.VAPIDPrivateKey)

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.webKey == key && n.webBuilt {
		return n.webPush
	}
	n.webKey = key
	n.webBuilt = true
	n.webPush = nil
	sender, err := push.NewWebPush(push.WebPushConfig{
		Subject:    cfg.VAPIDSubject,
		PublicKey:  cfg.VAPIDPublicKey,
		PrivateKey: cfg.VAPIDPrivateKey,
	}, nil)
	if err != nil {
		n.logger.Error("notify: build web push sender", "error", err)
		return nil
	}
	n.webPush = sender
	return sender
}

// render substitutes the template placeholders an entry notification supports.
func render(template string, entry store.Entry, base string) string {
	return strings.NewReplacer(
		"{word}", entry.Word,
		"{meaning}", entry.Meaning,
		"{person}", entry.Person,
		"{id}", strconv.FormatInt(entry.ID, 10),
		"{base}", base,
	).Replace(template)
}

// clickURL turns a rendered click target into an absolute URL. An empty target
// falls back to the canonical entry path; a relative one is resolved against
// the base origin.
func clickURL(target, base string, id int64) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return strings.TrimRight(base, "/") + "/e/" + strconv.FormatInt(id, 10)
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return target
	}
	if strings.HasPrefix(target, "/") {
		return base + target
	}
	return base + "/" + target
}

// fcmConfigured reports whether every field FCM needs is present.
func fcmConfigured(cfg config.Notifications) bool {
	return cfg.FCMProjectID != "" && cfg.FCMClientEmail != "" && cfg.FCMPrivateKey != ""
}

// vapidConfigured reports whether every field Web Push needs is present.
func vapidConfigured(cfg config.Notifications) bool {
	return cfg.VAPIDSubject != "" && cfg.VAPIDPublicKey != "" && cfg.VAPIDPrivateKey != ""
}

func hashSecret(secret string) string {
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("%x", sum)
}
