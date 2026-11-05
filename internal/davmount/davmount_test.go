package davmount

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

const basicChallenge = `Basic realm="ocis-groupware"`

// fakeVerifier accepts the tokens in its map and returns their claims.
type fakeVerifier struct {
	mu     sync.Mutex
	tokens map[string]auth.Claims
	err    error
	calls  int
}

func (f *fakeVerifier) Verify(_ context.Context, raw string) (auth.Claims, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return auth.Claims{}, f.err
	}
	c, ok := f.tokens[raw]
	if !ok {
		return auth.Claims{}, fmt.Errorf("%w: unknown token", auth.ErrInvalidToken)
	}
	return c, nil
}

func (f *fakeVerifier) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// syncBuffer is a bytes.Buffer safe for use by a logger and a test at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fixture struct {
	dir      string
	st       *filestore.Store
	reg      *principals.Registry
	verifier *fakeVerifier
	clock    *clock
	logs     *syncBuffer
	logger   *slog.Logger
	dav      http.Handler
	wk       http.Handler
	// lease is the identity lease of the registry, 0 for none.
	lease time.Duration
}

func newFixture(t *testing.T) *fixture { return newFixtureWithLease(t, 0) }

// newFixtureWithLease is newFixture with a registry that enforces an identity
// lease of that length.
func newFixtureWithLease(t *testing.T, lease time.Duration) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatalf("filestore.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	f := &fixture{
		dir:      dir,
		st:       st,
		verifier: &fakeVerifier{tokens: map[string]auth.Claims{}},
		clock:    &clock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)},
		logs:     &syncBuffer{},
		lease:    lease,
	}
	f.logger = slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f.reg = f.newRegistry(t)
	f.mount(t)
	return f
}

func (f *fixture) newRegistry(t *testing.T) *principals.Registry {
	t.Helper()
	reg, err := principals.NewRegistry(f.dir, f.st, f.st.OwnerDir, f.logger,
		principals.WithBcryptCost(bcrypt.MinCost), principals.WithClock(f.clock.Now),
		principals.WithIdentityMaxAge(f.lease))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return reg
}

func (f *fixture) mount(t *testing.T) {
	t.Helper()
	dav, wk, err := New(Config{Store: f.st, Registry: f.reg, Verifier: f.verifier, Logger: f.logger})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.dav, f.wk = dav, wk
}

// passwordTTL is the lifetime of the app passwords the fixture issues: an
// hour, or longer than the identity lease if there is one.
func (f *fixture) passwordTTL() time.Duration {
	if f.lease > 0 {
		return 10 * f.lease
	}
	return time.Hour
}

// confirm records that oCIS confirmed the principal now, as a bearer sign-in
// does.
func (f *fixture) confirm(t *testing.T, sub string) {
	t.Helper()
	if err := f.reg.SetIdentity(context.Background(), sub, principals.Identity{OCISID: "id-" + sub, At: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}
}

// user provisions a principal and returns an app password for it.
func (f *fixture) user(t *testing.T, sub, username string) string {
	t.Helper()
	ctx := context.Background()
	if _, _, err := f.reg.Provision(ctx, sub, username, username); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	_, secret, err := f.reg.IssueAppPassword(ctx, sub, "Phone", f.passwordTTL())
	if err != nil {
		t.Fatalf("IssueAppPassword: %v", err)
	}
	return secret
}

// do sends a request to the DAV handler.
func (f *fixture) do(t *testing.T, method, target string, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	f.dav.ServeHTTP(rec, req)
	return rec
}

func basic(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

const propfindPrincipal = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>`

const propfindName = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:displayname/></d:prop></d:propfind>`

const vcard = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:card-1\r\nFN:Ada Lovelace\r\nN:Lovelace;Ada;;;\r\n" +
	"EMAIL:ada@example.org\r\nEND:VCARD\r\n"

const event = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//test//EN\r\nBEGIN:VEVENT\r\n" +
	"UID:event-1\r\nDTSTAMP:20261104T120000Z\r\nDTSTART:20261105T090000Z\r\n" +
	"DTEND:20261105T100000Z\r\nSUMMARY:Standup\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

const mkcalendar = `<?xml version="1.0" encoding="utf-8"?>
<c:mkcalendar xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
<d:set><d:prop><d:displayname>Work</d:displayname></d:prop></d:set></c:mkcalendar>`

// assertUnauthorized checks a 401 with the Basic challenge and the body every
// refused credential gets.
func assertUnauthorized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != basicChallenge {
		t.Errorf("WWW-Authenticate = %q, want %q", got, basicChallenge)
	}
	if got := rec.Body.String(); got != "unauthorized\n" {
		t.Errorf("body = %q, want %q", got, "unauthorized\n")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	f := newFixture(t)
	for name, cfg := range map[string]Config{
		"store":    {Registry: f.reg, Verifier: f.verifier},
		"registry": {Store: f.st, Verifier: f.verifier},
		"verifier": {Store: f.st, Registry: f.reg},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := New(cfg); err == nil {
				t.Fatal("New succeeded")
			}
		})
	}
	if _, _, err := New(Config{Store: f.st, Registry: f.reg, Verifier: f.verifier}); err != nil {
		t.Fatalf("New without a logger: %v", err)
	}
}

