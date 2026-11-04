package principals

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// fakeClock is a clock that tests move by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (f fixture) openWith(t *testing.T, logger *slog.Logger, opts ...Option) *Registry {
	t.Helper()
	if logger == nil {
		logger = discardLogger()
	}
	r, err := NewRegistry(f.dataDir, f.st, testOwnerDir(f.dataDir), logger, opts...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

// appFixture is a registry with a fake clock, the lowest bcrypt cost and a
// provisioned principal sub-1 named alice.
type appFixture struct {
	fixture
	clock *fakeClock
	r     *Registry
}

func newAppFixture(t *testing.T) appFixture {
	t.Helper()
	f := newFixture(t)
	clock := newFakeClock()
	r := f.openWith(t, nil, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost))
	mustProvision(t, r, "sub-1", "alice", "Alice")
	return appFixture{fixture: f, clock: clock, r: r}
}

func (f appFixture) reopen(t *testing.T, logger *slog.Logger) *Registry {
	t.Helper()
	return f.openWith(t, logger, WithClock(f.clock.Now), WithBcryptCost(bcrypt.MinCost))
}

func (f fixture) appFile(sub string) string {
	return filepath.Join(testOwnerDir(f.dataDir)(sub), appPasswordsFileName)
}

func readAppFile(t *testing.T, f fixture, sub string) appPasswordsFile {
	t.Helper()
	data, err := os.ReadFile(f.appFile(sub))
	if err != nil {
		t.Fatal(err)
	}
	var file appPasswordsFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("decode %s: %v", f.appFile(sub), err)
	}
	return file
}

func mustIssue(t *testing.T, r *Registry, sub, name string, ttl time.Duration) (AppPassword, string) {
	t.Helper()
	ap, secret, err := r.IssueAppPassword(context.Background(), sub, name, ttl)
	if err != nil {
		t.Fatalf("IssueAppPassword(%q, %q): %v", sub, name, err)
	}
	return ap, secret
}

func mustVerify(t *testing.T, r *Registry, username, password, wantSub string) {
	t.Helper()
	p, err := r.VerifyBasic(context.Background(), username, password)
	if err != nil {
		t.Fatalf("VerifyBasic(%q): %v", username, err)
	}
	if p.Sub != wantSub || p.Username != username {
		t.Fatalf("VerifyBasic(%q) = %+v, want sub %q", username, p, wantSub)
	}
}

func assertVerifyErr(t *testing.T, r *Registry, username, password string, want error) {
	t.Helper()
	_, err := r.VerifyBasic(context.Background(), username, password)
	if !errors.Is(err, want) {
		t.Fatalf("VerifyBasic(%q) err = %v, want %v", username, err, want)
	}
	if password != "" && strings.Contains(err.Error(), password) {
		t.Fatalf("error %q contains the password", err)
	}
}

// countCompares replaces the bcrypt compare with one that counts its calls.
func countCompares(r *Registry) *atomic.Int64 {
	var n atomic.Int64
	r.compare = func(hash, password []byte) error {
		n.Add(1)
		return bcrypt.CompareHashAndPassword(hash, password)
	}
	return &n
}

// wrongPassword returns a well-formed secret that is not any of r's
// passwords for sub and does not share a lookup hint with them, so a check
// against it needs exactly one bcrypt compare.
func wrongPassword(t *testing.T, r *Registry, sub string) string {
	t.Helper()
	set := r.appSet(sub)
	set.mu.Lock()
	recs := slices.Clone(set.records)
	set.mu.Unlock()
	for {
		pw, err := newSecret()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(recs, func(rec appRecord) bool { return rec.Hint == secretHint(pw) }) {
			return pw
		}
	}
}

var secretPattern = regexp.MustCompile(`^[a-z2-7]{32}$`)

