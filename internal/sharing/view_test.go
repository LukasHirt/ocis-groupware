package sharing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/memory"
)

const (
	alice = "sub-alice" // owner
	bob   = "sub-bob"   // user grant
	carol = "sub-carol" // group grant
	eve   = "sub-eve"   // no grant
)

type viewFixture struct {
	*fixture
	inner  *memory.Store
	shares *Shares
	view   *View
	ids    map[string]ident
	cal    store.Collection
}

type ident struct {
	ocis   string
	groups []string
}

func newViewFixture(t *testing.T) *viewFixture {
	t.Helper()
	f := newFixture(t, alice, bob, carol, eve)
	vf := &viewFixture{fixture: f, inner: memory.New(), ids: map[string]ident{
		alice: {"u-alice", nil},
		bob:   {"u-bob", nil},
		carol: {"u-carol", []string{"g-phys"}},
		eve:   {"u-eve", []string{"g-other"}},
	}}
	for sub, id := range vf.ids {
		f.ocis[sub] = id.ocis
	}
	vf.shares = f.open(t)
	vf.view = vf.newView(t)
	ctx := context.Background()
	c, err := vf.inner.CreateCollection(ctx, store.CollectionRef{Owner: alice, Kind: store.KindCalendar, Name: "Personal"},
		store.Props{DisplayName: "Work", Color: "#ff0000ff", Order: 3})
	if err != nil {
		t.Fatal(err)
	}
	vf.cal = c
	if _, err := vf.inner.PutObject(ctx, c.Ref, "one.ics", []byte("data-one"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	return vf
}

func (vf *viewFixture) newView(t *testing.T) *View {
	t.Helper()
	v, err := NewView(ViewConfig{
		Inner:  vf.inner,
		Shares: vf.shares,
		Identity: func(sub string) (string, []string) {
			id := vf.ids[sub]
			return id.ocis, id.groups
		},
		OwnerName: func(sub string) string { return map[string]string{alice: "Alice A"}[sub] },
		OwnerDir:  vf.ownerDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (vf *viewFixture) grant(t *testing.T, m Member) {
	t.Helper()
	if err := vf.shares.Grant(context.Background(), vf.cal.Ref, vf.cal.ID, m); err != nil {
		t.Fatal(err)
	}
}

func (vf *viewFixture) sref(sub string) store.CollectionRef {
	return store.CollectionRef{Owner: sub, Kind: store.KindCalendar, Name: "shared-" + vf.cal.ID}
}

func TestOwnerSeesOnlyOwnCollectionsWithFullAccess(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", Read))
	ctx := context.Background()
	cols, err := vf.view.ListCollections(ctx, alice, store.KindCalendar)
	if err != nil || len(cols) != 1 || cols[0].Ref.Name != "Personal" || cols[0].Access != store.AccessFull {
		t.Fatalf("owner list = %+v, %v", cols, err)
	}
	c, err := vf.view.GetCollection(ctx, vf.cal.Ref)
	if err != nil || c.Access != store.AccessFull || c.Props.DisplayName != "Work" {
		t.Fatalf("owner get = %+v, %v", c, err)
	}
}

// access describes what a caller may do with the shared collection.
type outcome struct {
	read, writeObjects, props, remove bool
}

func TestRolesAgainstEveryStoreMethod(t *testing.T) {
	ctx := context.Background()

	// bob: read via user grant. carol: readwrite via group. eve: nothing.
	tests := []struct {
		name   string
		sub    string
		access store.Access
		want   outcome
	}{
		{"owner", alice, store.AccessFull, outcome{true, true, true, true}},
		{"recipient read", bob, store.AccessRead, outcome{true, false, false, false}},
		{"recipient readwrite by group", carol, store.AccessObjects, outcome{true, true, false, false}},
		{"stranger", eve, 0, outcome{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each role gets its own copy of the data, so deletes do not leak.
			vf := newViewFixture(t)
			vf.grant(t, user("u-bob", Read))
			vf.grant(t, group("g-phys", ReadWrite))
			ref := vf.sref(tt.sub)
			if tt.sub == alice {
				ref = vf.cal.Ref
			}
			v := vf.view

			c, err := v.GetCollection(ctx, ref)
			if tt.want.read {
				if err != nil || c.Access != tt.access || c.Ref != ref {
					t.Fatalf("GetCollection = %+v, %v", c, err)
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("GetCollection = %v, want ErrNotFound", err)
			}

			cols, err := v.ListCollections(ctx, tt.sub, store.KindCalendar)
			if err != nil {
				t.Fatal(err)
			}
			if listed := slices.ContainsFunc(cols, func(c store.Collection) bool { return c.Ref == ref }); listed != tt.want.read {
				t.Fatalf("listed = %v, want %v (%+v)", listed, tt.want.read, cols)
			}
			// The address book kind never lists a calendar.
			books, _ := v.ListCollections(ctx, tt.sub, store.KindAddressBook)
			if len(books) != 0 {
				t.Fatalf("address books = %+v", books)
			}

			o, err := v.GetObject(ctx, ref, "one.ics")
			if tt.want.read && (err != nil || string(o.Data) != "data-one") {
				t.Fatalf("GetObject = %+v, %v", o, err)
			} else if !tt.want.read && !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("GetObject = %v", err)
			}
			objs, err := v.ListObjects(ctx, ref, store.ListOptions{IncludeData: true})
			if tt.want.read && (err != nil || len(objs) != 1) {
				t.Fatalf("ListObjects = %+v, %v", objs, err)
			} else if !tt.want.read && !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("ListObjects = %v", err)
			}
			ch, err := v.Changes(ctx, ref, 0)
			if tt.want.read && (err != nil || len(ch.Changes) != 1) {
				t.Fatalf("Changes = %+v, %v", ch, err)
			} else if !tt.want.read && !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("Changes = %v", err)
			}

			_, err = v.PutObject(ctx, ref, "two.ics", []byte("data-two"), store.PutOptions{})
			checkWrite(t, "PutObject", err, tt.want.writeObjects, tt.want.read)
			err = v.DeleteObject(ctx, ref, "one.ics", store.DeleteOptions{})
			checkWrite(t, "DeleteObject", err, tt.want.writeObjects, tt.want.read)

			desc := "text"
			_, err = v.UpdateCollection(ctx, ref, store.PropsUpdate{Description: &desc})
			checkWrite(t, "UpdateCollection description", err, tt.want.props, tt.want.read)

			err = v.DeleteCollection(ctx, ref)
			checkWrite(t, "DeleteCollection", err, tt.want.remove, tt.want.read)

			// A caller may create collections of its own, never one that is shared.
			_, err = v.CreateCollection(ctx, store.CollectionRef{Owner: tt.sub, Kind: store.KindCalendar, Name: "Mine"}, store.Props{})
			if err != nil {
				t.Fatalf("CreateCollection of an own collection: %v", err)
			}
		})
	}
}

// checkWrite checks the error of a write: nil when allowed, ErrPermission
// when the caller can see the collection but may not write, ErrNotFound when
// the caller cannot see it at all.
func checkWrite(t *testing.T, what string, err error, allowed, visible bool) {
	t.Helper()
	switch {
	case allowed && err != nil:
		t.Fatalf("%s: %v", what, err)
	case !allowed && visible && !errors.Is(err, store.ErrPermission):
		t.Fatalf("%s = %v, want ErrPermission", what, err)
	case !allowed && !visible && !errors.Is(err, store.ErrNotFound):
		t.Fatalf("%s = %v, want ErrNotFound", what, err)
	}
}

func TestSharedCollectionPresentation(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", ReadWrite))
	ctx := context.Background()

	cols, err := vf.view.ListCollections(ctx, bob, store.KindCalendar)
	if err != nil || len(cols) != 1 {
		t.Fatalf("list = %+v, %v", cols, err)
	}
	c := cols[0]
	if c.Ref != vf.sref(bob) || c.ID != vf.cal.ID {
		t.Fatalf("ref and id = %+v %q", c.Ref, c.ID)
	}
	if c.Props.DisplayName != "Work (Alice A)" || c.Props.Color != "#ff0000ff" || c.Props.Order != 3 {
		t.Fatalf("props = %+v", c.Props)
	}
	// Sorted by name together with the recipient's own collections.
	if _, err := vf.view.CreateCollection(ctx, store.CollectionRef{Owner: bob, Kind: store.KindCalendar, Name: "Personal"}, store.Props{}); err != nil {
		t.Fatal(err)
	}
	cols, _ = vf.view.ListCollections(ctx, bob, store.KindCalendar)
	if len(cols) != 2 || cols[0].Ref.Name != "Personal" || cols[1].Ref.Name != "shared-"+vf.cal.ID {
		t.Fatalf("merged list = %+v", cols)
	}
}

func TestOwnerNameMissingLeavesTheNameAlone(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", Read))
	v, err := NewView(ViewConfig{Inner: vf.inner, Shares: vf.shares, OwnerDir: vf.ownerDir,
		Identity: func(sub string) (string, []string) { return vf.ids[sub].ocis, nil }})
	if err != nil {
		t.Fatal(err)
	}
	c, err := v.GetCollection(context.Background(), vf.sref(bob))
	if err != nil || c.Props.DisplayName != "Work" {
		t.Fatalf("get = %+v, %v", c, err)
	}
}

func TestRecipientOverrides(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", Read))
	ctx := context.Background()
	ref := vf.sref(bob)

	before, _ := vf.inner.GetCollection(ctx, vf.cal.Ref)
	name, color, order := "Team", "#00ff00ff", 9
	c, err := vf.view.UpdateCollection(ctx, ref, store.PropsUpdate{DisplayName: &name, Color: &color, Order: &order})
	if err != nil {
		t.Fatal(err)
	}
	if c.Props.DisplayName != "Team" || c.Props.Color != "#00ff00ff" || c.Props.Order != 9 || c.Access != store.AccessRead {
		t.Fatalf("updated = %+v", c)
	}
	// The owner's collection is untouched.
	own, _ := vf.inner.GetCollection(ctx, vf.cal.Ref)
	if own.Props.DisplayName != "Work" || own.Props.Color != "#ff0000ff" || own.Revision != before.Revision {
		t.Fatalf("owner's collection changed: %+v", own)
	}
	// Another recipient is unaffected.
	vf.grant(t, user("u-eve", Read))
	other, _ := vf.view.GetCollection(ctx, vf.sref(eve))
	if other.Props.DisplayName != "Work (Alice A)" {
		t.Fatalf("other recipient = %+v", other.Props)
	}

	// The overrides survive a restart.
	vf.view = vf.newView(t)
	c, _ = vf.view.GetCollection(ctx, ref)
	if c.Props.DisplayName != "Team" {
		t.Fatalf("after reload = %+v", c.Props)
	}

	// An empty value removes the override.
	empty := ""
	c, _ = vf.view.UpdateCollection(ctx, ref, store.PropsUpdate{DisplayName: &empty})
	if c.Props.DisplayName != "Work (Alice A)" || c.Props.Color != "#00ff00ff" {
		t.Fatalf("after clearing the name = %+v", c.Props)
	}

	// The file holds only what the recipient changed, below the recipient.
	if _, err := os.Stat(filepath.Join(vf.ownerDir(bob), OverridesFileName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vf.ownerDir(alice), OverridesFileName)); err == nil {
		t.Fatal("the owner got an overrides file")
	}
}

// A file written by a release with another layout is not read, and not
// rewritten by reading it: the recipient sees the owner's names until a
// change of their own replaces the file.
func TestOverridesFileOfAnotherVersionIsNotRead(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", Read))
	ctx := context.Background()
	ref := vf.sref(bob)

	path := filepath.Join(vf.ownerDir(bob), OverridesFileName)
	newer := `{"version":2,"collections":{"` + vf.cal.ID + `":{"display_name":"Team"}}}`
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}

	view := vf.newView(t)
	c, err := view.GetCollection(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if c.Props.DisplayName != "Work (Alice A)" {
		t.Fatalf("display name = %q, the override of version 2 was read", c.Props.DisplayName)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != newer {
		t.Fatalf("the file was changed by reading it: %q, %v", got, err)
	}
}

func TestRecipientMayNotChangeOtherProperties(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", ReadWrite))
	desc, tz, comps := "d", "tz", []string{"VEVENT"}
	for _, u := range []store.PropsUpdate{{Description: &desc}, {Timezone: &tz}, {Components: &comps}} {
		if _, err := vf.view.UpdateCollection(context.Background(), vf.sref(bob), u); !errors.Is(err, store.ErrPermission) {
			t.Fatalf("UpdateCollection(%+v) = %v", u, err)
		}
	}
}

func TestHiddenCollectionIsNotListedButStaysReachable(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", Read))
	ctx := context.Background()
	if err := vf.view.SetHidden(ctx, bob, vf.cal.ID, true); err != nil {
		t.Fatal(err)
	}
	cols, _ := vf.view.ListCollections(ctx, bob, store.KindCalendar)
	if len(cols) != 0 {
		t.Fatalf("list = %+v", cols)
	}
	if _, err := vf.view.GetCollection(ctx, vf.sref(bob)); err != nil {
		t.Fatalf("hidden collection is gone: %v", err)
	}
	if err := vf.view.SetHidden(ctx, bob, vf.cal.ID, false); err != nil {
		t.Fatal(err)
	}
	if cols, _ = vf.view.ListCollections(ctx, bob, store.KindCalendar); len(cols) != 1 {
		t.Fatalf("list after unhiding = %+v", cols)
	}
}

func TestRevokeIsImmediate(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", ReadWrite))
	ctx := context.Background()
	if _, err := vf.view.PutObject(ctx, vf.sref(bob), "x.ics", []byte("x"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := vf.shares.Revoke(ctx, vf.cal.Ref, User, "u-bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := vf.view.GetCollection(ctx, vf.sref(bob)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after revoke = %v", err)
	}
	if _, err := vf.view.PutObject(ctx, vf.sref(bob), "y.ics", []byte("y"), store.PutOptions{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("put after revoke = %v", err)
	}
}

func TestGroupSnapshotChangeIsImmediate(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, group("g-phys", Read))
	ctx := context.Background()
	ref := vf.sref(carol)
	if _, err := vf.view.GetCollection(ctx, ref); err != nil {
		t.Fatal(err)
	}
	vf.ids[carol] = ident{"u-carol", nil} // the user left the group
	if _, err := vf.view.GetCollection(ctx, ref); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after leaving the group = %v", err)
	}
}

func TestUserAndGroupGrantTakeTheHigherAccess(t *testing.T) {
	vf := newViewFixture(t)
	vf.ids[bob] = ident{"u-bob", []string{"g-phys"}}
	vf.grant(t, user("u-bob", Read))
	vf.grant(t, group("g-phys", ReadWrite))
	c, err := vf.view.GetCollection(context.Background(), vf.sref(bob))
	if err != nil || c.Access != store.AccessObjects {
		t.Fatalf("get = %+v, %v", c, err)
	}
}

func TestOwnerInGrantedGroupDoesNotSeeItTwice(t *testing.T) {
	vf := newViewFixture(t)
	vf.ids[alice] = ident{"u-alice", []string{"g-phys"}}
	vf.grant(t, group("g-phys", Read))
	cols, _ := vf.view.ListCollections(context.Background(), alice, store.KindCalendar)
	if len(cols) != 1 || cols[0].Ref.Name != "Personal" {
		t.Fatalf("list = %+v", cols)
	}
}

func TestSyncThroughTheView(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", ReadWrite))
	ctx := context.Background()
	ref := vf.sref(bob)

	first, err := vf.view.Changes(ctx, ref, 0)
	if err != nil || len(first.Changes) != 1 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if _, err := vf.view.PutObject(ctx, ref, "two.ics", []byte("two"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	// The recipient's token continues on the owner's sync, and the other way round.
	next, err := vf.view.Changes(ctx, ref, first.Revision)
	if err != nil || len(next.Changes) != 1 || next.Changes[0].Name != "two.ics" {
		t.Fatalf("recipient sync = %+v, %v", next, err)
	}
	if _, err := vf.inner.PutObject(ctx, vf.cal.Ref, "three.ics", []byte("three"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	owner, err := vf.view.Changes(ctx, vf.cal.Ref, next.Revision)
	if err != nil || len(owner.Changes) != 1 || owner.Changes[0].Name != "three.ics" {
		t.Fatalf("owner sync = %+v, %v", owner, err)
	}
	// What the recipient wrote is in the owner's collection.
	got, err := vf.view.GetObject(ctx, vf.cal.Ref, "two.ics")
	if err != nil || string(got.Data) != "two" {
		t.Fatalf("owner reads %+v, %v", got, err)
	}
}

func TestOwnerDeletingTheCollectionEndsTheShare(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", Read))
	ctx := context.Background()
	if err := vf.view.DeleteCollection(ctx, vf.cal.Ref); err != nil {
		t.Fatal(err)
	}
	if cols, _ := vf.view.ListCollections(ctx, bob, store.KindCalendar); len(cols) != 0 {
		t.Fatalf("recipient list = %+v", cols)
	}
	if _, err := vf.view.GetCollection(ctx, vf.sref(bob)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get = %v", err)
	}
	if got := vf.shares.Members(vf.cal.Ref); len(got) != 0 {
		t.Fatalf("members = %v", got)
	}
}

func TestRecreatedCollectionIsNotShared(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", Read))
	ctx := context.Background()
	// Deleted behind the view's back, as a purge or a crash would.
	if err := vf.inner.DeleteCollection(ctx, vf.cal.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := vf.inner.CreateCollection(ctx, vf.cal.Ref, store.Props{DisplayName: "New"}); err != nil {
		t.Fatal(err)
	}
	if cols, _ := vf.view.ListCollections(ctx, bob, store.KindCalendar); len(cols) != 0 {
		t.Fatalf("recipient sees the newcomer: %+v", cols)
	}
	if _, err := vf.view.GetCollection(ctx, vf.sref(bob)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get = %v", err)
	}
}

func TestSharedNamesCannotBeCreated(t *testing.T) {
	vf := newViewFixture(t)
	vf.grant(t, user("u-bob", Read))
	_, err := vf.view.CreateCollection(context.Background(), vf.sref(bob), store.Props{})
	if !errors.Is(err, store.ErrExists) {
		t.Fatalf("CreateCollection = %v", err)
	}
}

func TestNewViewRequiresItsParts(t *testing.T) {
	vf := newViewFixture(t)
	for name, cfg := range map[string]ViewConfig{
		"inner":    {Shares: vf.shares, Identity: func(string) (string, []string) { return "", nil }, OwnerDir: vf.ownerDir},
		"shares":   {Inner: vf.inner, Identity: func(string) (string, []string) { return "", nil }, OwnerDir: vf.ownerDir},
		"identity": {Inner: vf.inner, Shares: vf.shares, OwnerDir: vf.ownerDir},
		"dir":      {Inner: vf.inner, Shares: vf.shares, Identity: func(string) (string, []string) { return "", nil }},
	} {
		if _, err := NewView(cfg); err == nil {
			t.Errorf("NewView without %s succeeded", name)
		}
	}
}