func TestNoCredentialsIsUnauthorized(t *testing.T) {
	f := newFixture(t)
	f.user(t, "sub-alice", "alice")
	for _, tc := range []struct{ method, path string }{
		{"PROPFIND", "/dav"},
		{"PROPFIND", "/dav/"},
		{"PROPFIND", "/dav/principals/alice/"},
		{"PROPFIND", "/dav/addressbooks/alice/contacts/"},
		{"GET", "/dav/calendars/alice/personal/x.ics"},
		{"OPTIONS", "/dav/"},
		// Unknown paths fail closed instead of answering 404.
		{"GET", "/dav/nope"},
		{"PROPFIND", "/dav/addressbooks/nobody/"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			assertUnauthorized(t, f.do(t, tc.method, tc.path, ""))
		})
	}
	if n := f.verifier.callCount(); n != 0 {
		t.Errorf("verifier called %d times without a token", n)
	}
}

func TestBadCredentialsAreUnauthorized(t *testing.T) {
	f := newFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	for name, header := range map[string]string{
		"wrong password":       basic("alice", strings.Repeat("a", 32)),
		"unknown user":         basic("mallory", secret),
		"empty password":       basic("alice", ""),
		"not base64":           "Basic !!!",
		"no colon":             "Basic " + base64.StdEncoding.EncodeToString([]byte("alice")),
		"empty basic":          "Basic",
		"other scheme":         "Digest username=alice",
		"bare token":           secret,
		"invalid bearer token": "Bearer nope",
		"empty bearer":         "Bearer ",
	} {
		t.Run(name, func(t *testing.T) {
			assertUnauthorized(t, f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", header, "Depth", "0"))
		})
	}
}

// TestRefusalsLookTheSame compares the answers to an unknown user, a wrong
// password, an expired password, a disabled principal's password and a
// password whose state cannot be read: all of them must be identical, so a
// client cannot tell which users exist or what state they are in.
func TestRefusalsLookTheSame(t *testing.T) {
	f := newFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	brokenSecret := f.user(t, "sub-bob", "bob")
	expiredSecret := f.user(t, "sub-carol", "carol")
	// The passwords were issued for an hour.
	f.clock.advance(61 * time.Minute)
	disabledSecret := f.user(t, "sub-dave", "dave")
	if _, err := f.reg.SetDisabled(context.Background(), "sub-dave", true); err != nil {
		t.Fatal(err)
	}

	// bob's app password file breaks; a fresh registry has to read it.
	if err := os.WriteFile(filepath.Join(f.st.OwnerDir("sub-bob"), "app_passwords.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.reg = f.newRegistry(t)
	f.mount(t)

	cases := map[string]string{
		"unknown user":   basic("mallory", secret),
		"wrong password": basic("alice", strings.Repeat("b", 32)),
		"expired":        basic("carol", expiredSecret),
		"disabled":       basic("dave", disabledSecret),
		"state error":    basic("bob", brokenSecret),
	}
	var first *httptest.ResponseRecorder
	for _, name := range []string{"unknown user", "wrong password", "expired", "disabled", "state error"} {
		rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", cases[name], "Depth", "0")
		assertUnauthorized(t, rec)
		if first == nil {
			first = rec
			continue
		}
		if rec.Body.String() != first.Body.String() {
			t.Errorf("%s: body %q differs from %q", name, rec.Body.String(), first.Body.String())
		}
		for _, h := range []string{"WWW-Authenticate", "Content-Type"} {
			if rec.Header().Get(h) != first.Header().Get(h) {
				t.Errorf("%s: %s %q differs from %q", name, h, rec.Header().Get(h), first.Header().Get(h))
			}
		}
		for _, word := range []string{"expire", "disabled"} {
			if strings.Contains(strings.ToLower(rec.Body.String()), word) {
				t.Errorf("%s: body %q tells about the state", name, rec.Body.String())
			}
		}
	}

	logs := f.logs.String()
	if !strings.Contains(logs, "expired") {
		t.Errorf("expired app password not logged: %s", logs)
	}
	// The disabled principal's sign-in is an expected refusal: logged, with
	// the username, but not as a fault.
	i := slices.IndexFunc(strings.Split(logs, "\n"), func(line string) bool { return strings.Contains(line, "username=dave") })
	if i < 0 {
		t.Errorf("disabled principal's sign-in not logged: %s", logs)
	} else if line := strings.Split(logs, "\n")[i]; !strings.Contains(line, "level=INFO") || !strings.Contains(line, "disabled") {
		t.Errorf("disabled principal's sign-in logged as %q, want an INFO line that says disabled", line)
	}
	for _, s := range []string{secret, brokenSecret, expiredSecret, disabledSecret, "Basic "} {
		if strings.Contains(logs, s) {
			t.Errorf("logs contain a credential %q: %s", s, logs)
		}
	}
}

// A bearer token of a disabled principal is refused with 403. The caller
// holds a valid token for that very user, so the answer can say so by its
// status, and nothing is provisioned for the request.
func TestDisabledBearerIsForbidden(t *testing.T) {
	f := newFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	if _, err := f.reg.SetDisabled(context.Background(), "sub-alice", true); err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens["tok"] = auth.Claims{Subject: "sub-alice", PreferredUsername: "alice-renamed", DisplayName: "Alice"}

	for _, header := range []string{"Bearer tok", basic("alice", secret)} {
		rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", header, "Depth", "0")
		if strings.HasPrefix(header, "Bearer") {
			if rec.Code != http.StatusForbidden {
				t.Fatalf("bearer: status = %d, want 403; body %q", rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); got != "forbidden\n" {
				t.Errorf("bearer: body = %q, want %q", got, "forbidden\n")
			}
		} else {
			assertUnauthorized(t, rec)
		}
	}
	p, err := f.reg.Get("sub-alice")
	if err != nil || p.Username != "alice" || !p.Disabled {
		t.Errorf("principal changed by the refused request: %+v, %v", p, err)
	}

	if _, err := f.reg.SetDisabled(context.Background(), "sub-alice", false); err != nil {
		t.Fatal(err)
	}
	rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", "Bearer tok", "Depth", "0")
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("after enable: status = %d, want 207; body %q", rec.Code, rec.Body.String())
	}
	rec = f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", basic("alice-renamed", secret), "Depth", "0")
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("after enable, app password: status = %d, want 207; body %q", rec.Code, rec.Body.String())
	}
}

func TestAppPasswordPropfindReturnsCurrentUserPrincipal(t *testing.T) {
	f := newFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	for _, p := range []string{"/dav/", "/dav"} {
		rec := f.do(t, "PROPFIND", p, propfindPrincipal, "Authorization", basic("alice", secret), "Depth", "0")
		if rec.Code != http.StatusMultiStatus {
			t.Fatalf("%s: status = %d, want 207; body %q", p, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "/dav/principals/alice/") {
			t.Errorf("%s: body has no current-user-principal for alice: %s", p, rec.Body.String())
		}
	}
	if n := f.verifier.callCount(); n != 0 {
		t.Errorf("verifier called %d times for Basic", n)
	}
}

func TestBearerProvisionsOnFirstUse(t *testing.T) {
	f := newFixture(t)
	f.verifier.tokens["tok-alice"] = auth.Claims{Subject: "sub-alice", PreferredUsername: "alice"}
	if _, err := f.reg.Get("sub-alice"); !errors.Is(err, principals.ErrNotFound) {
		t.Fatalf("Get before the first request: %v, want ErrNotFound", err)
	}

	rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", "Bearer tok-alice", "Depth", "0")
	if rec.Code != http.StatusMultiStatus || !strings.Contains(rec.Body.String(), "/dav/principals/alice/") {
		t.Fatalf("status = %d, body %q; want 207 with alice's principal", rec.Code, rec.Body.String())
	}
	p, err := f.reg.Get("sub-alice")
	if err != nil || p.Username != "alice" {
		t.Fatalf("Get = %+v, %v; want alice provisioned", p, err)
	}
	if sub, ok := f.reg.LookupUsername("alice"); !ok || sub != "sub-alice" {
		t.Fatalf("LookupUsername = %q, %v", sub, ok)
	}
	for _, kind := range []store.Kind{store.KindAddressBook, store.KindCalendar} {
		cols, err := f.st.ListCollections(context.Background(), "sub-alice", kind)
		if err != nil || len(cols) != 1 {
			t.Errorf("%s collections = %v, %v; want the default one", kind, cols, err)
		}
	}
	// The default address book is reachable over DAV.
	rec = f.do(t, "PROPFIND", "/dav/addressbooks/alice/contacts/", propfindName,
		"Authorization", "Bearer tok-alice", "Depth", "0")
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("default address book: status = %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestConcurrentFirstBearerRequests(t *testing.T) {
	f := newFixture(t)
	f.verifier.tokens["tok-alice"] = auth.Claims{Subject: "sub-alice", PreferredUsername: "alice"}
	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("PROPFIND", "/dav/addressbooks/alice/", strings.NewReader(propfindName))
			req.Header.Set("Authorization", "Bearer tok-alice")
			req.Header.Set("Depth", "1")
			rec := httptest.NewRecorder()
			f.dav.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}()
	}
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusMultiStatus {
			t.Errorf("request %d: status %d, want 207", i, c)
		}
	}
	cols, err := f.st.ListCollections(context.Background(), "sub-alice", store.KindAddressBook)
	if err != nil || len(cols) != 1 {
		t.Fatalf("address books = %v, %v; want exactly one", cols, err)
	}
}

func TestBearerErrors(t *testing.T) {
	t.Run("identity provider unavailable", func(t *testing.T) {
		f := newFixture(t)
		f.verifier.err = fmt.Errorf("%w: connection refused", auth.ErrUnavailable)
		rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", "Bearer tok")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "refused") {
			t.Errorf("body %q leaks the error", rec.Body.String())
		}
	})
	t.Run("unexpected verifier error", func(t *testing.T) {
		f := newFixture(t)
		f.verifier.err = errors.New("something else")
		assertUnauthorized(t, f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", "Bearer tok"))
	})
}

func TestBearerWithUnusableUsernameIsForbidden(t *testing.T) {
	for name, username := range map[string]string{
		"slash": "a/b",
		"dots":  "..",
		"empty": "",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.verifier.tokens["tok"] = auth.Claims{Subject: "sub-x", PreferredUsername: username}
			rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", "Bearer tok", "Depth", "0")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body %q", rec.Code, rec.Body.String())
			}
			body := strings.ToLower(rec.Body.String())
			if strings.Contains(body, "username") || strings.Contains(body, "invalid") ||
				(username != "" && strings.Contains(body, username)) {
				t.Errorf("body %q is not generic", rec.Body.String())
			}
			if _, err := f.reg.Get("sub-x"); !errors.Is(err, principals.ErrNotFound) {
				t.Errorf("principal created for an unusable username: %v", err)
			}
			if !strings.Contains(f.logs.String(), "sub-x") {
				t.Errorf("refusal not logged: %s", f.logs.String())
			}
		})
	}
}

