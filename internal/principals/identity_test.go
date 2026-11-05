package principals

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

func testIdentity(id string, groups ...Group) Identity {
	return Identity{OCISID: id, Groups: groups, At: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
}

func TestIdentityOfNewPrincipalIsEmpty(t *testing.T) {
	r := newFixture(t).open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	got, err := r.Identity("sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.OCISID != "" || len(got.Groups) != 0 || !got.At.IsZero() {
		t.Fatalf("identity = %+v, want empty", got)
	}
	if _, err := r.Identity("nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Identity of an unknown sub: %v, want ErrNotFound", err)
	}
}

func TestSetIdentityIsReadBackAndSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	want := testIdentity("id-1", Group{ID: "g1", Name: "Physics"}, Group{ID: "g2", Name: "Users"})
	if err := r.SetIdentity(context.Background(), "sub-1", want); err != nil {
		t.Fatal(err)
	}
	got, err := r.Identity("sub-1")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Identity = %+v, %v, want %+v", got, err, want)
	}

	rec := readRecordFile(t, f, "sub-1")
	if rec.OCISID != "id-1" || len(rec.Groups) != 2 || !rec.IdentityAt.Equal(want.At) {
		t.Fatalf("file = %+v", rec)
	}

	r2 := f.open(t, nil)
	got, err = r2.Identity("sub-1")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("after restart Identity = %+v, %v, want %+v", got, err, want)
	}
}

func TestIdentityReturnsACopy(t *testing.T) {
	r := newFixture(t).open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	if err := r.SetIdentity(context.Background(), "sub-1", testIdentity("id-1", Group{ID: "g1", Name: "A"})); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Identity("sub-1")
	got.Groups[0].ID = "changed"
	again, _ := r.Identity("sub-1")
	if again.Groups[0].ID != "g1" {
		t.Fatal("changing a returned identity changed the registry")
	}
}

func TestSetIdentityKeepsTheOtherFields(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	before := readRecordFile(t, f, "sub-1")
	if err := r.SetIdentity(context.Background(), "sub-1", testIdentity("id-1")); err != nil {
		t.Fatal(err)
	}
	after := readRecordFile(t, f, "sub-1")
	after.OCISID, after.Groups, after.IdentityAt = "", nil, time.Time{}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("record changed beyond the identity: %+v became %+v", before, after)
	}
	// A later login keeps the identity.
	mustProvision(t, r, "sub-1", "alice2", "Alice Two")
	if got, _ := r.Identity("sub-1"); got.OCISID != "id-1" {
		t.Fatalf("identity lost on a name change: %+v", got)
	}
	assertLookup(t, r, "alice2", "sub-1")
}

func TestSetIdentityReplacesGroups(t *testing.T) {
	r := newFixture(t).open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	ctx := context.Background()
	_ = r.SetIdentity(ctx, "sub-1", testIdentity("id-1", Group{ID: "g1"}, Group{ID: "g2"}))
	if err := r.SetIdentity(ctx, "sub-1", testIdentity("id-1")); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Identity("sub-1"); len(got.Groups) != 0 {
		t.Fatalf("groups = %+v, want none", got.Groups)
	}
}

func TestSetIdentityOfUnknownPrincipal(t *testing.T) {
	r := newFixture(t).open(t, nil)
	err := r.SetIdentity(context.Background(), "nobody", testIdentity("id-1"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSetIdentityLeavesADisabledPrincipalAlone(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	mustSetDisabled(t, r, "sub-1", true)
	before, err := os.ReadFile(f.principalFile("sub-1"))
	if err != nil {
		t.Fatal(err)
	}
	err = r.SetIdentity(context.Background(), "sub-1", testIdentity("id-1"))
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	after, _ := os.ReadFile(f.principalFile("sub-1"))
	if string(before) != string(after) {
		t.Fatal("the file of a disabled principal changed")
	}
	if got, _ := r.Identity("sub-1"); got.OCISID != "" {
		t.Fatalf("identity = %+v, want empty", got)
	}
}

func TestOlderFileWithoutIdentityLoads(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, record{Sub: "sub-1", Username: "alice", DisplayName: "Alice", DefaultsCreated: true})
	r := f.open(t, nil)
	if _, err := r.Get("sub-1"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Identity("sub-1")
	if err != nil || got.OCISID != "" || !got.At.IsZero() {
		t.Fatalf("identity = %+v, %v", got, err)
	}
}

func TestConcurrentSignInsAndIdentityUpdates(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			if i%2 == 0 {
				if _, _, err := r.Provision(context.Background(), "sub-1", "alice", "Alice"); err != nil {
					t.Error(err)
				}
				return
			}
			if err := r.SetIdentity(context.Background(), "sub-1", testIdentity("id-1", Group{ID: "g1"})); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	rec := readRecordFile(t, f, "sub-1")
	if rec.OCISID != "id-1" || rec.Username != "alice" || !rec.DefaultsCreated {
		t.Fatalf("file = %+v", rec)
	}
}
