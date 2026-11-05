package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/provision"
)

const base = "/groupware/v1/app-passwords"

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

type fakeVerifier struct{ tokens map[string]auth.Claims }

func (f *fakeVerifier) Verify(_ context.Context, raw string) (auth.Claims, error) {
	c, ok := f.tokens[raw]
	if !ok {
		return auth.Claims{}, auth.ErrInvalidToken
	}
	return c, nil
}

type fixture struct {
	dir      string
	st       *filestore.Store
	reg      *principals.Registry
	verifier *fakeVerifier
	logs     *syncBuffer
	handler  http.Handler
}

func newFixture(t *testing.T) *fixture { return newFixtureWith(t, nil, nil) }

// newFixtureWith is newFixture with options for the registry and the handler.
func newFixtureWith(t *testing.T, regOpts []principals.Option, apiOpts []Option) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, logger, append([]principals.Option{principals.WithBcryptCost(bcrypt.MinCost)}, regOpts...)...)
	if err != nil {
		t.Fatal(err)
	}
	v := &fakeVerifier{tokens: map[string]auth.Claims{
		"tok-alice": {Subject: "sub-alice", PreferredUsername: "alice", Client: "web"},
		"tok-bob":   {Subject: "sub-bob", PreferredUsername: "bob"},
		"tok-bad":   {Subject: "sub-bad", PreferredUsername: "bad/name"},
	}}
	h := AppPasswords(reg, nil, logger, apiOpts...)
	mux := http.NewServeMux()
	mux.Handle(base, h)
	mux.Handle(base+"/{id}", h)
	mux.Handle("/groupware/", NotFound())
	return &fixture{dir: dir, st: st, reg: reg, verifier: v, logs: logs,
		handler: auth.Middleware(v, logger)(mux)}
}

func (f *fixture) do(method, target, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

type created struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Password  string  `json:"password"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt string  `json:"expires_at"`
	LastUsed  *string `json:"last_used_at"`
}

func (f *fixture) create(t *testing.T, token, name string) created {
	t.Helper()
	rec := f.do(http.MethodPost, base, token, fmt.Sprintf(`{"name":%q}`, name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: status %d, body %q", rec.Code, rec.Body.String())
	}
	var c created
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || len(b) != 1 || b["error"] == "" {
		t.Fatalf("body %q is not {\"error\": ...}", rec.Body.String())
	}
	return b["error"]
}

func TestUnauthenticated(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct{ method, target, token string }{
		{http.MethodGet, base, ""},
		{http.MethodPost, base, ""},
		{http.MethodDelete, base + "/x", ""},
		{http.MethodGet, base, "unknown"},
	} {
		rec := f.do(tc.method, tc.target, tc.token, `{"name":"x"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s token %q: status %d", tc.method, tc.target, tc.token, rec.Code)
		}
	}
}

func TestHandlerWithoutClaims(t *testing.T) {
	f := newFixture(t)
	h := AppPasswords(f.reg, nil, slog.New(slog.DiscardHandler))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
	errorBody(t, rec)
}

func TestCreateReturnsSecretOnce(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodPost, base, "tok-alice", `{"name":"Phone"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	var c created
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-z2-7]{32}$`).MatchString(c.Password) {
		t.Errorf("password %q is not 32 lowercase base32 characters", c.Password)
	}
	if c.ID == "" || c.Name != "Phone" || c.CreatedAt == "" || c.ExpiresAt == "" || c.LastUsed != nil {
		t.Errorf("unexpected body %+v", c)
	}

	// The secret is never returned again, logged or stored.
	list := f.do(http.MethodGet, base, "tok-alice", "")
	if strings.Contains(list.Body.String(), c.Password) {
		t.Error("the list contains the secret")
	}
	if strings.Contains(f.logs.String(), c.Password) {
		t.Error("the secret was logged")
	}
	data, err := os.ReadFile(filepath.Join(f.st.OwnerDir("sub-alice"), "app_passwords.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), c.Password) {
		t.Error("the secret is on disk")
	}
	if _, err := f.reg.VerifyBasic(context.Background(), "alice", c.Password); err != nil {
		t.Errorf("the password does not verify: %v", err)
	}
}