func TestBearerProvisionFailureIsServerError(t *testing.T) {
	f := newFixture(t)
	f.verifier.tokens["tok"] = auth.Claims{Subject: "sub-alice", PreferredUsername: "alice"}
	// A file where the owner directory goes makes writing principal.json fail.
	ownerDir := f.st.OwnerDir("sub-alice")
	if err := os.MkdirAll(filepath.Dir(ownerDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", "Bearer tok", "Depth", "0")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), f.dir) {
		t.Errorf("body %q leaks a path", rec.Body.String())
	}
}

func TestCardDAVRoundTrip(t *testing.T) {
	f := newFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	authz := basic("alice", secret)
	const href = "/dav/addressbooks/alice/contacts/ada.vcf"

	rec := f.do(t, http.MethodPut, href, vcard, "Authorization", authz, "Content-Type", "text/vcard; charset=utf-8")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT: status = %d, body %q", rec.Code, rec.Body.String())
	}
	rec = f.do(t, http.MethodGet, href, "", "Authorization", authz)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != vcard {
		t.Fatalf("GET body = %q, want %q", got, vcard)
	}
}

func TestCalDAVRoundTrip(t *testing.T) {
	f := newFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	authz := basic("alice", secret)

	rec := f.do(t, "MKCALENDAR", "/dav/calendars/alice/work/", mkcalendar, "Authorization", authz,
		"Content-Type", "application/xml")
	if rec.Code != http.StatusCreated {
		t.Fatalf("MKCALENDAR: status = %d, body %q", rec.Code, rec.Body.String())
	}
	const href = "/dav/calendars/alice/work/standup.ics"
	rec = f.do(t, http.MethodPut, href, event, "Authorization", authz, "Content-Type", "text/calendar; charset=utf-8")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT: status = %d, body %q", rec.Code, rec.Body.String())
	}
	rec = f.do(t, http.MethodGet, href, "", "Authorization", authz)
	if rec.Code != http.StatusOK || rec.Body.String() != event {
		t.Fatalf("GET: status = %d, body %q", rec.Code, rec.Body.String())
	}
	cols, err := f.st.ListCollections(context.Background(), "sub-alice", store.KindCalendar)
	if err != nil || len(cols) != 2 {
		t.Fatalf("calendars of sub-alice = %v, %v; want personal and work", cols, err)
	}
}

