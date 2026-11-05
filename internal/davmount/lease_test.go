package davmount

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/ratelimit"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

const testLease = 720 * time.Hour

// leaseFixture mounts the handler with a 720h identity lease, a limiter that
// allows 3 failures and a counter for refused leases. alice has an app
// password and an identity snapshot from now.
func leaseFixture(t *testing.T) (*fixture, string, prometheus.Counter) {
	t.Helper()
	f := newFixtureWithLease(t, testLease)
	lim, err := ratelimit.New(ratelimit.Config{MaxFailures: 3, Window: 10 * time.Minute, Now: f.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	stale := prometheus.NewCounter(prometheus.CounterOpts{Name: "stale_total"})
	dav, wk, err := New(Config{Store: f.st, Registry: f.reg, Verifier: f.verifier, Limiter: lim, IdentityStale: stale, Now: f.clock.Now, Logger: f.logger})
	if err != nil {
		t.Fatal(err)
	}
	f.dav, f.wk = dav, wk
	secret := f.user(t, "sub-alice", "alice")
	if err := f.reg.SetIdentity(t.Context(), "sub-alice", principals.Identity{OCISID: "u-alice", At: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	return f, secret, stale
}

func TestStaleLeaseIsRefusedLikeAWrongPassword(t *testing.T) {
	f, secret, stale := leaseFixture(t)
	// The user signs in for an hour, then the lease runs out.
	if rec := f.propfindAs(t, "alice", secret, "198.51.100.1:4000"); rec.Code != http.StatusMultiStatus {
		t.Fatalf("fresh lease: status = %d", rec.Code)
	}
	f.clock.advance(testLease)

	for i := 0; i < 10; i++ {
		assertUnauthorized(t, f.propfindAs(t, "alice", secret, "198.51.100.1:4000"))
	}
	// A stale lease is not guessing: the limiter was not charged, so the
	// user is not locked out and is let in as soon as the lease is renewed.
	if err := f.reg.SetIdentity(t.Context(), "sub-alice", principals.Identity{OCISID: "u-alice", At: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	if rec := f.propfindAs(t, "alice", secret, "198.51.100.1:4000"); rec.Code != http.StatusMultiStatus {
		t.Fatalf("after renewal: status = %d, want 207", rec.Code)
	}
	if got := counterValue(t, stale); got != 10 {
		t.Errorf("stale counter = %v, want 10", got)
	}
}

func TestStaleLeaseIsLoggedOncePerHour(t *testing.T) {
	f, secret, _ := leaseFixture(t)
	f.clock.advance(testLease)
	count := func() int { return strings.Count(f.logs.String(), "app password refused") }

	for i := 0; i < 5; i++ {
		assertUnauthorized(t, f.propfindAs(t, "alice", secret, ""))
	}
	if n := count(); n != 1 {
		t.Fatalf("%d log lines for five refusals, want 1:\n%s", n, f.logs.String())
	}
	f.clock.advance(time.Hour)
	assertUnauthorized(t, f.propfindAs(t, "alice", secret, ""))
	if n := count(); n != 2 {
		t.Fatalf("%d log lines after an hour, want 2", n)
	}
	if strings.Contains(f.logs.String(), secret) {
		t.Error("the log contains the password")
	}
}

func TestStaleLeaseAlsoRefusedOnCompatPaths(t *testing.T) {
	f, secret, _ := leaseFixture(t)
	compat, err := NewCompat(Config{Store: f.st, Registry: f.reg, Verifier: f.verifier, Now: f.clock.Now, Logger: f.logger})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(testLease)
	req := httptest.NewRequest("PROPFIND", "/remote.php/dav/principals/users/alice/", nil)
	req.Header.Set("Authorization", basic("alice", secret))
	req.Header.Set("Depth", "0")
	rec := httptest.NewRecorder()
	compat.ServeHTTP(rec, req)
	assertUnauthorized(t, rec)
}

// A collection shared with a group is not listed once the lease of the
// recipient has run out, and is listed again after a bearer request.
func TestGroupShareFollowsTheLease(t *testing.T) {
	g := &graphStub{}
	sf := newShareFixtureWith(t, "sub-alice", testLease, g)
	f := sf.fixture
	err := sf.shares.Grant(t.Context(), sf.cal.Ref, sf.cal.ID,
		sharing.Member{Type: sharing.Group, ID: "g-1", Name: "users", Access: sharing.Read})
	if err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens["tok-bob"] = auth.Claims{Subject: "sub-bob", PreferredUsername: "bob"}
	if err := f.reg.SetIdentity(t.Context(), "sub-bob", principals.Identity{OCISID: "u-bob", Groups: []principals.Group{{ID: "g-1", Name: "users"}}, At: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}

	list := func(header string) *httptest.ResponseRecorder {
		return sf.do(t, "PROPFIND", "/dav/calendars/bob/", propfindName, "Authorization", header, "Depth", "1")
	}
	if rec := list(sf.bob); !strings.Contains(rec.Body.String(), sf.sharedPath()) {
		t.Fatalf("fresh lease: not listed:\n%s", rec.Body.String())
	}

	f.clock.advance(testLease)
	rec := list(sf.bob)
	assertUnauthorized(t, rec)
	if strings.Contains(rec.Body.String(), "shared-") {
		t.Fatal("a stale lease still lists the shared calendar")
	}

	// One bearer request renews the lease, and the group snapshot with it.
	if rec := list("Bearer tok-bob"); rec.Code != http.StatusMultiStatus {
		t.Fatalf("bearer: status = %d", rec.Code)
	}
	if rec := list(sf.bob); !strings.Contains(rec.Body.String(), sf.sharedPath()) {
		t.Fatalf("after the bearer request: not listed:\n%s", rec.Body.String())
	}
}

// counterValue reads a counter through a registry, as a scrape does.
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatal(err)
	}
	families, err := reg.Gather()
	if err != nil || len(families) != 1 {
		t.Fatalf("Gather: %v, %d families", err, len(families))
	}
	return families[0].GetMetric()[0].GetCounter().GetValue()
}
