// Package settings resolves runtime settings that can be overridden from the
// admin interface, falling back to environment values when unset.
package settings

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

const (
	keyUmamiURL       = "umami.url"
	keyUmamiWebsiteID = "umami.website_id"
	keyUmamiDomains   = "umami.domains"

	keyWebhookURL    = "webhook.url"
	keyWebhookSecret = "webhook.secret"

	keyNotificationsEnabled          = "notifications.enabled"
	keyNotificationsRealtime         = "notifications.realtime.enabled"
	keyNotificationsWebPush          = "notifications.webpush.enabled"
	keyNotificationsAndroid          = "notifications.android.enabled"
	keyNotificationsEntryCreated     = "notifications.entry_created"
	keyNotificationsTitle            = "notifications.title"
	keyNotificationsBody             = "notifications.body"
	keyNotificationsClickURL         = "notifications.click_url"
	keyNotificationsVAPIDSubject     = "notifications.vapid.subject"
	keyNotificationsVAPIDPublicKey   = "notifications.vapid.public_key"
	keyNotificationsVAPIDPrivateKey  = "notifications.vapid.private_key"
	keyNotificationsFCMProjectID     = "notifications.fcm.project_id"
	keyNotificationsFCMClientEmail   = "notifications.fcm.client_email"
	keyNotificationsFCMPrivateKey    = "notifications.fcm.private_key"
	keyNotificationsFirebaseAPIKey   = "notifications.firebase.api_key"
	keyNotificationsFirebaseAppID    = "notifications.firebase.app_id"
	keyNotificationsFirebaseSenderID = "notifications.firebase.sender_id"

	// Built-in notification defaults, applied when neither storage nor the
	// environment supplies a value.
	defaultNotificationsTitle    = "New entry added"
	defaultNotificationsBody     = `{person} added “{word}” — {meaning}`
	defaultNotificationsClickURL = `{base}/e/{id}`
)

// Store keeps the effective Umami, webhook and notification configuration in
// memory and persists overrides in SQLite. The zero value is not usable.
type Store struct {
	store            *store.Store
	env              config.Umami
	envWebhook       config.Webhook
	envNotifications config.Notifications

	mu                   sync.RWMutex
	current              config.Umami
	currentWebhook       config.Webhook
	currentNotifications config.Notifications
}

// New loads persisted overrides and merges them over env.
func New(ctx context.Context, st *store.Store, env config.Umami, envWebhook config.Webhook,
	envNotifications config.Notifications) (*Store, error) {
	s := &Store{store: st, env: env, envWebhook: envWebhook, envNotifications: envNotifications}
	values, err := st.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	s.current = s.effective(values)
	s.currentWebhook = s.effectiveWebhook(values)
	s.currentNotifications = s.effectiveNotifications(values)
	return s, nil
}