func TestOtherUsersDataIsForbidden(t *testing.T) {
	f := newFixture(t)
	alice := f.user(t, "sub-alice", "alice")
	f.user(t, "sub-bob", "bob")
	authz := basic("alice", alice)

	for _, tc := range []struct{ method, path string }{
		{"PROPFIND", "/dav/addressbooks/bob/contacts/"},
		{"PROPFIND", "/dav/addressbooks/bob/"},
		{"PROPFIND", "/dav/calendars/bob/personal/"},
		{"PROPFIND", "/dav/principals/bob/"},
		{"PUT", "/dav/addressbooks/bob/contacts/x.vcf"},
		// Names are compared exactly, so a different case is someone else.
		{"PROPFIND", "/dav/addressbooks/Alice/contacts/"},
		// A user that does not exist looks the same as one that does.
		{"PROPFIND", "/dav/addressbooks/nobody/contacts/"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			body := propfindName
			if tc.method == "PUT" {
				body = vcard
			}
			rec := f.do(t, tc.method, tc.path, body, "Authorization", authz, "Depth", "0",
				"Content-Type", "text/vcard")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body %q", rec.Code, rec.Body.String())
			}
		})
	}
	if objs, err := f.st.ListObjects(context.Background(),
		store.CollectionRef{Owner: "sub-bob", Kind: store.KindAddressBook, Name: "contacts"}, store.ListOptions{}); err != nil || len(objs) != 0 {
		t.Fatalf("bob's contacts = %v, %v; want none", objs, err)
	}
}

