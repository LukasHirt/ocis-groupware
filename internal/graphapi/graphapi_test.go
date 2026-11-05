package graphapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeVerifier struct{ tokens map[string]auth.Claims }

func (f *fakeVerifier) Verify(_ context.Context, raw string) (auth.Claims, error) {
	c, ok := f.tokens[raw]
	if !ok {
		return auth.Claims{}, auth.ErrInvalidToken
	}
	return c, nil
}

type fixture struct {
	st      *filestore.Store
	reg     *principals.Registry
	handler http.Handler
}

// newFixture serves the facade behind the same authentication middleware the
// service uses. Tokens "einstein" and "marie" sign in as those users.
func newFixture(t *testing.T, mods ...func(*Config)) *fixture {
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
	v := &fakeVerifier{tokens: map[string]auth.Claims{
		"einstein": {Subject: "sub-einstein", PreferredUsername: "einstein", DisplayName: "Albert Einstein"},
		"marie":    {Subject: "sub-marie", PreferredUsername: "marie", DisplayName: "Marie Curie"},
	}}
	cfg := Config{Store: st, Registry: reg, Logger: testLogger()}
	for _, mod := range mods {
		mod(&cfg)
	}
	h, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(Prefix+"/", h)
	mux.Handle(GroupwarePrefix+"/", h.Groupware())
	return &fixture{st: st, reg: reg, handler: auth.Middleware(v, testLogger())(mux)}
}

func (f *fixture) do(method, target, token, body string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

type gotError struct {
	Error struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		InnerError struct {
			RequestID string `json:"request-id"`
			Date      string `json:"date"`
		} `json:"innerError"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) gotError {
	t.Helper()
	var e gotError
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q is not a Graph error: %v", rec.Body.String(), err)
	}
	return e
}

func TestUnsupportedPathsAnswer501(t *testing.T) {
	f := newFixture(t)
	for _, target := range []string{
		"/graph/v1.0/me/events/delta",
		"/graph/v1.0/me/calendars/x/events/$count",
		"/graph/v1.0/$batch",
		"/graph/v1.0/me/contacts/delta",
		"/graph/v1.0/",
	} {
		rec := f.do(http.MethodGet, target, "einstein", "")
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("GET %s = %d, want 501", target, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("GET %s content type = %q", target, ct)
		}
		e := decodeError(t, rec)
		if e.Error.Code != "notImplemented" || e.Error.Message == "" {
			t.Errorf("GET %s error = %+v", target, e.Error)
		}
		if e.Error.InnerError.RequestID == "" || e.Error.InnerError.Date == "" {
			t.Errorf("GET %s error lacks innerError: %+v", target, e.Error)
		}
	}
}

func TestRequiresBearerToken(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodGet, "/graph/v1.0/me/calendars", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", rec.Code)
	}
	rec = f.do(http.MethodGet, "/graph/v1.0/me/calendars", "wrong", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token: %d, want 401", rec.Code)
	}
	// Without the middleware the handler itself refuses.
	h, err := New(Config{Store: f.st, Registry: f.reg})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/graph/v1.0/me/calendars", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no claims: %d, want 401", rec.Code)
	}
	if e := decodeError(t, rec); e.Error.Code != "unauthenticated" {
		t.Errorf("code = %q", e.Error.Code)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("New with an empty config succeeded")
	}
}

func TestRouterMethodsAndParams(t *testing.T) {
	f := newFixture(t)
	var gotID string
	h := f.handler
	_ = h
	api, err := New(Config{Store: f.st, Registry: f.reg, Logger: testLogger()}, withRoute(http.MethodGet, "me/things/{id}", func(w http.ResponseWriter, r *http.Request, c *call) {
		gotID = c.params["id"]
		writeJSON(w, http.StatusOK, map[string]string{"user": c.caller.Username})
	}))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(Prefix+"/", api)
	v := &fakeVerifier{tokens: map[string]auth.Claims{"t": {Subject: "s", PreferredUsername: "u"}}}
	srv := auth.Middleware(v, testLogger())(mux)

	do := func(method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("Authorization", "Bearer t")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	if rec := do(http.MethodGet, "/graph/v1.0/me/things/a%2Fb"); rec.Code != 200 || gotID != "a/b" {
		t.Errorf("GET = %d, id %q", rec.Code, gotID)
	}
	rec := do(http.MethodPost, "/graph/v1.0/me/things/x")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET" {
		t.Errorf("POST = %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	if e := decodeError(t, rec); e.Error.Code != "notAllowed" {
		t.Errorf("code = %q", e.Error.Code)
	}
	if rec := do(http.MethodGet, "/graph/v1.0/me/things/x/more"); rec.Code != http.StatusNotImplemented {
		t.Errorf("deeper path = %d, want 501", rec.Code)
	}
}

func TestPaging(t *testing.T) {
	items := make([]int, 25)
	for i := range items {
		items[i] = i
	}
	get := func(query string) ([]int, string, int) {
		req := httptest.NewRequest(http.MethodGet, "/graph/v1.0/me/calendars?"+query, nil)
		req.Host = "ocis.example"
		req.Header.Set("X-Forwarded-Proto", "https")
		page, next, aerr := paginate(req, items)
		if aerr != nil {
			return nil, "", aerr.status
		}
		return page, next, 0
	}

	page, next, _ := get("$top=10")
	if len(page) != 10 || page[0] != 0 {
		t.Fatalf("first page = %v", page)
	}
	if want := "https://ocis.example/graph/v1.0/me/calendars?%24skip=10&%24top=10"; next != want {
		t.Errorf("next = %q, want %q", next, want)
	}
	page, next, _ = get("$top=10&$skip=20")
	if len(page) != 5 || next != "" {
		t.Errorf("last page = %v, next %q", page, next)
	}
	page, _, _ = get("$skip=30")
	if len(page) != 0 {
		t.Errorf("past the end = %v", page)
	}
	if page, next, _ = get(""); len(page) != 25 || next != "" {
		t.Errorf("default = %d items, next %q", len(page), next)
	}
	for _, q := range []string{"$top=0", "$top=-1", "$top=x", "$top=100000", "$skip=-1", "$skip=x"} {
		if _, _, status := get(q); status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, status)
		}
	}
}
