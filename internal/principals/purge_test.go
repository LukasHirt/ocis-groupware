package principals

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"
)

// fileFixture is a registry on the real file store, so that a test can check
// the directory tree. sub-1 is provisioned as alice.
type fileFixture struct {
	dataDir string
	fs      *filestore.Store
	clock   *fakeClock
	r       *Registry
}

func newFileFixture(t *testing.T) *fileFixture {
	t.Helper()
	dir := t.TempDir()
	fs, err := filestore.Open(dir)
	if err != nil {
		t.Fatalf("filestore.Open: %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	f := &fileFixture{dataDir: dir, fs: fs, clock: newFakeClock()}
	f.r = f.open(t)
	mustProvision(t, f.r, "sub-1", "alice", "Alice")
	return f
}

// open returns a new registry on the fixture's store, as after a restart.
func (f *fileFixture) open(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(f.dataDir, f.fs, f.fs.OwnerDir, discardLogger(), WithClock(f.clock.Now), WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func (f *fileFixture) ownerDir(sub string) string {
	return f.fs.OwnerDir(sub)
}

// tree lists every path below dir, relative to it, in order.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(paths)
	return paths
}

func mustCreateCollection(t *testing.T, st store.Store, owner string, kind store.Kind, name string) {
	t.Helper()
	ref := store.CollectionRef{Owner: owner, Kind: kind, Name: name}
	if _, err := st.CreateCollection(context.Background(), ref, store.Props{DisplayName: name}); err != nil {
		t.Fatalf("CreateCollection(%v): %v", ref, err)
	}
}

func assertNoOwnerDir(t *testing.T, f *fileFixture, sub string) {
	t.Helper()
	if _, err := os.Stat(f.ownerDir(sub)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner directory of %q still exists (stat err %v):\n%s", sub, err, strings.Join(tree(t, f.ownerDir(sub)), "\n"))
	}
}

func TestPurgeRequiresDisabled(t *testing.T) {
	f := newFileFixture(t)
	mustCreateCollection(t, f.fs, "sub-1", store.KindCalendar, "work")
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	before := tree(t, f.ownerDir("sub-1"))
	beforeFile, err := os.ReadFile(filepath.Join(f.ownerDir("sub-1"), principalFileName))
	if err != nil {
		t.Fatal(err)
	}

	for _, dryRun := range []bool{false, true} {
		if _, err := f.r.Purge(context.Background(), "sub-1", dryRun); !errors.Is(err, ErrNotDisabled) {
			t.Fatalf("Purge(dryRun=%v) err = %v, want ErrNotDisabled", dryRun, err)
		}
	}

	if after := tree(t, f.ownerDir("sub-1")); !slices.Equal(after, before) {
		t.Fatalf("owner directory changed:\n%v\nwas:\n%v", after, before)
	}
	afterFile, err := os.ReadFile(filepath.Join(f.ownerDir("sub-1"), principalFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterFile) != string(beforeFile) {
		t.Fatalf("principal file changed:\n%s\nwas:\n%s", afterFile, beforeFile)
	}
	assertCollectionCount(t, f.fs, "sub-1", store.KindCalendar, 2)
	assertCollectionCount(t, f.fs, "sub-1", store.KindAddressBook, 1)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	assertLookup(t, f.r, "alice", "sub-1")
}

func TestPurgeRemovesEverything(t *testing.T) {
	f := newFileFixture(t)
	mustCreateCollection(t, f.fs, "sub-1", store.KindCalendar, "work")
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	mustIssue(t, f.r, "sub-1", "Tablet", 0)
	mustVerify(t, f.r, "alice", secret, "sub-1")
	mustProvision(t, f.r, "sub-2", "bob", "Bob")
	mustSetDisabled(t, f.r, "sub-1", true)

	report, err := f.r.Purge(context.Background(), "sub-1", false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if want := (PurgeReport{Calendars: 2, AddressBooks: 1, AppPasswords: 2}); report != want {
		t.Errorf("report = %+v, want %+v", report, want)
	}

	assertNoOwnerDir(t, f, "sub-1")
	assertCollectionCount(t, f.fs, "sub-1", store.KindCalendar, 0)
	assertCollectionCount(t, f.fs, "sub-1", store.KindAddressBook, 0)
	if _, err := f.r.Get("sub-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get err = %v, want ErrNotFound", err)
	}
	assertLookup(t, f.r, "alice", "")
	assertVerifyErr(t, f.r, "alice", secret, ErrInvalidCredentials)
	if _, err := f.r.Purge(context.Background(), "sub-1", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Purge err = %v, want ErrNotFound", err)
	}
	if got := f.r.ListPrincipals(context.Background()); len(got) != 1 || got[0].Sub != "sub-2" {
		t.Errorf("ListPrincipals = %+v, want bob only", got)
	}

	// Nothing stale stays in memory: the store can make a collection for the
	// owner again, and bob is untouched.
	mustCreateCollection(t, f.fs, "sub-1", store.KindCalendar, "personal")
	assertCollectionCount(t, f.fs, "sub-1", store.KindCalendar, 1)
	assertDefaultCollections(t, f.fs, "sub-2")
	assertLookup(t, f.r, "bob", "sub-2")

	// The same after a restart.
	r2 := f.open(t)
	if _, err := r2.Get("sub-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after restart err = %v, want ErrNotFound", err)
	}
	assertLookup(t, r2, "bob", "sub-2")
}

// An owner directory without a principal file is what an interrupted purge
// leaves behind. Nothing can sign in as it, so it needs no disabled flag.
func TestPurgeOrphanDirectory(t *testing.T) {
	f := newFileFixture(t)
	mustCreateCollection(t, f.fs, "sub-orphan", store.KindCalendar, "work")
	mustCreateCollection(t, f.fs, "sub-orphan", store.KindAddressBook, "friends")
	if _, err := f.r.Get("sub-orphan"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get err = %v, want ErrNotFound", err)
	}

	report, err := f.r.Purge(context.Background(), "sub-orphan", true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if want := (PurgeReport{Calendars: 1, AddressBooks: 1}); report != want {
		t.Errorf("dry run report = %+v, want %+v", report, want)
	}
	assertCollectionCount(t, f.fs, "sub-orphan", store.KindCalendar, 1)

	report, err = f.r.Purge(context.Background(), "sub-orphan", false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if want := (PurgeReport{Calendars: 1, AddressBooks: 1}); report != want {
		t.Errorf("report = %+v, want %+v", report, want)
	}
	assertNoOwnerDir(t, f, "sub-orphan")
	assertCollectionCount(t, f.fs, "sub-orphan", store.KindCalendar, 0)
	if _, err := f.r.Purge(context.Background(), "sub-orphan", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("Purge of a gone directory err = %v, want ErrNotFound", err)
	}
	// alice is untouched.
	assertLookup(t, f.r, "alice", "sub-1")
	assertDefaultCollections(t, f.fs, "sub-1")
}

// failDeleteStore fails the first DeleteCollection.
type failDeleteStore struct {
	store.Store
	failed bool
}

func (s *failDeleteStore) DeleteCollection(ctx context.Context, ref store.CollectionRef) error {
	if !s.failed {
		s.failed = true
		return errors.New("disk on fire")
	}
	return s.Store.DeleteCollection(ctx, ref)
}

// A purge that fails half way leaves either a disabled principal with fewer
// collections or an orphan directory. Running it again finishes the job.
func TestPurgeIsRerunnable(t *testing.T) {
	t.Run("collection deletion fails", func(t *testing.T) {
		f := newFileFixture(t)
		st := &failDeleteStore{Store: f.fs}
		r, err := NewRegistry(f.dataDir, st, f.fs.OwnerDir, discardLogger(), WithClock(f.clock.Now), WithBcryptCost(bcrypt.MinCost))
		if err != nil {
			t.Fatal(err)
		}
		mustCreateCollection(t, f.fs, "sub-1", store.KindCalendar, "work")
		mustIssue(t, r, "sub-1", "Phone", 0)
		mustSetDisabled(t, r, "sub-1", true)

		if _, err := r.Purge(context.Background(), "sub-1", false); err == nil {
			t.Fatal("Purge succeeded with a failing store, want an error")
		}
		p, err := r.Get("sub-1")
		if err != nil || !p.Disabled {
			t.Fatalf("Get after the failed purge = %+v, %v, want a disabled principal", p, err)
		}
		if _, err := os.Stat(filepath.Join(f.ownerDir("sub-1"), principalFileName)); err != nil {
			t.Fatalf("principal file after the failed purge: %v", err)
		}

		report, err := r.Purge(context.Background(), "sub-1", false)
		if err != nil {
			t.Fatalf("second Purge: %v", err)
		}
		if report.Calendars+report.AddressBooks != 3 || report.AppPasswords != 1 {
			t.Errorf("second report = %+v", report)
		}
		assertNoOwnerDir(t, f, "sub-1")
		if _, err := r.Get("sub-1"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get err = %v, want ErrNotFound", err)
		}
	})

	t.Run("directory removal fails", func(t *testing.T) {
		f := newFileFixture(t)
		mustCreateCollection(t, f.fs, "sub-1", store.KindCalendar, "work")
		_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
		mustSetDisabled(t, f.r, "sub-1", true)
		f.r.beforeOwnerDirRemoved = func() error { return errors.New("disk on fire") }

		if _, err := f.r.Purge(context.Background(), "sub-1", false); err == nil {
			t.Fatal("Purge succeeded with a failing removal, want an error")
		}
		if _, err := os.Stat(f.ownerDir("sub-1")); err != nil {
			t.Fatalf("owner directory after the failed purge: %v", err)
		}
		for _, name := range []string{principalFileName, appPasswordsFileName} {
			if _, err := os.Stat(filepath.Join(f.ownerDir("sub-1"), name)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s after the failed purge: %v, want it gone", name, err)
			}
		}
		if _, err := f.r.Get("sub-1"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get err = %v, want ErrNotFound", err)
		}
		assertLookup(t, f.r, "alice", "")
		assertVerifyErr(t, f.r, "alice", secret, ErrInvalidCredentials)

		// A restart does not bring the principal back, and the orphan can be
		// purged by either registry.
		r2 := f.open(t)
		if _, err := r2.Get("sub-1"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get after restart err = %v, want ErrNotFound", err)
		}
		f.r.beforeOwnerDirRemoved = nil
		report, err := f.r.Purge(context.Background(), "sub-1", false)
		if err != nil {
			t.Fatalf("second Purge: %v", err)
		}
		if report != (PurgeReport{}) {
			t.Errorf("second report = %+v, want nothing left to count", report)
		}
		assertNoOwnerDir(t, f, "sub-1")
	})
}

// Purge does not tell the identity provider anything. A user who signs in
// again after a purge starts with an empty principal and the defaults.
func TestPurgeThenProvisionStartsFresh(t *testing.T) {
	f := newFileFixture(t)
	mustCreateCollection(t, f.fs, "sub-1", store.KindCalendar, "work")
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	first, _ := mustProvision(t, f.r, "sub-1", "alice", "Alice")
	mustSetDisabled(t, f.r, "sub-1", true)
	if _, err := f.r.Purge(context.Background(), "sub-1", false); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	f.clock.Add(time.Hour)

	p, created := mustProvision(t, f.r, "sub-1", "alice", "Alice Again")
	if !created {
		t.Fatal("created = false after a purge, want true")
	}
	if p.Disabled || p.DisplayName != "Alice Again" || !p.CreatedAt.After(first.CreatedAt) {
		t.Fatalf("Provision after purge = %+v, want a new enabled principal", p)
	}
	assertDefaultCollections(t, f.fs, "sub-1")
	assertLookup(t, f.r, "alice", "sub-1")
	assertVerifyErr(t, f.r, "alice", secret, ErrInvalidCredentials)
	list, err := f.r.ListAppPasswords("sub-1")
	if err != nil || len(list) != 0 {
		t.Fatalf("ListAppPasswords after purge = %+v, %v, want none", list, err)
	}
	if rec := readRecordFromDir(t, f.ownerDir("sub-1")); rec.Disabled || !rec.DefaultsCreated {
		t.Fatalf("record on disk = %+v", rec)
	}
}

func readRecordFromDir(t *testing.T, dir string) record {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, principalFileName))
	if err != nil {
		t.Fatal(err)
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

// blockingDeleteStore blocks the first DeleteCollection until released.
type blockingDeleteStore struct {
	store.Store
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingDeleteStore) DeleteCollection(ctx context.Context, ref store.CollectionRef) error {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.Store.DeleteCollection(ctx, ref)
}

// While a purge is deleting collections, logins of other principals go on.
func TestPurgeDoesNotBlockOtherLogins(t *testing.T) {
	f := newFileFixture(t)
	st := &blockingDeleteStore{Store: f.fs, entered: make(chan struct{}), release: make(chan struct{})}
	r, err := NewRegistry(f.dataDir, st, f.fs.OwnerDir, discardLogger(), WithClock(f.clock.Now), WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	mustProvision(t, r, "sub-2", "bob", "Bob")
	_, bobSecret := mustIssue(t, r, "sub-2", "Phone", 0)
	mustSetDisabled(t, r, "sub-1", true)

	done := make(chan error, 1)
	go func() {
		_, err := r.Purge(context.Background(), "sub-1", false)
		done <- err
	}()
	<-st.entered

	subs := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	var wg sync.WaitGroup
	for _, sub := range subs {
		for range 4 {
			wg.Go(func() {
				if _, _, err := r.Provision(context.Background(), sub, "user-"+sub, sub); err != nil {
					t.Errorf("Provision(%q): %v", sub, err)
				}
				r.LookupUsername("user-" + sub)
				_, _ = r.Get(sub)
				mustVerify(t, r, "bob", bobSecret, "sub-2")
			})
		}
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("logins of other principals are blocked by the purge")
	}
	if _, err := r.Get("sub-1"); err != nil {
		t.Errorf("Get(sub-1) during the purge err = %v, want the disabled principal still listed", err)
	}
	if _, _, err := r.Provision(context.Background(), "sub-1", "alice", "Alice"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Provision(sub-1) during the purge err = %v, want ErrDisabled", err)
	}

	close(st.release)
	if err := <-done; err != nil {
		t.Fatalf("Purge: %v", err)
	}
	assertNoOwnerDir(t, f, "sub-1")
	for _, sub := range subs {
		assertLookup(t, r, "user-"+sub, sub)
		assertDefaultCollections(t, f.fs, sub)
	}
}

func TestPurgeReportCounts(t *testing.T) {
	f := newFileFixture(t)
	mustCreateCollection(t, f.fs, "sub-1", store.KindCalendar, "work")
	mustCreateCollection(t, f.fs, "sub-1", store.KindCalendar, "family")
	mustCreateCollection(t, f.fs, "sub-1", store.KindAddressBook, "friends")
	for _, name := range []string{"Phone", "Tablet", "Laptop"} {
		mustIssue(t, f.r, "sub-1", name, 0)
	}
	mustSetDisabled(t, f.r, "sub-1", true)
	before := tree(t, f.ownerDir("sub-1"))
	want := PurgeReport{Calendars: 3, AddressBooks: 2, AppPasswords: 3}

	report, err := f.r.Purge(context.Background(), "sub-1", true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if report != want {
		t.Errorf("dry run report = %+v, want %+v", report, want)
	}
	if after := tree(t, f.ownerDir("sub-1")); !slices.Equal(after, before) {
		t.Fatalf("dry run changed the owner directory:\n%v\nwas:\n%v", after, before)
	}
	if p, err := f.r.Get("sub-1"); err != nil || !p.Disabled {
		t.Fatalf("Get after dry run = %+v, %v", p, err)
	}

	report, err = f.r.Purge(context.Background(), "sub-1", false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if report != want {
		t.Errorf("report = %+v, want %+v", report, want)
	}
	assertNoOwnerDir(t, f, "sub-1")

	// A principal that never had app passwords or extra collections.
	mustProvision(t, f.r, "sub-2", "bob", "Bob")
	mustSetDisabled(t, f.r, "sub-2", true)
	report, err = f.r.Purge(context.Background(), "sub-2", false)
	if err != nil {
		t.Fatalf("Purge(sub-2): %v", err)
	}
	if want := (PurgeReport{Calendars: 1, AddressBooks: 1}); report != want {
		t.Errorf("report for sub-2 = %+v, want %+v", report, want)
	}
}

func TestPurgeUnknownSub(t *testing.T) {
	f := newFileFixture(t)
	for _, sub := range []string{"sub-nobody", "", "sub\n1"} {
		for _, dryRun := range []bool{true, false} {
			if _, err := f.r.Purge(context.Background(), sub, dryRun); !errors.Is(err, ErrNotFound) {
				t.Errorf("Purge(%q, dryRun=%v) err = %v, want ErrNotFound", sub, dryRun, err)
			}
		}
	}
	if _, err := os.Stat(f.ownerDir("sub-nobody")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner directory created for an unknown sub: %v", err)
	}
}