func TestIssueAppPassword(t *testing.T) {
	f := newAppFixture(t)
	ap, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)

	if !secretPattern.MatchString(secret) {
		t.Fatalf("secret %q is not 32 characters of lowercase base32", secret)
	}
	now := f.clock.Now()
	if ap.ID == "" || ap.Name != "Phone" || !ap.CreatedAt.Equal(now) || ap.LastUsedAt != nil {
		t.Fatalf("app password = %+v", ap)
	}
	if want := now.Add(365 * 24 * time.Hour); !ap.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v (default lifetime)", ap.ExpiresAt, want)
	}

	ap2, secret2 := mustIssue(t, f.r, "sub-1", "Laptop", 2*time.Hour)
	if secret2 == secret || ap2.ID == ap.ID {
		t.Fatal("two app passwords share a secret or an id")
	}
	if want := now.Add(2 * time.Hour); !ap2.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", ap2.ExpiresAt, want)
	}

	// The file holds a bcrypt hash at the configured cost, never the secret.
	raw, err := os.ReadFile(f.appFile("sub-1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), secret2) {
		t.Fatal("the app password file contains a secret")
	}
	info, err := os.Stat(f.appFile("sub-1"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}
	file := readAppFile(t, f.fixture, "sub-1")
	if len(file.AppPasswords) != 2 {
		t.Fatalf("file has %d app passwords, want 2", len(file.AppPasswords))
	}
	rec := file.AppPasswords[0]
	if rec.ID != ap.ID || rec.Name != "Phone" || !rec.CreatedAt.Equal(now) || !rec.ExpiresAt.Equal(ap.ExpiresAt) {
		t.Fatalf("record = %+v", rec)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(rec.Hash), []byte(secret)); err != nil {
		t.Fatalf("stored hash does not match the secret: %v", err)
	}
	if cost, err := bcrypt.Cost([]byte(rec.Hash)); err != nil || cost != bcrypt.MinCost {
		t.Fatalf("bcrypt cost = %d, %v, want %d", cost, err, bcrypt.MinCost)
	}
}

func TestIssueAppPasswordDefaultCost(t *testing.T) {
	f := newFixture(t)
	r := f.openWith(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	mustIssue(t, r, "sub-1", "Phone", 0)
	rec := readAppFile(t, f, "sub-1").AppPasswords[0]
	if cost, err := bcrypt.Cost([]byte(rec.Hash)); err != nil || cost != 10 {
		t.Fatalf("bcrypt cost = %d, %v, want 10", cost, err)
	}
}

func TestIssueAppPasswordRejectsInvalidInput(t *testing.T) {
	f := newAppFixture(t)
	tests := []struct {
		name    string
		sub     string
		apName  string
		ttl     time.Duration
		wantErr error
	}{
		{"unknown principal", "sub-9", "Phone", 0, ErrNotFound},
		{"empty name", "sub-1", "", 0, nil},
		{"blank name", "sub-1", " \t ", 0, nil},
		{"name with right-to-left override", "sub-1", "Phone\u202egnp", 0, nil},
		{"name with zero width joiner", "sub-1", "Ph\u200done", 0, nil},
		{"name with BOM", "sub-1", "\ufeffPhone", 0, nil},
		{"name too long", "sub-1", strings.Repeat("n", 101), 0, nil},
		{"name not UTF-8", "sub-1", "Ph\xffone", 0, nil},
		{"name with control character", "sub-1", "Ph\none", 0, nil},
		{"negative lifetime", "sub-1", "Phone", -time.Second, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, secret, err := f.r.IssueAppPassword(context.Background(), tt.sub, tt.apName, tt.ttl)
			if err == nil {
				t.Fatal("IssueAppPassword succeeded, want an error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if secret != "" {
				t.Fatal("a secret was returned with an error")
			}
		})
	}
	if _, err := os.Stat(f.appFile("sub-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("app password file written for rejected input (err %v)", err)
	}
	if _, err := f.r.ListAppPasswords("sub-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListAppPasswords(unknown) err = %v, want ErrNotFound", err)
	}
}

func TestNewRegistryRejectsInvalidBcryptCost(t *testing.T) {
	f := newFixture(t)
	for _, cost := range []int{bcrypt.MinCost - 1, bcrypt.MaxCost + 1} {
		if _, err := NewRegistry(f.dataDir, f.st, testOwnerDir(f.dataDir), nil, WithBcryptCost(cost)); err == nil {
			t.Fatalf("NewRegistry with cost %d succeeded, want an error", cost)
		}
	}
}

func TestVerifyBasic(t *testing.T) {
	f := newAppFixture(t)
	mustProvision(t, f.r, "sub-2", "bob", "Bob")
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	_, bobSecret := mustIssue(t, f.r, "sub-2", "Phone", 0)

	mustVerify(t, f.r, "alice", secret, "sub-1")
	mustVerify(t, f.r, "bob", bobSecret, "sub-2")

	for _, tt := range []struct{ name, username, password string }{
		{"wrong password", "alice", wrongPassword(t, f.r, "sub-1")},
		{"another user's password", "alice", bobSecret},
		{"unknown user", "carol", secret},
		{"empty password", "alice", ""},
		{"empty username", "", secret},
		{"upper case secret", "alice", strings.ToUpper(secret)},
		{"secret with a suffix", "alice", secret + "x"},
		{"very long password", "alice", strings.Repeat("a", 200)},
		{"sub instead of username", "sub-1", secret},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertVerifyErr(t, f.r, tt.username, tt.password, ErrInvalidCredentials)
		})
	}

	// Wrong password and unknown user give the same error.
	_, errWrong := f.r.VerifyBasic(context.Background(), "alice", wrongPassword(t, f.r, "sub-1"))
	_, errUnknown := f.r.VerifyBasic(context.Background(), "nobody", secret)
	if errWrong.Error() != errUnknown.Error() {
		t.Fatalf("errors differ: %q and %q", errWrong, errUnknown)
	}
}