// Current returns the effective Umami settings.
func (s *Store) Current() config.Umami {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// CurrentWebhook returns the effective webhook settings.
func (s *Store) CurrentWebhook() config.Webhook {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentWebhook
}

// CurrentNotifications returns the effective notification settings.
func (s *Store) CurrentNotifications() config.Notifications {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentNotifications
}

// Env returns the environment-provided fallback settings.
func (s *Store) Env() config.Umami { return s.env }

// EnvWebhook returns the environment-provided webhook fallback.
func (s *Store) EnvWebhook() config.Webhook { return s.envWebhook }

// EnvNotifications returns the environment-provided notification fallback.
func (s *Store) EnvNotifications() config.Notifications { return s.envNotifications }

// Overrides returns the persisted values, without environment fallbacks.
func (s *Store) Overrides(ctx context.Context) (config.Umami, error) {
	values, err := s.store.GetSettings(ctx)
	if err != nil {
		return config.Umami{}, err
	}
	return config.Umami{
		URL:       strings.TrimSpace(values[keyUmamiURL]),
		WebsiteID: strings.TrimSpace(values[keyUmamiWebsiteID]),
		Domains:   strings.TrimSpace(values[keyUmamiDomains]),
	}, nil
}

// OverridesNotifications returns the persisted notification values without
// environment fallbacks. Secrets are always returned empty; callers learn
// whether one is configured from CurrentNotifications.
func (s *Store) OverridesNotifications(ctx context.Context) (config.Notifications, error) {
	values, err := s.store.GetSettings(ctx)
	if err != nil {
		return config.Notifications{}, err
	}
	return config.Notifications{
		Enabled:          values[keyNotificationsEnabled] == "1",
		Realtime:         values[keyNotificationsRealtime] == "1",
		WebPush:          values[keyNotificationsWebPush] == "1",
		Android:          values[keyNotificationsAndroid] == "1",
		EntryCreated:     values[keyNotificationsEntryCreated] == "1",
		TitleTemplate:    strings.TrimSpace(values[keyNotificationsTitle]),
		BodyTemplate:     strings.TrimSpace(values[keyNotificationsBody]),
		ClickURL:         strings.TrimSpace(values[keyNotificationsClickURL]),
		VAPIDSubject:     strings.TrimSpace(values[keyNotificationsVAPIDSubject]),
		VAPIDPublicKey:   strings.TrimSpace(values[keyNotificationsVAPIDPublicKey]),
		VAPIDPrivateKey:  "",
		FCMProjectID:     strings.TrimSpace(values[keyNotificationsFCMProjectID]),
		FCMClientEmail:   strings.TrimSpace(values[keyNotificationsFCMClientEmail]),
		FCMPrivateKey:    "",
		FirebaseAPIKey:   strings.TrimSpace(values[keyNotificationsFirebaseAPIKey]),
		FirebaseAppID:    strings.TrimSpace(values[keyNotificationsFirebaseAppID]),
		FirebaseSenderID: strings.TrimSpace(values[keyNotificationsFirebaseSenderID]),
	}, nil
}

// Save validates and persists Umami overrides, then refreshes the cached
// effective settings. Empty fields fall back to the environment.
func (s *Store) Save(ctx context.Context, values config.Umami) error {
	values.URL = strings.TrimSpace(values.URL)
	values.WebsiteID = strings.TrimSpace(values.WebsiteID)
	values.Domains = strings.TrimSpace(values.Domains)
	if values.URL != "" {
		parsed, err := url.Parse(values.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("umami URL must be an absolute http or https URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("umami URL must use http or https")
		}
	}
	if err := s.store.SetSettings(ctx, map[string]string{
		keyUmamiURL:       values.URL,
		keyUmamiWebsiteID: values.WebsiteID,
		keyUmamiDomains:   values.Domains,
	}); err != nil {
		return err
	}
	s.mu.Lock()
	s.current = s.effective(map[string]string{
		keyUmamiURL:       values.URL,
		keyUmamiWebsiteID: values.WebsiteID,
		keyUmamiDomains:   values.Domains,
	})
	s.mu.Unlock()
	return nil
}

// SaveWebhook validates and persists the webhook URL and secret. A blank secret
// leaves the stored secret unchanged, and clearSecret removes it; the URL is
// always written so clearing it disables dispatch. Secrets are never returned
// to callers, so a form can round-trip safely.
func (s *Store) SaveWebhook(ctx context.Context, values config.Webhook, clearSecret bool) error {
	values.URL = strings.TrimSpace(values.URL)
	values.Secret = strings.TrimSpace(values.Secret)
	if values.URL != "" {
		parsed, err := url.Parse(values.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("webhook URL must be an absolute http or https URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("webhook URL must use http or https")
		}
	}

	updates := map[string]string{keyWebhookURL: values.URL}
	switch {
	case clearSecret:
		updates[keyWebhookSecret] = ""
	case values.Secret != "":
		updates[keyWebhookSecret] = values.Secret
	}
	if err := s.store.SetSettings(ctx, updates); err != nil {
		return err
	}

	persisted, err := s.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.currentWebhook = s.effectiveWebhook(persisted)
	s.mu.Unlock()
	return nil
}

// SaveNotifications validates and persists notification settings. Empty
// title/body/click URL values are stored empty so the environment or built-in
// defaults apply. A blank private key leaves the stored key unchanged, and the
// matching clear flag removes it; secrets are never returned to callers.
func (s *Store) SaveNotifications(ctx context.Context, v config.Notifications, clearVAPIDPrivate, clearFCMPrivate bool) error {
	v.TitleTemplate = strings.TrimSpace(v.TitleTemplate)
	v.BodyTemplate = strings.TrimSpace(v.BodyTemplate)
	v.ClickURL = strings.TrimSpace(v.ClickURL)
	v.VAPIDSubject = strings.TrimSpace(v.VAPIDSubject)
	v.VAPIDPublicKey = strings.TrimSpace(v.VAPIDPublicKey)
	v.VAPIDPrivateKey = strings.TrimSpace(v.VAPIDPrivateKey)
	v.FCMProjectID = strings.TrimSpace(v.FCMProjectID)
	v.FCMClientEmail = strings.TrimSpace(v.FCMClientEmail)
	v.FCMPrivateKey = strings.TrimSpace(v.FCMPrivateKey)
	v.FirebaseAPIKey = strings.TrimSpace(v.FirebaseAPIKey)
	v.FirebaseAppID = strings.TrimSpace(v.FirebaseAppID)
	v.FirebaseSenderID = strings.TrimSpace(v.FirebaseSenderID)

	if v.ClickURL != "" {
		if err := validateNotificationClickURL(v.ClickURL); err != nil {
			return err
		}
	}

	updates := map[string]string{
		keyNotificationsEnabled:          boolSetting(v.Enabled),
		keyNotificationsRealtime:         boolSetting(v.Realtime),
		keyNotificationsWebPush:          boolSetting(v.WebPush),
		keyNotificationsAndroid:          boolSetting(v.Android),
		keyNotificationsEntryCreated:     boolSetting(v.EntryCreated),
		keyNotificationsTitle:            v.TitleTemplate,
		keyNotificationsBody:             v.BodyTemplate,
		keyNotificationsClickURL:         v.ClickURL,
		keyNotificationsVAPIDSubject:     v.VAPIDSubject,
		keyNotificationsVAPIDPublicKey:   v.VAPIDPublicKey,
		keyNotificationsFCMProjectID:     v.FCMProjectID,
		keyNotificationsFCMClientEmail:   v.FCMClientEmail,
		keyNotificationsFirebaseAPIKey:   v.FirebaseAPIKey,
		keyNotificationsFirebaseAppID:    v.FirebaseAppID,
		keyNotificationsFirebaseSenderID: v.FirebaseSenderID,
	}
	switch {
	case clearVAPIDPrivate:
		updates[keyNotificationsVAPIDPrivateKey] = ""
	case v.VAPIDPrivateKey != "":
		updates[keyNotificationsVAPIDPrivateKey] = v.VAPIDPrivateKey
	}
	switch {
	case clearFCMPrivate:
		updates[keyNotificationsFCMPrivateKey] = ""
	case v.FCMPrivateKey != "":
		updates[keyNotificationsFCMPrivateKey] = v.FCMPrivateKey
	}
	if err := s.store.SetSettings(ctx, updates); err != nil {
		return err
	}

	persisted, err := s.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.currentNotifications = s.effectiveNotifications(persisted)
	s.mu.Unlock()
	return nil
}

// Effective returns the merged configuration for testable use.
func (s *Store) Effective(values map[string]string) config.Umami { return s.effective(values) }

// EffectiveNotifications returns the merged notification configuration for
// testable use.
func (s *Store) EffectiveNotifications(values map[string]string) config.Notifications {
	return s.effectiveNotifications(values)
}

func (s *Store) effective(values map[string]string) config.Umami {
	pick := func(key, fallback string) string {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
		return fallback
	}
	return config.Umami{
		URL:       pick(keyUmamiURL, s.env.URL),
		WebsiteID: pick(keyUmamiWebsiteID, s.env.WebsiteID),
		Domains:   pick(keyUmamiDomains, s.env.Domains),
	}
}

func (s *Store) effectiveWebhook(values map[string]string) config.Webhook {
	pick := func(key, fallback string) string {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
		return fallback
	}
	return config.Webhook{
		URL:    pick(keyWebhookURL, s.envWebhook.URL),
		Secret: pick(keyWebhookSecret, s.envWebhook.Secret),
	}
}

func (s *Store) effectiveNotifications(values map[string]string) config.Notifications {
	pick := func(key, env, fallback string) string {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
		if e := strings.TrimSpace(env); e != "" {
			return e
		}
		return fallback
	}
	pickBool := func(key string, env, fallback bool) bool {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v == "1"
		}
		if env {
			return true
		}
		return fallback
	}
	env := s.envNotifications
	return config.Notifications{
		Enabled:          pickBool(keyNotificationsEnabled, env.Enabled, false),
		Realtime:         pickBool(keyNotificationsRealtime, env.Realtime, true),
		WebPush:          pickBool(keyNotificationsWebPush, env.WebPush, true),
		Android:          pickBool(keyNotificationsAndroid, env.Android, true),
		EntryCreated:     pickBool(keyNotificationsEntryCreated, env.EntryCreated, true),
		TitleTemplate:    pick(keyNotificationsTitle, env.TitleTemplate, defaultNotificationsTitle),
		BodyTemplate:     pick(keyNotificationsBody, env.BodyTemplate, defaultNotificationsBody),
		ClickURL:         pick(keyNotificationsClickURL, env.ClickURL, defaultNotificationsClickURL),
		VAPIDSubject:     pick(keyNotificationsVAPIDSubject, env.VAPIDSubject, ""),
		VAPIDPublicKey:   pick(keyNotificationsVAPIDPublicKey, env.VAPIDPublicKey, ""),
		VAPIDPrivateKey:  pick(keyNotificationsVAPIDPrivateKey, env.VAPIDPrivateKey, ""),
		FCMProjectID:     pick(keyNotificationsFCMProjectID, env.FCMProjectID, ""),
		FCMClientEmail:   pick(keyNotificationsFCMClientEmail, env.FCMClientEmail, ""),
		FCMPrivateKey:    pick(keyNotificationsFCMPrivateKey, env.FCMPrivateKey, ""),
		FirebaseAPIKey:   pick(keyNotificationsFirebaseAPIKey, env.FirebaseAPIKey, ""),
		FirebaseAppID:    pick(keyNotificationsFirebaseAppID, env.FirebaseAppID, ""),
		FirebaseSenderID: pick(keyNotificationsFirebaseSenderID, env.FirebaseSenderID, ""),
	}
}

func boolSetting(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func validateNotificationClickURL(raw string) error {
	if !strings.Contains(raw, "{id}") {
		return errors.New("notifications click URL must contain {id}")
	}
	if strings.HasPrefix(raw, "/") {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("notifications click URL must start with / or be an absolute http or https URL")
	}
	return nil
}
