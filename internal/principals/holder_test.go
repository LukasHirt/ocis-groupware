package principals

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// holderFixture is a registry on a fake clock. holder owns the username
// "alice" and has an app password. State helpers put the holder into the
// states the username rule looks at.
type holderFixture struct {
	appFixture
	logs   *syncBuffer
	secret string
}

func newHolderFixture(t *testing.T, lease time.Duration) holderFixture {
	t.Helper()
	f := newFixture(t)
	clock := newFakeClock()
	logger, logs := bufferLogger()
	r := f.openWith(t, logger, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost), WithIdentityMaxAge(lease))
	mustProvision(t, r, "sub-holder", "alice", "Alice Holder")
	if err := r.SetIdentity(context.Background(), "sub-holder", Identity{OCISID: "id-holder", At: clock.Now()}); err != nil {
		t.Fatal(err)
	}
	_, secret := mustIssue(t, r, "sub-holder", "phone", 100*testLease)
	return holderFixture{appFixture: appFixture{fixture: f, clock: clock, r: r}, logs: logs, secret: secret}
}

func TestUsernameMovesOnlyFromAHolderWhoCannotUseIt(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		lease     time.Duration
		setup     func(t *testing.T, f holderFixture)
		wantMoved bool
	}{
		{"enabled and fresh", testLease, func(*testing.T, holderFixture) {}, false},
		{"disabled", testLease, func(t *testing.T, f holderFixture) {
			if _, err := f.r.SetDisabled(ctx, "sub-holder", true); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"stale", testLease, func(_ *testing.T, f holderFixture) { f.clock.Add(testLease) }, true},
		{"just inside the lease", testLease, func(_ *testing.T, f holderFixture) { f.clock.Add(testLease - time.Second) }, false},
		{"lease off and enabled", 0, func(_ *testing.T, f holderFixture) { f.clock.Add(100 * testLease) }, false},
		{"lease off and disabled", 0, func(t *testing.T, f holderFixture) {
			if _, err := f.r.SetDisabled(ctx, "sub-holder", true); err != nil {
				t.Fatal(err)
			}
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newHolderFixture(t, tt.lease)
			tt.setup(t, f)

			p, _, err := f.r.Provision(ctx, "sub-new", "alice", "Alice New")
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantMoved {
				if p.Username != "alice" || p.UsernameConflict {
					t.Fatalf("newcomer = %+v, want the username", p)
				}
				assertLookup(t, f.r, "alice", "sub-new")
				return
			}
			if p.Username != "" || !p.UsernameConflict {
				t.Fatalf("newcomer = %+v, want no username and a conflict", p)
			}
			assertLookup(t, f.r, "alice", "sub-holder")
			if rec := readRecordFile(t, f.fixture, "sub-new"); rec.Username != "" || rec.DisplayName != "Alice New" {
				t.Fatalf("newcomer on disk = %+v", rec)
			}
		})
	}
}

func TestImportedHolderKeepsItsUsernameWhileFresh(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name      string
		age       time.Duration
		wantMoved bool
	}{{"fresh", time.Hour, false}, {"stale", testLease, true}} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			clock := newFakeClock()
			r := f.openWith(t, nil, WithClock(clock.Now), WithBcryptCost(bcrypt.MinCost), WithIdentityMaxAge(testLease))
			if _, _, err := r.Import(ctx, "migrated-1", "dave", "Dave", Identity{OCISID: "ocis-dave", At: clock.Now()}); err != nil {
				t.Fatal(err)
			}
			clock.Add(tt.age)
			p, _, err := r.Provision(ctx, "sub-mallory", "dave", "Mallory")
			if err != nil {
				t.Fatal(err)
			}
			if moved := p.Username == "dave"; moved != tt.wantMoved {
				t.Fatalf("moved = %v, want %v (newcomer %+v)", moved, tt.wantMoved, p)
			}
		})
	}
}