func TestVerifyBasicExpired(t *testing.T) {
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", time.Hour)

	f.clock.Add(time.Hour - time.Second)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	f.clock.Add(time.Second)
	assertVerifyErr(t, f.r, "alice", secret, ErrExpired)

	// A fresh registry without a cache says the same.
	assertVerifyErr(t, f.reopen(t, nil), "alice", secret, ErrExpired)
}

// A credential verified a moment before its password expired is not
// accepted from the cache afterwards.
func TestCachedCredentialExpiresWithThePassword(t *testing.T) {
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 2*time.Minute)
	mustVerify(t, f.r, "alice", secret, "sub-1")

	f.clock.Add(2 * time.Minute)
	assertVerifyErr(t, f.r, "alice", secret, ErrExpired)
}

func TestListAppPasswords(t *testing.T) {
	f := newAppFixture(t)
	if list, err := f.r.ListAppPasswords("sub-1"); err != nil || len(list) != 0 {
		t.Fatalf("ListAppPasswords before any issue = %v, %v, want empty", list, err)
	}
	phone, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	f.clock.Add(time.Minute)
	laptop, _ := mustIssue(t, f.r, "sub-1", "Laptop", 0)

	list, err := f.r.ListAppPasswords("sub-1")
	if err != nil {
		t.Fatalf("ListAppPasswords: %v", err)
	}
	if len(list) != 2 || list[0].ID != phone.ID || list[1].ID != laptop.ID {
		t.Fatalf("list = %+v, want phone then laptop", list)
	}

	// Nothing in the list leads back to the secret.
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	hash := readAppFile(t, f.fixture, "sub-1").AppPasswords[0].Hash
	if strings.Contains(string(data), secret) || strings.Contains(string(data), hash) || strings.Contains(string(data), "$2") {
		t.Fatalf("list exposes the secret or the hash: %s", data)
	}

	// The caller cannot change the registry through the list.
	mustVerify(t, f.r, "alice", secret, "sub-1")
	list, _ = f.r.ListAppPasswords("sub-1")
	*list[0].LastUsedAt = time.Time{}
	list[0].Name = "changed"
	again, _ := f.r.ListAppPasswords("sub-1")
	if again[0].Name != "Phone" || again[0].LastUsedAt.IsZero() {
		t.Fatalf("list changed through a returned value: %+v", again[0])
	}
}

