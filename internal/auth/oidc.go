package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCConfig is the environment configuration for an OpenID Connect provider.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Enabled reports whether OIDC login is configured.
func (c OIDCConfig) Enabled() bool {
	return c.Issuer != "" && c.ClientID != ""
}

// Identity is the verified identity returned by an OIDC provider.
type Identity struct {
	Issuer   string
	Subject  string
	Email    string
	Name     string
	Username string
}

// OIDC wraps a discovered provider and its token verifier.
type OIDC struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	config   oauth2.Config
}

// NewOIDC discovers the provider metadata and prepares the authorization flow.
func NewOIDC(ctx context.Context, cfg OIDCConfig) (*OIDC, error) {
	if !cfg.Enabled() {
		return nil, errors.New("oidc is not configured")
	}
	issuer := strings.TrimRight(cfg.Issuer, "/")
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	return &OIDC{
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
	}, nil
}

// AuthCodeURL builds the provider authorization URL for a PKCE flow.
func (o *OIDC) AuthCodeURL(redirectURL, state, nonce, verifier string) string {
	cfg := o.config
	cfg.RedirectURL = redirectURL
	return cfg.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
}

// Exchange swaps an authorization code for a verified identity.
func (o *OIDC) Exchange(ctx context.Context, code, redirectURL, verifier, nonce string) (Identity, error) {
	cfg := o.config
	cfg.RedirectURL = redirectURL
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("exchange oidc code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Identity{}, errors.New("oidc response did not include an id token")
	}
	idToken, err := o.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Identity{}, fmt.Errorf("verify id token: %w", err)
	}
	if idToken.Nonce != nonce {
		return Identity{}, errors.New("oidc nonce mismatch")
	}
	var claims struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("read id token claims: %w", err)
	}
	if idToken.Subject == "" {
		return Identity{}, errors.New("oidc identity has no subject")
	}
	return Identity{
		Issuer:   idToken.Issuer,
		Subject:  idToken.Subject,
		Email:    claims.Email,
		Name:     claims.Name,
		Username: claims.PreferredUsername,
	}, nil
}

// NewPKCE returns a verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string, err error) {
	verifier, err = RandomToken(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
