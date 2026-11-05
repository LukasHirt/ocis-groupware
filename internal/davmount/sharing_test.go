package davmount

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/provision"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// shareFixture mounts the DAV handler over a sharing view, with alice who
// owns a calendar and bob who receives it.
type shareFixture struct {
	*fixture
	shares *sharing.Shares
	alice  string // Basic header
	bob    string
	cal    store.Collection
}

func newShareFixture(t *testing.T) *shareFixture {
	t.Helper()
	return newShareFixtureFor(t, "sub-alice")
}

// newShareFixtureFor is newShareFixture with alice's data kept under
// aliceOwner. When that is not her sub, alice is a principal that was bound
// to her sub after it was created, as an imported user is.
func newShareFixtureFor(t *testing.T, aliceOwner string) *shareFixture {
	t.Helper()
	return newShareFixtureWith(t, aliceOwner, 0, nil)
}

// newShareFixtureWith is newShareFixtureFor with an identity lease and, when
// graph is not nil, an identity lookup at bearer sign-ins.
func newShareFixtureWith(t *testing.T, aliceOwner string, lease time.Duration, graph provision.Looker) *shareFixture {
	t.Helper()
	f := newFixtureWithLease(t, lease)
	sf := &shareFixture{fixture: f}
	ctx := context.Background()
	if aliceOwner != "sub-alice" {
		dir := f.st.OwnerDir(aliceOwner)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		file := `{"owner":"` + aliceOwner + `","sub":"sub-alice","username":"alice","display_name":"alice","created_at":"2026-11-01T09:00:00Z"}`
		if err := os.WriteFile(filepath.Join(dir, "principal.json"), []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
		f.reg = f.newRegistry(t)
	}
	if _, _, err := f.reg.Provision(ctx, "sub-alice", "alice", "alice"); err != nil {
		t.Fatal(err)
	}
	_, secret, err := f.reg.IssueAppPassword(ctx, aliceOwner, "Phone", f.passwordTTL())
	if err != nil {
		t.Fatal(err)
	}
	sf.alice = basic("alice", secret)
	sf.bob = basic("bob", f.user(t, "sub-bob", "bob"))
	for owner, id := range map[string]string{aliceOwner: "u-alice", "sub-bob": "u-bob"} {
		if err := f.reg.SetIdentity(ctx, owner, principals.Identity{OCISID: id}); err != nil {
			t.Fatal(err)
		}
	}

	shares, err := sharing.Open(sharing.Config{
		OwnerDir: f.st.OwnerDir,
		Owners: func() []string {
			var owners []string
			for _, p := range f.reg.ListPrincipals(ctx) {
				owners = append(owners, p.Owner)
			}
			return owners
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := sharing.NewView(sharing.ViewConfig{
		Inner:  f.st,
		Shares: shares,
		Identity: func(owner string) (string, []string) {
			id, _ := f.reg.Identity(owner)
			ids := make([]string, 0, len(id.Groups))
			for _, g := range id.Groups {
				ids = append(ids, g.ID)
			}
			return id.OCISID, ids
		},
		OwnerName: func(owner string) string { p, _ := f.reg.Get(owner); return p.DisplayName },
		OwnerDir:  f.st.OwnerDir,
		Logger:    f.logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Store: view, Registry: f.reg, Verifier: f.verifier, Now: f.clock.Now, Logger: f.logger}
	if graph != nil {
		cfg.Identities = provision.NewIdentities(f.reg, graph, f.logger, provision.WithClock(f.clock.Now))
	}
	dav, wk, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.dav, f.wk = dav, wk
	sf.shares = shares
	sf.cal, err = f.st.GetCollection(ctx, store.CollectionRef{Owner: aliceOwner, Kind: store.KindCalendar, Name: "personal"})
	if err != nil {
		t.Fatalf("alice's default calendar: %v", err)
	}
	return sf
}

func (sf *shareFixture) share(t *testing.T, a sharing.Access) {
	t.Helper()
	err := sf.shares.Grant(context.Background(), sf.cal.Ref, sf.cal.ID,
		sharing.Member{Type: sharing.User, ID: "u-bob", Name: "Bob", Access: a})
	if err != nil {
		t.Fatal(err)
	}
}

func (sf *shareFixture) sharedPath() string { return "/dav/calendars/bob/shared-" + sf.cal.ID + "/" }

const propfindPrivileges = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:current-user-privilege-set/></d:prop></d:propfind>`

func TestSharedCalendarAppearsInTheRecipientsHome(t *testing.T) {
	sf := newShareFixture(t)
	sf.share(t, sharing.Read)

	rec := sf.do(t, "PROPFIND", "/dav/calendars/bob/", propfindName, "Authorization", sf.bob, "Depth", "1")
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"/dav/calendars/bob/personal/", sf.sharedPath(), "Personal (alice)"} {
		if !strings.Contains(body, want) {
			t.Errorf("home listing lacks %q:\n%s", want, body)
		}
	}

	// A stranger's home has nothing shared.
	f := sf.fixture
	carol := basic("carol", f.user(t, "sub-carol", "carol"))
	rec = sf.do(t, "PROPFIND", "/dav/calendars/carol/", propfindName, "Authorization", carol, "Depth", "1")
	if strings.Contains(rec.Body.String(), "shared-") {
		t.Fatalf("carol sees a shared calendar:\n%s", rec.Body.String())
	}
	rec = sf.do(t, "PROPFIND", "/dav/calendars/carol/shared-"+sf.cal.ID+"/", propfindName, "Authorization", carol, "Depth", "0")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("carol on the shared path: status = %d", rec.Code)
	}
}

func TestReadOnlyShareRefusesWritesWithNeedPrivileges(t *testing.T) {
	sf := newShareFixture(t)
	sf.share(t, sharing.Read)

	rec := sf.do(t, "PROPFIND", sf.sharedPath(), propfindPrivileges, "Authorization", sf.bob, "Depth", "0")
	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "read") || strings.Contains(body, "write") {
		t.Fatalf("privileges of a read share:\n%s", body)
	}

	rec = sf.do(t, http.MethodPut, sf.sharedPath()+"x.ics", event, "Authorization", sf.bob, "Content-Type", "text/calendar")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "need-privileges") {
		t.Fatalf("PUT: status = %d, body %q", rec.Code, rec.Body.String())
	}
	if _, err := sf.st.GetObject(context.Background(), sf.cal.Ref, "x.ics"); err == nil {
		t.Fatal("the refused write reached the owner's calendar")
	}
	rec = sf.do(t, http.MethodDelete, sf.sharedPath(), "", "Authorization", sf.bob)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE of the collection: status = %d", rec.Code)
	}
}

func TestReadWriteShareWritesAreSeenByTheOwnersSync(t *testing.T) {
	sf := newShareFixture(t)
	sf.share(t, sharing.ReadWrite)

	rec := sf.do(t, http.MethodPut, sf.sharedPath()+"standup.ics", event, "Authorization", sf.bob, "Content-Type", "text/calendar")
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT: status = %d, body %q", rec.Code, rec.Body.String())
	}
	rec = sf.do(t, http.MethodGet, "/dav/calendars/alice/personal/standup.ics", "", "Authorization", sf.alice)
	if rec.Code != http.StatusOK || rec.Body.String() != event {
		t.Fatalf("owner GET: status = %d, body %q", rec.Code, rec.Body.String())
	}
	const syncReport = `<?xml version="1.0" encoding="utf-8"?>
<d:sync-collection xmlns:d="DAV:"><d:sync-token/><d:sync-level>1</d:sync-level><d:prop><d:getetag/></d:prop></d:sync-collection>`
	rec = sf.do(t, "REPORT", "/dav/calendars/alice/personal/", syncReport, "Authorization", sf.alice, "Content-Type", "application/xml")
	if rec.Code != http.StatusMultiStatus || !strings.Contains(rec.Body.String(), "standup.ics") {
		t.Fatalf("owner sync: status = %d, body %q", rec.Code, rec.Body.String())
	}
	// The recipient deletes it again; a read-write share does not let them
	// delete the calendar, though.
	if rec = sf.do(t, http.MethodDelete, sf.sharedPath()+"standup.ics", "", "Authorization", sf.bob); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE object: status = %d", rec.Code)
	}
	if rec = sf.do(t, http.MethodDelete, sf.sharedPath(), "", "Authorization", sf.bob); rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE collection: status = %d", rec.Code)
	}
}

func TestRecipientPropPatchIsKeptForTheRecipient(t *testing.T) {
	sf := newShareFixture(t)
	sf.share(t, sharing.Read)
	const patch = `<?xml version="1.0" encoding="utf-8"?>
<d:propertyupdate xmlns:d="DAV:" xmlns:a="http://apple.com/ns/ical/"><d:set><d:prop>
<a:calendar-color>#112233FF</a:calendar-color><d:displayname>Team</d:displayname></d:prop></d:set></d:propertyupdate>`
	rec := sf.do(t, "PROPPATCH", sf.sharedPath(), patch, "Authorization", sf.bob, "Content-Type", "application/xml")
	if rec.Code != http.StatusMultiStatus || !strings.Contains(rec.Body.String(), "200") {
		t.Fatalf("PROPPATCH: status = %d, body %q", rec.Code, rec.Body.String())
	}
	rec = sf.do(t, "PROPFIND", sf.sharedPath(), propfindName, "Authorization", sf.bob, "Depth", "0")
	if !strings.Contains(rec.Body.String(), "Team") {
		t.Fatalf("recipient name:\n%s", rec.Body.String())
	}
	rec = sf.do(t, "PROPFIND", "/dav/calendars/alice/personal/", propfindName, "Authorization", sf.alice, "Depth", "0")
	if strings.Contains(rec.Body.String(), "Team") {
		t.Fatalf("owner's name changed:\n%s", rec.Body.String())
	}
}

func TestRevokedShareDisappears(t *testing.T) {
	sf := newShareFixture(t)
	sf.share(t, sharing.ReadWrite)
	if err := sf.shares.Revoke(context.Background(), sf.cal.Ref, sharing.User, "u-bob"); err != nil {
		t.Fatal(err)
	}
	rec := sf.do(t, "PROPFIND", sf.sharedPath(), propfindName, "Authorization", sf.bob, "Depth", "0")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	rec = sf.do(t, http.MethodPut, sf.sharedPath()+"x.ics", event, "Authorization", sf.bob, "Content-Type", "text/calendar")
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusConflict {
		t.Fatalf("PUT after revoke: status = %d", rec.Code)
	}
}

// A principal whose data is kept under another owner than its sub shares and
// receives over DAV like any other.
func TestShareFromAPrincipalWhoseOwnerIsNotItsSub(t *testing.T) {
	sf := newShareFixtureFor(t, "migrated-alice")
	sf.share(t, sharing.ReadWrite)

	rec := sf.do(t, "PROPFIND", "/dav/calendars/alice/", propfindName, "Authorization", sf.alice, "Depth", "1")
	if rec.Code != http.StatusMultiStatus || !strings.Contains(rec.Body.String(), "/dav/calendars/alice/personal/") {
		t.Fatalf("alice's own home: status %d\n%s", rec.Code, rec.Body.String())
	}
	rec = sf.do(t, "PROPFIND", "/dav/calendars/bob/", propfindName, "Authorization", sf.bob, "Depth", "1")
	body := rec.Body.String()
	if rec.Code != http.StatusMultiStatus || !strings.Contains(body, sf.sharedPath()) || !strings.Contains(body, "Personal (alice)") {
		t.Fatalf("bob's home: status %d\n%s", rec.Code, body)
	}
	// Nothing was created under the sub.
	if cols, _ := sf.st.ListCollections(context.Background(), "sub-alice", store.KindCalendar); len(cols) != 0 {
		t.Fatalf("collections under the sub: %v", cols)
	}
}