func TestRevokeAppPassword(t *testing.T) {
	f := newAppFixture(t)
	phone, phoneSecret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	_, laptopSecret := mustIssue(t, f.r, "sub-1", "Laptop", 0)
	mustVerify(t, f.r, "alice", phoneSecret, "sub-1")
	mustVerify(t, f.r, "alice", laptopSecret, "sub-1")
	if f.r.cache.len() != 2 {
		t.Fatalf("cache has %d entries, want 2", f.r.cache.len())
	}

	if err := f.r.RevokeAppPassword(context.Background(), "sub-1", phone.ID); err != nil {
		t.Fatalf("RevokeAppPassword: %v", err)
	}
	if f.r.cache.len() != 0 {
		t.Fatalf("cache has %d entries after a revoke, want 0", f.r.cache.len())
	}
	assertVerifyErr(t, f.r, "alice", phoneSecret, ErrInvalidCredentials)
	mustVerify(t, f.r, "alice", laptopSecret, "sub-1")

	list, err := f.r.ListAppPasswords("sub-1")
	if err != nil || len(list) != 1 || list[0].Name != "Laptop" {
		t.Fatalf("list after revoke = %+v, %v", list, err)
	}
	if file := readAppFile(t, f.fixture, "sub-1"); len(file.AppPasswords) != 1 {
		t.Fatalf("file has %d app passwords after revoke, want 1", len(file.AppPasswords))
	}
	assertVerifyErr(t, f.reopen(t, nil), "alice", phoneSecret, ErrInvalidCredentials)

	for _, tt := range []struct{ sub, id string }{
		{"sub-1", phone.ID},
		{"sub-1", "no-such-id"},
		{"sub-9", phone.ID},
	} {
		if err := f.r.RevokeAppPassword(context.Background(), tt.sub, tt.id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("RevokeAppPassword(%q, %q) err = %v, want ErrNotFound", tt.sub, tt.id, err)
		}
	}
}

// One principal cannot revoke another one's app password.
func TestRevokeIsPerPrincipal(t *testing.T) {
	f := newAppFixture(t)
	mustProvision(t, f.r, "sub-2", "bob", "Bob")
	bobs, bobSecret := mustIssue(t, f.r, "sub-2", "Phone", 0)
	if err := f.r.RevokeAppPassword(context.Background(), "sub-1", bobs.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	mustVerify(t, f.r, "bob", bobSecret, "sub-2")
}

func TestVerifyBasicUsesTheCache(t *testing.T) {
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	compares := countCompares(f.r)

	mustVerify(t, f.r, "alice", secret, "sub-1")
	if n := compares.Load(); n != 1 {
		t.Fatalf("%d compares on the first verify, want 1", n)
	}
	f.clock.Add(5*time.Minute - time.Second)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	if n := compares.Load(); n != 1 {
		t.Fatalf("%d compares within the cache TTL, want 1", n)
	}
	f.clock.Add(time.Second)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	if n := compares.Load(); n != 2 {
		t.Fatalf("%d compares after the cache TTL, want 2", n)
	}

	// The cache key is the HMAC of the Authorization header; neither the
	// header nor the password is kept.
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+secret))
	if _, ok := f.r.cache.get(f.r.cache.key(header), f.clock.Now()); !ok {
		t.Fatal("no cache entry under the HMAC of the Authorization header")
	}
	f.r.cache.mu.Lock()
	dump := fmt.Sprintf("%#v", f.r.cache.entries)
	f.r.cache.mu.Unlock()
	if strings.Contains(dump, secret) || strings.Contains(dump, header[len("Basic "):]) {
		t.Fatalf("cache holds the credential: %s", dump)
	}
}

