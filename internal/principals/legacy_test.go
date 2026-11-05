package principals

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// classicVector is one app token of the classic test instance, from the
// description next to its database dump.
type classicVector struct {
	User, Name, Token, SHA512 string
}

// loadClassicVector returns the instance secret and the app tokens that the
// classic fixture's description lists.
func loadClassicVector(t *testing.T) (string, []classicVector) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "classic", "dumps", "mariadb.json"))
	if err != nil {
		t.Fatal(err)
	}
	var desc struct {
		Secret    string          `json:"secret"`
		AppTokens []classicVector `json:"app_tokens"`
	}
	if err := json.Unmarshal(data, &desc); err != nil {
		t.Fatal(err)
	}
	if desc.Secret == "" || len(desc.AppTokens) == 0 {
		t.Fatal("fixture description has no secret or tokens")
	}
	return desc.Secret, desc.AppTokens
}

type legacyFixture struct {
	appFixture
	secret string
	tokens []classicVector
}

const legacyTTL = 180 * 24 * time.Hour

func newLegacyFixture(t *testing.T, opts ...Option) legacyFixture {
	t.Helper()
	secret, tokens := loadClassicVector(t)
	f := newFixture(t)
	clock := newFakeClock()
	opts = append([]Option{WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost), WithLegacyTokens(secret, legacyTTL)}, opts...)
	r := f.openWith(t, nil, opts...)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	return legacyFixture{appFixture: appFixture{fixture: f, clock: clock, r: r}, secret: secret, tokens: tokens}
}

func (f legacyFixture) importToken(t *testing.T, i int) AppPassword {
	t.Helper()
	tok := f.tokens[i]
	ap, err := f.r.ImportLegacyToken(context.Background(), "sub-1", tok.Name, tok.SHA512, time.Time{})
	if err != nil {
		t.Fatalf("ImportLegacyToken(%q): %v", tok.Name, err)
	}
	return ap
}

func TestFixtureDigestsMatchTheScheme(t *testing.T) {
	secret, tokens := loadClassicVector(t)
	for _, tok := range tokens {
		sum := sha512.Sum512([]byte(tok.Token + secret))
		if got := hex.EncodeToString(sum[:]); got != tok.SHA512 {
			t.Errorf("token %q: sha512(token . secret) = %s, fixture says %s", tok.Name, got, tok.SHA512)
		}
	}
}

func TestLegacyTokenFromTheClassicFixtureVerifiesOnce(t *testing.T) {
	f := newLegacyFixture(t)
	ap := f.importToken(t, 0)
	if want := f.clock.Now().Add(legacyTTL); !ap.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", ap.ExpiresAt, want)
	}
	if got := readAppFile(t, f.fixture, "sub-1").AppPasswords[0]; got.LegacySHA512 != f.tokens[0].SHA512 || got.Hash != "" {
		t.Fatalf("stored record = %+v, want the digest and no bcrypt hash", got)
	}

	mustVerify(t, f.r, "alice", f.tokens[0].Token, "sub-1")

	got := readAppFile(t, f.fixture, "sub-1").AppPasswords
	if len(got) != 1 || got[0].ID != ap.ID {
		t.Fatalf("records = %+v, want the one imported record", got)
	}
	if got[0].LegacySHA512 != "" {
		t.Fatal("the legacy digest is still stored after the first sign-in")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(got[0].Hash), []byte(f.tokens[0].Token)); err != nil {
		t.Fatalf("stored hash does not match the token: %v", err)
	}
	if want := f.clock.Now().Add(defaultAppPasswordLifetime); !got[0].ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt after the swap = %v, want %v", got[0].ExpiresAt, want)
	}

	// A fresh registry has no cache and no legacy digest to go by: the token
	// now has to verify through bcrypt.
	r2 := f.reopen(t, nil)
	gate := 0
	r2.legacySecret, r2.legacyTTL = f.secret, legacyTTL
	r2.legacyGate = func(context.Context, string) error { gate++; return nil }
	mustVerify(t, r2, "alice", f.tokens[0].Token, "sub-1")
	if gate != 0 {
		t.Fatal("the legacy path ran for a token that was swapped already")
	}
}

