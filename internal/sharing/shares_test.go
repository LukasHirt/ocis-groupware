package sharing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/memory"
)

type fixture struct {
	dir string
	// ocis maps an owner to the oCIS user id of that owner.
	ocis map[string]string
}

func newFixture(t *testing.T, owners ...string) *fixture {
	t.Helper()
	f := &fixture{dir: t.TempDir(), ocis: map[string]string{}}
	for _, o := range owners {
		if err := os.MkdirAll(f.ownerDir(o), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *fixture) ownerDir(owner string) string { return filepath.Join(f.dir, "owners", owner) }

func (f *fixture) owners() []string {
	entries, _ := os.ReadDir(filepath.Join(f.dir, "owners"))
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func (f *fixture) open(t *testing.T) *Shares {
	t.Helper()
	s, err := Open(Config{
		OwnerDir: f.ownerDir,
		Owners:   f.owners,
		OCISID:   func(owner string) string { return f.ocis[owner] },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var (
	cal  = store.CollectionRef{Owner: "alice", Kind: store.KindCalendar, Name: "Personal"}
	book = store.CollectionRef{Owner: "alice", Kind: store.KindAddressBook, Name: "Contacts"}
)

func user(id string, a Access) Member {
	return Member{Type: User, ID: id, Name: "Name " + id, Access: a}
}
func group(id string, a Access) Member {
	return Member{Type: Group, ID: id, Name: "Group " + id, Access: a}
}

func TestGrantAndMembers(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	ctx := context.Background()

	if got := s.Members(cal); len(got) != 0 {
		t.Fatalf("members of an unshared collection = %v", got)
	}
	if err := s.Grant(ctx, cal, "c1", user("u-bob", Read)); err != nil {
		t.Fatal(err)
	}
	if err := s.Grant(ctx, cal, "c1", group("g-phys", ReadWrite)); err != nil {
		t.Fatal(err)
	}
	// Granting again changes the access and the name of the same grantee.
	if err := s.Grant(ctx, cal, "c1", Member{Type: User, ID: "u-bob", Name: "Bob B", Access: ReadWrite}); err != nil {
		t.Fatal(err)
	}
	got := s.Members(cal)
	want := []Member{
		{Type: User, ID: "u-bob", Name: "Bob B", Access: ReadWrite},
		{Type: Group, ID: "g-phys", Name: "Group g-phys", Access: ReadWrite},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("members = %v, want %v", got, want)
	}
	if s.Members(book) != nil && len(s.Members(book)) != 0 {
		t.Fatal("the address book of the same name must not share")
	}
}

func TestRevoke(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	ctx := context.Background()
	_ = s.Grant(ctx, cal, "c1", user("u-bob", Read))
	_ = s.Grant(ctx, cal, "c1", user("u-eve", Read))

	if err := s.Revoke(ctx, cal, User, "u-bob"); err != nil {
		t.Fatal(err)
	}
	if got := s.Members(cal); len(got) != 1 || got[0].ID != "u-eve" {
		t.Fatalf("members = %v", got)
	}
	if got := s.Received("u-bob", nil); len(got) != 0 {
		t.Fatalf("revoked grantee still receives %v", got)
	}
	if err := s.Revoke(ctx, cal, User, "u-nobody"); !errors.Is(err, ErrNotShared) {
		t.Fatalf("revoking a stranger = %v, want ErrNotShared", err)
	}
	// Revoking the last member removes the file.
	if err := s.Revoke(ctx, cal, User, "u-eve"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.ownerDir("alice"), FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shares file after the last revoke: %v", err)
	}
}

func TestSetReplacesMembers(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	ctx := context.Background()
	_ = s.Grant(ctx, cal, "c1", user("u-old", Read))
	if err := s.Set(ctx, cal, "c1", []Member{user("u-a", Read), group("g-b", ReadWrite)}); err != nil {
		t.Fatal(err)
	}
	if got := s.Members(cal); len(got) != 2 {
		t.Fatalf("members = %v", got)
	}
	if got := s.Received("u-old", nil); len(got) != 0 {
		t.Fatalf("replaced grantee still receives %v", got)
	}
	if err := s.Set(ctx, cal, "c1", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Members(cal); len(got) != 0 {
		t.Fatalf("members after clearing = %v", got)
	}
}

func TestValidation(t *testing.T) {
	f := newFixture(t, "alice")
	f.ocis["alice"] = "u-alice"
	s := f.open(t)
	ctx := context.Background()

	tests := []struct {
		name string
		m    Member
		want error
	}{
		{"self", user("u-alice", Read), ErrSelfShare},
		{"bad access", Member{Type: User, ID: "u-x", Access: "write"}, ErrInvalidMember},
		{"empty access", Member{Type: User, ID: "u-x"}, ErrInvalidMember},
		{"bad type", Member{Type: "team", ID: "u-x", Access: Read}, ErrInvalidMember},
		{"empty id", Member{Type: User, Access: Read}, ErrInvalidMember},
		{"long id", Member{Type: User, ID: strings.Repeat("a", 300), Access: Read}, ErrInvalidMember},
		{"long name", Member{Type: User, ID: "u-x", Name: strings.Repeat("a", 300), Access: Read}, ErrInvalidMember},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.Grant(ctx, cal, "c1", tt.m); !errors.Is(err, tt.want) {
				t.Fatalf("Grant = %v, want %v", err, tt.want)
			}
		})
	}
	// A group with the id of the owner's user is not a self-share.
	if err := s.Grant(ctx, cal, "c1", group("u-alice", Read)); err != nil {
		t.Fatalf("group named like the owner: %v", err)
	}
	if err := s.Grant(ctx, cal, "", user("u-x", Read)); !errors.Is(err, ErrInvalidMember) {
		t.Fatal("an empty collection id must be refused")
	}
}

func TestMemberLimit(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	ctx := context.Background()
	var members []Member
	for i := 0; i < MaxMembers; i++ {
		members = append(members, user("u-"+itoa(i), Read))
	}
	if err := s.Set(ctx, cal, "c1", members); err != nil {
		t.Fatal(err)
	}
	if err := s.Grant(ctx, cal, "c1", user("u-extra", Read)); !errors.Is(err, ErrTooManyMembers) {
		t.Fatalf("Grant over the limit = %v", err)
	}
	// Changing an existing member is not growth.
	if err := s.Grant(ctx, cal, "c1", user("u-0", ReadWrite)); err != nil {
		t.Fatalf("changing a member at the limit: %v", err)
	}
	if err := s.Set(ctx, cal, "c1", append(members, user("u-extra", Read))); !errors.Is(err, ErrTooManyMembers) {
		t.Fatalf("Set over the limit = %v", err)
	}
	if got := s.Members(cal); len(got) != MaxMembers {
		t.Fatalf("a refused Set changed the members: %d", len(got))
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{digits[i%10]}, b...)
	}
	return string(b)
}

func TestDuplicateMembersInSetAreRefused(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	err := s.Set(context.Background(), cal, "c1", []Member{user("u-a", Read), user("u-a", ReadWrite)})
	if !errors.Is(err, ErrInvalidMember) {
		t.Fatalf("Set with a duplicate = %v", err)
	}
}

func TestReceivedByUserAndGroup(t *testing.T) {
	f := newFixture(t, "alice", "carol")
	s := f.open(t)
	ctx := context.Background()
	_ = s.Grant(ctx, cal, "c1", user("u-bob", Read))
	_ = s.Grant(ctx, cal, "c1", group("g-phys", ReadWrite))
	_ = s.Grant(ctx, book, "b1", group("g-phys", Read))
	other := store.CollectionRef{Owner: "carol", Kind: store.KindCalendar, Name: "Team"}
	_ = s.Grant(ctx, other, "c9", group("g-phys", Read))

	got := s.Received("u-bob", []string{"g-phys"})
	if len(got) != 3 {
		t.Fatalf("received = %+v", got)
	}
	byID := map[string]Received{}
	for _, r := range got {
		byID[r.CollectionID] = r
	}
	// The user grant says read, the group grant readwrite: the higher wins.
	if r := byID["c1"]; r.Access != ReadWrite || r.Owner != "alice" || r.Ref() != cal {
		t.Fatalf("c1 = %+v", r)
	}
	if r := byID["b1"]; r.Access != Read || r.Ref() != book {
		t.Fatalf("b1 = %+v", r)
	}
	if got := s.Received("u-bob", nil); len(got) != 1 {
		t.Fatalf("without the group snapshot = %+v", got)
	}
	if got := s.Received("u-stranger", []string{"g-other"}); len(got) != 0 {
		t.Fatalf("stranger = %+v", got)
	}
	if got := s.Received("", []string{""}); len(got) != 0 {
		t.Fatalf("empty ids = %+v", got)
	}
}

func TestReloadBuildsTheIndex(t *testing.T) {
	f := newFixture(t, "alice", "carol")
	s := f.open(t)
	ctx := context.Background()
	_ = s.Grant(ctx, cal, "c1", user("u-bob", ReadWrite))
	_ = s.Grant(ctx, cal, "c1", group("g-phys", Read))
	other := store.CollectionRef{Owner: "carol", Kind: store.KindCalendar, Name: "Team"}
	_ = s.Grant(ctx, other, "c9", user("u-bob", Read))

	again := f.open(t)
	if got := again.Members(cal); len(got) != 2 {
		t.Fatalf("members after reload = %v", got)
	}
	got := again.Received("u-bob", []string{"g-phys"})
	if len(got) != 2 {
		t.Fatalf("received after reload = %+v", got)
	}
	if again.Count("alice") != 2 || again.Count("carol") != 1 || again.Count("nobody") != 0 {
		t.Fatalf("counts = %d %d", again.Count("alice"), again.Count("carol"))
	}
}

func TestOpenRemovesTempFilesAndSkipsDamagedFiles(t *testing.T) {
	f := newFixture(t, "alice", "carol", "dave")
	s := f.open(t)
	_ = s.Grant(context.Background(), cal, "c1", user("u-bob", Read))

	tmp := filepath.Join(f.ownerDir("alice"), "."+FileName+".tmp-123")
	if err := os.WriteFile(tmp, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.ownerDir("carol"), FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A file of the wrong version is not read either.
	if err := os.WriteFile(filepath.Join(f.ownerDir("dave"), FileName), []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}

	again := f.open(t)
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file left behind: %v", err)
	}
	if got := again.Received("u-bob", nil); len(got) != 1 {
		t.Fatalf("healthy owner lost its shares: %+v", got)
	}
	if again.Count("carol") != 0 || again.Count("dave") != 0 {
		t.Fatal("a damaged file must not grant anything")
	}
}

func TestWriteLeavesNoTempFile(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	_ = s.Grant(context.Background(), cal, "c1", user("u-bob", Read))
	entries, _ := os.ReadDir(f.ownerDir("alice"))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover %s", e.Name())
		}
	}
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	ctx := context.Background()
	_ = s.Grant(ctx, cal, "c1", user("u-bob", Read))
	if err := os.Chmod(f.ownerDir("alice"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.ownerDir("alice"), 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	if err := s.Grant(ctx, cal, "c1", user("u-eve", Read)); err == nil {
		t.Fatal("expected a write error")
	}
	if got := s.Members(cal); len(got) != 1 {
		t.Fatalf("members after a failed write = %v", got)
	}
	if got := s.Received("u-eve", nil); len(got) != 0 {
		t.Fatalf("index after a failed write = %v", got)
	}
}

func TestRemoveCollectionAndOwner(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	ctx := context.Background()
	_ = s.Grant(ctx, cal, "c1", user("u-bob", Read))
	_ = s.Grant(ctx, book, "b1", user("u-bob", Read))

	if err := s.RemoveCollection(ctx, cal); err != nil {
		t.Fatal(err)
	}
	if got := s.Received("u-bob", nil); len(got) != 1 || got[0].CollectionID != "b1" {
		t.Fatalf("received = %+v", got)
	}
	// A collection that was not shared is fine.
	if err := s.RemoveCollection(ctx, cal); err != nil {
		t.Fatal(err)
	}
	n, err := s.RemoveOwner(ctx, "alice")
	if err != nil || n != 1 {
		t.Fatalf("RemoveOwner = %d, %v", n, err)
	}
	if got := s.Received("u-bob", nil); len(got) != 0 {
		t.Fatalf("received after RemoveOwner = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(f.ownerDir("alice"), FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shares file after RemoveOwner: %v", err)
	}
}

func TestConcurrentGrants(t *testing.T) {
	f := newFixture(t, "alice")
	s := f.open(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Grant(context.Background(), cal, "c1", user("u-"+itoa(i), Read))
			_ = s.Received("u-1", nil)
		}()
	}
	wg.Wait()
	if got := s.Members(cal); len(got) != 20 {
		t.Fatalf("members = %d", len(got))
	}
	if got := f.open(t).Members(cal); len(got) != 20 {
		t.Fatalf("members after reload = %d", len(got))
	}
}

func TestOwnersWithoutAPrincipalAreNotLoaded(t *testing.T) {
	f := newFixture(t, "alice", "ghost")
	s := f.open(t)
	ctx := context.Background()
	_ = s.Grant(ctx, cal, "c1", user("u-bob", Read))
	ghost := store.CollectionRef{Owner: "ghost", Kind: store.KindCalendar, Name: "Old"}
	_ = s.Grant(ctx, ghost, "c2", user("u-bob", Read))

	// The registry no longer lists ghost, say after an interrupted purge.
	again, err := Open(Config{OwnerDir: f.ownerDir, Owners: func() []string { return []string{"alice"} }})
	if err != nil {
		t.Fatal(err)
	}
	got := again.Received("u-bob", nil)
	if len(got) != 1 || got[0].Owner != "alice" {
		t.Fatalf("received = %+v, want only alice's", got)
	}
}

func TestPruneDropsSharesOfCollectionsThatAreGone(t *testing.T) {
	f := newFixture(t, "alice")
	inner := memory.New()
	ctx := context.Background()
	keep, err := inner.CreateCollection(ctx, cal, store.Props{})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := inner.CreateCollection(ctx, book, store.Props{})
	if err != nil {
		t.Fatal(err)
	}
	other := store.CollectionRef{Owner: "alice", Kind: store.KindCalendar, Name: "Replaced"}
	old, err := inner.CreateCollection(ctx, other, store.Props{})
	if err != nil {
		t.Fatal(err)
	}
	s := f.open(t)
	_ = s.Grant(ctx, keep.Ref, keep.ID, user("u-bob", Read))
	_ = s.Grant(ctx, gone.Ref, gone.ID, user("u-bob", Read))
	_ = s.Grant(ctx, old.Ref, old.ID, group("g-x", Read))
	_ = inner.DeleteCollection(ctx, gone.Ref)
	_ = inner.DeleteCollection(ctx, old.Ref)
	if _, err := inner.CreateCollection(ctx, other, store.Props{}); err != nil {
		t.Fatal(err)
	}

	n, err := s.Prune(ctx, inner)
	if err != nil || n != 2 {
		t.Fatalf("Prune = %d, %v; want 2", n, err)
	}
	if got := s.Received("u-bob", []string{"g-x"}); len(got) != 1 || got[0].CollectionID != keep.ID {
		t.Fatalf("received after prune = %+v", got)
	}
	// The pruned state is what a restart reads.
	if got := f.open(t).Received("u-bob", []string{"g-x"}); len(got) != 1 {
		t.Fatalf("received after reload = %+v", got)
	}
	if n, err := s.Prune(ctx, inner); err != nil || n != 0 {
		t.Fatalf("second Prune = %d, %v", n, err)
	}
}

func TestPruneKeepsSharesWhenTheStoreFails(t *testing.T) {
	f := newFixture(t, "alice")
	inner := memory.New()
	ctx := context.Background()
	c, _ := inner.CreateCollection(ctx, cal, store.Props{})
	s := f.open(t)
	_ = s.Grant(ctx, c.Ref, c.ID, user("u-bob", Read))

	boom := errors.New("store unavailable")
	n, err := s.Prune(ctx, failingGet{inner, boom})
	if !errors.Is(err, boom) || n != 0 {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	if got := s.Received("u-bob", nil); len(got) != 1 {
		t.Fatalf("a store fault dropped the share: %+v", got)
	}
}

type failingGet struct {
	store.Store
	err error
}

func (f failingGet) GetCollection(context.Context, store.CollectionRef) (store.Collection, error) {
	return store.Collection{}, f.err
}
