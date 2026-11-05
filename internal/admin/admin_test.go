package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

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

// fixture is the admin handler over a registry on the real file store, with
// alice (sub-1) and bob (sub-2) provisioned.
type fixture struct {
	fs      *filestore.Store
	reg     *principals.Registry
	logs    *syncBuffer
	handler http.Handler
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	fs, err := filestore.Open(dir)
	if err != nil {
		t.Fatalf("filestore.Open: %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	reg, err := principals.NewRegistry(dir, fs, fs.OwnerDir, logger, principals.WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	for _, p := range [][3]string{{"sub-1", "alice", "Alice Adams"}, {"sub-2", "bob", "Bob Brown"}} {
		if _, _, err := reg.Provision(context.Background(), p[0], p[1], p[2]); err != nil {
			t.Fatalf("Provision(%q): %v", p[0], err)
		}
	}
	return &fixture{fs: fs, reg: reg, logs: logs, handler: Handler(reg, logger)}
}

// do sends a request to the handler. A non-empty body is sent as JSON.
func (f *fixture) do(method, path, body string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) disable(t *testing.T, sub string) {
	t.Helper()
	if _, err := f.reg.SetDisabled(context.Background(), sub, true); err != nil {
		t.Fatalf("SetDisabled(%q): %v", sub, err)
	}
}

func (f *fixture) createCollection(t *testing.T, owner string, kind store.Kind, name string) {
	t.Helper()
	ref := store.CollectionRef{Owner: owner, Kind: kind, Name: name}
	if _, err := f.fs.CreateCollection(context.Background(), ref, store.Props{DisplayName: name}); err != nil {
		t.Fatalf("CreateCollection(%v): %v", ref, err)
	}
}

func (f *fixture) issue(t *testing.T, sub, name string) {
	t.Helper()
	if _, _, err := f.reg.IssueAppPassword(context.Background(), sub, name, 0); err != nil {
		t.Fatalf("IssueAppPassword(%q): %v", sub, err)
	}
}

type listedPrincipal struct {
	Sub         string `json:"sub"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
	Disabled    bool   `json:"disabled"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d %q, want %d", rec.Code, rec.Body.String(), want)
	}
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	assertStatus(t, rec, status)
	var body struct {
		Error string `json:"error"`
	}
	decode(t, rec, &body)
	if body.Error != msg {
		t.Fatalf("error = %q, want %q", body.Error, msg)
	}
}

func TestList(t *testing.T) {
	f := newFixture(t)
	f.disable(t, "sub-2")

	rec := f.do(http.MethodGet, "/v1/principals", "")
	assertStatus(t, rec, http.StatusOK)
	var body struct {
		Principals []listedPrincipal `json:"principals"`
	}
	decode(t, rec, &body)
	if len(body.Principals) != 2 {
		t.Fatalf("principals = %+v, want alice and bob", body.Principals)
	}
	alice, bob := body.Principals[0], body.Principals[1]
	if alice.Sub != "sub-1" || alice.Username != "alice" || alice.DisplayName != "Alice Adams" || alice.Disabled {
		t.Errorf("alice = %+v", alice)
	}
	if bob.Sub != "sub-2" || bob.Username != "bob" || !bob.Disabled {
		t.Errorf("bob = %+v", bob)
	}
	if alice.CreatedAt == "" {
		t.Errorf("alice has no created_at: %+v", alice)
	}
}