func TestLegacyTokenIsCachedAfterTheSwap(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)
	mustVerify(t, f.r, "alice", f.tokens[0].Token, "sub-1")
	n := countCompares(f.r)
	mustVerify(t, f.r, "alice", f.tokens[0].Token, "sub-1")
	if got := n.Load(); got != 0 {
		t.Fatalf("the second sign-in made %d bcrypt compares, want 0 (cached)", got)
	}
}

func TestLegacyTokenSecondTokenAndOtherUser(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)
	f.importToken(t, 1)
	mustVerify(t, f.r, "alice", f.tokens[1].Token, "sub-1")
	mustVerify(t, f.r, "alice", f.tokens[0].Token, "sub-1")

	// Bob's token does not work for alice.
	mustProvision(t, f.r, "sub-2", "bob", "Bob")
	assertVerifyErr(t, f.r, "alice", f.tokens[2].Token, ErrInvalidCredentials)
}

func TestLegacyTokenWrongSecretOrToken(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)

	other := f.openWith(t, nil, WithClock(f.clock.Now), WithBcryptCost(bcrypt.MinCost), WithLegacyTokens("another secret", legacyTTL))
	assertVerifyErr(t, other, "alice", f.tokens[0].Token, ErrInvalidCredentials)

	none := f.openWith(t, nil, WithClock(f.clock.Now), WithBcryptCost(bcrypt.MinCost))
	assertVerifyErr(t, none, "alice", f.tokens[0].Token, ErrInvalidCredentials)

	assertVerifyErr(t, f.r, "alice", f.tokens[1].Token, ErrInvalidCredentials)
	assertVerifyErr(t, f.r, "alice", strings.ToLower(f.tokens[0].Token), ErrInvalidCredentials)

	// None of that used the token up.
	if got := readAppFile(t, f.fixture, "sub-1").AppPasswords[0]; got.LegacySHA512 == "" {
		t.Fatal("a failed attempt removed the legacy digest")
	}
	mustVerify(t, f.r, "alice", f.tokens[0].Token, "sub-1")
}

func TestLegacyTokenSwapHappensOnceUnderConcurrency(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)
	var hashes atomic.Int64
	f.r.hash = func(p []byte, cost int) ([]byte, error) {
		hashes.Add(1)
		return bcrypt.GenerateFromPassword(p, cost)
	}

	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.r.VerifyBasic(context.Background(), "alice", f.tokens[0].Token)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("VerifyBasic: %v", err)
		}
	}
	if got := hashes.Load(); got != 1 {
		t.Fatalf("the token was hashed %d times, want 1", got)
	}
	if recs := readAppFile(t, f.fixture, "sub-1").AppPasswords; len(recs) != 1 || recs[0].LegacySHA512 != "" || recs[0].Hash == "" {
		t.Fatalf("records = %+v", recs)
	}
}

func TestLegacyTokenExpires(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)
	f.clock.Add(legacyTTL + time.Hour)
	assertVerifyErr(t, f.r, "alice", f.tokens[0].Token, ErrExpired)
	if got := readAppFile(t, f.fixture, "sub-1").AppPasswords[0]; got.LegacySHA512 == "" {
		t.Fatal("an expired token was swapped")
	}
}

func TestLegacyTokenExpiryCountsFromLastUse(t *testing.T) {
	f := newLegacyFixture(t)
	lastUsed := f.clock.Now().Add(-100 * 24 * time.Hour)
	tok := f.tokens[0]
	ap, err := f.r.ImportLegacyToken(context.Background(), "sub-1", tok.Name, tok.SHA512, lastUsed)
	if err != nil {
		t.Fatal(err)
	}
	if want := lastUsed.Add(legacyTTL); !ap.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", ap.ExpiresAt, want)
	}
	if ap.LastUsedAt == nil || !ap.LastUsedAt.Equal(lastUsed) {
		t.Fatalf("LastUsedAt = %v, want %v", ap.LastUsedAt, lastUsed)
	}
}

