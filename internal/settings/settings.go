// Package settings resolves runtime settings that can be overridden from the
// admin interface, falling back to environment values when unset.
package settings

import (
	"context"
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
)

// Store keeps the effective Umami and webhook configuration in memory and
// persists overrides in SQLite. The zero value is not usable.
type Store struct {
	store      *store.Store
	env        config.Umami
	envWebhook config.Webhook

	mu             sync.RWMutex
	current        config.Umami
	currentWebhook config.Webhook
}

// New loads persisted overrides and merges them over env.
func New(ctx context.Context, st *store.Store, env config.Umami, envWebhook config.Webhook) (*Store, error) {
	s := &Store{store: st, env: env, envWebhook: envWebhook}
	values, err := st.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	s.current = s.effective(values)
	s.currentWebhook = s.effectiveWebhook(values)
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

// Env returns the environment-provided fallback settings.
func (s *Store) Env() config.Umami { return s.env }

// EnvWebhook returns the environment-provided webhook fallback.
func (s *Store) EnvWebhook() config.Webhook { return s.envWebhook }

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

// Effective returns the merged configuration for testable use.
func (s *Store) Effective(values map[string]string) config.Umami { return s.effective(values) }

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