func TestListIsEmptyArrayWithoutPrincipals(t *testing.T) {
	dir := t.TempDir()
	fs, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	reg, err := principals.NewRegistry(dir, fs, fs.OwnerDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	Handler(reg, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/principals", nil))
	assertStatus(t, rec, http.StatusOK)
	if got := strings.TrimSpace(rec.Body.String()); got != `{"principals":[]}` {
		t.Fatalf("body = %s, want an empty array", got)
	}
}

func TestDisableAndEnable(t *testing.T) {
	f := newFixture(t)

	rec := f.do(http.MethodPost, "/v1/principals/disable", `{"sub":"sub-1"}`)
	assertStatus(t, rec, http.StatusOK)
	var p listedPrincipal
	decode(t, rec, &p)
	if p.Sub != "sub-1" || p.Username != "alice" || !p.Disabled {
		t.Fatalf("disable answered %+v", p)
	}
	if got, _ := f.reg.Get("sub-1"); !got.Disabled {
		t.Fatal("principal is not disabled in the registry")
	}
	if logs := f.logs.String(); !strings.Contains(logs, "principal disabled by admin") ||
		!strings.Contains(logs, "sub=sub-1") || !strings.Contains(logs, "username=alice") {
		t.Fatalf("no audit line for the disable in logs:\n%s", logs)
	}

	// Disabling twice is fine: the answer is the same.
	rec = f.do(http.MethodPost, "/v1/principals/disable", `{"sub":"sub-1"}`)
	assertStatus(t, rec, http.StatusOK)

	rec = f.do(http.MethodPost, "/v1/principals/enable", `{"sub":"sub-1"}`)
	assertStatus(t, rec, http.StatusOK)
	decode(t, rec, &p)
	if p.Disabled {
		t.Fatalf("enable answered %+v", p)
	}
	if got, _ := f.reg.Get("sub-1"); got.Disabled {
		t.Fatal("principal is still disabled in the registry")
	}
	if !strings.Contains(f.logs.String(), "principal enabled by admin") {
		t.Fatalf("no audit line for the enable in logs:\n%s", f.logs.String())
	}
}

func TestDisableUnknownSub(t *testing.T) {
	f := newFixture(t)
	for _, verb := range []string{"disable", "enable"} {
		rec := f.do(http.MethodPost, "/v1/principals/"+verb, `{"sub":"nobody"}`)
		assertError(t, rec, http.StatusNotFound, "not found")
	}
}

type purgeJSON struct {
	Sub          string `json:"sub"`
	Calendars    int    `json:"calendars"`
	AddressBooks int    `json:"addressbooks"`
	AppPasswords int    `json:"app_passwords"`
}

func TestPurgeRefusesEnabledPrincipal(t *testing.T) {
	f := newFixture(t)
	f.createCollection(t, "sub-1", store.KindCalendar, "work")

	rec := f.do(http.MethodPost, "/v1/principals/purge", `{"sub":"sub-1"}`)
	assertError(t, rec, http.StatusConflict, "principal is not disabled")
	if _, err := os.Stat(f.fs.OwnerDir("sub-1")); err != nil {
		t.Fatalf("owner directory was touched: %v", err)
	}
	if _, err := f.reg.Get("sub-1"); err != nil {
		t.Fatalf("principal is gone: %v", err)
	}
}

func TestPurgeDryRunCountsAndKeeps(t *testing.T) {
	f := newFixture(t)
	// Besides the default "personal" calendar and "contacts" address book.
	f.createCollection(t, "sub-1", store.KindCalendar, "work")
	f.createCollection(t, "sub-1", store.KindCalendar, "home")
	f.createCollection(t, "sub-1", store.KindAddressBook, "friends")
	f.issue(t, "sub-1", "Phone")
	f.issue(t, "sub-1", "Laptop")
	f.issue(t, "sub-1", "Tablet")
	f.disable(t, "sub-1")

	rec := f.do(http.MethodPost, "/v1/principals/purge", `{"sub":"sub-1","dry_run":true}`)
	assertStatus(t, rec, http.StatusOK)
	var report purgeJSON
	decode(t, rec, &report)
	want := purgeJSON{Sub: "sub-1", Calendars: 3, AddressBooks: 2, AppPasswords: 3}
	if report != want {
		t.Fatalf("dry run = %+v, want %+v", report, want)
	}
	if _, err := os.Stat(f.fs.OwnerDir("sub-1")); err != nil {
		t.Fatalf("dry run removed the owner directory: %v", err)
	}
	if strings.Contains(f.logs.String(), "purged by admin") {
		t.Fatalf("dry run wrote an audit line:\n%s", f.logs.String())
	}
}

func TestPurge(t *testing.T) {
	f := newFixture(t)
	f.createCollection(t, "sub-1", store.KindCalendar, "work")
	f.issue(t, "sub-1", "Phone")
	f.disable(t, "sub-1")

	rec := f.do(http.MethodPost, "/v1/principals/purge", `{"sub":"sub-1"}`)
	assertStatus(t, rec, http.StatusOK)
	var report purgeJSON
	decode(t, rec, &report)
	want := purgeJSON{Sub: "sub-1", Calendars: 2, AddressBooks: 1, AppPasswords: 1}
	if report != want {
		t.Fatalf("purge = %+v, want %+v", report, want)
	}
	if _, err := os.Stat(f.fs.OwnerDir("sub-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner directory still exists (stat err %v)", err)
	}
	if _, err := f.reg.Get("sub-1"); !errors.Is(err, principals.ErrNotFound) {
		t.Fatalf("Get after purge = %v, want ErrNotFound", err)
	}
	if logs := f.logs.String(); !strings.Contains(logs, "principal purged by admin") ||
		!strings.Contains(logs, "sub=sub-1") || !strings.Contains(logs, "username=alice") {
		t.Fatalf("no audit line for the purge in logs:\n%s", logs)
	}

	rec = f.do(http.MethodPost, "/v1/principals/purge", `{"sub":"sub-1"}`)
	assertError(t, rec, http.StatusNotFound, "not found")
}

func TestBadRequests(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"not json", http.MethodPost, "/v1/principals/disable", `sub-1`, http.StatusBadRequest},
		{"unknown field", http.MethodPost, "/v1/principals/disable", `{"sub":"sub-1","force":true}`, http.StatusBadRequest},
		{"empty sub", http.MethodPost, "/v1/principals/disable", `{"sub":""}`, http.StatusBadRequest},
		{"no body", http.MethodPost, "/v1/principals/disable", ``, http.StatusBadRequest},
		{"two values", http.MethodPost, "/v1/principals/purge", `{"sub":"sub-1"} {}`, http.StatusBadRequest},
		{"too large", http.MethodPost, "/v1/principals/enable", `{"sub":"` + strings.Repeat("x", 5000) + `"}`, http.StatusRequestEntityTooLarge},
		{"list is GET only", http.MethodPost, "/v1/principals", `{}`, http.StatusMethodNotAllowed},
		{"disable is POST only", http.MethodGet, "/v1/principals/disable", ``, http.StatusMethodNotAllowed},
		{"purge is POST only", http.MethodDelete, "/v1/principals/purge", ``, http.StatusMethodNotAllowed},
		{"unknown path", http.MethodGet, "/v1/principals/sub-1", ``, http.StatusNotFound},
		{"root", http.MethodGet, "/", ``, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.do(tc.method, tc.path, tc.body)
			assertStatus(t, rec, tc.status)
			var body struct {
				Error string `json:"error"`
			}
			decode(t, rec, &body)
			if body.Error == "" {
				t.Fatalf("no error message in %q", rec.Body.String())
			}
			if tc.status == http.StatusMethodNotAllowed && rec.Header().Get("Allow") == "" {
				t.Fatal("405 without an Allow header")
			}
		})
	}
	// Nothing above changed alice.
	if p, err := f.reg.Get("sub-1"); err != nil || p.Disabled {
		t.Fatalf("alice after the bad requests: %+v, %v", p, err)
	}
}