func TestLegacyTokenGate(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)
	var seen []string
	deny := true
	f.r.legacyGate = func(_ context.Context, username string) error {
		seen = append(seen, username)
		if deny {
			return ErrRateLimited
		}
		return nil
	}

	assertVerifyErr(t, f.r, "alice", f.tokens[0].Token, ErrRateLimited)
	if len(seen) != 1 || seen[0] != "alice" {
		t.Fatalf("gate saw %v", seen)
	}
	if got := readAppFile(t, f.fixture, "sub-1").AppPasswords[0]; got.LegacySHA512 == "" {
		t.Fatal("a refused attempt used the token up")
	}

	deny = false
	mustVerify(t, f.r, "alice", f.tokens[0].Token, "sub-1")

	// An app password, a cached login and an unknown user never reach the gate.
	seen = nil
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	mustVerify(t, f.r, "alice", f.tokens[0].Token, "sub-1")
	assertVerifyErr(t, f.r, "nobody", f.tokens[0].Token, ErrInvalidCredentials)
	if len(seen) != 0 {
		t.Fatalf("gate saw %v, want no calls", seen)
	}
}

func TestLegacyTokenDisabledPrincipal(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)
	if _, err := f.r.SetDisabled(context.Background(), "sub-1", true); err != nil {
		t.Fatal(err)
	}
	assertVerifyErr(t, f.r, "alice", f.tokens[0].Token, ErrDisabled)
	if got := readAppFile(t, f.fixture, "sub-1").AppPasswords[0]; got.LegacySHA512 == "" {
		t.Fatal("a disabled principal's token was swapped")
	}
}

func TestLegacyTokenRevokeForgetsCachedLogin(t *testing.T) {
	f := newLegacyFixture(t)
	ap := f.importToken(t, 0)
	mustVerify(t, f.r, "alice", f.tokens[0].Token, "sub-1")
	if err := f.r.RevokeAppPassword(context.Background(), "sub-1", ap.ID); err != nil {
		t.Fatal(err)
	}
	assertVerifyErr(t, f.r, "alice", f.tokens[0].Token, ErrInvalidCredentials)
}

func TestImportLegacyToken(t *testing.T) {
	f := newLegacyFixture(t)
	ctx := context.Background()
	good := f.tokens[0].SHA512

	for name, digest := range map[string]string{
		"empty":     "",
		"short":     good[:127],
		"not hex":   strings.Repeat("g", 128),
		"too long":  good + "0",
		"bcrypt":    "$2a$10$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234",
		"separator": good[:64] + "-" + good[65:],
	} {
		if _, err := f.r.ImportLegacyToken(ctx, "sub-1", "Phone", digest, time.Time{}); !errors.Is(err, ErrInvalidLegacyToken) {
			t.Errorf("%s: err = %v, want ErrInvalidLegacyToken", name, err)
		}
	}
	if _, err := f.r.ImportLegacyToken(ctx, "sub-1", "", good, time.Time{}); !errors.Is(err, ErrInvalidAppPasswordName) {
		t.Errorf("empty name: err = %v", err)
	}
	if _, err := f.r.ImportLegacyToken(ctx, "nobody", "Phone", good, time.Time{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown owner: err = %v", err)
	}

	// Importing the same digest again changes nothing, and upper case is the same digest.
	first := f.importToken(t, 0)
	again, err := f.r.ImportLegacyToken(ctx, "sub-1", "Phone One", strings.ToUpper(good), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("second import made a new record %q, want %q", again.ID, first.ID)
	}
	if got, _ := f.r.ListAppPasswords("sub-1"); len(got) != 1 {
		t.Fatalf("list = %+v", got)
	}

	// The token counts toward the limit.
	for i := 1; i < MaxAppPasswords; i++ {
		mustIssue(t, f.r, "sub-1", "Device", 0)
	}
	if _, err := f.r.ImportLegacyToken(ctx, "sub-1", "One more", f.tokens[1].SHA512, time.Time{}); !errors.Is(err, ErrTooManyAppPasswords) {
		t.Errorf("past the limit: err = %v", err)
	}
}

func TestLegacyTokenSurvivesRestart(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)
	r2 := f.reopen(t, nil)
	r2.legacySecret, r2.legacyTTL = f.secret, legacyTTL
	mustVerify(t, r2, "alice", f.tokens[0].Token, "sub-1")
}

func TestListShowsLegacyTokens(t *testing.T) {
	f := newLegacyFixture(t)
	f.importToken(t, 0)
	list, err := f.r.ListAppPasswords("sub-1")
	if err != nil || len(list) != 1 || list[0].Name != f.tokens[0].Name {
		t.Fatalf("list = %+v, %v", list, err)
	}
}
