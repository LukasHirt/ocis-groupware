package principals

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestIssueWithoutLifetimeUsesTheConfiguredOne(t *testing.T) {
	f := newFixture(t)
	clock := newFakeClock()
	r := f.openWith(t, nil, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost), WithAppPasswordLifetime(30*24*time.Hour))
	mustProvision(t, r, "sub-1", "alice", "Alice")

	ap, _ := mustIssue(t, r, "sub-1", "Phone", 0)
	if want := clock.Now().Add(30 * 24 * time.Hour); !ap.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", ap.ExpiresAt, want)
	}
	// A given lifetime is kept as it is.
	ap, _ = mustIssue(t, r, "sub-1", "Trip", 7*24*time.Hour)
	if want := clock.Now().Add(7 * 24 * time.Hour); !ap.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", ap.ExpiresAt, want)
	}
	if got := r.AppPasswordLifetime(); got != 30*24*time.Hour {
		t.Fatalf("AppPasswordLifetime = %v", got)
	}
}

func TestDefaultAppPasswordLifetimeIsOneYearWithoutAnOption(t *testing.T) {
	f := newAppFixture(t)
	if got := f.r.AppPasswordLifetime(); got != 365*24*time.Hour {
		t.Fatalf("AppPasswordLifetime = %v, want 365 days", got)
	}
}

// An imported token that signs in for the first time becomes an ordinary app
// password with the configured lifetime, not a longer one.
func TestLegacyTokenSwapUsesTheConfiguredLifetime(t *testing.T) {
	secret, tokens := loadClassicVector(t)
	f := newFixture(t)
	clock := newFakeClock()
	r := f.openWith(t, nil, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost),
		WithLegacyTokens(secret, legacyTTL), WithAppPasswordLifetime(30*24*time.Hour))
	mustProvision(t, r, "sub-1", "alice", "Alice")
	tok := tokens[0]
	if _, err := r.ImportLegacyToken(context.Background(), "sub-1", tok.Name, tok.SHA512, time.Time{}); err != nil {
		t.Fatal(err)
	}
	mustVerify(t, r, "alice", tok.Token, "sub-1")
	recs, err := r.appPasswordRecords("sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := clock.Now().Add(30 * 24 * time.Hour); !recs[0].ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt after the swap = %v, want %v", recs[0].ExpiresAt, want)
	}
}