// TestPathTricks sends paths that try to leave the user's own data through
// encoded slashes or dot segments. None of them may reach bob's data.
func TestPathTricks(t *testing.T) {
	f := newFixture(t)
	alice := f.user(t, "sub-alice", "alice")
	bob := f.user(t, "sub-bob", "bob")
	rec := f.do(t, http.MethodPut, "/dav/addressbooks/bob/contacts/secret.vcf", vcard,
		"Authorization", basic("bob", bob), "Content-Type", "text/vcard")
	if rec.Code != http.StatusCreated {
		t.Fatalf("bob's PUT: %d %q", rec.Code, rec.Body.String())
	}

	for _, p := range []string{
		"/dav/addressbooks/alice%2F..%2Fbob/contacts/secret.vcf",
		"/dav/addressbooks/alice/..%2F..%2Faddressbooks%2Fbob/contacts/secret.vcf",
		"/dav/addressbooks/alice/../bob/contacts/secret.vcf",
		"/dav/addressbooks/alice/%2e%2e/bob/contacts/secret.vcf",
		"/dav/addressbooks/%2e%2e/addressbooks/bob/contacts/secret.vcf",
		"/dav//addressbooks/bob/contacts/secret.vcf",
		"/dav/addressbooks//bob/contacts/secret.vcf",
		"/dav/addressbooks/alice/contacts/..%2F..%2Fbob%2Fcontacts%2Fsecret.vcf",
	} {
		t.Run(p, func(t *testing.T) {
			rec := f.do(t, http.MethodGet, p, "", "Authorization", basic("alice", alice))
			if rec.Code < 400 {
				t.Fatalf("status = %d, want a refusal", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "Lovelace") {
				t.Fatal("bob's contact leaked")
			}
		})
	}
}

// TestUsernameTakeover follows a username that moves to another subject once
// its holder can no longer use it: the new holder sees only its own data under
// the name, and the old holder's app password stays refused.
func TestUsernameTakeover(t *testing.T) {
	f := newFixtureWithLease(t, testLease)
	old := f.user(t, "sub-old", "alice")
	f.confirm(t, "sub-old")
	rec := f.do(t, http.MethodPut, "/dav/addressbooks/alice/contacts/ada.vcf", vcard,
		"Authorization", basic("alice", old), "Content-Type", "text/vcard")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT: %d %q", rec.Code, rec.Body.String())
	}

	// The old holder's lease runs out, then the name moves.
	f.clock.advance(testLease)
	f.verifier.tokens["tok-new"] = auth.Claims{Subject: "sub-new", PreferredUsername: "alice"}
	rec = f.do(t, http.MethodGet, "/dav/addressbooks/alice/contacts/ada.vcf", "", "Authorization", "Bearer tok-new")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("new holder GET: status = %d, want 404 (own, empty address book); body %q", rec.Code, rec.Body.String())
	}
	assertUnauthorized(t, f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", basic("alice", old)))
}

