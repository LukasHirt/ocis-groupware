package principals

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const testLease = 720 * time.Hour

// leaseFixture is an appFixture whose registry enforces an identity lease of
// testLease. Principal sub-1 (alice) has an identity snapshot taken now.
func newLeaseFixture(t *testing.T, opts ...Option) appFixture {
	t.Helper()
	f := newFixture(t)
	clock := newFakeClock()
	opts = append([]Option{WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost), WithIdentityMaxAge(testLease)}, opts...)
	r := f.openWith(t, nil, opts...)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	af := appFixture{fixture: f, clock: clock, r: r}
	af.renew(t)
	return af
}

// renew takes an identity snapshot of alice at the clock's time, as a bearer
// sign-in does.
func (f appFixture) renew(t *testing.T) {
	t.Helper()
	if err := f.r.SetIdentity(context.Background(), "sub-1", Identity{OCISID: "id-1", At: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBasicLease(t *testing.T) {
	tests := []struct {
		name string
		age  time.Duration // clock advance after the snapshot
		want error
	}{
		{"fresh", time.Hour, nil},
		{"just inside the lease", testLease - time.Second, nil},
		{"just expired", testLease, ErrIdentityStale},
		{"long expired", 10 * testLease, ErrIdentityStale},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLeaseFixture(t, WithLegacyTokens("", 0))
			_, secret := mustIssue(t, f.r, "sub-1", "phone", 20*testLease)
			f.clock.Add(tt.age)
			if tt.want == nil {
				mustVerify(t, f.r, "alice", secret, "sub-1")
				return
			}
			assertVerifyErr(t, f.r, "alice", secret, tt.want)
		})
	}
}

func TestVerifyBasicLeaseNeverSnapshotted(t *testing.T) {
	f := newFixture(t)
	clock := newFakeClock()
	r := f.openWith(t, nil, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost), WithIdentityMaxAge(testLease))
	mustProvision(t, r, "sub-1", "alice", "Alice")
	_, secret := mustIssue(t, r, "sub-1", "phone", 24*time.Hour)
	assertVerifyErr(t, r, "alice", secret, ErrIdentityStale)
}

func TestVerifyBasicLeaseOff(t *testing.T) {
	f := newFixture(t)
	clock := newFakeClock()
	r := f.openWith(t, nil, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost), WithIdentityMaxAge(0))
	mustProvision(t, r, "sub-1", "alice", "Alice")
	_, secret := mustIssue(t, r, "sub-1", "phone", 100*testLease)
	clock.Add(50 * testLease)
	mustVerify(t, r, "alice", secret, "sub-1")
}

func TestVerifyBasicLeaseOrder(t *testing.T) {
	f := newLeaseFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "phone", 24*time.Hour)
	wrong := wrongPassword(t, f.r, "sub-1")

	// Both the lease and the password's lifetime are over: the expiry is
	// reported first.
	f.clock.Add(testLease + time.Hour)
	assertVerifyErr(t, f.r, "alice", secret, ErrExpired)
	// A wrong password is a wrong password, not a stale lease.
	assertVerifyErr(t, f.r, "alice", wrong, ErrInvalidCredentials)
	if errors.Is(ErrInvalidCredentials, ErrIdentityStale) {
		t.Fatal("the errors must differ")
	}
}

func TestVerifyBasicStaleLeaseCostsTheSameCompares(t *testing.T) {
	f := newLeaseFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "phone", 5*testLease)
	compares := countCompares(f.r)

	// The cache must not hide the compare: use a fresh registry state per case.
	f.r.cache.invalidate("sub-1")
	mustVerify(t, f.r, "alice", secret, "sub-1")
	fresh := compares.Swap(0)

	f.r.cache.invalidate("sub-1")
	f.clock.Add(testLease)
	assertVerifyErr(t, f.r, "alice", secret, ErrIdentityStale)
	if stale := compares.Load(); stale != fresh {
		t.Fatalf("stale lease cost %d compares, fresh %d", stale, fresh)
	}
}

func TestVerifyBasicCachedCredentialRefusedWhenLeaseRunsOut(t *testing.T) {
	f := newLeaseFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "phone", 5*testLease)
	mustVerify(t, f.r, "alice", secret, "sub-1")

	// Seconds before the lease ends the credential is cached.
	f.clock.Add(testLease - 2*time.Minute)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	// The cache entry would last five minutes more. The lease does not.
	f.clock.Add(3 * time.Minute)
	assertVerifyErr(t, f.r, "alice", secret, ErrIdentityStale)
}

func TestVerifyBasicCacheEntryExpiresAtTheLease(t *testing.T) {
	f := newLeaseFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "phone", 5*testLease)
	f.clock.Add(testLease - time.Minute)
	mustVerify(t, f.r, "alice", secret, "sub-1")

	key := f.r.cache.key(basicHeader("alice", secret))
	e, ok := f.r.cache.get(key, f.clock.Now())
	if !ok {
		t.Fatal("credential is not cached")
	}
	if want := f.clock.Now().Add(time.Minute); !e.expires.Equal(want) {
		t.Fatalf("entry expires %v, want the end of the lease %v", e.expires, want)
	}
}

func TestVerifyBasicRenewedLeaseWorksAgain(t *testing.T) {
	f := newLeaseFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "phone", 5*testLease)
	f.clock.Add(testLease)
	assertVerifyErr(t, f.r, "alice", secret, ErrIdentityStale)
	f.renew(t)
	mustVerify(t, f.r, "alice", secret, "sub-1")
}

func TestVerifyBasicDisabledBeatsStaleLease(t *testing.T) {
	f := newLeaseFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "phone", 5*testLease)
	if _, err := f.r.SetDisabled(context.Background(), "sub-1", true); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(testLease)
	assertVerifyErr(t, f.r, "alice", secret, ErrDisabled)
}

func TestVerifyBasicLegacyTokenWithStaleLeaseIsNotUsedUp(t *testing.T) {
	f := newLeaseFixtureLegacy(t)
	tok := f.tokens[0]
	if _, err := f.r.ImportLegacyToken(context.Background(), "sub-1", tok.Name, tok.SHA512, time.Time{}); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(testLease)
	assertVerifyErr(t, f.r, "alice", tok.Token, ErrIdentityStale)

	// The digest is still there, so the token works once the user is back.
	recs, err := f.r.appPasswordRecords("sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].LegacySHA512 != tok.SHA512 {
		t.Fatalf("records = %+v, want the digest kept", recs)
	}
	f.renew(t)
	mustVerify(t, f.r, "alice", tok.Token, "sub-1")
}

func newLeaseFixtureLegacy(t *testing.T) legacyFixture {
	t.Helper()
	secret, tokens := loadClassicVector(t)
	f := newFixture(t)
	clock := newFakeClock()
	r := f.openWith(t, nil, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost),
		WithLegacyTokens(secret, 20*testLease), WithIdentityMaxAge(testLease))
	mustProvision(t, r, "sub-1", "alice", "Alice")
	lf := legacyFixture{appFixture: appFixture{fixture: f, clock: clock, r: r}, secret: secret, tokens: tokens}
	lf.renew(t)
	return lf
}