// A wrong password is never cached, so it costs a compare every time.
func TestFailedVerifyIsNotCached(t *testing.T) {
	f := newAppFixture(t)
	mustIssue(t, f.r, "sub-1", "Phone", 0)
	wrong := wrongPassword(t, f.r, "sub-1")
	compares := countCompares(f.r)
	for range 3 {
		assertVerifyErr(t, f.r, "alice", wrong, ErrInvalidCredentials)
	}
	if n := compares.Load(); n != 3 {
		t.Fatalf("%d compares for 3 failed checks, want 3", n)
	}
	if f.r.cache.len() != 0 {
		t.Fatalf("cache has %d entries, want 0", f.r.cache.len())
	}
}

// When a username moves to another principal, a cached credential for the
// old holder no longer authenticates under that name.
func TestCacheIsNotUsedAfterRename(t *testing.T) {
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	mustVerify(t, f.r, "alice", secret, "sub-1")

	mustProvision(t, f.r, "sub-2", "alice", "Alice B")
	assertVerifyErr(t, f.r, "alice", secret, ErrInvalidCredentials)

	mustProvision(t, f.r, "sub-1", "alice-a", "Alice A")
	mustVerify(t, f.r, "alice-a", secret, "sub-1")
}

func TestRevokedPasswordIsRejectedEvenWhenCached(t *testing.T) {
	f := newAppFixture(t)
	ap, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	if err := f.r.RevokeAppPassword(context.Background(), "sub-1", ap.ID); err != nil {
		t.Fatal(err)
	}
	assertVerifyErr(t, f.r, "alice", secret, ErrInvalidCredentials)
}

func TestLastUsedAtIsThrottled(t *testing.T) {
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	lastUsed := func() *time.Time {
		t.Helper()
		return readAppFile(t, f.fixture, "sub-1").AppPasswords[0].LastUsedAt
	}
	if lastUsed() != nil {
		t.Fatal("last_used_at set before any use")
	}

	first := f.clock.Now()
	mustVerify(t, f.r, "alice", secret, "sub-1")
	if got := lastUsed(); got == nil || !got.Equal(first) {
		t.Fatalf("last_used_at = %v, want %v", got, first)
	}

	// Uses within 10 minutes, from the cache or not, do not write it again.
	for _, step := range []time.Duration{time.Minute, 4 * time.Minute, 4*time.Minute + 59*time.Second} {
		f.clock.Add(step)
		mustVerify(t, f.r, "alice", secret, "sub-1")
		if got := lastUsed(); !got.Equal(first) {
			t.Fatalf("last_used_at = %v after %v, want %v", got, f.clock.Now().Sub(first), first)
		}
	}
	f.clock.Add(time.Second)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	if got := lastUsed(); !got.Equal(f.clock.Now()) {
		t.Fatalf("last_used_at = %v after 10 minutes, want %v", got, f.clock.Now())
	}
	list, _ := f.r.ListAppPasswords("sub-1")
	if !list[0].LastUsedAt.Equal(f.clock.Now()) {
		t.Fatalf("listed LastUsedAt = %v, want %v", list[0].LastUsedAt, f.clock.Now())
	}
}

// A failed write of last_used_at is logged and does not fail the
// authentication.
func TestLastUsedAtWriteFailureDoesNotFailVerify(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	logger, logs := bufferLogger()
	f.r.logger = logger

	dir := testOwnerDir(f.dataDir)("sub-1")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	mustVerify(t, f.r, "alice", secret, "sub-1")
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("no warning logged for the failed write:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatal("the log contains the secret")
	}
}

