package principals

import (
	"context"
	"errors"
	"testing"

	"github.com/LukasHirt/groupkit/store"
)

// migrated is a principal that exists before its user signed in: it has an
// owner and an oCIS id, but no subject.
func migrated(owner, username string) record {
	return record{Owner: owner, Username: username, DisplayName: username, OCISID: "ocis-" + owner, DefaultsCreated: true}
}

func TestOwnerDefaultsToSub(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	p, _ := mustProvision(t, r, "sub-1", "alice", "Alice")
	if p.Owner != "sub-1" || p.Sub != "sub-1" {
		t.Fatalf("principal = %+v, want owner and sub sub-1", p)
	}
	if got := readRecordFile(t, f, "sub-1"); got.Owner != "sub-1" {
		t.Fatalf("file owner = %q, want sub-1", got.Owner)
	}
}

func TestFileWithoutOwnerLoadsWithOwnerSub(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, record{Sub: "sub-1", Username: "alice", DisplayName: "Alice", DefaultsCreated: true})
	r := f.open(t, nil)

	p, err := r.Get("sub-1")
	if err != nil || p.Owner != "sub-1" || p.Sub != "sub-1" {
		t.Fatalf("Get = %+v, %v", p, err)
	}
	if list := r.ListPrincipals(context.Background()); len(list) != 1 || list[0].Owner != "sub-1" {
		t.Fatalf("list = %+v", list)
	}
	assertLookup(t, r, "alice", "sub-1")
	got, created, err := r.Provision(context.Background(), "sub-1", "alice", "Alice")
	if err != nil || created || got.Owner != "sub-1" {
		t.Fatalf("Provision = %+v, %v, %v", got, created, err)
	}
}

func TestPrincipalWithoutSubLoads(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, migrated("migrated-1", "alice"))
	r := f.open(t, nil)

	p, err := r.Get("migrated-1")
	if err != nil || p.Sub != "" || p.Owner != "migrated-1" || p.Username != "alice" {
		t.Fatalf("Get = %+v, %v", p, err)
	}
	assertLookup(t, r, "alice", "migrated-1")
	if _, err := r.BySub(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BySub(\"\") = %v, want ErrNotFound", err)
	}
}

func TestBySub(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, record{Owner: "migrated-1", Sub: "sub-9", Username: "alice", DefaultsCreated: true})
	r := f.open(t, nil)
	p, err := r.BySub("sub-9")
	if err != nil || p.Owner != "migrated-1" {
		t.Fatalf("BySub = %+v, %v", p, err)
	}
	if _, err := r.BySub("migrated-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BySub(owner) = %v, want ErrNotFound", err)
	}
}

func TestFileInTheWrongOwnerDirectoryIsSkipped(t *testing.T) {
	f := newFixture(t)
	rec := migrated("migrated-1", "alice")
	writeRecordFile(t, f, rec)
	// The same file copied into another owner's directory.
	other := testOwnerDir(f.dataDir)("elsewhere")
	writeRecordFileAt(t, other, rec)
	r := f.open(t, nil)
	if got := r.ListPrincipals(context.Background()); len(got) != 1 {
		t.Fatalf("list = %+v, want the one in its own directory", got)
	}
}

func TestSubClaimedTwiceKeepsTheFirstOwner(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, record{Owner: "a-owner", Sub: "sub-1", Username: "a", DefaultsCreated: true})
	writeRecordFile(t, f, record{Owner: "b-owner", Sub: "sub-1", Username: "b", DefaultsCreated: true})
	r := f.open(t, nil)
	p, err := r.BySub("sub-1")
	if err != nil || p.Owner != "a-owner" {
		t.Fatalf("BySub = %+v, %v, want a-owner", p, err)
	}
	if _, err := r.Get("b-owner"); err != nil {
		t.Fatalf("second file is lost: %v", err)
	}
}

func TestProvisionFindsABoundPrincipalBySub(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, record{Owner: "migrated-1", Sub: "sub-9", Username: "alice", DisplayName: "Alice", DefaultsCreated: true})
	r := f.open(t, nil)

	p, created, err := r.Provision(context.Background(), "sub-9", "alice", "Alice B")
	if err != nil || created {
		t.Fatalf("Provision = %+v, %v, %v", p, created, err)
	}
	if p.Owner != "migrated-1" || p.DisplayName != "Alice B" {
		t.Fatalf("principal = %+v", p)
	}
	if _, err := r.Get("sub-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a principal for the sub was created: %v", err)
	}
	assertLookup(t, r, "alice", "migrated-1")
}

