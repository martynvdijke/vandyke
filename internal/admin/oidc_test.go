package admin

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"vandyke.thor.edu/vandyke/internal/config"
)

type fakeOIDC struct {
	server *httptest.Server
	nonce  string
	key    *rsa.PrivateKey
}

func newFakeOIDC(t *testing.T) *fakeOIDC {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	f := &fakeOIDC{key: key}
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
				"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("/authorize", func(rw http.ResponseWriter, r *http.Request) {
		f.nonce = r.URL.Query().Get("nonce")
		rw.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/token", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		claims, _ := json.Marshal(map[string]any{
			"iss": issuer, "sub": "oidc-subject", "aud": "client",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"nonce": f.nonce, "email": "oidc@example.com", "name": "OIDC User",
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
		raw, _ := object.CompactSerialize()
		_ = json.NewEncoder(rw).Encode(map[string]any{
			"access_token": "access", "token_type": "Bearer", "expires_in": 3600, "id_token": raw,
		})
	})
	f.server = httptest.NewServer(mux)
	issuer = f.server.URL
	t.Cleanup(f.server.Close)
	return f
}

func TestOIDCLoginAutoProvisionsAdmin(t *testing.T) {
	provider := newFakeOIDC(t)
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
		OIDC: config.OIDC{
			Issuer: provider.server.URL, ClientID: "client", ClientSecret: "secret",
		},
	})
	setupAdmin(t, srv, client)

	resp, _ := get(t, client, srv.URL+"/admin/oidc/start")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("oidc start = %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" || parsed.Query().Get("nonce") == "" {
		t.Fatalf("missing state/nonce in %q", location)
	}
	// The fake provider records the nonce when the browser would visit the
	// authorization endpoint; set it directly for this callback-only test.
	provider.nonce = parsed.Query().Get("nonce")

	resp, _ = get(t, client, srv.URL+"/admin/oidc/callback?code=code-1&state="+url.QueryEscape(state))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin" {
		t.Fatalf("callback = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, body := get(t, client, srv.URL+"/admin")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Ledger entries") {
		t.Fatalf("admin after oidc = %d", resp.StatusCode)
	}
	count, err := st.AdminCount(t.Context())
	if err != nil || count != 2 {
		t.Fatalf("admin count = %d, %v; want 2", count, err)
	}
}