// A user who signs in with the username of an active account does not take it
// over: the holder's device keeps working and nothing of the holder's data is
// reachable for the newcomer.
func TestUsernameTakeoverOfAnActiveHolderIsRefused(t *testing.T) {
	f := newFixtureWithLease(t, testLease)
	holder := f.user(t, "sub-holder", "alice")
	f.confirm(t, "sub-holder")
	rec := f.do(t, http.MethodPut, "/dav/addressbooks/alice/contacts/ada.vcf", vcard,
		"Authorization", basic("alice", holder), "Content-Type", "text/vcard")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT: %d %q", rec.Code, rec.Body.String())
	}

	f.verifier.tokens["tok-new"] = auth.Claims{Subject: "sub-new", PreferredUsername: "alice"}
	for range 3 {
		rec = f.do(t, http.MethodGet, "/dav/addressbooks/alice/contacts/ada.vcf", "", "Authorization", "Bearer tok-new")
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "Lovelace") {
			t.Fatalf("the newcomer reached the holder's contact: status %d", rec.Code)
		}
	}
	rec = f.do(t, http.MethodGet, "/dav/addressbooks/alice/contacts/ada.vcf", "", "Authorization", basic("alice", holder))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Lovelace") {
		t.Fatalf("the holder's device after the attempt: status %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestWellKnownRedirectsWithoutCredentials(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{"/.well-known/caldav", "/.well-known/carddav"} {
		for _, m := range []string{http.MethodGet, "PROPFIND"} {
			req := httptest.NewRequest(m, p, nil)
			rec := httptest.NewRecorder()
			f.wk.ServeHTTP(rec, req)
			if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/dav/" {
				t.Errorf("%s %s: status = %d, Location = %q; want 301 to /dav/", m, p, rec.Code, rec.Header().Get("Location"))
			}
		}
	}
	if n := f.verifier.callCount(); n != 0 {
		t.Errorf("verifier called %d times", n)
	}
}

