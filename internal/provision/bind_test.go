package provision

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

// bindFixture has an imported principal, dave with the oCIS id u-dave and no
// sub, besides a user who signed in on their own.
type bindFixture struct {
	reg   *principals.Registry
	st    *filestore.Store
	graph *fakeGraph
	ids   *Identities
}

func newBindFixture(t *testing.T, extra string) bindFixture {
	t.Helper()
	dir := t.TempDir()
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	write := func(owner, body string) {
		d := st.OwnerDir(owner)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "principal.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("migrated-dave", `{"owner":"migrated-dave","username":"dave","display_name":"Dave","ocis_id":"u-dave","defaults_created":true`+extra+`}`)
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, nil, principals.WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeGraph{user: ocisgraph.User{ID: "u-dave", Groups: []ocisgraph.Group{{ID: "g-1", Name: "users"}}}}
	return bindFixture{reg: reg, st: st, graph: g, ids: NewIdentities(reg, g, slog.New(slog.DiscardHandler))}
}

func (f bindFixture) signIn(sub, name string) (principals.Principal, int, string) {
	return f.ids.SignIn(context.Background(), auth.Claims{Subject: sub, PreferredUsername: name}, "tok", slog.New(slog.DiscardHandler))
}

func TestFirstSignInBindsAnImportedUser(t *testing.T) {
	f := newBindFixture(t, "")
	p, status, msg := f.signIn("sub-dave", "dave")
	if status != 0 {
		t.Fatalf("status %d %q", status, msg)
	}
	if p.Owner != "migrated-dave" || p.Sub != "sub-dave" {
		t.Fatalf("principal = %+v", p)
	}
	if n := len(f.reg.ListPrincipals(context.Background())); n != 1 {
		t.Fatalf("%d principals, want 1: no second principal for the sub", n)
	}
	if id, _ := f.reg.Identity("migrated-dave"); id.OCISID != "u-dave" || len(id.Groups) != 1 || id.At.IsZero() {
		t.Fatalf("identity = %+v", id)
	}
	if n := f.graph.count(); n != 1 {
		t.Fatalf("%d lookups, want 1", n)
	}

	// The next sign-in finds the binding and needs no lookup.
	if p, status, _ := f.signIn("sub-dave", "dave"); status != 0 || p.Owner != "migrated-dave" {
		t.Fatalf("second sign-in: %+v, %d", p, status)
	}
	if n := f.graph.count(); n != 1 {
		t.Fatalf("%d lookups after the second sign-in, want 1", n)
	}
}

func TestSignInOfAnotherUserDoesNotBind(t *testing.T) {
	f := newBindFixture(t, "")
	f.graph.user = ocisgraph.User{ID: "u-eve"}
	p, status, _ := f.signIn("sub-eve", "eve")
	if status != 0 || p.Owner != "sub-eve" {
		t.Fatalf("principal = %+v, status %d", p, status)
	}
	if d, _ := f.reg.Get("migrated-dave"); d.Sub != "" {
		t.Fatalf("dave was bound to %q", d.Sub)
	}
}

func TestBoundUserIsNotTakenOverBySomeoneWithTheSameID(t *testing.T) {
	f := newBindFixture(t, "")
	if _, status, _ := f.signIn("sub-dave", "dave"); status != 0 {
		t.Fatal(status)
	}
	// Another subject whose lookup returns the same id.
	p, status, _ := f.signIn("sub-mallory", "mallory")
	if status != 0 || p.Owner != "sub-mallory" {
		t.Fatalf("principal = %+v, status %d", p, status)
	}
	if d, _ := f.reg.Get("migrated-dave"); d.Sub != "sub-dave" {
		t.Fatalf("dave's sub = %q", d.Sub)
	}
}

func TestDisabledImportedUserIsRefused(t *testing.T) {
	f := newBindFixture(t, `,"disabled":true`)
	_, status, msg := f.signIn("sub-dave", "dave")
	if status != http.StatusForbidden || msg != "account disabled" {
		t.Fatalf("status %d %q", status, msg)
	}
	if d, _ := f.reg.Get("migrated-dave"); d.Sub != "" {
		t.Fatalf("a disabled user was bound: %+v", d)
	}
	if _, err := f.reg.BySub("sub-dave"); !errors.Is(err, principals.ErrNotFound) {
		t.Fatalf("a principal exists for the sub: %v", err)
	}
}

func TestFailedLookupDoesNotCreateADuplicate(t *testing.T) {
	f := newBindFixture(t, "")
	f.graph.err = errors.New("boom")
	_, status, _ := f.signIn("sub-dave", "dave")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}
	if n := len(f.reg.ListPrincipals(context.Background())); n != 1 {
		t.Fatalf("%d principals, want 1", n)
	}
	// Once oCIS answers, the user is bound.
	f.graph.err = nil
	if p, status, _ := f.signIn("sub-dave", "dave"); status != 0 || p.Owner != "migrated-dave" {
		t.Fatalf("retry: %+v, %d", p, status)
	}
}

func TestWithoutALookerSignInJustProvisions(t *testing.T) {
	f := newBindFixture(t, "")
	ids := NewIdentities(f.reg, nil, nil)
	p, status, _ := ids.SignIn(context.Background(), auth.Claims{Subject: "sub-dave", PreferredUsername: "dave"}, "tok", slog.New(slog.DiscardHandler))
	if status != 0 || p.Sub != "sub-dave" {
		t.Fatalf("principal = %+v, status %d", p, status)
	}
}
