package provision

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

type fakeGraph struct {
	mu     sync.Mutex
	calls  int
	tokens []string
	user   ocisgraph.User
	err    error
	block  chan struct{}
}

func (f *fakeGraph) Me(_ context.Context, token string) (ocisgraph.User, error) {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.tokens = append(f.tokens, token)
	return f.user, f.err
}

func (f *fakeGraph) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type clock struct{ t atomic.Int64 }

func (c *clock) now() time.Time      { return time.Unix(c.t.Load(), 0).UTC() }
func (c *clock) add(d time.Duration) { c.t.Add(int64(d / time.Second)) }

type idFixture struct {
	reg   *principals.Registry
	graph *fakeGraph
	ids   *Identities
	clock *clock
	logs  *bytes.Buffer
}

func newIDFixture(t *testing.T) idFixture {
	t.Helper()
	reg, _, _, _ := fixture(t)
	if _, _, err := reg.Provision(context.Background(), "sub-1", "einstein", "Albert"); err != nil {
		t.Fatal(err)
	}
	g := &fakeGraph{user: ocisgraph.User{ID: "u-1", Groups: []ocisgraph.Group{{ID: "g-1", Name: "users"}, {ID: "g-2", Name: "physics"}}}}
	c := &clock{}
	c.t.Store(1_800_000_000)
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return idFixture{reg: reg, graph: g, ids: NewIdentities(reg, g, logger, WithClock(c.now)), clock: c, logs: logs}
}

func (f idFixture) identity(t *testing.T) principals.Identity {
	t.Helper()
	id, err := f.reg.Identity("sub-1")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRefreshStoresTheSnapshot(t *testing.T) {
	f := newIDFixture(t)
	f.ids.Refresh(context.Background(), "sub-1", "tok-1")
	id := f.identity(t)
	if id.OCISID != "u-1" || len(id.Groups) != 2 || id.Groups[1] != (principals.Group{ID: "g-2", Name: "physics"}) || !id.At.Equal(f.clock.now()) {
		t.Fatalf("identity = %+v", id)
	}
	if f.graph.tokens[0] != "tok-1" {
		t.Fatalf("token = %q", f.graph.tokens[0])
	}
}

func TestRefreshIsThrottled(t *testing.T) {
	f := newIDFixture(t)
	ctx := context.Background()
	f.ids.Refresh(ctx, "sub-1", "s3cret-tok")
	f.clock.add(4*time.Minute + 59*time.Second)
	f.ids.Refresh(ctx, "sub-1", "s3cret-tok")
	if n := f.graph.count(); n != 1 {
		t.Fatalf("%d lookups inside the interval, want 1", n)
	}
	f.clock.add(time.Second)
	f.graph.user.Groups = nil
	f.ids.Refresh(ctx, "sub-1", "s3cret-tok")
	if n := f.graph.count(); n != 2 {
		t.Fatalf("%d lookups after the interval, want 2", n)
	}
	if id := f.identity(t); len(id.Groups) != 0 {
		t.Fatalf("groups = %+v, want the new, empty list", id.Groups)
	}
}

func TestRefreshThrottleSurvivesARestart(t *testing.T) {
	f := newIDFixture(t)
	f.ids.Refresh(context.Background(), "sub-1", "t")
	again := NewIdentities(f.reg, f.graph, slog.New(slog.DiscardHandler), WithClock(f.clock.now))
	f.clock.add(time.Minute)
	again.Refresh(context.Background(), "sub-1", "t")
	if n := f.graph.count(); n != 1 {
		t.Fatalf("%d lookups, want 1: the snapshot in the file is recent", n)
	}
}

func TestRefreshFailureKeepsTheSnapshotAndIsThrottled(t *testing.T) {
	for name, err := range map[string]error{
		"unauthorized": ocisgraph.ErrUnauthorized,
		"server":       errors.New("ocisgraph: status 500"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newIDFixture(t)
			ctx := context.Background()
			f.ids.Refresh(ctx, "sub-1", "s3cret-tok")
			before := f.identity(t)

			f.clock.add(10 * time.Minute)
			f.graph.err = err
			f.graph.user = ocisgraph.User{ID: "other"}
			f.ids.Refresh(ctx, "sub-1", "s3cret-tok")
			if got := f.identity(t); got.OCISID != before.OCISID || len(got.Groups) != 2 || !got.At.Equal(before.At) {
				t.Fatalf("snapshot changed by a failure: %+v", got)
			}
			if !strings.Contains(f.logs.String(), "level=WARN") && !strings.Contains(f.logs.String(), "level=INFO") {
				t.Errorf("failure not logged: %q", f.logs.String())
			}
			// The failed attempt counts: no new call a minute later.
			f.clock.add(time.Minute)
			f.ids.Refresh(ctx, "sub-1", "s3cret-tok")
			if n := f.graph.count(); n != 2 {
				t.Fatalf("%d lookups, want 2", n)
			}
			// A failure never logs the token.
			if strings.Contains(f.logs.String(), "s3cret") {
				t.Errorf("log mentions the token: %q", f.logs.String())
			}
		})
	}
}