func TestCreateWithShorterLifetime(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodPost, base, "tok-alice", `{"name":"Trip","expires_in_days":7}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	var c created
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	created, err := time.Parse(time.RFC3339, c.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	expires, err := time.Parse(time.RFC3339, c.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if got := expires.Sub(created); got != 7*24*time.Hour {
		t.Errorf("lifetime = %v, want 7 days", got)
	}

	// Without the field the lifetime stays 365 days.
	rec = f.do(http.MethodPost, base, "tok-alice", `{"name":"Phone"}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	created, _ = time.Parse(time.RFC3339, c.CreatedAt)
	expires, _ = time.Parse(time.RFC3339, c.ExpiresAt)
	if got := expires.Sub(created); got != 365*24*time.Hour {
		t.Errorf("default lifetime = %v, want 365 days", got)
	}
}

func TestListHidesSecretAndHash(t *testing.T) {
	f := newFixture(t)
	c := f.create(t, "tok-alice", "Phone")
	f.create(t, "tok-alice", "Laptop")

	rec := f.do(http.MethodGet, base, "tok-alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var got struct {
		AppPasswords    []map[string]any `json:"app_passwords"`
		MaxLifetimeDays int              `json:"max_lifetime_days"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.AppPasswords) != 2 {
		t.Fatalf("got %d entries: %s", len(got.AppPasswords), rec.Body.String())
	}
	for _, e := range got.AppPasswords {
		for k := range e {
			switch k {
			case "id", "name", "created_at", "expires_at", "last_used_at":
			default:
				t.Errorf("unexpected field %q in a list entry", k)
			}
		}
	}
	if got.AppPasswords[0]["id"] != c.ID || got.AppPasswords[0]["name"] != "Phone" {
		t.Errorf("first entry %v", got.AppPasswords[0])
	}
	if strings.Contains(rec.Body.String(), "$2a$") {
		t.Error("the list contains a hash")
	}
}

func TestListEmptyIsAnArray(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodGet, base, "tok-alice", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"app_passwords":[],"max_lifetime_days":365}` {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestDeleteRevokes(t *testing.T) {
	f := newFixture(t)
	c := f.create(t, "tok-alice", "Phone")
	other := f.create(t, "tok-alice", "Laptop")
	// Warm the credential cache.
	if _, err := f.reg.VerifyBasic(context.Background(), "alice", c.Password); err != nil {
		t.Fatal(err)
	}

	rec := f.do(http.MethodDelete, base+"/"+c.ID, "tok-alice", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	if _, err := f.reg.VerifyBasic(context.Background(), "alice", c.Password); err == nil {
		t.Error("the revoked password still works")
	}
	if _, err := f.reg.VerifyBasic(context.Background(), "alice", other.Password); err != nil {
		t.Errorf("the other password stopped working: %v", err)
	}
	again := f.do(http.MethodDelete, base+"/"+c.ID, "tok-alice", "")
	if again.Code != http.StatusNotFound {
		t.Errorf("second DELETE: status %d, want 404", again.Code)
	}
}

func TestPrincipalsAreIsolated(t *testing.T) {
	f := newFixture(t)
	a := f.create(t, "tok-alice", "Phone")
	f.create(t, "tok-bob", "Bob phone")

	bobList := f.do(http.MethodGet, base, "tok-bob", "")
	if strings.Contains(bobList.Body.String(), a.ID) {
		t.Errorf("bob sees alice's app password: %s", bobList.Body.String())
	}
	if strings.Count(bobList.Body.String(), `"id"`) != 1 {
		t.Errorf("bob's list: %s", bobList.Body.String())
	}

	cross := f.do(http.MethodDelete, base+"/"+a.ID, "tok-bob", "")
	unknown := f.do(http.MethodDelete, base+"/doesnotexist", "tok-bob", "")
	if cross.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound {
		t.Fatalf("cross %d, unknown %d, want 404 both", cross.Code, unknown.Code)
	}
	if cross.Body.String() != unknown.Body.String() {
		t.Errorf("bodies differ: %q vs %q", cross.Body.String(), unknown.Body.String())
	}
	if _, err := f.reg.VerifyBasic(context.Background(), "alice", a.Password); err != nil {
		t.Errorf("bob revoked alice's password: %v", err)
	}
}

func TestSubComesFromClaimsOnly(t *testing.T) {
	f := newFixture(t)
	a := f.create(t, "tok-alice", "Phone")
	// A body or query naming another owner changes nothing.
	rec := f.do(http.MethodPost, base+"?sub=sub-alice", "tok-bob", `{"name":"x","sub":"sub-alice"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status %d", rec.Code)
	}
	rec = f.do(http.MethodPost, base+"?sub=sub-alice", "tok-bob", `{"name":"x"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d", rec.Code)
	}
	list, err := f.reg.ListAppPasswords("sub-alice")
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Errorf("alice's passwords changed: %v %v", list, err)
	}
}

func TestBadRequests(t *testing.T) {
	f := newFixture(t)
	for name, body := range map[string]string{
		"empty body":    ``,
		"bad json":      `{"name":`,
		"not an object": `"Phone"`,
		"array":         `[]`,
		"null":          `null`,
		"missing name":  `{}`,
		"empty name":    `{"name":""}`,
		"blank name":    `{"name":"   "}`,
		"long name":     `{"name":"` + strings.Repeat("a", 101) + `"}`,
		"control name":  `{"name":"a\u0000b"}`,
		"name not text": `{"name":5}`,
		"unknown field": `{"name":"x","ttl":5}`,
		"zero days":     `{"name":"x","expires_in_days":0}`,
		"negative days": `{"name":"x","expires_in_days":-1}`,
		"too many days": `{"name":"x","expires_in_days":366}`,
		"days not int":  `{"name":"x","expires_in_days":1.5}`,
		"days as text":  `{"name":"x","expires_in_days":"7"}`,
		"two values":    `{"name":"x"}{"name":"y"}`,
		"trailing junk": `{"name":"x"} x`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(http.MethodPost, base, "tok-alice", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
			}
			errorBody(t, rec)
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
		})
	}
	list, _ := f.reg.ListAppPasswords("sub-alice")
	if len(list) != 0 {
		t.Errorf("a bad request created %d app passwords", len(list))
	}
}

func TestNameOfExactlyTheLimit(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodPost, base, "tok-alice", `{"name":"`+strings.Repeat("a", 100)+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestBodyIsSizeLimited(t *testing.T) {
	f := newFixture(t)
	big := `{"name":"` + strings.Repeat("a", 5000) + `"}`
	rec := f.do(http.MethodPost, base, "tok-alice", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", rec.Code)
	}
	errorBody(t, rec)
	// Padding after a valid value counts too.
	rec = f.do(http.MethodPost, base, "tok-alice", `{"name":"x"}`+strings.Repeat(" ", 5000))
	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Fatalf("padded body: status %d", rec.Code)
	}
}

func TestTooManyAppPasswords(t *testing.T) {
	f := newFixture(t)
	f.create(t, "tok-alice", "first")
	for i := 1; i < principals.MaxAppPasswords; i++ {
		if _, _, err := f.reg.IssueAppPassword(context.Background(), "sub-alice", "n", 0); err != nil {
			t.Fatal(err)
		}
	}
	rec := f.do(http.MethodPost, base, "tok-alice", `{"name":"one more"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", rec.Code)
	}
	errorBody(t, rec)
}

func TestFirstRequestProvisions(t *testing.T) {
	f := newFixture(t)
	if _, err := f.reg.Get("sub-alice"); err == nil {
		t.Fatal("alice exists before the first request")
	}
	if rec := f.do(http.MethodGet, base, "tok-alice", ""); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	p, err := f.reg.Get("sub-alice")
	if err != nil {
		t.Fatal(err)
	}
	// Username and display name are both the preferred username, as on the DAV path.
	if p.Username != "alice" || p.DisplayName != "alice" {
		t.Errorf("principal %+v", p)
	}
	for _, name := range []string{"principal.json"} {
		if _, err := os.Stat(filepath.Join(f.st.OwnerDir("sub-alice"), name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, kind := range []store.Kind{store.KindAddressBook, store.KindCalendar} {
		cols, err := f.st.ListCollections(context.Background(), "sub-alice", kind)
		if err != nil || len(cols) == 0 {
			t.Errorf("default %s collections: %v, %v", kind, cols, err)
		}
	}
}

func TestUnservableUsernameIsForbidden(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodGet, base, "tok-bad", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
	if body := errorBody(t, rec); strings.Contains(body, "bad") {
		t.Errorf("body %q names the user", body)
	}
}

// A disabled principal's bearer token is refused with 403 and a body that
// says why: the caller holds a valid token for that very user.
func TestDisabledPrincipalIsForbidden(t *testing.T) {
	f := newFixture(t)
	c := f.create(t, "tok-alice", "Phone")
	if _, err := f.reg.SetDisabled(context.Background(), "sub-alice", true); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodGet, base, ""},
		{http.MethodPost, base, `{"name":"Tablet"}`},
		{http.MethodDelete, base + "/" + c.ID, ""},
	} {
		rec := f.do(tc.method, tc.target, "tok-alice", tc.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status %d, want 403; body %q", tc.method, tc.target, rec.Code, rec.Body.String())
		}
		if body := errorBody(t, rec); body != "account disabled" {
			t.Errorf("%s %s: body %q, want \"account disabled\"", tc.method, tc.target, body)
		}
	}
	// bob is not affected, and alice's app password still exists.
	if rec := f.do(http.MethodGet, base, "tok-bob", ""); rec.Code != http.StatusOK {
		t.Errorf("bob: status %d, want 200", rec.Code)
	}
	if _, err := f.reg.SetDisabled(context.Background(), "sub-alice", false); err != nil {
		t.Fatal(err)
	}
	rec := f.do(http.MethodGet, base, "tok-alice", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), c.ID) {
		t.Errorf("after enable: status %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestUnavailableStore(t *testing.T) {
	f := newFixture(t)
	// A file where the owner directory must go makes provisioning fail.
	if err := os.WriteFile(f.st.OwnerDir("sub-alice"), []byte("x"), 0o600); err != nil {
		if mkErr := os.MkdirAll(filepath.Dir(f.st.OwnerDir("sub-alice")), 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
		if err := os.WriteFile(f.st.OwnerDir("sub-alice"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rec := f.do(http.MethodGet, base, "tok-alice", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if body := errorBody(t, rec); strings.Contains(body, f.dir) {
		t.Errorf("body %q leaks a path", body)
	}
}

func TestWrongMethodsAndPaths(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		method, target, allow string
		want                  int
	}{
		{http.MethodPut, base, "GET, POST", http.StatusMethodNotAllowed},
		{http.MethodPatch, base, "GET, POST", http.StatusMethodNotAllowed},
		{http.MethodHead, base, "GET, POST", http.StatusMethodNotAllowed},
		{http.MethodOptions, base, "GET, POST", http.StatusMethodNotAllowed},
		{http.MethodDelete, base, "GET, POST", http.StatusMethodNotAllowed},
		{http.MethodGet, base + "/x", "DELETE", http.StatusMethodNotAllowed},
		{http.MethodPost, base + "/x", "DELETE", http.StatusMethodNotAllowed},
		{http.MethodHead, base + "/x", "DELETE", http.StatusMethodNotAllowed},
		{http.MethodGet, base + "/", "", http.StatusNotFound},
		{http.MethodGet, "/groupware/v1/nope", "", http.StatusNotFound},
		{http.MethodPut, "/groupware/", "", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			rec := f.do(tc.method, tc.target, "tok-alice", "")
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d", rec.Code, tc.want)
			}
			if got := rec.Header().Get("Allow"); got != tc.allow {
				t.Errorf("Allow = %q, want %q", got, tc.allow)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			if tc.method != http.MethodHead {
				errorBody(t, rec)
			}
			// Unauthenticated, the middleware answers first.
			if un := f.do(tc.method, tc.target, "", ""); un.Code != http.StatusUnauthorized {
				t.Errorf("without a token: status %d, want 401", un.Code)
			}
		})
	}
	// A request that cannot succeed provisions nobody.
	if _, err := f.reg.Get("sub-alice"); err == nil {
		t.Error("a wrong method provisioned the principal")
	}
}

func TestContentType(t *testing.T) {
	f := newFixture(t)
	post := func(ct string) int {
		req := httptest.NewRequest(http.MethodPost, base, strings.NewReader(`{"name":"x"}`))
		req.Header.Set("Authorization", "Bearer tok-alice")
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusUnsupportedMediaType {
			errorBody(t, rec)
		}
		return rec.Code
	}
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/jsonx", "json", "application/json;;"} {
		if got := post(ct); got != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: status %d, want 415", ct, got)
		}
	}
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON"} {
		if got := post(ct); got != http.StatusCreated {
			t.Errorf("Content-Type %q: status %d, want 201", ct, got)
		}
	}
}

func TestLoggedIDIsClipped(t *testing.T) {
	f := newFixture(t)
	// A store that fails on revoke: the principal's app password file is not JSON.
	if _, _, err := f.reg.Provision(context.Background(), "sub-alice", "alice", "alice"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.st.OwnerDir("sub-alice"), "app_passwords.json")
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("z", 5000)
	if rec := f.do(http.MethodDelete, base+"/"+id, "tok-alice", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	logs := f.logs.String()
	if strings.Contains(logs, strings.Repeat("z", 65)) {
		t.Error("the caller's id was logged in full")
	}
	if !strings.Contains(logs, strings.Repeat("z", 64)+"...(truncated)") {
		t.Errorf("no clipped id in the logs: %q", logs)
	}
}

func TestClip(t *testing.T) {
	if got := clip("abc"); got != "abc" {
		t.Errorf("clip(abc) = %q", got)
	}
	if got := clip(strings.Repeat("a", 64)); len(got) != 64 {
		t.Errorf("a 64 byte id was changed: %q", got)
	}
	if got := clip(strings.Repeat("a", 65)); got != strings.Repeat("a", 64)+"...(truncated)" {
		t.Errorf("clip(65) = %q", got)
	}
}

type graphStub struct{ tokens []string }

func (g *graphStub) Me(_ context.Context, token string) (ocisgraph.User, error) {
	g.tokens = append(g.tokens, token)
	return ocisgraph.User{ID: "u-alice", Groups: []ocisgraph.Group{{ID: "g-1", Name: "users"}, {ID: "g-2", Name: "physics"}}}, nil
}

func TestSignInRecordsTheIdentity(t *testing.T) {
	f := newFixture(t)
	g := &graphStub{}
	h := AppPasswords(f.reg, provision.NewIdentities(f.reg, g, nil), nil)
	mux := http.NewServeMux()
	mux.Handle(base, h)
	f.handler = auth.Middleware(f.verifier, slog.New(slog.DiscardHandler))(mux)

	if rec := f.do(http.MethodGet, base, "tok-alice", ""); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if len(g.tokens) != 1 || g.tokens[0] != "tok-alice" {
		t.Fatalf("tokens = %v", g.tokens)
	}
	id, err := f.reg.Identity("sub-alice")
	if err != nil || id.OCISID != "u-alice" || len(id.Groups) != 2 {
		t.Fatalf("identity = %+v, %v", id, err)
	}
	// A second request within the interval asks nothing.
	f.do(http.MethodGet, base, "tok-alice", "")
	if len(g.tokens) != 1 {
		t.Fatalf("%d lookups, want 1", len(g.tokens))
	}
}

// The configured lifetime is the default and the ceiling.
func TestLifetimeFollowsTheConfiguredMaximum(t *testing.T) {
	f := newFixtureWith(t, []principals.Option{principals.WithAppPasswordLifetime(30 * 24 * time.Hour)}, nil)

	lifetime := func(body string) (time.Duration, *httptest.ResponseRecorder) {
		rec := f.do(http.MethodPost, base, "tok-alice", body)
		if rec.Code != http.StatusCreated {
			return 0, rec
		}
		var c created
		if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		created, _ := time.Parse(time.RFC3339, c.CreatedAt)
		expires, _ := time.Parse(time.RFC3339, c.ExpiresAt)
		return expires.Sub(created), rec
	}
	if got, rec := lifetime(`{"name":"a"}`); got != 30*24*time.Hour {
		t.Errorf("default lifetime = %v, want 30 days (status %d)", got, rec.Code)
	}
	if got, rec := lifetime(`{"name":"b","expires_in_days":30}`); got != 30*24*time.Hour {
		t.Errorf("the maximum itself = %v, want 30 days (status %d)", got, rec.Code)
	}
	_, rec := lifetime(`{"name":"c","expires_in_days":31}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("31 days: status %d, want 400", rec.Code)
	}
	if msg := errorBody(t, rec); !strings.Contains(msg, "1 to 30") {
		t.Errorf("message %q does not name the limit", msg)
	}

	rec = f.do(http.MethodGet, base, "tok-alice", "")
	var got struct {
		MaxLifetimeDays int `json:"max_lifetime_days"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.MaxLifetimeDays != 30 {
		t.Fatalf("max_lifetime_days = %d (%v), body %s", got.MaxLifetimeDays, err, rec.Body.String())
	}
}

// Issuing and revoking leave an audit line with who, what, with which token
// and from where, and never the secret.
func TestIssueAndRevokeAreLogged(t *testing.T) {
	f := newFixtureWith(t, nil, []Option{WithClientAddr(func(r *http.Request) string { return "203.0.113.9" })})
	c := f.create(t, "tok-alice", "Phone")
	if rec := f.do(http.MethodDelete, base+"/"+c.ID, "tok-alice", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status %d", rec.Code)
	}
	logs := f.logs.String()
	for _, line := range []string{"app password issued", "app password revoked"} {
		var got string
		for _, l := range strings.Split(logs, "\n") {
			if strings.Contains(l, line) {
				got = l
			}
		}
		if got == "" {
			t.Fatalf("no %q line in the logs:\n%s", line, logs)
		}
		for _, want := range []string{"owner=sub-alice", "id=" + c.ID, "client=web", "address=203.0.113.9"} {
			if !strings.Contains(got, want) {
				t.Errorf("%q line lacks %s:\n%s", line, want, got)
			}
		}
		if strings.Contains(line, "issued") {
			for _, want := range []string{"name=Phone", "expires_at="} {
				if !strings.Contains(got, want) {
					t.Errorf("%q line lacks %s:\n%s", line, want, got)
				}
			}
		}
	}
	if strings.Contains(logs, c.Password) {
		t.Error("the secret was logged")
	}
}

func TestAuditAddressIsOmittedWithoutAFunction(t *testing.T) {
	f := newFixture(t)
	f.create(t, "tok-alice", "Phone")
	if strings.Contains(f.logs.String(), "address=") {
		t.Errorf("an address was logged without a source for it:\n%s", f.logs.String())
	}
}
