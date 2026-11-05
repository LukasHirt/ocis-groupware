package davmount

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/ratelimit"
)

const (
	legacySecret = "instance-secret"
	legacyToken  = "legacy-token-0123456789"
)

// limitedFixture mounts the handler with a limiter that allows 3 failures,
// and a registry that accepts imported classic tokens and asks the limiter
// before it compares one.
func limitedFixture(t *testing.T, proxies ...netip.Prefix) (*fixture, *prometheus.Registry) {
	t.Helper()
	f := newFixture(t)
	metrics := prometheus.NewRegistry()
	lim, err := ratelimit.New(ratelimit.Config{
		MaxFailures: 3, Window: 10 * time.Minute, TrustedProxies: proxies,
		Now: f.clock.Now, Registerer: metrics,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := principals.NewRegistry(f.dir, f.st, f.st.OwnerDir, f.logger,
		principals.WithBcryptCost(bcrypt.MinCost), principals.WithClock(f.clock.Now),
		principals.WithLegacyTokens(legacySecret, 0), principals.WithLegacyGate(lim.Gate))
	if err != nil {
		t.Fatal(err)
	}
	f.reg = reg
	dav, wk, err := New(Config{Store: f.st, Registry: reg, Verifier: f.verifier, Limiter: lim, Logger: f.logger})
	if err != nil {
		t.Fatal(err)
	}
	f.dav, f.wk = dav, wk
	return f, metrics
}

func (f *fixture) propfindAs(t *testing.T, user, password, remote string, extra ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", basic(user, password))
	req.Header.Set("Depth", "0")
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	rec := httptest.NewRecorder()
	f.dav.ServeHTTP(rec, req)
	return rec
}

func assertTooMany(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body %q", rec.Code, rec.Body.String())
	}
	secs, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || secs <= 0 {
		t.Errorf("Retry-After = %q, want positive seconds", rec.Header().Get("Retry-After"))
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Error("a 429 must not challenge again")
	}
	if got := rec.Body.String(); got != "too many requests\n" {
		t.Errorf("body = %q", got)
	}
}

func TestRateLimiterRefusesAfterFailedPasswords(t *testing.T) {
	f, metrics := limitedFixture(t)
	secret := f.user(t, "sub-alice", "alice")

	for i := 0; i < 3; i++ {
		assertUnauthorized(t, f.propfindAs(t, "alice", "wrong", "198.51.100.1:4000"))
	}
	rec := f.propfindAs(t, "alice", "wrong", "198.51.100.1:4000")
	assertTooMany(t, rec)
	if rec.Header().Get("Retry-After") != "600" {
		t.Errorf("Retry-After = %q, want 600", rec.Header().Get("Retry-After"))
	}
	// The right password does not get through while blocked.
	assertTooMany(t, f.propfindAs(t, "alice", secret, "198.51.100.1:4000"))
	// The user from another address is not blocked.
	if rec := f.propfindAs(t, "alice", secret, "198.51.100.2:4000"); rec.Code != http.StatusMultiStatus {
		t.Errorf("other address: status = %d, want 207", rec.Code)
	}
	// The block ends with the window.
	f.clock.advance(10 * time.Minute)
	if rec := f.propfindAs(t, "alice", secret, "198.51.100.1:4000"); rec.Code != http.StatusMultiStatus {
		t.Errorf("after the window: status = %d, want 207", rec.Code)
	}

	families, _ := metrics.Gather()
	seen := map[string]float64{}
	for _, mf := range families {
		for _, m := range mf.GetMetric() {
			seen[mf.GetName()] += m.GetCounter().GetValue()
		}
	}
	if seen["groupware_auth_failures_total"] != 3 || seen["groupware_auth_rate_limited_total"] != 2 {
		t.Errorf("metrics = %v", seen)
	}
}

func TestSuccessfulSignInsAreNotLimited(t *testing.T) {
	f, _ := limitedFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	for i := 0; i < 50; i++ {
		if rec := f.propfindAs(t, "alice", secret, "198.51.100.1:4000"); rec.Code != http.StatusMultiStatus {
			t.Fatalf("request %d: status = %d, want 207", i, rec.Code)
		}
	}
	// A typo between good requests does not add up.
	for i := 0; i < 10; i++ {
		assertUnauthorized(t, f.propfindAs(t, "alice", "typo", "198.51.100.1:4000"))
		if rec := f.propfindAs(t, "alice", secret, "198.51.100.1:4000"); rec.Code != http.StatusMultiStatus {
			t.Fatalf("round %d: status = %d, want 207", i, rec.Code)
		}
	}
}

func TestBearerIsNotLimited(t *testing.T) {
	f, _ := limitedFixture(t)
	for i := 0; i < 5; i++ {
		f.propfindAs(t, "alice", "wrong", "198.51.100.1:4000")
	}
	f.verifier.tokens["good"] = auth.Claims{Subject: "sub-bob", PreferredUsername: "bob"}
	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", "Bearer good")
	req.Header.Set("Depth", "0")
	req.RemoteAddr = "198.51.100.1:4000"
	rec := httptest.NewRecorder()
	f.dav.ServeHTTP(rec, req)
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("bearer from a blocked address: status = %d, want 207", rec.Code)
	}
}