func TestLogsHoldNoCredentials(t *testing.T) {
	f := newFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	disabledSecret := f.user(t, "sub-dave", "dave")
	if _, err := f.reg.SetDisabled(context.Background(), "sub-dave", true); err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens["tok-secret-value"] = auth.Claims{Subject: "sub-alice", PreferredUsername: "alice"}
	f.verifier.tokens["tok-disabled-value"] = auth.Claims{Subject: "sub-dave", PreferredUsername: "dave"}
	headers := []string{
		basic("alice", secret),
		basic("alice", "wrong-"+secret),
		basic("nobody", secret),
		basic("dave", disabledSecret),
		"Bearer tok-secret-value",
		"Bearer tok-unknown-value",
		"Bearer tok-disabled-value",
	}
	for _, h := range headers {
		f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", h, "Depth", "0")
	}
	logs := f.logs.String()
	for _, s := range []string{secret, disabledSecret, "tok-secret-value", "tok-unknown-value", "tok-disabled-value", "Authorization", "Basic "} {
		if strings.Contains(logs, s) {
			t.Errorf("logs contain %q:\n%s", s, logs)
		}
	}
	for _, h := range headers {
		if strings.Contains(logs, strings.TrimPrefix(h, "Basic ")) {
			t.Errorf("logs contain an encoded header:\n%s", logs)
		}
	}
}

// TestUncleanPathsAreBadRequests checks that an authenticated request whose
// path has an empty, "." or ".." segment or an encoded slash gets 400 before
// it reaches the DAV server.
func TestUncleanPathsAreBadRequests(t *testing.T) {
	f := newFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	for _, p := range []string{
		"/dav/addressbooks/alice%2Fx/",
		"/dav/addressbooks/alice/contacts/a%2Fb.vcf",
		"/dav/addressbooks//contacts/",
		"/dav/addressbooks/alice/./contacts/",
		"/dav/addressbooks/alice/%2E/contacts/",
		"/dav/addressbooks/alice/../alice/contacts/",
		"/dav/addressbooks/alice/%2e%2e/alice/contacts/",
	} {
		t.Run(p, func(t *testing.T) {
			rec := f.do(t, "PROPFIND", p, propfindName, "Authorization", basic("alice", secret), "Depth", "0")
			if rec.Code != http.StatusBadRequest || rec.Body.String() != "bad request\n" {
				t.Fatalf("status = %d, body %q; want 400 %q", rec.Code, rec.Body.String(), "bad request\n")
			}
		})
	}
	// Without credentials the answer is still 401.
	assertUnauthorized(t, f.do(t, "PROPFIND", "/dav/addressbooks/alice%2Fx/", propfindName))
	// A clean path with the same credentials works.
	rec := f.do(t, "PROPFIND", "/dav/addressbooks/alice/contacts/", propfindName, "Authorization", basic("alice", secret), "Depth", "0")
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("clean path: status = %d", rec.Code)
	}
}
