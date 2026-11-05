package principals

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestBindGivesAnImportedPrincipalItsSub(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, migrated("migrated-1", "alice"))
	r := f.open(t, nil)

	p, bound, err := r.Bind(context.Background(), "ocis-migrated-1", "sub-9")
	if err != nil || !bound || p.Owner != "migrated-1" || p.Sub != "sub-9" {
		t.Fatalf("Bind = %+v, %v, %v", p, bound, err)
	}
	if got, err := r.BySub("sub-9"); err != nil || got.Owner != "migrated-1" {
		t.Fatalf("BySub = %+v, %v", got, err)
	}
	if r.HasUnbound() {
		t.Fatal("HasUnbound = true after the only unbound principal was bound")
	}
	// The binding is in the file, so it survives a restart.
	r = f.open(t, nil)
	if got, err := r.BySub("sub-9"); err != nil || got.Owner != "migrated-1" {
		t.Fatalf("after restart: BySub = %+v, %v", got, err)
	}
	if got := readRecordFromDir(t, testOwnerDir(f.dataDir)("migrated-1")); got.Sub != "sub-9" || got.Owner != "migrated-1" || got.Username != "alice" {
		t.Fatalf("file = %+v", got)
	}
}

func TestBindIsIdempotentForTheSameSub(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, migrated("migrated-1", "alice"))
	r := f.open(t, nil)
	ctx := context.Background()
	if _, _, err := r.Bind(ctx, "ocis-migrated-1", "sub-9"); err != nil {
		t.Fatal(err)
	}
	p, bound, err := r.Bind(ctx, "ocis-migrated-1", "sub-9")
	if err != nil || bound || p.Owner != "migrated-1" {
		t.Fatalf("second Bind = %+v, %v, %v; want the principal and no new binding", p, bound, err)
	}
}

func TestBindDoesNotTakeABoundPrincipal(t *testing.T) {
	f := newFixture(t)
	rec := migrated("migrated-1", "alice")
	rec.Sub = "sub-1"
	writeRecordFile(t, f, rec)
	r := f.open(t, nil)

	_, _, err := r.Bind(context.Background(), "ocis-migrated-1", "sub-evil")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bind = %v, want ErrNotFound", err)
	}
	if p, _ := r.Get("migrated-1"); p.Sub != "sub-1" {
		t.Fatalf("sub = %q, want sub-1", p.Sub)
	}
	if _, err := r.BySub("sub-evil"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BySub(sub-evil) = %v", err)
	}
}

func TestBindOnlyMatchesTheOCISID(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, migrated("migrated-1", "alice"))
	r := f.open(t, nil)
	for _, id := range []string{"ocis-other", ""} {
		if _, _, err := r.Bind(context.Background(), id, "sub-9"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Bind(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

func TestBindRefusesADisabledPrincipal(t *testing.T) {
	f := newFixture(t)
	rec := migrated("migrated-1", "alice")
	rec.Disabled = true
	writeRecordFile(t, f, rec)
	r := f.open(t, nil)
	if _, _, err := r.Bind(context.Background(), "ocis-migrated-1", "sub-9"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Bind = %v, want ErrDisabled", err)
	}
	if p, _ := r.Get("migrated-1"); p.Sub != "" {
		t.Fatalf("a disabled principal was bound: %+v", p)
	}
}

func TestBindRefusesASubThatHasAPrincipal(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, migrated("migrated-1", "alice"))
	r := f.open(t, nil)
	mustProvision(t, r, "sub-9", "bob", "Bob")
	p, bound, err := r.Bind(context.Background(), "ocis-migrated-1", "sub-9")
	if err != nil || bound || p.Owner != "sub-9" {
		t.Fatalf("Bind = %+v, %v, %v; want bob's own principal back", p, bound, err)
	}
	if got, _ := r.Get("migrated-1"); got.Sub != "" {
		t.Fatalf("imported principal bound: %+v", got)
	}
}

func TestBindRefusesAnAmbiguousID(t *testing.T) {
	f := newFixture(t)
	a, b := migrated("migrated-1", "alice"), migrated("migrated-2", "alice2")
	b.OCISID = a.OCISID
	writeRecordFile(t, f, a)
	writeRecordFile(t, f, b)
	r := f.open(t, nil)
	if _, _, err := r.Bind(context.Background(), a.OCISID, "sub-9"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("Bind = %v, want ErrAmbiguous", err)
	}
}

func TestConcurrentBindsBindOnce(t *testing.T) {
	f := newFixture(t)
	writeRecordFile(t, f, migrated("migrated-1", "alice"))
	r := f.open(t, nil)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		wins  int
		other int
	)
	for i := range 10 {
		sub := "sub-" + string(rune('a'+i))
		wg.Go(func() {
			_, bound, err := r.Bind(context.Background(), "ocis-migrated-1", sub)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && bound:
				wins++
			case errors.Is(err, ErrNotFound):
				other++
			default:
				t.Errorf("Bind(%s) = %v, %v", sub, bound, err)
			}
		})
	}
	wg.Wait()
	if wins != 1 || other != 9 {
		t.Fatalf("wins %d, refused %d; want 1 and 9", wins, other)
	}
}
