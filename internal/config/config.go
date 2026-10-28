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
	Umami                   Umami
	OIDC                    OIDC
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		Port:            getenv("PORT", "8080"),
		DBPath:          getenv("DB_PATH", "./data/vandyke.db"),
		AdminSetupToken: os.Getenv("ADMIN_SETUP_TOKEN"),
		Umami: Umami{
			URL:       os.Getenv("UMAMI_URL"),
			WebsiteID: os.Getenv("UMAMI_WEBSITE_ID"),
			Domains:   os.Getenv("UMAMI_DOMAINS"),
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
