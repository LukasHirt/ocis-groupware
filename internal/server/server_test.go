package server

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/davmount"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthz(t *testing.T) {
	s := New(":0", testLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"status":"ok"`) {
		t.Errorf("body = %q, want it to contain %q", got, `"status":"ok"`)
	}
}

func TestHealthzReadiness(t *testing.T) {
	calls := 0
	var readyErr error
	s := New(":0", testLogger(), nil, WithReadiness(func(context.Context) error {
		calls++
		return readyErr
	}))
	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.httpServer.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		return rec
	}

	if rec := get(); rec.Code != http.StatusOK {
		t.Fatalf("ready: status = %d, want %d", rec.Code, http.StatusOK)
	}
	readyErr = errors.New("open /secret/path: read-only")
	rec := get()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("not ready: status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Body.String(); strings.Contains(got, "read-only") || strings.Contains(got, "error") {
		t.Errorf("body = %q, must not contain error text", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if calls != 2 {
		t.Errorf("readiness called %d times, want once per request (2)", calls)
	}
}

func TestMetrics(t *testing.T) {
	s := New(":0", testLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); !strings.Contains(got, "go_goroutines") {
		t.Errorf("body does not contain expected metric name %q", "go_goroutines")
	}
}

func TestUnknownRoute(t *testing.T) {
	s := New(":0", testLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	s := New("127.0.0.1:0", testLogger(), nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- s.Run(ctx)
	}()

	// Give the server a moment to start listening, then request shutdown.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

type stubVerifier struct{ err error }

func (s stubVerifier) Verify(_ context.Context, _ string) (auth.Claims, error) {
	return auth.Claims{Subject: "u-1"}, s.err
}

func TestAuthMiddlewareProtectsEverythingButPublicPaths(t *testing.T) {
	s := New(":0", testLogger(), auth.Middleware(stubVerifier{}, testLogger()))

	tests := []struct {
		name   string
		path   string
		header string
		want   int
	}{
		{"healthz is public", "/healthz", "", http.StatusOK},
		{"metrics is public", "/metrics", "", http.StatusOK},
		{"unregistered path without token fails closed", "/groupware/v1/x", "", http.StatusUnauthorized},
		{"unregistered path with token reaches the router", "/groupware/v1/x", "Bearer tok", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			s.httpServer.Handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// davFixture is a server with the DAV handler mounted behind the bearer
// middleware, as main builds it.
type davFixture struct {
	srv      *Server
	reg      *principals.Registry
	st       *filestore.Store
	verifier *tokenVerifier
}

type tokenVerifier struct{ tokens map[string]auth.Claims }

func (v *tokenVerifier) Verify(_ context.Context, raw string) (auth.Claims, error) {
	c, ok := v.tokens[raw]
	if !ok {
		return auth.Claims{}, auth.ErrInvalidToken
	}
	return c, nil
}

func newDAVFixture(t *testing.T) *davFixture {
	t.Helper()
	dir := t.TempDir()
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, testLogger(), principals.WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	v := &tokenVerifier{tokens: map[string]auth.Claims{}}
	dav, wk, err := davmount.New(davmount.Config{Store: st, Registry: reg, Verifier: v, Logger: testLogger()})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(":0", testLogger(), auth.Middleware(v, testLogger()), WithDAV(dav, wk))
	return &davFixture{srv: srv, reg: reg, st: st, verifier: v}
}

func (f *davFixture) user(t *testing.T, sub, username string) string {
	t.Helper()
	if _, _, err := f.reg.Provision(context.Background(), sub, username, username); err != nil {
		t.Fatal(err)
	}
	_, secret, err := f.reg.IssueAppPassword(context.Background(), sub, "Phone", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func (f *davFixture) do(method, target, authz, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "text/vcard")
	}
	rec := httptest.NewRecorder()
	f.srv.httpServer.Handler.ServeHTTP(rec, req)
	return rec
}

func basicAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

const testCard = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c1\r\nFN:Bob Secret\r\nN:Secret;Bob;;;\r\nEND:VCARD\r\n"

func TestDAVRoutes(t *testing.T) {
	f := newDAVFixture(t)
	secret := f.user(t, "sub-alice", "alice")
	f.verifier.tokens["tok-alice"] = auth.Claims{Subject: "sub-alice", PreferredUsername: "alice"}

	tests := []struct {
		name, method, path, authz string
		want                      int
	}{
		{"root without credentials", "PROPFIND", "/dav/", "", http.StatusUnauthorized},
		{"bare prefix without credentials", "PROPFIND", "/dav", "", http.StatusUnauthorized},
		{"unknown DAV path without credentials", "GET", "/dav/nope/deeper", "", http.StatusUnauthorized},
		{"root with an app password", "PROPFIND", "/dav/", basicAuth("alice", secret), http.StatusMultiStatus},
		{"bare prefix with an app password", "PROPFIND", "/dav", basicAuth("alice", secret), http.StatusMultiStatus},
		{"root with a bearer token", "PROPFIND", "/dav/", "Bearer tok-alice", http.StatusMultiStatus},
		{"app password outside /dav", "GET", "/groupware/v1/x", basicAuth("alice", secret), http.StatusUnauthorized},
		{"caldav discovery", "PROPFIND", "/.well-known/caldav", "", http.StatusMovedPermanently},
		{"carddav discovery", "GET", "/.well-known/carddav", "", http.StatusMovedPermanently},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.do(tc.method, tc.path, tc.authz, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %q", rec.Code, tc.want, rec.Body.String())
			}
			switch tc.want {
			case http.StatusUnauthorized:
				if tc.path != "/groupware/v1/x" && rec.Header().Get("WWW-Authenticate") != `Basic realm="ocis-groupware"` {
					t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
				}
			case http.StatusMovedPermanently:
				if loc := rec.Header().Get("Location"); loc != "/dav/" {
					t.Errorf("Location = %q, want /dav/", loc)
				}
			}
		})
	}
}

// TestDAVPathTricks sends paths through the whole server that try to reach
// another user's data or a public prefix by way of /dav.
func TestDAVPathTricks(t *testing.T) {
	f := newDAVFixture(t)
	alice := f.user(t, "sub-alice", "alice")
	bob := f.user(t, "sub-bob", "bob")
	if rec := f.do(http.MethodPut, "/dav/addressbooks/bob/contacts/c1.vcf", basicAuth("bob", bob), testCard); rec.Code != http.StatusCreated {
		t.Fatalf("bob's PUT: %d %q", rec.Code, rec.Body.String())
	}

	for _, p := range []string{
		"/dav/../web/x",
		"/dav/..%2Fweb/x",
		"/dav%2F..%2Fweb/x",
		"/web/../dav/addressbooks/bob/contacts/c1.vcf",
		"/web/..%2Fdav/addressbooks/bob/contacts/c1.vcf",
		"/dav/addressbooks/alice/../bob/contacts/c1.vcf",
		"/dav/addressbooks/alice%2F..%2Fbob/contacts/c1.vcf",
		"/dav/addressbooks/%2e%2e/bob/contacts/c1.vcf",
		"/dav//addressbooks/bob/contacts/c1.vcf",
		"//dav/addressbooks/bob/contacts/c1.vcf",
		"/dav/addressbooks/Bob/contacts/c1.vcf",
		"/dav/addressbooks/bob/contacts/c1.vcf",
	} {
		for _, authz := range []string{"", basicAuth("alice", alice)} {
			t.Run(p+" "+authz, func(t *testing.T) {
				rec := f.do(http.MethodGet, p, authz, "")
				if rec.Code < 300 {
					t.Fatalf("status = %d, want a redirect or a refusal; body %q", rec.Code, rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), "Secret") {
					t.Fatal("bob's contact leaked")
				}
				if loc := rec.Header().Get("Location"); loc != "" {
					// A redirect names the clean path, which must refuse too.
					follow := f.do(http.MethodGet, loc, authz, "")
					if follow.Code < 300 || strings.Contains(follow.Body.String(), "Secret") {
						t.Fatalf("redirect to %q answered %d", loc, follow.Code)
					}
				}
			})
		}
	}
}

// connectPaths are CONNECT targets with dot segments. ServeMux neither cleans
// nor redirects CONNECT requests, so the router would match them as given
// while the authentication middleware classifies the cleaned path.
var connectPaths = []string{
	"/graph/../dav/x",
	"/groupware/v1/x/../../../dav/y",
	"/graph/x/../../web/y",
}

func connectRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Method = http.MethodConnect
	req.URL.Path = target
	req.RequestURI = target
	return req
}

func assertConnectRefused(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != allowedMethods {
		t.Errorf("Allow = %q, want %q", got, allowedMethods)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got := rec.Body.String(); got != "{\"error\":\"method not allowed\"}\n" {
		t.Errorf("body = %q", got)
	}
}

// TestConnectIsRefusedBeforeRouting drives the whole server: a CONNECT never
// reaches the authentication middleware, the DAV handler or the router.
func TestConnectIsRefusedBeforeRouting(t *testing.T) {
	var reached []string
	record := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = append(reached, name+" "+r.Method+" "+r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		})
	}
	authn := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = append(reached, "authn "+r.Method+" "+r.URL.Path)
			next.ServeHTTP(w, r)
		})
	}
	s := New(":0", testLogger(), authn, WithDAV(record("dav"), record("well-known")))

	for _, p := range append(slices.Clone(connectPaths), "/dav/", "/healthz") {
		t.Run(p, func(t *testing.T) {
			reached = nil
			rec := httptest.NewRecorder()
			s.httpServer.Handler.ServeHTTP(rec, connectRequest(p))
			assertConnectRefused(t, rec)
			if len(reached) != 0 {
				t.Fatalf("CONNECT reached %v", reached)
			}
		})
	}

	// Other methods are unaffected.
	for _, m := range []string{http.MethodGet, "PROPFIND", http.MethodPut, http.MethodOptions} {
		reached = nil
		rec := httptest.NewRecorder()
		s.httpServer.Handler.ServeHTTP(rec, httptest.NewRequest(m, "/dav/x", nil))
		if rec.Code != http.StatusNoContent || len(reached) != 2 {
			t.Errorf("%s /dav/x: status = %d, reached %v; want the DAV handler", m, rec.Code, reached)
		}
	}
}

// TestConnectGuardWithCatchAllRoutes simulates a router with method-less
// protected routes, where a CONNECT with dot segments would otherwise pass
// the middleware as DAV or public and reach a protected handler.
func TestConnectGuardWithCatchAllRoutes(t *testing.T) {
	var reached []string
	mux := http.NewServeMux()
	for _, p := range []string{"/graph/", "/groupware/", "/web/", "/dav/"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			reached = append(reached, p)
			w.WriteHeader(http.StatusNoContent)
		})
	}
	unguarded := auth.Middleware(stubVerifier{}, testLogger())(mux)
	guarded := refuseConnect(unguarded)

	for _, p := range connectPaths {
		t.Run(p, func(t *testing.T) {
			// Without the guard the gap is real, which shows the test
			// exercises it.
			reached = nil
			unguarded.ServeHTTP(httptest.NewRecorder(), connectRequest(p))
			if len(reached) == 0 {
				t.Fatal("unguarded CONNECT did not reach a handler; the test does not exercise the gap")
			}

			reached = nil
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, connectRequest(p))
			assertConnectRefused(t, rec)
			if len(reached) != 0 {
				t.Fatalf("guarded CONNECT reached %v", reached)
			}
		})
	}

	reached = nil
	req := httptest.NewRequest(http.MethodGet, "/graph/x", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || len(reached) != 1 {
		t.Fatalf("GET /graph/x: status = %d, reached %v", rec.Code, reached)
	}
}
