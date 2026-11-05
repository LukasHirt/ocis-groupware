package davmount

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/owncloud/ocis-groupware/internal/auth"
)

// compat mounts the classic-layout handler over the fixture's store and
// registry, as the service does when compat paths are on.
func (f *fixture) compat(t *testing.T) http.Handler {
	t.Helper()
	h, err := NewCompat(Config{Store: f.st, Registry: f.reg, Verifier: f.verifier, Logger: f.logger})
	if err != nil {
		t.Fatalf("NewCompat: %v", err)
	}
	return h
}

func serve(h http.Handler, method, target, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestNewCompatRejectsMissingDependencies(t *testing.T) {
	f := newFixture(t)
	if _, err := NewCompat(Config{Registry: f.reg, Verifier: f.verifier}); err == nil {
		t.Error("nil store accepted")
	}
	if _, err := NewCompat(Config{Store: f.st, Verifier: f.verifier}); err == nil {
		t.Error("nil registry accepted")
	}
	if _, err := NewCompat(Config{Store: f.st, Registry: f.reg}); err == nil {
		t.Error("nil verifier accepted")
	}
}

func TestCompatPrincipalUsesClassicHref(t *testing.T) {
	f := newFixture(t)
	h := f.compat(t)
	secret := f.user(t, "sub-alice", "alice")
	for _, p := range []string{"/remote.php/dav/", "/remote.php/dav"} {
		rec := serve(h, "PROPFIND", p, propfindPrincipal, "Authorization", basic("alice", secret), "Depth", "0")
		if rec.Code != http.StatusMultiStatus {
			t.Fatalf("%s: status = %d, body %q", p, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "/remote.php/dav/principals/users/alice/") {
			t.Errorf("%s: no classic principal href: %s", p, rec.Body.String())
		}
	}
	rec := serve(h, "PROPFIND", "/remote.php/dav/principals/users/alice/", `<?xml version="1.0"?>
<d:propfind xmlns:d="DAV:" xmlns:card="urn:ietf:params:xml:ns:carddav" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop>
<card:addressbook-home-set/><c:calendar-home-set/></d:prop></d:propfind>`,
		"Authorization", basic("alice", secret), "Depth", "0")
	body := rec.Body.String()
	for _, want := range []string{"/remote.php/dav/addressbooks/users/alice/", "/remote.php/dav/calendars/alice/"} {
		if rec.Code != http.StatusMultiStatus || !strings.Contains(body, want) {
			t.Errorf("home sets: status %d, want %q in %s", rec.Code, want, body)
		}
	}
}

func TestCompatSharesDataWithFlatMount(t *testing.T) {
	f := newFixture(t)
	h := f.compat(t)
	secret := f.user(t, "sub-alice", "alice")
	authz := basic("alice", secret)

	rec := serve(h, http.MethodPut, "/remote.php/dav/addressbooks/users/alice/contacts/ada.vcf", vcard,
		"Authorization", authz, "Content-Type", "text/vcard; charset=utf-8")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT: status = %d, body %q", rec.Code, rec.Body.String())
	}
	rec = f.do(t, http.MethodGet, "/dav/addressbooks/alice/contacts/ada.vcf", "", "Authorization", authz)
	if rec.Code != http.StatusOK || rec.Body.String() != vcard {
		t.Fatalf("GET through /dav: status = %d, body %q", rec.Code, rec.Body.String())
	}

	rec = serve(h, "MKCALENDAR", "/remote.php/dav/calendars/alice/work/", mkcalendar,
		"Authorization", authz, "Content-Type", "application/xml")
	if rec.Code != http.StatusCreated {
		t.Fatalf("MKCALENDAR: status = %d, body %q", rec.Code, rec.Body.String())
	}
	rec = serve(h, http.MethodPut, "/remote.php/dav/calendars/alice/work/standup.ics", event,
		"Authorization", authz, "Content-Type", "text/calendar; charset=utf-8")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT event: status = %d, body %q", rec.Code, rec.Body.String())
	}
	rec = f.do(t, http.MethodGet, "/dav/calendars/alice/work/standup.ics", "", "Authorization", authz)
	if rec.Code != http.StatusOK || rec.Body.String() != event {
		t.Fatalf("GET event through /dav: status = %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestCompatRefusesWhatDavRefuses(t *testing.T) {
	f := newFixture(t)
	h := f.compat(t)
	alice := f.user(t, "sub-alice", "alice")
	f.user(t, "sub-bob", "bob")

	rec := serve(h, "PROPFIND", "/remote.php/dav/", propfindPrincipal, "Depth", "0")
	assertUnauthorized(t, rec)
	rec = serve(h, "PROPFIND", "/remote.php/dav/", propfindPrincipal, "Authorization", basic("alice", "wrong"), "Depth", "0")
	assertUnauthorized(t, rec)

	rec = serve(h, "PROPFIND", "/remote.php/dav/addressbooks/users/bob/", propfindName,
		"Authorization", basic("alice", alice), "Depth", "1")
	if rec.Code != http.StatusForbidden {
		t.Errorf("another user's address books: status = %d, want 403", rec.Code)
	}
	rec = serve(h, "PROPFIND", "/remote.php/dav/principals/users/bob/", propfindName,
		"Authorization", basic("alice", alice), "Depth", "0")
	if rec.Code != http.StatusForbidden {
		t.Errorf("another user's principal: status = %d, want 403", rec.Code)
	}
	rec = serve(h, "PROPFIND", "/remote.php/dav/addressbooks/users/../../../x/", propfindName,
		"Authorization", basic("alice", alice), "Depth", "1")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("dot segments: status = %d, want 400", rec.Code)
	}
}

func TestCompatDoesNotServeTheFlatAddressBookLayout(t *testing.T) {
	f := newFixture(t)
	h := f.compat(t)
	secret := f.user(t, "sub-alice", "alice")
	rec := serve(h, "PROPFIND", "/remote.php/dav/addressbooks/alice/", propfindName,
		"Authorization", basic("alice", secret), "Depth", "1")
	if rec.Code != http.StatusNotFound {
		t.Errorf("flat address book path: status = %d, want 404", rec.Code)
	}
}

func TestCompatBearer(t *testing.T) {
	f := newFixture(t)
	h := f.compat(t)
	f.verifier.tokens["tok"] = auth.Claims{Subject: "sub-carol", PreferredUsername: "carol"}
	rec := serve(h, "PROPFIND", "/remote.php/dav/", propfindPrincipal, "Authorization", "Bearer tok", "Depth", "0")
	if rec.Code != http.StatusMultiStatus || !strings.Contains(rec.Body.String(), "/remote.php/dav/principals/users/carol/") {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
}