func TestHolderKeepsWorkingAfterARefusedClaim(t *testing.T) {
	f := newHolderFixture(t, testLease)
	if _, _, err := f.r.Provision(context.Background(), "sub-new", "alice", "Alice New"); err != nil {
		t.Fatal(err)
	}
	mustVerify(t, f.r, "alice", f.secret, "sub-holder")
	// The newcomer has no username, so nothing signs in as it.
	if _, err := f.r.VerifyBasic(context.Background(), "", f.secret); err == nil {
		t.Fatal("an empty username signed in")
	}
}

// The claim is tried again at every sign-in but the file is written only when
// the outcome changes.
func TestRefusedClaimIsRetriedAndWritesOnce(t *testing.T) {
	ctx := context.Background()
	f := newHolderFixture(t, testLease)
	if _, _, err := f.r.Provision(ctx, "sub-new", "alice", "Alice New"); err != nil {
		t.Fatal(err)
	}
	var writes int
	// Count the principal file's modification by its content hash: the
	// updated_at of a rewritten record would move with the clock.
	before := readRecordFile(t, f.fixture, "sub-new")
	for i := 0; i < 5; i++ {
		f.clock.Add(time.Minute)
		p, _, err := f.r.Provision(ctx, "sub-new", "alice", "Alice New")
		if err != nil {
			t.Fatal(err)
		}
		if !p.UsernameConflict {
			t.Fatalf("sign-in %d: no conflict reported", i)
		}
		if got := readRecordFile(t, f.fixture, "sub-new"); !got.UpdatedAt.Equal(before.UpdatedAt) {
			writes++
		}
	}
	if writes != 0 {
		t.Fatalf("the principal file was rewritten %d times by refused claims", writes)
	}

	// The admin disables the holder, and the newcomer's next sign-in takes the name.
	if _, err := f.r.SetDisabled(ctx, "sub-holder", true); err != nil {
		t.Fatal(err)
	}
	p, _, err := f.r.Provision(ctx, "sub-new", "alice", "Alice New")
	if err != nil {
		t.Fatal(err)
	}
	if p.Username != "alice" || p.UsernameConflict {
		t.Fatalf("after the holder was disabled: %+v", p)
	}
	assertLookup(t, f.r, "alice", "sub-new")
	assertLookup(t, f.open(t, nil), "alice", "sub-new")
}

func TestRefusedClaimIsLoggedWithBothOwnersAndNotEveryTime(t *testing.T) {
	ctx := context.Background()
	f := newHolderFixture(t, testLease)
	for i := 0; i < 4; i++ {
		if _, _, err := f.r.Provision(ctx, "sub-new", "alice", "Alice New"); err != nil {
			t.Fatal(err)
		}
	}
	out := f.logs.String()
	if n := strings.Count(out, "username is held by another principal"); n != 1 {
		t.Fatalf("%d log lines for four refusals, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "sub-holder") || !strings.Contains(out, "sub-new") || !strings.Contains(out, "level=WARN") {
		t.Fatalf("log lacks both owners or the level:\n%s", out)
	}
	f.clock.Add(time.Hour)
	if _, _, err := f.r.Provision(ctx, "sub-new", "alice", "Alice New"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(f.logs.String(), "username is held by another principal"); n != 2 {
		t.Fatalf("%d log lines after an hour, want 2", n)
	}
}

// A user who renames in oCIS into a name that an active user holds loses the
// old name and gets none.
func TestRenameIntoAHeldNameReleasesTheOldOne(t *testing.T) {
	ctx := context.Background()
	f := newHolderFixture(t, testLease)
	mustProvision(t, f.r, "sub-bob", "bob", "Bob")
	p, _, err := f.r.Provision(ctx, "sub-bob", "alice", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if p.Username != "" || !p.UsernameConflict {
		t.Fatalf("renamed principal = %+v", p)
	}
	assertLookup(t, f.r, "bob", "")
	assertLookup(t, f.r, "alice", "sub-holder")
}
