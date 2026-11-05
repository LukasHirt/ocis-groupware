package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeIdP is a minimal OIDC provider: discovery document, JWKS, and a helper
// to mint tokens signed with its key.
type fakeIdP struct {
	*httptest.Server
	key  *rsa.PrivateKey
	down atomic.Bool
}

func newFakeIdP(t *testing.T, tls bool) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.URL,
			"jwks_uri":                              idp.URL + "/jwks",
			"authorization_endpoint":                idp.URL + "/authorize",
			"token_endpoint":                        idp.URL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig",
		}}})
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if idp.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	})
	if tls {
		idp.Server = httptest.NewTLSServer(handler)
	} else {
		idp.Server = httptest.NewServer(handler)
	}
	t.Cleanup(idp.Close)
	return idp
}

// token signs claims with the IdP key. Zero-value iss/exp/sub get sane defaults.
func (idp *fakeIdP) token(t *testing.T, claims map[string]any) string {
	t.Helper()
	return signToken(t, idp.key, idp.URL, claims)
}

func signToken(t *testing.T, key *rsa.PrivateKey, issuer string, claims map[string]any) string {
	t.Helper()
	return signTokenAlg(t, jose.RS256, key, issuer, claims)
}

func signTokenAlg(t *testing.T, alg jose.SignatureAlgorithm, key *rsa.PrivateKey, issuer string, claims map[string]any) string {
	t.Helper()
	all := map[string]any{
		"iss": issuer,
		"sub": "u-123",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range claims {
		if v == nil {
			delete(all, k)
			continue
		}
		all[k] = v
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: alg, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(all).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// anyClient skips the client check in tests that are about something else.
var anyClient = []string{"*"}

func newVerifier(t *testing.T, cfg OIDCConfig) *OIDCVerifier {
	t.Helper()
	v, err := NewOIDCVerifier(cfg)
	if err != nil {
		t.Fatalf("NewOIDCVerifier: %v", err)
	}
	return v
}

func TestOIDCVerifierAcceptsValidToken(t *testing.T) {
	idp := newFakeIdP(t, false)
	v := newVerifier(t, OIDCConfig{Issuer: idp.URL, Clients: anyClient})

	got, err := v.Verify(context.Background(), idp.token(t, map[string]any{
		"sub":                "u-123",
		"preferred_username": "alice",
	}))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	want := Claims{Subject: "u-123", PreferredUsername: "alice"}
	if got != want {
		t.Errorf("claims = %+v, want %+v", got, want)
	}
}

// oCIS's built-in identity provider signs access tokens with PS256.
func TestOIDCVerifierAcceptsSigningAlgorithmsOfOCIS(t *testing.T) {
	idp := newFakeIdP(t, false)
	v := newVerifier(t, OIDCConfig{Issuer: idp.URL, Clients: anyClient})

	for _, alg := range []jose.SignatureAlgorithm{jose.RS256, jose.PS256, jose.PS512} {
		t.Run(string(alg), func(t *testing.T) {
			raw := signTokenAlg(t, alg, idp.key, idp.URL, nil)
			if _, err := v.Verify(context.Background(), raw); err != nil {
				t.Fatalf("Verify: %v", err)
			}
		})
	}
}

func TestOIDCVerifierUsernameFallsBackToOCISLoginClaim(t *testing.T) {
	idp := newFakeIdP(t, false)
	v := newVerifier(t, OIDCConfig{Issuer: idp.URL, Clients: anyClient})

	// oCIS access tokens carry no preferred_username. The identity sits in a
	// claim whose name is literally "lg.i" (a flat key with a dot, not a nested
	// object): un is the login name and dn the display name.
	got, err := v.Verify(context.Background(), idp.token(t, map[string]any{
		"lg.i": map[string]any{"id": "ownCloudUUID=1", "un": "einstein", "dn": "Albert Einstein"},
	}))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.PreferredUsername != "einstein" {
		t.Errorf("PreferredUsername = %q, want %q", got.PreferredUsername, "einstein")
	}
	if got.DisplayName != "Albert Einstein" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Albert Einstein")
	}

	// The standard claims win when both are present.
	got, err = v.Verify(context.Background(), idp.token(t, map[string]any{
		"preferred_username": "alice",
		"name":               "Alice Liddell",
		"lg.i":               map[string]any{"un": "einstein", "dn": "Albert Einstein"},
	}))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.PreferredUsername != "alice" {
		t.Errorf("PreferredUsername = %q, want preferred_username to win", got.PreferredUsername)
	}
	if got.DisplayName != "Alice Liddell" {
		t.Errorf("DisplayName = %q, want the name claim to win", got.DisplayName)
	}

	// A token with neither source has no names; the caller decides what to do.
	got, err = v.Verify(context.Background(), idp.token(t, map[string]any{}))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.PreferredUsername != "" || got.DisplayName != "" {
		t.Errorf("names = %q, %q, want none", got.PreferredUsername, got.DisplayName)
	}
}

func TestOIDCVerifierRejectsBadTokens(t *testing.T) {
	idp := newFakeIdP(t, false)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"expired":        idp.token(t, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}),
		"wrong issuer":   idp.token(t, map[string]any{"iss": "https://evil.example"}),
		"unknown signer": signToken(t, otherKey, idp.URL, nil),
		"missing sub":    idp.token(t, map[string]any{"sub": nil}),
		"empty sub":      idp.token(t, map[string]any{"sub": ""}),
		"not a jwt":      "definitely-not-a-jwt",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			v := newVerifier(t, OIDCConfig{Issuer: idp.URL, Clients: anyClient})
			_, err := v.Verify(context.Background(), raw)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestOIDCVerifierClients(t *testing.T) {
	idp := newFakeIdP(t, false)

	tests := []struct {
		name    string
		clients []string
		claims  map[string]any
		ok      bool
	}{
		{"aud is the client", []string{"web"}, map[string]any{"aud": "web"}, true},
		{"aud is another client", []string{"web"}, map[string]any{"aud": "other"}, false},
		{"aud list contains the client", []string{"web"}, map[string]any{"aud": []string{"other", "web"}}, true},
		{"aud list without the client", []string{"web"}, map[string]any{"aud": []string{"other", "third"}}, false},
		{"aud account and azp is the client", []string{"web"}, map[string]any{"aud": "account", "azp": "web"}, true},
		{"aud account and azp is another client", []string{"web"}, map[string]any{"aud": "account", "azp": "other"}, false},
		{"no aud and no azp", []string{"web"}, nil, false},
		{"second of two clients", []string{"web", "desktop"}, map[string]any{"aud": "desktop"}, true},
		{"third client not listed", []string{"web", "desktop"}, map[string]any{"aud": "mobile"}, false},
		{"wildcard accepts any client", []string{"*"}, map[string]any{"aud": "anything"}, true},
		{"wildcard accepts a token without aud", []string{"*"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVerifier(t, OIDCConfig{Issuer: idp.URL, Clients: tt.clients})
			_, err := v.Verify(context.Background(), idp.token(t, tt.claims))
			if tt.ok && err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestNewOIDCVerifierRequiresClients(t *testing.T) {
	if _, err := NewOIDCVerifier(OIDCConfig{Issuer: "https://idp.example"}); err == nil {
		t.Fatal("no clients: want an error, so a forgotten setting cannot turn the check off")
	}
}

func TestOIDCVerifierRecoversWhenIdentityProviderComesUp(t *testing.T) {
	idp := newFakeIdP(t, false)
	idp.down.Store(true)
	v := newVerifier(t, OIDCConfig{Issuer: idp.URL, Clients: anyClient})
	raw := idp.token(t, nil)

	if _, err := v.Verify(context.Background(), raw); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("while down: err = %v, want ErrUnavailable", err)
	}

	idp.down.Store(false)
	if _, err := v.Verify(context.Background(), raw); err != nil {
		t.Fatalf("after recovery: %v", err)
	}
}

func TestOIDCVerifierUnreachableIssuer(t *testing.T) {
	idp := newFakeIdP(t, false)
	issuer := idp.URL
	idp.Close()

	v := newVerifier(t, OIDCConfig{Issuer: issuer, Clients: anyClient})
	_, err := v.Verify(context.Background(), "a.b.c")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestOIDCVerifierCABundle(t *testing.T) {
	idp := newFakeIdP(t, true)
	raw := idp.token(t, nil)

	t.Run("untrusted certificate", func(t *testing.T) {
		v := newVerifier(t, OIDCConfig{Issuer: idp.URL, Clients: anyClient})
		if _, err := v.Verify(context.Background(), raw); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	})
	t.Run("certificate trusted via CA file", func(t *testing.T) {
		caFile := filepath.Join(t.TempDir(), "ca.pem")
		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: idp.Certificate().Raw})
		if err := os.WriteFile(caFile, pemBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		v := newVerifier(t, OIDCConfig{Issuer: idp.URL, CAFile: caFile, Clients: anyClient})
		if _, err := v.Verify(context.Background(), raw); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	})
}

func TestNewOIDCVerifierValidatesConfig(t *testing.T) {
	dir := t.TempDir()
	emptyPEM := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(emptyPEM, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := map[string]OIDCConfig{
		"empty issuer":          {},
		"missing CA file":       {Issuer: "https://ocis.example", Clients: anyClient, CAFile: filepath.Join(dir, "nope.pem")},
		"CA file without certs": {Issuer: "https://ocis.example", Clients: anyClient, CAFile: emptyPEM},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewOIDCVerifier(cfg); err == nil {
				t.Fatal("NewOIDCVerifier succeeded, want an error")
			}
		})
	}
}
