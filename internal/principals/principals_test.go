package principals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/filestore"
	"github.com/LukasHirt/groupkit/store/memory"
)

// testOwnerDir maps an owner to a directory below dataDir the way a file
// store does: one escaped directory per owner under owners/. The escaping is
// url.PathEscape, not the file store's own; TestWithFileStore covers the real
// layout.
func testOwnerDir(dataDir string) func(string) string {
	return func(owner string) string {
		if store.ValidateOwner(owner) != nil {
			return ""
		}
		return filepath.Join(dataDir, "owners", url.PathEscape(owner))
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// syncBuffer is a bytes.Buffer safe for use by a logger and a test at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func bufferLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

type fixture struct {
	dataDir string
	st      store.Store
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{dataDir: t.TempDir(), st: memory.New()}
}

func (f fixture) open(t *testing.T, logger *slog.Logger) *Registry {
	t.Helper()
	if logger == nil {
		logger = discardLogger()
	}
	r, err := NewRegistry(f.dataDir, f.st, testOwnerDir(f.dataDir), logger)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func (f fixture) principalFile(sub string) string {
	return filepath.Join(testOwnerDir(f.dataDir)(sub), principalFileName)
}

func mustProvision(t *testing.T, r *Registry, sub, username, displayName string) (Principal, bool) {
	t.Helper()
	p, created, err := r.Provision(context.Background(), sub, username, displayName)
	if err != nil {
		t.Fatalf("Provision(%q, %q): %v", sub, username, err)
	}
	return p, created
}

func assertLookup(t *testing.T, r *Registry, username, wantSub string) {
	t.Helper()
	sub, ok := r.LookupUsername(username)
	if wantSub == "" {
		if ok {
			t.Fatalf("LookupUsername(%q) = %q, want not found", username, sub)
		}
		return
	}
	if !ok || sub != wantSub {
		t.Fatalf("LookupUsername(%q) = %q, %v, want %q", username, sub, ok, wantSub)
	}
}

func assertDefaultCollections(t *testing.T, st store.Store, owner string) {
	t.Helper()
	for _, kind := range []store.Kind{store.KindAddressBook, store.KindCalendar} {
		cols, err := st.ListCollections(context.Background(), owner, kind)
		if err != nil {
			t.Fatalf("ListCollections(%q, %s): %v", owner, kind, err)
		}
		if len(cols) != 1 {
			t.Fatalf("owner %q has %d %ss, want 1", owner, len(cols), kind)
		}
	}
}

func writeRecordFile(t *testing.T, f fixture, rec record) {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	dir := testOwnerDir(f.dataDir)(rec.Sub)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, principalFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readRecordFile(t *testing.T, f fixture, sub string) record {
	t.Helper()
	data, err := os.ReadFile(f.principalFile(sub))
	if err != nil {
		t.Fatal(err)
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode %s: %v", f.principalFile(sub), err)
	}
	return rec
}

func assertCollectionCount(t *testing.T, st store.Store, owner string, kind store.Kind, want int) {
	t.Helper()
	cols, err := st.ListCollections(context.Background(), owner, kind)
	if err != nil {
		t.Fatalf("ListCollections(%q, %s): %v", owner, kind, err)
	}
	if len(cols) != want {
		t.Fatalf("owner %q has %d %ss, want %d", owner, len(cols), kind, want)
	}
}

func deleteDefaultCalendar(t *testing.T, st store.Store, owner string) {
	t.Helper()
	ref := store.CollectionRef{Owner: owner, Kind: store.KindCalendar, Name: "personal"}
	if err := st.DeleteCollection(context.Background(), ref); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
}

func TestNewRegistryRejectsMissingDependencies(t *testing.T) {
	dir := t.TempDir()
	st := memory.New()
	od := testOwnerDir(dir)
	tests := []struct {
		name     string
		dataDir  string
		st       store.Store
		ownerDir func(string) string
	}{
		{"empty data dir", "", st, od},
		{"nil store", dir, nil, od},
		{"nil owner dir", dir, st, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRegistry(tt.dataDir, tt.st, tt.ownerDir, discardLogger()); err == nil {
				t.Fatal("NewRegistry succeeded, want an error")
			}
		})
	}
}

func TestNewRegistryAcceptsNilLogger(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewRegistry(dir, memory.New(), testOwnerDir(dir), nil); err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
}

func TestProvisionCreatesPrincipal(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)

	before := time.Now()
	p, created := mustProvision(t, r, "sub-1", "alice", "Alice Liddell")
	if !created {
		t.Fatal("created = false, want true")
	}
	if p.Sub != "sub-1" || p.Username != "alice" || p.DisplayName != "Alice Liddell" {
		t.Fatalf("principal = %+v", p)
	}
	if p.CreatedAt.Before(before.Add(-time.Second)) || p.CreatedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("CreatedAt = %v, want about now", p.CreatedAt)
	}

	got, err := r.Get("sub-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != p {
		t.Fatalf("Get = %+v, want %+v", got, p)
	}
	assertLookup(t, r, "alice", "sub-1")

	info, err := os.Stat(f.principalFile("sub-1"))
	if err != nil {
		t.Fatalf("principal file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("principal file mode = %o, want 600", perm)
	}
}

func TestProvisionCreatesDefaultCollections(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	assertDefaultCollections(t, f.st, "sub-1")
}

func TestProvisionIsIdempotent(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	first, _ := mustProvision(t, r, "sub-1", "alice", "Alice")

	second, created := mustProvision(t, r, "sub-1", "alice", "Alice")
	if created {
		t.Fatal("second Provision created = true, want false")
	}
	if second != first {
		t.Fatalf("second Provision = %+v, want %+v", second, first)
	}
	assertDefaultCollections(t, f.st, "sub-1")
}

func TestProvisionRefreshesChangedNames(t *testing.T) {
	tests := []struct {
		name            string
		username        string
		displayName     string
		wantOldUsername bool
	}{
		{"username", "alice2", "Alice", false},
		{"display name", "alice", "Alice L.", true},
		{"both", "al", "Al", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			r := f.open(t, nil)
			first, _ := mustProvision(t, r, "sub-1", "alice", "Alice")

			p, created := mustProvision(t, r, "sub-1", tt.username, tt.displayName)
			if created {
				t.Fatal("created = true, want false")
			}
			want := Principal{Sub: "sub-1", Username: tt.username, DisplayName: tt.displayName, CreatedAt: first.CreatedAt}
			if p != want {
				t.Fatalf("Provision = %+v, want %+v", p, want)
			}
			assertLookup(t, r, tt.username, "sub-1")
			if !tt.wantOldUsername {
				assertLookup(t, r, "alice", "")
			}

			// The change is on disk, not only in memory.
			reopened := f.open(t, nil)
			got, err := reopened.Get("sub-1")
			if err != nil {
				t.Fatalf("Get after reopen: %v", err)
			}
			if !got.CreatedAt.Equal(want.CreatedAt) {
				t.Fatalf("CreatedAt after reopen = %v, want %v", got.CreatedAt, want.CreatedAt)
			}
			got.CreatedAt = want.CreatedAt
			if got != want {
				t.Fatalf("Get after reopen = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLookupUsername(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	mustProvision(t, r, "sub-2", "bob", "Bob")

	tests := []struct {
		username string
		wantSub  string
	}{
		{"alice", "sub-1"},
		{"bob", "sub-2"},
		{"Alice", ""},
		{"carol", ""},
		{"", ""},
		{"sub-1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			assertLookup(t, r, tt.username, tt.wantSub)
		})
	}
}

func TestGetUnknownSub(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	for _, sub := range []string{"nobody", ""} {
		if _, err := r.Get(sub); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(%q) err = %v, want ErrNotFound", sub, err)
		}
	}
}

func TestRestartRebuildsIndex(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	alice, _ := mustProvision(t, r, "sub-1", "alice", "Alice")
	bob, _ := mustProvision(t, r, "user@example.com/2", "bob", "Bob")

	reopened := f.open(t, nil)
	for _, want := range []Principal{alice, bob} {
		assertLookup(t, reopened, want.Username, want.Sub)
		got, err := reopened.Get(want.Sub)
		if err != nil {
			t.Fatalf("Get(%q): %v", want.Sub, err)
		}
		if !got.CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
		}
	}
	_, created := mustProvision(t, reopened, "sub-1", "alice", "Alice")
	if created {
		t.Fatal("Provision after restart created = true, want false")
	}
}

func TestRestartSkipsUnreadableFiles(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")

	owners := filepath.Join(f.dataDir, "owners")
	write := func(dir, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(owners, dir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(owners, dir, principalFileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("broken", "{not json")
	write("nosub", `{"username":"ghost"}`)
	// A file whose sub does not belong to the directory it is in.
	write("elsewhere", `{"sub":"sub-9","username":"mallory","created_at":"2026-01-01T00:00:00Z"}`)
	// A directory without a principal file is the store's business.
	if err := os.MkdirAll(filepath.Join(owners, "empty", "calendars"), 0o700); err != nil {
		t.Fatal(err)
	}

	logger, logs := bufferLogger()
	reopened := f.open(t, logger)
	assertLookup(t, reopened, "alice", "sub-1")
	assertLookup(t, reopened, "ghost", "")
	assertLookup(t, reopened, "mallory", "")
	if _, err := reopened.Get("sub-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(sub-9) err = %v, want ErrNotFound", err)
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 3 {
		t.Fatalf("got %d warnings, want 3:\n%s", n, logs.String())
	}
}

func TestEmptyDataDir(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	assertLookup(t, r, "alice", "")
}

func TestUsernameCollisionMostRecentLoginWins(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-a", "alice", "Alice A")

	logger, logs := bufferLogger()
	r.logger = logger
	mustProvision(t, r, "sub-b", "alice", "Alice B")
	assertLookup(t, r, "alice", "sub-b")
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("no warning logged for the username takeover:\n%s", logs.String())
	}
	a, err := r.Get("sub-a")
	if err != nil {
		t.Fatalf("Get(sub-a): %v", err)
	}
	if a.Username != "" {
		t.Fatalf("old holder keeps username %q, want it released", a.Username)
	}
	if rec := readRecordFile(t, f, "sub-a"); rec.Username != "" || !rec.DefaultsCreated {
		t.Fatalf("old holder on disk = %+v, want no username and defaults_created", rec)
	}

	// The outcome survives a restart.
	reopened := f.open(t, nil)
	assertLookup(t, reopened, "alice", "sub-b")

	// The old holder logs in again and takes the name back.
	mustProvision(t, reopened, "sub-a", "alice", "Alice A")
	assertLookup(t, reopened, "alice", "sub-a")
	b, err := reopened.Get("sub-b")
	if err != nil {
		t.Fatalf("Get(sub-b): %v", err)
	}
	if b.Username != "" {
		t.Fatalf("sub-b keeps username %q, want it released", b.Username)
	}
	assertLookup(t, f.open(t, nil), "alice", "sub-a")
}

// A crash between two writes of a takeover can leave two files that claim
// one username. The newer claim wins when the index is rebuilt.
func TestRestartResolvesDuplicateUsernames(t *testing.T) {
	f := newFixture(t)
	write := func(sub string, updated time.Time) {
		t.Helper()
		writeRecordFile(t, f, record{Sub: sub, Username: "alice", CreatedAt: updated, UpdatedAt: updated, DefaultsCreated: true})
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	write("sub-old", base)
	write("sub-new", base.Add(time.Hour))

	logger, logs := bufferLogger()
	r := f.open(t, logger)
	assertLookup(t, r, "alice", "sub-new")
	old, err := r.Get("sub-old")
	if err != nil {
		t.Fatalf("Get(sub-old): %v", err)
	}
	if old.Username != "" {
		t.Fatalf("older claim keeps username %q, want it released", old.Username)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("no warning logged for the duplicate username:\n%s", logs.String())
	}
	if rec := readRecordFile(t, f, "sub-old"); rec.Username != "" {
		t.Fatalf("older claim keeps username %q on disk, want it released", rec.Username)
	}

	// The winner renames. The old claim must not come back after a restart.
	mustProvision(t, r, "sub-new", "alice2", "Alice")
	reopened := f.open(t, nil)
	assertLookup(t, reopened, "alice", "")
	assertLookup(t, reopened, "alice2", "sub-new")
}

func TestProvisionRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		sub      string
		username string
	}{
		{"empty sub", "", "alice"},
		{"sub with control character", "sub\n1", "alice"},
		{"sub not UTF-8", "sub\xff", "alice"},
		{"sub too long", strings.Repeat("s", 1025), "alice"},
		{"empty username", "sub-1", ""},
		{"username with slash", "sub-1", "al/ice"},
		{"username dot dot", "sub-1", ".."},
		{"username with control character", "sub-1", "al\tice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			r := f.open(t, nil)
			if _, _, err := r.Provision(context.Background(), tt.sub, tt.username, "X"); err == nil {
				t.Fatal("Provision succeeded, want an error")
			}
			entries, err := os.ReadDir(f.dataDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("data dir has %d entries after a rejected Provision, want 0", len(entries))
			}
			assertLookup(t, r, tt.username, "")
		})
	}
}

func TestInvalidSubIsAStoreError(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	_, _, err := r.Provision(context.Background(), "", "alice", "Alice")
	if !errors.Is(err, store.ErrInvalidName) {
		t.Fatalf("err = %v, want store.ErrInvalidName", err)
	}
}

func TestConcurrentProvisionOfOneSub(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)

	const n = 32
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		errs    []error
		got     []Principal
	)
	start := make(chan struct{})
	for range n {
		wg.Go(func() {
			<-start
			p, c, err := r.Provision(context.Background(), "sub-1", "alice", "Alice")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if c {
				created++
			}
			got = append(got, p)
		})
	}
	close(start)
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("Provision errors: %v", errs)
	}
	if created != 1 {
		t.Fatalf("%d calls report created, want 1", created)
	}
	for _, p := range got {
		if p != got[0] {
			t.Fatalf("calls disagree: %+v and %+v", p, got[0])
		}
	}
	assertDefaultCollections(t, f.st, "sub-1")
	assertLookup(t, r, "alice", "sub-1")
}

