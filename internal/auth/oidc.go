package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// discoveryTimeout bounds each request to the identity provider (discovery
// document and key set).
const discoveryTimeout = 10 * time.Second

// signingAlgs lists the RSA signature algorithms accepted on tokens. go-oidc
// defaults to RS256 only, but the oCIS identity provider signs with PS256.
var signingAlgs = []string{
	oidc.RS256, oidc.RS384, oidc.RS512,
	oidc.PS256, oidc.PS384, oidc.PS512,
}

// OIDCConfig configures an OIDCVerifier.
type OIDCConfig struct {
	// Issuer is the OIDC issuer URL, e.g. the oCIS public URL.
	Issuer string
	// Clients lists the OAuth client ids whose tokens are accepted. A token
	// passes when its "aud" claim contains one of them or its "azp" claim
	// equals one of them. oCIS puts the client id of the requesting app in
	// "aud" (for example "web"). Other identity providers, Keycloak among
	// them, often put a generic audience there and name the client in "azp".
	// The single entry "*" accepts tokens of any client. The list must not be
	// empty.
	Clients []string
	// CAFile is an optional PEM bundle of extra CAs to trust when talking to
	// the issuer, on top of the system roots.
	CAFile string
}

// OIDCVerifier validates access tokens against an OIDC issuer's published
// keys. The issuer is contacted on first use and again after any failure, so
// ocis-groupware can start before the identity provider is ready.
type OIDCVerifier struct {
	cfg    OIDCConfig
	client *http.Client

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

// NewOIDCVerifier validates cfg and loads the CA bundle. It doesn't contact the
// issuer.
func NewOIDCVerifier(cfg OIDCConfig) (*OIDCVerifier, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("auth: oidc issuer must not be empty")
	}
	if len(cfg.Clients) == 0 {
		return nil, errors.New(`auth: no oidc clients configured (use "*" to accept any client)`)
	}

	client, err := NewHTTPClient(cfg.CAFile, discoveryTimeout)
	if err != nil {
		return nil, err
	}
	return &OIDCVerifier{cfg: cfg, client: client}, nil
}

// NewHTTPClient returns an HTTP client with the given timeout that trusts the
// system roots and, when caFile is not empty, the certificates in that PEM
// bundle. It is how the service reaches oCIS, for the identity provider and
// for the Graph API alike.
func NewHTTPClient(caFile string, timeout time.Duration) (*http.Client, error) {
	client := &http.Client{Timeout: timeout}
	if caFile != "" {
		pool, err := loadCAPool(caFile)
		if err != nil {
			return nil, err
		}
		client.Transport = &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		}
	}
	return client, nil
}

func loadCAPool(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth: reading oidc CA file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("auth: oidc CA file %s contains no PEM certificates", path)
	}
	return pool, nil
}

// Verify implements Verifier.
func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (Claims, error) {
	verifier, err := v.idTokenVerifier(ctx)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	// The verifier checks signature, issuer and expiry. The client check is
	// ours, because go-oidc only knows a single audience and no "azp".
	token, err := verifier.Verify(oidc.ClientContext(ctx, v.client), rawToken)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if token.Subject == "" {
		return Claims{}, fmt.Errorf("%w: missing sub claim", ErrInvalidToken)
	}

	var extra struct {
		AuthorizedParty   string `json:"azp"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
		// oCIS access tokens carry no preferred_username. The identity is in a
		// claim whose name is literally "lg.i" (a flat key with a dot): un is the
		// login name and dn the display name.
		Login struct {
			Username    string `json:"un"`
			DisplayName string `json:"dn"`
		} `json:"lg.i"`
	}
	if err := token.Claims(&extra); err != nil {
		return Claims{}, fmt.Errorf("%w: decoding claims: %w", ErrInvalidToken, err)
	}
	if !v.clientAllowed(token.Audience, extra.AuthorizedParty) {
		return Claims{}, fmt.Errorf("%w: token was not issued to a configured client", ErrInvalidToken)
	}
	claims := Claims{
		Subject:           token.Subject,
		PreferredUsername: extra.PreferredUsername,
		DisplayName:       extra.Name,
	}
	if claims.PreferredUsername == "" {
		claims.PreferredUsername = extra.Login.Username
	}
	if claims.DisplayName == "" {
		claims.DisplayName = extra.Login.DisplayName
	}
	return claims, nil
}

// idTokenVerifier returns the cached go-oidc verifier, running discovery
// first if it hasn't succeeded yet.
func (v *OIDCVerifier) idTokenVerifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}

	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, v.client), v.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	v.verifier = provider.Verifier(&oidc.Config{
		SkipClientIDCheck:    true,
		SupportedSigningAlgs: signingAlgs,
	})
	return v.verifier, nil
}

// clientAllowed reports whether a token with these audiences and authorized
// party was issued to one of the configured clients.
func (v *OIDCVerifier) clientAllowed(audience []string, azp string) bool {
	for _, client := range v.cfg.Clients {
		if client == "*" || (azp != "" && azp == client) {
			return true
		}
		for _, aud := range audience {
			if aud == client {
				return true
			}
		}
	}
	return false
}