func TestProvisionCreatesDefaultsForTheOwner(t *testing.T) {
	f := newFixture(t)
	rec := migrated("migrated-1", "alice")
	rec.Sub, rec.DefaultsCreated = "sub-9", false
	writeRecordFile(t, f, rec)
	r := f.open(t, nil)
	if _, _, err := r.Provision(context.Background(), "sub-9", "alice", "Alice"); err != nil {
		t.Fatal(err)
	}
	cals, err := f.st.ListCollections(context.Background(), "migrated-1", store.KindCalendar)
	if err != nil || len(cals) != 1 {
		t.Fatalf("calendars of the owner = %v, %v", cals, err)
	}
	if cals, _ := f.st.ListCollections(context.Background(), "sub-9", store.KindCalendar); len(cals) != 0 {
		t.Fatalf("calendars under the sub = %v", cals)
	}
}

func TestProvisionRefusesASubThatIsAnotherOwner(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, migrated("migrated-1", "alice"))
	r := f.open(t, nil)
	if _, _, err := r.Provision(context.Background(), "migrated-1", "bob", "Bob"); err == nil {
		t.Fatal("Provision created a principal on another principal's owner")
	}
	if p, _ := r.Get("migrated-1"); p.Username != "alice" {
		t.Fatalf("principal changed: %+v", p)
	}
}

func TestOwnerAppliesToDisablePurgeAndAppPasswords(t *testing.T) {
	f := newFileFixture(t)
	rec := migrated("migrated-1", "carol")
	writeRecordFileAt(t, f.ownerDir("migrated-1"), rec)
	r := f.open(t)
	ctx := context.Background()
	shares := &fakeShares{grants: map[string]int{"migrated-1": 2}}
	r.SetShares(shares)
	mustCreateCollection(t, f.fs, "migrated-1", store.KindCalendar, "work")

	_, secret := mustIssue(t, r, "migrated-1", "Phone", 0)
	p, err := r.VerifyBasic(ctx, "carol", secret)
	if err != nil || p.Owner != "migrated-1" || p.Sub != "" {
		t.Fatalf("VerifyBasic = %+v, %v", p, err)
	}
	if list, err := r.ListAppPasswords("migrated-1"); err != nil || len(list) != 1 {
		t.Fatalf("ListAppPasswords = %v, %v", list, err)
	}

	if _, err := r.Purge(ctx, "migrated-1", true); !errors.Is(err, ErrNotDisabled) {
		t.Fatalf("Purge of an enabled principal = %v", err)
	}
	if _, err := r.SetDisabled(ctx, "migrated-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.VerifyBasic(ctx, "carol", secret); !errors.Is(err, ErrDisabled) {
		t.Fatalf("VerifyBasic of a disabled principal = %v", err)
	}
	report, err := r.Purge(ctx, "migrated-1", false)
	if err != nil || report.Calendars != 1 || report.AppPasswords != 1 || report.Shares != 2 {
		t.Fatalf("Purge = %+v, %v", report, err)
	}
	if len(shares.removed) != 1 || shares.removed[0] != "migrated-1" {
		t.Fatalf("shares removed for %v", shares.removed)
	}
	assertNoOwnerDir(t, f, "migrated-1")
	if got := r.ListPrincipals(ctx); len(got) != 1 {
		t.Fatalf("list after purge = %+v, want only alice", got)
	}
	assertLookup(t, r, "carol", "")
}

func TestPurgeForgetsTheSub(t *testing.T) {
	f := newFileFixture(t)
	ctx := context.Background()
	if _, err := f.r.SetDisabled(ctx, "sub-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.Purge(ctx, "sub-1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.BySub("sub-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BySub after purge = %v, want ErrNotFound", err)
	}
}

func TestSetDisabledByOwnerSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	rec := migrated("migrated-1", "alice")
	rec.Sub = "sub-9"
	writeRecordFile(t, f, rec)
	r := f.open(t, nil)
	if _, err := r.SetDisabled(context.Background(), "migrated-1", true); err != nil {
		t.Fatal(err)
	}
	r = f.open(t, nil)
	if _, _, err := r.Provision(context.Background(), "sub-9", "alice", "alice"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Provision = %v, want ErrDisabled", err)
	}
}
