package config

import "testing"

func TestLoadPublicBaseURL(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://vandyke.example.com/")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PublicBaseURL != "https://vandyke.example.com" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
}

func TestLoadRejectsInvalidPublicBaseURL(t *testing.T) {
	for _, raw := range []string{"vandyke.example.com", "/ledger", "ftp://vandyke.example.com"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("PUBLIC_BASE_URL", raw)
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %q", raw)
			}
		})
	}
}