func TestRateLimiterHonoursForwardedForFromTrustedProxiesOnly(t *testing.T) {
	f, _ := limitedFixture(t, netip.MustParsePrefix("10.0.0.0/8"))
	secret := f.user(t, "sub-alice", "alice")

	// Behind the proxy, failures are counted per client.
	for i := 0; i < 3; i++ {
		f.propfindAs(t, "alice", "wrong", "10.0.0.2:4000", "X-Forwarded-For", "203.0.113.7")
	}
	assertTooMany(t, f.propfindAs(t, "alice", secret, "10.0.0.2:4000", "X-Forwarded-For", "203.0.113.7"))
	if rec := f.propfindAs(t, "alice", secret, "10.0.0.2:4000", "X-Forwarded-For", "203.0.113.8"); rec.Code != http.StatusMultiStatus {
		t.Errorf("another client behind the proxy: status = %d, want 207", rec.Code)
	}

	// A peer that is not a proxy cannot pick its own address.
	for i := 0; i < 3; i++ {
		f.propfindAs(t, "alice", "wrong", "198.51.100.1:4000", "X-Forwarded-For", "192.0.2."+strconv.Itoa(i))
	}
	assertTooMany(t, f.propfindAs(t, "alice", secret, "198.51.100.1:4000", "X-Forwarded-For", "192.0.2.99"))
}

func legacyDigest(token string) string {
	sum := sha512.Sum512([]byte(token + legacySecret))
	return hex.EncodeToString(sum[:])
}

func TestLegacyTokenChecksAreLimited(t *testing.T) {
	f, _ := limitedFixture(t)
	f.user(t, "sub-alice", "alice")
	if _, err := f.reg.ImportLegacyToken(context.Background(), "sub-alice", "Old phone", legacyDigest(legacyToken), time.Time{}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		assertUnauthorized(t, f.propfindAs(t, "alice", "guess-"+strconv.Itoa(i), "198.51.100.1:4000"))
	}
	// The digest of the real token is not compared any more, so the right
	// token does not sign in while blocked and is not used up.
	assertTooMany(t, f.propfindAs(t, "alice", legacyToken, "198.51.100.1:4000"))

	f.clock.advance(10 * time.Minute)
	if rec := f.propfindAs(t, "alice", legacyToken, "198.51.100.1:4000"); rec.Code != http.StatusMultiStatus {
		t.Fatalf("right token after the window: status = %d, want 207", rec.Code)
	}
}

// stubGate refuses every legacy token check, with or without a retry time.
func TestRateLimitedGateAnswers429(t *testing.T) {
	for name, gateErr := range map[string]error{
		"plain":      principals.ErrRateLimited,
		"with retry": &ratelimit.LimitedError{RetryAfter: 90 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			reg, err := principals.NewRegistry(f.dir, f.st, f.st.OwnerDir, f.logger,
				principals.WithBcryptCost(bcrypt.MinCost), principals.WithClock(f.clock.Now),
				principals.WithLegacyTokens(legacySecret, 0),
				principals.WithLegacyGate(func(context.Context, string) error { return gateErr }))
			if err != nil {
				t.Fatal(err)
			}
			f.reg = reg
			f.mount(t)
			f.user(t, "sub-alice", "alice")
			if _, err := reg.ImportLegacyToken(context.Background(), "sub-alice", "Old phone", legacyDigest(legacyToken), time.Time{}); err != nil {
				t.Fatal(err)
			}
			rec := f.propfindAs(t, "alice", legacyToken, "")
			assertTooMany(t, rec)
			if name == "with retry" && rec.Header().Get("Retry-After") != "90" {
				t.Errorf("Retry-After = %q, want 90", rec.Header().Get("Retry-After"))
			}
		})
	}
}