func TestConcurrentProvisionOfManySubs(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
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
			})
		}
	}
	wg.Wait()
	reopened := f.open(t, nil)
	for _, sub := range subs {
		assertLookup(t, reopened, "user-"+sub, sub)
		assertDefaultCollections(t, f.st, sub)
	}
}

// A crash after principal.json is written but before the default collections
// exist leaves a file whose flag is false. The next Provision heals it.
func TestProvisionHealsMissingDefaultCollections(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRecordFile(t, f, record{Sub: "sub-1", Username: "alice", DisplayName: "Alice", CreatedAt: created, UpdatedAt: created})

	r := f.open(t, nil)
	p, wasCreated := mustProvision(t, r, "sub-1", "alice", "Alice")
	if wasCreated {
		t.Fatal("created = true for an existing principal, want false")
	}
	if !p.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %v, want %v", p.CreatedAt, created)
	}
	assertDefaultCollections(t, f.st, "sub-1")
	if rec := readRecordFile(t, f, "sub-1"); !rec.DefaultsCreated {
		t.Fatal("defaults_created is false on disk after healing")
	}
}

// A file written before the flag existed has no defaults_created field. It
// counts as false: the principal is ensured once and the flag is saved.
func TestFileWithoutFlagIsEnsuredOnce(t *testing.T) {
	f := newFixture(t)
	dir := testOwnerDir(f.dataDir)("sub-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"sub":"sub-1","username":"alice","display_name":"Alice",` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, principalFileName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	mustProvision(t, f.open(t, nil), "sub-1", "alice", "Alice")
	assertDefaultCollections(t, f.st, "sub-1")
	if rec := readRecordFile(t, f, "sub-1"); !rec.DefaultsCreated {
		t.Fatal("defaults_created not saved")
	}

	// Ensured once: a default deleted afterwards is not recreated.
	deleteDefaultCalendar(t, f.st, "sub-1")
	mustProvision(t, f.open(t, nil), "sub-1", "alice", "Alice")
	assertCollectionCount(t, f.st, "sub-1", store.KindCalendar, 0)
}

// A user who deletes a default collection keeps it deleted, also after a
// restart.
func TestDeletedDefaultStaysDeletedAcrossRestart(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	deleteDefaultCalendar(t, f.st, "sub-1")

	mustProvision(t, r, "sub-1", "alice", "Alice")
	assertCollectionCount(t, f.st, "sub-1", store.KindCalendar, 0)

	reopened := f.open(t, nil)
	mustProvision(t, reopened, "sub-1", "alice", "Alice")
	mustProvision(t, reopened, "sub-1", "alice2", "Alice")
	assertCollectionCount(t, f.st, "sub-1", store.KindCalendar, 0)
	assertCollectionCount(t, f.st, "sub-1", store.KindAddressBook, 1)
}

// blockingStore blocks the first ListCollections for one owner until it is
// released, so a test can run code while Provision is ensuring defaults.
type blockingStore struct {
	store.Store
	owner   string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingStore) ListCollections(ctx context.Context, owner string, kind store.Kind) ([]store.Collection, error) {
	if owner == s.owner {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return s.Store.ListCollections(ctx, owner, kind)
}

// A takeover of a username while its old holder is still ensuring its
// defaults must not be undone when the holder saves its flag.
func TestFlagWriteDoesNotRestoreReleasedUsername(t *testing.T) {
	st := &blockingStore{Store: memory.New(), owner: "sub-a", entered: make(chan struct{}), release: make(chan struct{})}
	f := fixture{dataDir: t.TempDir(), st: st}
	r := f.open(t, nil)

	done := make(chan error, 1)
	go func() {
		_, _, err := r.Provision(context.Background(), "sub-a", "alice", "Alice A")
		done <- err
	}()
	<-st.entered
	mustProvision(t, r, "sub-b", "alice", "Alice B")
	close(st.release)
	if err := <-done; err != nil {
		t.Fatalf("Provision(sub-a): %v", err)
	}

	assertLookup(t, r, "alice", "sub-b")
	a, err := r.Get("sub-a")
	if err != nil {
		t.Fatalf("Get(sub-a): %v", err)
	}
	if a.Username != "" {
		t.Fatalf("sub-a has username %q again, want it released", a.Username)
	}
	rec := readRecordFile(t, f, "sub-a")
	if rec.Username != "" || !rec.DefaultsCreated {
		t.Fatalf("sub-a on disk = %+v, want no username and defaults_created", rec)
	}
	assertLookup(t, f.open(t, nil), "alice", "sub-b")
}

// failingStore fails to list collections until it is told otherwise.
type failingStore struct {
	store.Store
	mu   sync.Mutex
	fail bool
}

func (s *failingStore) ListCollections(ctx context.Context, owner string, kind store.Kind) ([]store.Collection, error) {
	s.mu.Lock()
	fail := s.fail
	s.mu.Unlock()
	if fail {
		return nil, errors.New("disk on fire")
	}
	return s.Store.ListCollections(ctx, owner, kind)
}

func (s *failingStore) setFail(v bool) {
	s.mu.Lock()
	s.fail = v
	s.mu.Unlock()
}

func TestProvisionRetriesDefaultCollectionsAfterAnError(t *testing.T) {
	st := &failingStore{Store: memory.New(), fail: true}
	f := fixture{dataDir: t.TempDir(), st: st}
	r := f.open(t, nil)

	if _, _, err := r.Provision(context.Background(), "sub-1", "alice", "Alice"); err == nil {
		t.Fatal("Provision succeeded with a failing store, want an error")
	}
	if rec := readRecordFile(t, f, "sub-1"); rec.DefaultsCreated {
		t.Fatal("defaults_created is true on disk after a failed ensure")
	}
	st.setFail(false)
	_, created := mustProvision(t, r, "sub-1", "alice", "Alice")
	if created {
		t.Fatal("created = true, want false: the principal file was already written")
	}
	assertDefaultCollections(t, st.Store, "sub-1")
	if rec := readRecordFile(t, f, "sub-1"); !rec.DefaultsCreated {
		t.Fatal("defaults_created is false on disk after the retry")
	}
}

// lockingStore makes the owner directory read-only while defaults are being
// ensured, so that saving the flag afterwards fails.
type lockingStore struct {
	store.Store
	dir    string
	locked bool
}

func (s *lockingStore) ListCollections(ctx context.Context, owner string, kind store.Kind) ([]store.Collection, error) {
	if !s.locked {
		s.locked = true
		if err := os.Chmod(s.dir, 0o500); err != nil {
			return nil, err
		}
	}
	return s.Store.ListCollections(ctx, owner, kind)
}

// When the defaults exist but the flag cannot be saved, Provision succeeds
// and logs it. The flag stays false, so the next call ensures again (a no-op)
// and saves it.
func TestFlagWriteFailureIsRetried(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dataDir := t.TempDir()
	dir := testOwnerDir(dataDir)("sub-1")
	st := &lockingStore{Store: memory.New(), dir: dir}
	f := fixture{dataDir: dataDir, st: st}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	logger, logs := bufferLogger()
	r := f.open(t, logger)
	if _, created := mustProvision(t, r, "sub-1", "alice", "Alice"); !created {
		t.Fatal("created = false, want true")
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("no warning logged for the failed flag write:\n%s", logs.String())
	}
	if rec := readRecordFile(t, f, "sub-1"); rec.DefaultsCreated {
		t.Fatal("defaults_created is true on disk, want false")
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mustProvision(t, r, "sub-1", "alice", "Alice")
	if rec := readRecordFile(t, f, "sub-1"); !rec.DefaultsCreated {
		t.Fatal("defaults_created not saved on the retry")
	}
	assertDefaultCollections(t, st.Store, "sub-1")
}

func TestPrincipalFileIsNeverPartial(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	mustProvision(t, r, "sub-1", "alice", "Alice")
	path := f.principalFile("sub-1")

	stop := make(chan struct{})
	readerDone := make(chan error, 1)
	go func() {
		reads := 0
		for {
			select {
			case <-stop:
				if reads == 0 {
					readerDone <- errors.New("reader never ran")
					return
				}
				readerDone <- nil
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				readerDone <- err
				return
			}
			var rec record
			if err := json.Unmarshal(data, &rec); err != nil {
				readerDone <- err
				return
			}
			if rec.Sub != "sub-1" || !strings.HasPrefix(rec.DisplayName, "Alice") {
				readerDone <- errors.New("unexpected content: " + string(data))
				return
			}
			reads++
		}
	}()

	// A long display name makes a torn write easy to catch.
	long := strings.Repeat("x", 64<<10)
	for i := range 200 {
		name := "Alice"
		if i%2 == 0 {
			name += long
		}
		if _, _, err := r.Provision(context.Background(), "sub-1", "alice", name); err != nil {
			close(stop)
			t.Fatalf("Provision: %v", err)
		}
	}
	close(stop)
	if err := <-readerDone; err != nil {
		t.Fatalf("reader saw a partial file: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != principalFileName {
			t.Fatalf("leftover file %q next to the principal file", e.Name())
		}
	}
}

// With the real file store the principal file sits in the owner's directory
// next to the store's own data, and a restart finds it there.
func TestWithFileStore(t *testing.T) {
	dir := t.TempDir()
	fs, err := filestore.Open(dir)
	if err != nil {
		t.Fatalf("filestore.Open: %v", err)
	}
	r, err := NewRegistry(dir, fs, fs.OwnerDir, discardLogger())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	mustProvision(t, r, "User@Example.com", "alice", "Alice")
	if _, err := os.Stat(filepath.Join(fs.OwnerDir("User@Example.com"), principalFileName)); err != nil {
		t.Fatalf("principal file: %v", err)
	}
	assertDefaultCollections(t, fs, "User@Example.com")
	if err := fs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	fs, err = filestore.Open(dir)
	if err != nil {
		t.Fatalf("filestore.Open again: %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	r, err = NewRegistry(dir, fs, fs.OwnerDir, discardLogger())
	if err != nil {
		t.Fatalf("NewRegistry again: %v", err)
	}
	assertLookup(t, r, "alice", "User@Example.com")
	assertDefaultCollections(t, fs, "User@Example.com")
}

// syncRecorder records directory fsyncs and fails those of one directory.
type syncRecorder struct {
	mu     sync.Mutex
	dirs   []string
	failOn string
}

func (s *syncRecorder) sync(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirs = append(s.dirs, filepath.Clean(dir))
	if s.failOn != "" && filepath.Clean(dir) == filepath.Clean(s.failOn) {
		return errors.New("fsync failed")
	}
	return nil
}

func (s *syncRecorder) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	dirs := s.dirs
	s.dirs = nil
	return dirs
}

// Creating an owner directory flushes its parent, so the new directory
// survives a power loss together with the file in it.
func TestNewDirectoriesAreSynced(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	rec := &syncRecorder{}
	r.syncDir = rec.sync
	owners := filepath.Join(f.dataDir, "owners")
	dirA := testOwnerDir(f.dataDir)("sub-a")
	dirB := testOwnerDir(f.dataDir)("sub-b")

	mustProvision(t, r, "sub-a", "alice", "Alice")
	got := rec.take()
	for _, want := range []string{filepath.Clean(f.dataDir), owners, dirA} {
		if !slices.Contains(got, want) {
			t.Fatalf("first owner: synced %v, want %s among them", got, want)
		}
	}

	mustProvision(t, r, "sub-b", "bob", "Bob")
	got = rec.take()
	if !slices.Contains(got, owners) || !slices.Contains(got, dirB) {
		t.Fatalf("second owner: synced %v, want %s and %s", got, owners, dirB)
	}
	if slices.Contains(got, filepath.Clean(f.dataDir)) {
		t.Fatalf("second owner: synced the data directory again: %v", got)
	}

	mustProvision(t, r, "sub-a", "alice2", "Alice")
	got = rec.take()
	if slices.Contains(got, owners) {
		t.Fatalf("existing owner: synced %s again: %v", owners, got)
	}
	if !slices.Contains(got, dirA) {
		t.Fatalf("existing owner: synced %v, want %s", got, dirA)
	}
}

// When the directory fsync after the rename fails, the new file is already
// in place. Provision logs the failure and succeeds, so that memory matches
// what is on disk.
func TestDirSyncFailureAfterRename(t *testing.T) {
	f := newFixture(t)
	logger, logs := bufferLogger()
	r := f.open(t, logger)
	mustProvision(t, r, "sub-1", "alice", "Alice")

	r.syncDir = (&syncRecorder{failOn: testOwnerDir(f.dataDir)("sub-1")}).sync
	p, _ := mustProvision(t, r, "sub-1", "alice2", "Alice")
	if p.Username != "alice2" {
		t.Fatalf("Username = %q, want alice2", p.Username)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("no warning logged for the failed fsync:\n%s", logs.String())
	}
	assertLookup(t, r, "alice2", "sub-1")
	assertLookup(t, r, "alice", "")
	if rec := readRecordFile(t, f, "sub-1"); rec.Username != "alice2" {
		t.Fatalf("username on disk = %q, want alice2", rec.Username)
	}
}

// A crash between creating the temporary file and the rename leaves the
// temporary file behind. The next start removes it.
func TestRestartRemovesLeftoverTempFiles(t *testing.T) {
	f := newFixture(t)
	mustProvision(t, f.open(t, nil), "sub-1", "alice", "Alice")
	dir := testOwnerDir(f.dataDir)("sub-1")
	orphanDir := filepath.Join(f.dataDir, "owners", "orphan")
	if err := os.MkdirAll(orphanDir, 0o700); err != nil {
		t.Fatal(err)
	}
	leftovers := []string{
		filepath.Join(dir, ".principal.json.tmp-123"),
		filepath.Join(orphanDir, ".principal.json.tmp-456"),
	}
	other := filepath.Join(dir, "notes.txt")
	for _, path := range append(leftovers, other) {
		if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	r := f.open(t, nil)
	assertLookup(t, r, "alice", "sub-1")
	for _, path := range leftovers {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists (err %v)", path, err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
}
