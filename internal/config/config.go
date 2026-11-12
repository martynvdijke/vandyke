// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Umami holds self-hosted analytics settings. Analytics is enabled only when
// both URL and WebsiteID are set.
type Umami struct {
	URL       string
	WebsiteID string
	Domains   string
}

// OIDC holds optional OpenID Connect settings.
type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Webhook holds the optional outbound entry-created webhook. An empty URL
// disables dispatch; an empty Secret disables signing.
type Webhook struct {
	URL    string
	Secret string
}

// Enabled reports whether a webhook target is configured.
func (w Webhook) Enabled() bool { return strings.TrimSpace(w.URL) != "" }

// Enabled reports whether OIDC login is configured.
func (o OIDC) Enabled() bool { return o.Issuer != "" && o.ClientID != "" }

// Enabled reports whether the tracker script should be injected.
func (u Umami) Enabled() bool { return u.URL != "" && u.WebsiteID != "" }

// Origin returns scheme://host for the tracker URL, or "" if unparseable.
func (u Umami) Origin() string {
	if u.URL == "" {
		return ""
	}
	parsed, err := url.Parse(u.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// Notifications holds runtime-configurable push/realtime notification settings.
type Notifications struct {
	Enabled          bool
	Realtime         bool
	WebPush          bool
	Android          bool
	EntryCreated     bool
	TitleTemplate    string
	BodyTemplate     string
	ClickURL         string
	VAPIDSubject     string
	VAPIDPublicKey   string
	VAPIDPrivateKey  string
	FCMProjectID     string
	FCMClientEmail   string
	FCMPrivateKey    string
	FirebaseAPIKey   string
	FirebaseAppID    string
	FirebaseSenderID string
}

// Config is the fully resolved application configuration.
type Config struct {
	Port                    string
	DBPath                  string
	TrustProxy              bool
	AddRateLimitPerHour     float64
	AddRateLimitBurst       int
	AdminSetupToken         string
	AdminLoginRatePerMinute float64
	AdminLoginRateBurst     int
	PublicBaseURL           string
	MetricsToken            string
	Umami                   Umami
	Webhook                 Webhook
	OIDC                    OIDC
	Notifications           Notifications
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		Port:            getenv("PORT", "8080"),
		DBPath:          getenv("DB_PATH", "./data/vandyke.db"),
		AdminSetupToken: os.Getenv("ADMIN_SETUP_TOKEN"),
		MetricsToken:    strings.TrimSpace(os.Getenv("METRICS_TOKEN")),
		Umami: Umami{
			URL:       os.Getenv("UMAMI_URL"),
			WebsiteID: os.Getenv("UMAMI_WEBSITE_ID"),
			Domains:   os.Getenv("UMAMI_DOMAINS"),
		},
		Webhook: Webhook{
			URL:    os.Getenv("LEDGER_WEBHOOK_URL"),
			Secret: os.Getenv("LEDGER_WEBHOOK_SECRET"),
		},
		OIDC: OIDC{
			Issuer:       os.Getenv("OIDC_ISSUER"),
			ClientID:     os.Getenv("OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
		},
	}

	var err error
	if cfg.TrustProxy, err = getenvBool("TRUST_PROXY", false); err != nil {
		return Config{}, err
	}
	if cfg.AddRateLimitPerHour, err = getenvFloat("ADD_RATE_LIMIT_PER_HOUR", 10); err != nil {
		return Config{}, err
	}
	if cfg.AddRateLimitBurst, err = getenvInt("ADD_RATE_LIMIT_BURST", 5); err != nil {
		return Config{}, err
	}
	if cfg.AdminLoginRatePerMinute, err = getenvFloat("ADMIN_LOGIN_RATE_PER_MINUTE", 10); err != nil {
		return Config{}, err
	}
	if cfg.AdminLoginRateBurst, err = getenvInt("ADMIN_LOGIN_RATE_BURST", 5); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.Enabled, err = getenvBool("NOTIFICATIONS_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.Realtime, err = getenvBool("NOTIFICATIONS_REALTIME_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.WebPush, err = getenvBool("NOTIFICATIONS_WEBPUSH_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.Android, err = getenvBool("NOTIFICATIONS_ANDROID_ENABLED", true); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.EntryCreated, err = getenvBool("NOTIFICATIONS_ENTRY_CREATED", true); err != nil {
		return Config{}, err
	}
	cfg.Notifications.TitleTemplate = strings.TrimSpace(getenv("NOTIFICATIONS_TITLE", "New entry added"))
	cfg.Notifications.BodyTemplate = strings.TrimSpace(getenv("NOTIFICATIONS_BODY", `{person} added “{word}” — {meaning}`))
	cfg.Notifications.ClickURL = strings.TrimSpace(getenv("NOTIFICATIONS_CLICK_URL", `{base}/e/{id}`))
	cfg.Notifications.VAPIDSubject = strings.TrimSpace(getenv("NOTIFICATIONS_VAPID_SUBJECT", ""))
	cfg.Notifications.VAPIDPublicKey = strings.TrimSpace(getenv("NOTIFICATIONS_VAPID_PUBLIC_KEY", ""))
	cfg.Notifications.VAPIDPrivateKey = strings.TrimSpace(getenv("NOTIFICATIONS_VAPID_PRIVATE_KEY", ""))
	cfg.Notifications.FCMProjectID = strings.TrimSpace(getenv("NOTIFICATIONS_FCM_PROJECT_ID", ""))
	cfg.Notifications.FCMClientEmail = strings.TrimSpace(getenv("NOTIFICATIONS_FCM_CLIENT_EMAIL", ""))
	// Private keys are often pasted from a JSON service account, where newlines
	// arrive escaped; turn them back into real newlines.
	cfg.Notifications.FCMPrivateKey = strings.ReplaceAll(
		strings.TrimSpace(getenv("NOTIFICATIONS_FCM_PRIVATE_KEY", "")), `\n`, "\n")
	cfg.Notifications.FirebaseAPIKey = strings.TrimSpace(getenv("NOTIFICATIONS_FIREBASE_API_KEY", ""))
	cfg.Notifications.FirebaseAppID = strings.TrimSpace(getenv("NOTIFICATIONS_FIREBASE_APP_ID", ""))
	cfg.Notifications.FirebaseSenderID = strings.TrimSpace(getenv("NOTIFICATIONS_FIREBASE_SENDER_ID", ""))
	if raw := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); raw != "" {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Config{}, fmt.Errorf("PUBLIC_BASE_URL: expected an absolute http or https URL, got %q", raw)
		}
		cfg.PublicBaseURL = strings.TrimRight(parsed.String(), "/")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvBool(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: expected a boolean, got %q", key, raw)
	}
	return v, nil
}

func getenvFloat(key string, fallback float64) (float64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: expected a number, got %q", key, raw)
	}
	return v, nil
}

func getenvInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: expected an integer, got %q", key, raw)
	}
	return v, nil
}
