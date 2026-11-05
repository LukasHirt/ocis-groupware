package auth

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeVerifier struct {
	claims Claims
	err    error
	gotRaw string
	calls  int
}

func (f *fakeVerifier) Verify(_ context.Context, raw string) (Claims, error) {
	f.calls++
	f.gotRaw = raw
	return f.claims, f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// serve runs a request through the middleware wrapped around a handler that
// records whether it was reached.
func serve(t *testing.T, v Verifier, req *http.Request) (rec *httptest.ResponseRecorder, reached bool, got Claims) {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		got, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	rec = httptest.NewRecorder()
	Middleware(v, testLogger())(next).ServeHTTP(rec, req)
	return rec, reached, got
}

func TestMiddlewarePublicPathsSkipAuth(t *testing.T) {
	for _, path := range []string{
		"/healthz",
		"/metrics",
		"/.well-known/caldav",
		"/.well-known/carddav",
		"/web/groupware.js",
		"/web/assets/chunk-abc.js",
	} {
		t.Run(path, func(t *testing.T) {
			v := &fakeVerifier{}
			rec, reached, _ := serve(t, v, httptest.NewRequest(http.MethodGet, path, nil))
			if !reached || rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, reached = %v; want the request to pass through", rec.Code, reached)
			}
			if v.calls != 0 {
				t.Errorf("verifier called %d times for a public path", v.calls)
			}
		})
	}
}