func TestRefreshFirstSignInFailureLeavesNoSnapshot(t *testing.T) {
	f := newIDFixture(t)
	f.graph.err = errors.New("boom")
	f.ids.Refresh(context.Background(), "sub-1", "t")
	if id := f.identity(t); id.OCISID != "" || !id.At.IsZero() {
		t.Fatalf("identity = %+v, want none", id)
	}
}

func TestRefreshDisabledPrincipalIsUntouched(t *testing.T) {
	f := newIDFixture(t)
	if _, err := f.reg.SetDisabled(context.Background(), "sub-1", true); err != nil {
		t.Fatal(err)
	}
	f.ids.Refresh(context.Background(), "sub-1", "t")
	if id := f.identity(t); id.OCISID != "" {
		t.Fatalf("identity = %+v, want none", id)
	}
}

func TestRefreshConcurrentSignInsLookUpOnce(t *testing.T) {
	f := newIDFixture(t)
	f.graph.block = make(chan struct{})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { f.ids.Refresh(context.Background(), "sub-1", "t") })
	}
	time.Sleep(50 * time.Millisecond)
	close(f.graph.block)
	wg.Wait()
	if n := f.graph.count(); n != 1 {
		t.Fatalf("%d lookups, want 1", n)
	}
}

func TestRefreshUnknownPrincipalAndNilAreNoOps(t *testing.T) {
	f := newIDFixture(t)
	f.ids.Refresh(context.Background(), "nobody", "t")
	var none *Identities
	none.Refresh(context.Background(), "sub-1", "t")
	if n := f.graph.count(); n > 1 {
		t.Fatalf("%d lookups", n)
	}
}

func TestRefreshWithoutTokenDoesNothing(t *testing.T) {
	f := newIDFixture(t)
	f.ids.Refresh(context.Background(), "sub-1", "")
	if f.graph.count() != 0 {
		t.Fatal("looked up without a token")
	}
}

func TestSignInRefreshesTheIdentity(t *testing.T) {
	f := newIDFixture(t)
	claims := auth.Claims{Subject: "sub-1", PreferredUsername: "einstein"}
	p, status, _ := f.ids.SignIn(context.Background(), claims, "tok", slog.New(slog.DiscardHandler))
	if status != 0 || p.Sub != "sub-1" {
		t.Fatalf("status %d, principal %+v", status, p)
	}
	if f.identity(t).OCISID != "u-1" {
		t.Fatal("no snapshot after SignIn")
	}
	// A refused sign-in looks nothing up.
	if _, err := f.reg.SetDisabled(context.Background(), "sub-1", true); err != nil {
		t.Fatal(err)
	}
	f.clock.add(time.Hour)
	calls := f.graph.count()
	if _, status, _ := f.ids.SignIn(context.Background(), claims, "tok", slog.New(slog.DiscardHandler)); status == 0 {
		t.Fatal("disabled principal signed in")
	}
	if f.graph.count() != calls {
		t.Fatal("looked up for a refused sign-in")
	}
}

// leaseFixture is a registry with a 720h identity lease that shares its clock
// with the Identities, and an app password for einstein.
func newLeaseIDFixture(t *testing.T) (idFixture, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	c := &clock{}
	c.t.Store(1_800_000_000)
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, slog.New(slog.DiscardHandler),
		principals.WithBcryptCost(bcrypt.MinCost), principals.WithClock(c.now), principals.WithIdentityMaxAge(720*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Provision(context.Background(), "sub-1", "einstein", "Albert"); err != nil {
		t.Fatal(err)
	}
	g := &fakeGraph{user: ocisgraph.User{ID: "u-1"}}
	f := idFixture{reg: reg, graph: g, ids: NewIdentities(reg, g, slog.New(slog.DiscardHandler), WithClock(c.now)), clock: c, logs: &bytes.Buffer{}}
	f.ids.Refresh(context.Background(), "sub-1", "tok")
	_, secret, err := reg.IssueAppPassword(context.Background(), "sub-1", "phone", 5*720*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return f, secret
}

func TestRefreshRenewsTheIdentityLease(t *testing.T) {
	f, secret := newLeaseIDFixture(t)
	ctx := context.Background()
	f.clock.add(720 * time.Hour)
	if _, err := f.reg.VerifyBasic(ctx, "einstein", secret); !errors.Is(err, principals.ErrIdentityStale) {
		t.Fatalf("after the lease: %v, want ErrIdentityStale", err)
	}
	f.ids.Refresh(ctx, "sub-1", "tok")
	if _, err := f.reg.VerifyBasic(ctx, "einstein", secret); err != nil {
		t.Fatalf("after a refresh: %v", err)
	}
}

func TestFailedRefreshDoesNotRenewTheIdentityLease(t *testing.T) {
	f, secret := newLeaseIDFixture(t)
	ctx := context.Background()
	f.clock.add(720 * time.Hour)
	f.graph.err = errors.New("ocisgraph: status 500")
	f.ids.Refresh(ctx, "sub-1", "tok")
	if _, err := f.reg.VerifyBasic(ctx, "einstein", secret); !errors.Is(err, principals.ErrIdentityStale) {
		t.Fatalf("after a failed refresh: %v, want ErrIdentityStale", err)
	}
}
