package principals

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestImportCreatesAnUnboundPrincipal(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	ctx := context.Background()
	id := testIdentity("ocis-1", Group{ID: "g1", Name: "team"})
	id.At = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

	p, created, err := r.Import(ctx, "migrated-ocis-1", "alice", "Alice A", id)
	if err != nil || !created {
		t.Fatalf("Import = %+v, %v, %v", p, created, err)
	}
	if p.Owner != "migrated-ocis-1" || p.Sub != "" || p.Username != "alice" || p.DisplayName != "Alice A" {
		t.Fatalf("principal = %+v", p)
	}
	if owner, ok := r.LookupUsername("alice"); !ok || owner != "migrated-ocis-1" {
		t.Fatalf("LookupUsername = %q, %v", owner, ok)
	}
	got, err := r.Identity("migrated-ocis-1")
	if err != nil || got.OCISID != "ocis-1" || len(got.Groups) != 1 || got.Groups[0].Name != "team" {
		t.Fatalf("Identity = %+v, %v", got, err)
	}
	if !r.HasUnbound() {
		t.Fatal("HasUnbound = false after an import")
	}

	// The file survives a restart, and the default collections are not
	// created for the user at the first sign-in.
	r = f.open(t, nil)
	if rec := readRecordFile(t, f, "migrated-ocis-1"); !rec.DefaultsCreated || rec.Sub != "" || rec.OCISID != "ocis-1" {
		t.Fatalf("file = %+v", rec)
	}
	if _, bound, err := r.Bind(ctx, "ocis-1", "sub-7"); err != nil || !bound {
		t.Fatalf("Bind = %v, %v", bound, err)
	}
}

func TestImportTwiceChangesNothing(t *testing.T) {
	r := newFixture(t).open(t, nil)
	ctx := context.Background()
	if _, _, err := r.Import(ctx, "migrated-1", "alice", "Alice", testIdentity("1")); err != nil {
		t.Fatal(err)
	}
	p, created, err := r.Import(ctx, "migrated-1", "alice", "Other Name", testIdentity("1"))
	if err != nil || created || p.DisplayName != "Alice" {
		t.Fatalf("second Import = %+v, %v, %v", p, created, err)
	}
}

func TestImportDoesNotTakeAUsername(t *testing.T) {
	r := newFixture(t).open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	_, _, err := r.Import(context.Background(), "migrated-2", "alice", "Alice", testIdentity("2"))
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("Import = %v, want ErrUsernameTaken", err)
	}
	if owner, _ := r.LookupUsername("alice"); owner != "sub-1" {
		t.Fatalf("owner of alice = %q", owner)
	}
	if _, err := r.Get("migrated-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

func TestImportRejectsBadNames(t *testing.T) {
	r := newFixture(t).open(t, nil)
	ctx := context.Background()
	if _, _, err := r.Import(ctx, "", "alice", "A", testIdentity("1")); err == nil {
		t.Fatal("empty owner accepted")
	}
	if _, _, err := r.Import(ctx, "o", "a/b", "A", testIdentity("1")); err == nil {
		t.Fatal("username with a slash accepted")
	}
	if _, _, err := r.Import(ctx, "o", "alice", "A", Identity{}); err == nil {
		t.Fatal("import without an oCIS id accepted")
	}
}
