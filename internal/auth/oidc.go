package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// discoveryTimeout bounds each request to the identity provider (discovery
// document and key set).
const discoveryTimeout = 10 * time.Second

// jwksCooldown is the shortest time between two requests for the key set. A
// token with an unknown key id makes go-oidc fetch the key set, and anyone can
// send one, so without a limit each forged token could cost a request to the
// identity provider. A key that was rotated in is found after the cooldown.
const jwksCooldown = 30 * time.Second

// Discovery that fails is not retried at once. The wait starts at
// discoveryBackoffMin and doubles up to discoveryBackoffMax.
const (
	discoveryBackoffMin = time.Second
	discoveryBackoffMax = time.Minute
)

// errDiscoveryBusy is what a request gets while another one runs discovery.
var errDiscoveryBusy = errors.New("auth: identity provider discovery is in progress")

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

	now func() time.Time

	// mu guards the fields below and is never held during a request to the
	// identity provider.
	mu          sync.Mutex
	verifier    *oidc.IDTokenVerifier
	discovering bool
	retryAt     time.Time
	backoff     time.Duration
	lastErr     error
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
	return &OIDCVerifier{cfg: cfg, client: client, now: time.Now}, nil
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
		Client:            v.clientOf(token.Audience, extra.AuthorizedParty),
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
// first if it hasn't succeeded yet. Only one request runs discovery at a time.
// The others, and every request during the wait after a failure, fail at once
// instead of queueing behind a slow identity provider.
func (v *OIDCVerifier) idTokenVerifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	switch {
	case v.verifier != nil:
		defer v.mu.Unlock()
		return v.verifier, nil
	case v.discovering:
		v.mu.Unlock()
		return nil, errDiscoveryBusy
	case v.now().Before(v.retryAt):
		defer v.mu.Unlock()
		return nil, v.lastErr
	}
	v.discovering = true
	v.mu.Unlock()

	// The result is shared, so one request that gives up must not cancel it.
	verifier, err := v.discover(context.WithoutCancel(ctx))

	v.mu.Lock()
	defer v.mu.Unlock()
	v.discovering = false
	if err != nil {
		v.backoff = min(max(2*v.backoff, discoveryBackoffMin), discoveryBackoffMax)
		v.retryAt = v.now().Add(v.backoff)
		v.lastErr = err
		return nil, err
	}
	v.verifier, v.backoff, v.lastErr = verifier, 0, nil
	return verifier, nil
}

func (v *OIDCVerifier) discover(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, v.client), v.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	var doc struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if err := provider.Claims(&doc); err != nil {
		return nil, fmt.Errorf("auth: reading the discovery document: %w", err)
	}
	if doc.JWKSURL == "" {
		return nil, errors.New("auth: the discovery document has no jwks_uri")
	}

	// The key set is fetched through a client that limits how often it may
	// ask for the key set.
	limited := *v.client
	limited.Transport = &cooldownTransport{base: transportOf(v.client), url: doc.JWKSURL, every: jwksCooldown, now: v.now}
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(ctx, &limited), doc.JWKSURL)
	return oidc.NewVerifier(v.cfg.Issuer, keys, &oidc.Config{
		SkipClientIDCheck:    true,
		SupportedSigningAlgs: signingAlgs,
	}), nil
}

func transportOf(c *http.Client) http.RoundTripper {
	if c.Transport != nil {
		return c.Transport
	}
	return http.DefaultTransport
}

// cooldownTransport refuses a request for url that comes less than every after
// the previous one. Other requests pass.
type cooldownTransport struct {
	base  http.RoundTripper
	url   string
	every time.Duration
	now   func() time.Time

	mu   sync.Mutex
	last time.Time
}

func (t *cooldownTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() == t.url {
		t.mu.Lock()
		now := t.now()
		if !t.last.IsZero() && now.Sub(t.last) < t.every {
			t.mu.Unlock()
			return nil, errors.New("auth: key set was fetched recently, not fetching it again yet")
		}
		t.last = now
		t.mu.Unlock()
	}
	return t.base.RoundTrip(req)
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

// clientOf names the client a token was issued to, for the audit log.
func (v *OIDCVerifier) clientOf(audience []string, azp string) string {
	if azp != "" {
		return azp
	}
	for _, aud := range audience {
		if slices.Contains(v.cfg.Clients, aud) {
			return aud
		}
	}
	if len(audience) > 0 {
		return audience[0]
	}
	return ""
}