func TestMiddlewareProtectedPathsRequireToken(t *testing.T) {
	for _, path := range []string{
		"/graph/v1.0/me/events",
		"/groupware/v1/app-passwords",
		// The router matches escaped paths, so encoded dot segments must not
		// borrow a public or a self-authenticated prefix.
		"/graph/..%2Fweb/x",
		"/graph/..%2Fdav/x",
		"/graph/%2e%2e/dav/x",
		"/dav%2F..%2Fgraph/x",
		"/davx",
		"/dav../x",
		"/healthzx",
		"/metrics/extra",
		"/web",
		"/webx/groupware.js",
		"/.well-known",
		"/web/../graph/v1.0/me",
		"/dav/../graph/v1.0/me",
		"/",
	} {
		t.Run(path, func(t *testing.T) {
			v := &fakeVerifier{}
			rec, reached, _ := serve(t, v, httptest.NewRequest(http.MethodGet, path, nil))
			if reached {
				t.Fatal("request reached the handler without a token")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="ocis-groupware"` {
				t.Errorf("WWW-Authenticate = %q, want %q", got, `Bearer realm="ocis-groupware"`)
			}
		})
	}
}

func TestMiddlewareRejectsNonBearerAuthorization(t *testing.T) {
	for _, header := range []string{
		"Basic YWxpY2U6c2VjcmV0",
		"Bearer",
		"Bearer ",
		"Token abc",
		"abc",
	} {
		t.Run(header, func(t *testing.T) {
			v := &fakeVerifier{}
			req := httptest.NewRequest(http.MethodGet, "/graph/v1.0/me/events", nil)
			req.Header.Set("Authorization", header)
			rec, reached, _ := serve(t, v, req)
			if reached || rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, reached = %v; want 401 and no pass-through", rec.Code, reached)
			}
			if v.calls != 0 {
				t.Errorf("verifier called %d times for a malformed header", v.calls)
			}
		})
	}
}

// TestMiddlewareLeavesDAVToItsHandler checks that requests below /dav pass
// through untouched, whatever their Authorization header: the DAV handler
// authenticates them itself, with Basic or Bearer.
func TestMiddlewareLeavesDAVToItsHandler(t *testing.T) {
	for _, path := range []string{
		"/dav",
		"/dav/",
		"/dav/calendars/alice/personal/",
		"/dav/addressbooks/alice/contacts/x.vcf",
		"/dav/..%2Fweb/x",
		"/web/../dav/calendars/alice/",
	} {
		for _, header := range []string{"", "Basic YWxpY2U6c2VjcmV0", "Bearer tok"} {
			t.Run(path+" "+header, func(t *testing.T) {
				v := &fakeVerifier{}
				req := httptest.NewRequest("PROPFIND", path, nil)
				if header != "" {
					req.Header.Set("Authorization", header)
				}
				rec, reached, got := serve(t, v, req)
				if !reached || rec.Code != http.StatusNoContent {
					t.Fatalf("status = %d, reached = %v; want the request to pass through", rec.Code, reached)
				}
				if v.calls != 0 {
					t.Errorf("verifier called %d times", v.calls)
				}
				if got != (Claims{}) {
					t.Errorf("claims %+v attached to a DAV request", got)
				}
				if req.Header.Get("Authorization") != header {
					t.Error("Authorization header changed")
				}
			})
		}
	}
}

func TestMiddlewarePassesVerifiedClaimsToHandler(t *testing.T) {
	want := Claims{Subject: "u-123", PreferredUsername: "alice"}
	v := &fakeVerifier{claims: want}
	req := httptest.NewRequest(http.MethodGet, "/graph/v1.0/me/events", nil)
	req.Header.Set("Authorization", "bearer tok-abc")

	rec, reached, got := serve(t, v, req)

	if !reached || rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, reached = %v; want the request to pass through", rec.Code, reached)
	}
	if v.gotRaw != "tok-abc" {
		t.Errorf("verifier got token %q, want %q", v.gotRaw, "tok-abc")
	}
	if got != want {
		t.Errorf("claims in context = %+v, want %+v", got, want)
	}
}

func TestMiddlewareInvalidToken(t *testing.T) {
	v := &fakeVerifier{err: fmt.Errorf("%w: expired", ErrInvalidToken)}
	req := httptest.NewRequest(http.MethodGet, "/graph/v1.0/me/events", nil)
	req.Header.Set("Authorization", "Bearer bad")

	rec, reached, _ := serve(t, v, req)

	if reached || rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, reached = %v; want 401 and no pass-through", rec.Code, reached)
	}
	want := `Bearer realm="ocis-groupware", error="invalid_token"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestMiddlewareIdentityProviderUnavailable(t *testing.T) {
	v := &fakeVerifier{err: fmt.Errorf("%w: connection refused", ErrUnavailable)}
	req := httptest.NewRequest(http.MethodGet, "/graph/v1.0/me/events", nil)
	req.Header.Set("Authorization", "Bearer tok")

	rec, reached, _ := serve(t, v, req)

	if reached || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, reached = %v; want 503 and no pass-through", rec.Code, reached)
	}
}

func TestMiddlewareUnexpectedVerifierErrorFailsClosed(t *testing.T) {
	v := &fakeVerifier{err: fmt.Errorf("something else")}
	req := httptest.NewRequest(http.MethodGet, "/graph/v1.0/me/events", nil)
	req.Header.Set("Authorization", "Bearer tok")

	rec, reached, _ := serve(t, v, req)

	if reached || rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, reached = %v; want 401 and no pass-through", rec.Code, reached)
	}
}

func TestClaimsFromContextEmpty(t *testing.T) {
	if _, ok := ClaimsFromContext(context.Background()); ok {
		t.Error("ClaimsFromContext on an empty context reported ok")
	}
}

// TestMiddlewareExtraDAVPrefix checks that a prefix added with WithDAVPrefix
// is left to its DAV handler like /dav, and that without the option the same
// paths need a bearer token.
func TestMiddlewareExtraDAVPrefix(t *testing.T) {
	paths := []string{
		"/remote.php/dav",
		"/remote.php/dav/",
		"/remote.php/dav/principals/users/alice/",
		"/web/../remote.php/dav/calendars/alice/",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

			rec := httptest.NewRecorder()
			Middleware(&fakeVerifier{}, testLogger(), WithDAVPrefix("/remote.php/dav"))(next).
				ServeHTTP(rec, httptest.NewRequest("PROPFIND", path, nil))
			if rec.Code != http.StatusNoContent {
				t.Errorf("with the prefix: status = %d, want the request to pass through", rec.Code)
			}

			rec = httptest.NewRecorder()
			Middleware(&fakeVerifier{}, testLogger())(next).
				ServeHTTP(rec, httptest.NewRequest("PROPFIND", path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("without the prefix: status = %d, want 401", rec.Code)
			}
		})
	}
	for _, path := range []string{"/remote.php/davx", "/remote.php/webdav/", "/remote.php/dav%2F..%2Fx"} {
		rec := httptest.NewRecorder()
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		Middleware(&fakeVerifier{}, testLogger(), WithDAVPrefix("/remote.php/dav"))(next).
			ServeHTTP(rec, httptest.NewRequest("PROPFIND", path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, rec.Code)
		}
	}
}