// Unknown users cost a bcrypt compare against a fixed hash at the configured
// cost, so they cannot be told apart from a wrong password by timing.
func TestEveryFailedVerifyCostsOneCompare(t *testing.T) {
	f := newAppFixture(t)
	mustProvision(t, f.r, "sub-2", "bob", "Bob")
	for i := range 5 {
		mustIssue(t, f.r, "sub-1", fmt.Sprintf("Device %d", i), 0)
	}
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	compares := countCompares(f.r)

	for _, tt := range []struct{ name, username, password string }{
		{"unknown user", "nobody", secret},
		{"wrong password", "alice", wrongPassword(t, f.r, "sub-1")},
		{"user without app passwords", "bob", secret},
		{"malformed password", "alice", "short"},
		{"empty password", "nobody", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := compares.Load()
			assertVerifyErr(t, f.r, tt.username, tt.password, ErrInvalidCredentials)
			if n := compares.Load() - before; n != 1 {
				t.Fatalf("%d compares, want 1", n)
			}
		})
	}

	if cost, err := bcrypt.Cost(f.r.dummyHash); err != nil || cost != bcrypt.MinCost {
		t.Fatalf("dummy hash cost = %d, %v, want %d", cost, err, bcrypt.MinCost)
	}
}

// The hash for unknown users exists once NewRegistry returns, so the first
// check after a start costs no more than any other.
func TestDummyHashIsMadeByNewRegistry(t *testing.T) {
	f := newFixture(t)
	r := f.openWith(t, nil, WithBcryptCost(bcrypt.MinCost+1))
	if cost, err := bcrypt.Cost(r.dummyHash); err != nil || cost != bcrypt.MinCost+1 {
		t.Fatalf("dummy hash cost = %d, %v, want %d", cost, err, bcrypt.MinCost+1)
	}
}

// No operation writes the secret to a log, an error or a file.
func TestSecretNeverLeaks(t *testing.T) {
	f := newFixture(t)
	clock := newFakeClock()
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := f.openWith(t, logger, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost))
	mustProvision(t, r, "sub-1", "alice", "Alice")

	ap, secret := mustIssue(t, r, "sub-1", "Phone", time.Hour)
	var errs []error
	collect := func(_ Principal, err error) { errs = append(errs, err) }
	collect(r.VerifyBasic(context.Background(), "alice", secret))
	collect(r.VerifyBasic(context.Background(), "nobody", secret))
	collect(r.VerifyBasic(context.Background(), "alice", secret[:31]+"a"))
	clock.Add(11 * time.Minute)
	collect(r.VerifyBasic(context.Background(), "alice", secret))
	clock.Add(time.Hour)
	collect(r.VerifyBasic(context.Background(), "alice", secret))
	errs = append(errs, r.RevokeAppPassword(context.Background(), "sub-1", ap.ID))
	collect(r.VerifyBasic(context.Background(), "alice", secret))

	for _, err := range errs {
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q contains the secret", err)
		}
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("the log contains the secret:\n%s", logs.String())
	}
	err := filepath.WalkDir(f.dataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("%s contains the secret", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan data directory: %v", err)
	}
}

func TestAppPasswordsSurviveRestart(t *testing.T) {
	f := newAppFixture(t)
	ap, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	mustVerify(t, f.r, "alice", secret, "sub-1")

	r := f.reopen(t, nil)
	mustVerify(t, r, "alice", secret, "sub-1")
	list, err := r.ListAppPasswords("sub-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list after restart = %+v, %v", list, err)
	}
	got := list[0]
	if got.ID != ap.ID || got.Name != ap.Name || !got.CreatedAt.Equal(ap.CreatedAt) ||
		!got.ExpiresAt.Equal(ap.ExpiresAt) || got.LastUsedAt == nil {
		t.Fatalf("after restart = %+v, issued %+v", got, ap)
	}
}

