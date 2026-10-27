package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || strings.Contains(hash, "correct horse") {
		t.Fatalf("bad hash %q", hash)
	}
	if !VerifyPassword(hash, "correct horse battery") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("invalid password accepted")
	}
	if VerifyPassword("garbage", "correct horse battery") {
		t.Fatal("garbage hash accepted")
	}
}

func TestTokens(t *testing.T) {
	a, err := RandomToken(32)
	if err != nil || a == "" {
		t.Fatalf("RandomToken: %q, %v", a, err)
	}
	b, _ := RandomToken(32)
	if a == b {
		t.Fatal("tokens are not random")
	}
	if !TokensEqual(a, a) || TokensEqual(a, b) {
		t.Fatal("token comparison is wrong")
	}
	if HashToken(a) == a || len(HashToken(a)) != 64 {
		t.Fatalf("unexpected token hash %q", HashToken(a))
	}
}

func TestPKCE(t *testing.T) {
	verifier, challenge, err := NewPKCE()
	if err != nil {
		t.Fatalf("NewPKCE: %v", err)
	}
	if verifier == "" || challenge == "" || verifier == challenge {
		t.Fatalf("verifier=%q challenge=%q", verifier, challenge)
	}
}

func TestOIDCDiscoveryAndExchange(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const nonce = "nonce-value"
	var issuer string

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(rw http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(rw).Encode(map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(rw http.ResponseWriter, r *http.Request) {
		pub := key.PublicKey
		_ = json.NewEncoder(rw).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"kid": "test",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("/token", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		if err := r.ParseForm(); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		if r.FormValue("code") != "code-123" {
			http.Error(rw, "bad code", http.StatusBadRequest)
			return
		}
		claims, _ := json.Marshal(map[string]any{
			"iss":   issuer,
			"sub":   "subject-1",
			"aud":   "client",
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iat":   time.Now().Unix(),
			"nonce": nonce,
			"email": "alice@example.com",
			"name":  "Alice",
		})
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithHeader("kid", "test"))
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		object, err := signer.Sign(claims)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		raw, err := object.CompactSerialize()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{
			"access_token": "access",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     raw,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	issuer = srv.URL

	client, err := NewOIDC(context.Background(), OIDCConfig{
		Issuer: issuer, ClientID: "client", ClientSecret: "secret",
	})
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	authURL := client.AuthCodeURL("https://app.example.com/admin/oidc/callback", "state", nonce, "verifier")
	for _, want := range []string{"response_type=code", "client_id=client", "state=state", "nonce=" + nonce, "code_challenge_method=S256"} {
		if !strings.Contains(authURL, want) {
			t.Errorf("authorization URL missing %q: %s", want, authURL)
		}
	}
	identity, err := client.Exchange(context.Background(), "code-123",
		"https://app.example.com/admin/oidc/callback", "verifier", nonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if identity.Subject != "subject-1" || identity.Email != "alice@example.com" || identity.Name != "Alice" {
		t.Fatalf("identity = %+v", identity)
	}
	if _, err := client.Exchange(context.Background(), "code-123",
		"https://app.example.com/admin/oidc/callback", "verifier", "wrong-nonce"); err == nil {
		t.Fatal("nonce mismatch accepted")
	}
}