// A file the registry cannot read is never overwritten, and nobody is let in
// through it.
func TestUnreadableAppPasswordFile(t *testing.T) {
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	if err := os.WriteFile(f.appFile("sub-1"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := f.reopen(t, nil)

	if _, err := r.VerifyBasic(context.Background(), "alice", secret); err == nil {
		t.Fatal("VerifyBasic succeeded with a broken file")
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("error %q contains the secret", err)
	}
	if _, _, err := r.IssueAppPassword(context.Background(), "sub-1", "Laptop", 0); err == nil {
		t.Fatal("IssueAppPassword succeeded with a broken file")
	}
	if _, err := r.ListAppPasswords("sub-1"); err == nil {
		t.Fatal("ListAppPasswords succeeded with a broken file")
	}
	data, err := os.ReadFile(f.appFile("sub-1"))
	if err != nil || string(data) != "{broken" {
		t.Fatalf("broken file was changed: %q, %v", data, err)
	}
}

func TestAppPasswordFileIsNeverPartial(t *testing.T) {
	f := newAppFixture(t)
	mustIssue(t, f.r, "sub-1", "first", 0)
	path := f.appFile("sub-1")

	stop := make(chan struct{})
	readerDone := make(chan error, 1)
	go func() {
		reads := 0
		for {
			select {
			case <-stop:
				if reads == 0 {
					readerDone <- errors.New("reader never ran")
					return
				}
				readerDone <- nil
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				readerDone <- err
				return
			}
			var file appPasswordsFile
			if err := json.Unmarshal(data, &file); err != nil {
				readerDone <- err
				return
			}
			reads++
		}
	}()

	long := strings.Repeat("x", 100)
	// Issue and revoke in turn, so the file grows and shrinks within the
	// limit on app passwords.
	var ids []string
	for i := range 100 {
		ap, _, err := f.r.IssueAppPassword(context.Background(), "sub-1", long[:1+i%100], 0)
		if err != nil {
			close(stop)
			t.Fatalf("IssueAppPassword: %v", err)
		}
		ids = append(ids, ap.ID)
		if len(ids) > MaxAppPasswords/2 {
			if err := f.r.RevokeAppPassword(context.Background(), "sub-1", ids[0]); err != nil {
				close(stop)
				t.Fatalf("RevokeAppPassword: %v", err)
			}
			ids = ids[1:]
		}
	}
	close(stop)
	if err := <-readerDone; err != nil {
		t.Fatalf("reader saw a partial file: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != principalFileName && e.Name() != appPasswordsFileName {
			t.Fatalf("leftover file %q next to the app password file", e.Name())
		}
	}
}

func TestRestartRemovesLeftoverAppPasswordTempFiles(t *testing.T) {
	f := newAppFixture(t)
	mustIssue(t, f.r, "sub-1", "Phone", 0)
	leftover := filepath.Join(testOwnerDir(f.dataDir)("sub-1"), ".app_passwords.json.tmp-123")
	if err := os.WriteFile(leftover, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.reopen(t, nil)
	if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s still exists (err %v)", leftover, err)
	}
}

func TestConcurrentAppPasswordOperations(t *testing.T) {
	f := newAppFixture(t)
	mustProvision(t, f.r, "sub-2", "bob", "Bob")
	users := map[string]string{"sub-1": "alice", "sub-2": "bob"}

	var wg sync.WaitGroup
	for sub, username := range users {
		for w := range 4 {
			wg.Go(func() {
				for i := range 5 {
					ap, secret, err := f.r.IssueAppPassword(context.Background(), sub, fmt.Sprintf("d%d-%d", w, i), 0)
					if err != nil {
						t.Errorf("IssueAppPassword: %v", err)
						return
					}
					for range 3 {
						if _, err := f.r.VerifyBasic(context.Background(), username, secret); err != nil {
							t.Errorf("VerifyBasic: %v", err)
							return
						}
					}
					f.clock.Add(time.Minute)
					if _, err := f.r.ListAppPasswords(sub); err != nil {
						t.Errorf("ListAppPasswords: %v", err)
					}
					if i%2 == 0 {
						if err := f.r.RevokeAppPassword(context.Background(), sub, ap.ID); err != nil {
							t.Errorf("RevokeAppPassword: %v", err)
							return
						}
						if _, err := f.r.VerifyBasic(context.Background(), username, secret); !errors.Is(err, ErrInvalidCredentials) {
							t.Errorf("VerifyBasic after revoke err = %v, want ErrInvalidCredentials", err)
						}
					}
				}
			})
		}
	}
	wg.Wait()

	for sub := range users {
		list, err := f.r.ListAppPasswords(sub)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 8 {
			t.Fatalf("%s has %d app passwords, want 8", sub, len(list))
		}
		if file := readAppFile(t, f.fixture, sub); len(file.AppPasswords) != 8 {
			t.Fatalf("%s file has %d app passwords, want 8", sub, len(file.AppPasswords))
		}
	}
}

// A revoke that lands between the last check of a verification and its
// cache write must not leave a cache entry behind.
func TestRevokeBeforeCachePutLeavesNoCacheEntry(t *testing.T) {
	f := newAppFixture(t)
	ap, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	var once sync.Once
	f.r.beforeCachePut = func() {
		once.Do(func() {
			if err := f.r.RevokeAppPassword(context.Background(), "sub-1", ap.ID); err != nil {
				t.Errorf("RevokeAppPassword: %v", err)
			}
		})
	}
	mustVerify(t, f.r, "alice", secret, "sub-1")
	if n := f.r.cache.len(); n != 0 {
		t.Fatalf("cache has %d entries after the revoke, want 0", n)
	}
	assertVerifyErr(t, f.r, "alice", secret, ErrInvalidCredentials)
}

// Verifications racing a revoke of the same password leave no cache entry,
// and the password is rejected afterwards.
func TestVerifyRacingRevokeLeavesNoCacheEntry(t *testing.T) {
	for range 20 {
		f := newAppFixture(t)
		ap, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range 4 {
			wg.Go(func() {
				<-start
				_, _ = f.r.VerifyBasic(context.Background(), "alice", secret)
			})
		}
		wg.Go(func() {
			<-start
			if err := f.r.RevokeAppPassword(context.Background(), "sub-1", ap.ID); err != nil {
				t.Errorf("RevokeAppPassword: %v", err)
			}
		})
		close(start)
		wg.Wait()
		if n := f.r.cache.len(); n != 0 {
			t.Fatalf("cache has %d entries after the revoke, want 0", n)
		}
		assertVerifyErr(t, f.r, "alice", secret, ErrInvalidCredentials)
	}
}

func TestIssueAppPasswordLimit(t *testing.T) {
	f := newAppFixture(t)
	var last AppPassword
	for i := range MaxAppPasswords {
		last, _ = mustIssue(t, f.r, "sub-1", fmt.Sprintf("Device %d", i), time.Minute)
	}
	// Expired app passwords count until they are revoked.
	f.clock.Add(time.Hour)
	_, secret, err := f.r.IssueAppPassword(context.Background(), "sub-1", "One more", 0)
	if !errors.Is(err, ErrTooManyAppPasswords) || secret != "" {
		t.Fatalf("IssueAppPassword over the limit = %q, %v, want ErrTooManyAppPasswords", secret, err)
	}
	if n := len(readAppFile(t, f.fixture, "sub-1").AppPasswords); n != MaxAppPasswords {
		t.Fatalf("file has %d app passwords, want %d", n, MaxAppPasswords)
	}
	if err := f.r.RevokeAppPassword(context.Background(), "sub-1", last.ID); err != nil {
		t.Fatal(err)
	}
	mustIssue(t, f.r, "sub-1", "One more", 0)
}
