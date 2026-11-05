package principals

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mustSetDisabled flips the flag and returns the principal.
func mustSetDisabled(t *testing.T, r *Registry, sub string, disabled bool) Principal {
	t.Helper()
	p, err := r.SetDisabled(context.Background(), sub, disabled)
	if err != nil {
		t.Fatalf("SetDisabled(%q, %v): %v", sub, disabled, err)
	}
	if p.Disabled != disabled {
		t.Fatalf("SetDisabled(%q, %v) returned Disabled=%v", sub, disabled, p.Disabled)
	}
	return p
}

func TestDisableRefusesBasic(t *testing.T) {
	t.Run("uncached", func(t *testing.T) {
		f := newAppFixture(t)
		_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
		mustVerify(t, f.r, "alice", secret, "sub-1")
		f.clock.Add(10 * time.Minute)

		mustSetDisabled(t, f.r, "sub-1", true)
		assertVerifyErr(t, f.r, "alice", secret, ErrDisabled)
		if n := f.r.cache.len(); n != 0 {
			t.Fatalf("cache has %d entries after a refused check, want 0", n)
		}
	})
	t.Run("cached a moment before", func(t *testing.T) {
		f := newAppFixture(t)
		_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
		mustVerify(t, f.r, "alice", secret, "sub-1")
		if n := f.r.cache.len(); n != 1 {
			t.Fatalf("cache has %d entries, want 1", n)
		}

		mustSetDisabled(t, f.r, "sub-1", true)
		f.clock.Add(time.Second)
		assertVerifyErr(t, f.r, "alice", secret, ErrDisabled)
	})
	t.Run("other principals keep working", func(t *testing.T) {
		f := newAppFixture(t)
		mustProvision(t, f.r, "sub-2", "bob", "Bob")
		_, bobSecret := mustIssue(t, f.r, "sub-2", "Phone", 0)
		mustSetDisabled(t, f.r, "sub-1", true)
		mustVerify(t, f.r, "bob", bobSecret, "sub-2")
	})
}

func TestDisableRefusesProvision(t *testing.T) {
	f := newAppFixture(t)
	mustSetDisabled(t, f.r, "sub-1", true)
	before, err := os.ReadFile(f.principalFile("sub-1"))
	if err != nil {
		t.Fatal(err)
	}

	for name, names := range map[string][2]string{
		"same names":    {"alice", "Alice"},
		"changed names": {"alice-2", "Alice Two"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := f.r.Provision(context.Background(), "sub-1", names[0], names[1])
			if !errors.Is(err, ErrDisabled) {
				t.Fatalf("Provision err = %v, want ErrDisabled", err)
			}
			after, err := os.ReadFile(f.principalFile("sub-1"))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Errorf("principal file changed:\n%s\nwas:\n%s", after, before)
			}
			p, err := f.r.Get("sub-1")
			if err != nil || p.Username != "alice" || p.DisplayName != "Alice" || !p.Disabled {
				t.Errorf("Get = %+v, %v", p, err)
			}
			assertLookup(t, f.r, "alice", "sub-1")
			assertLookup(t, f.r, "alice-2", "")
		})
	}
}

func TestDisableSurvivesRestart(t *testing.T) {
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	mustSetDisabled(t, f.r, "sub-1", true)

	var raw map[string]any
	data, err := os.ReadFile(f.principalFile("sub-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["disabled"] != true {
		t.Fatalf("principal file holds disabled=%v, want true:\n%s", raw["disabled"], data)
	}

	r2 := f.reopen(t, nil)
	assertVerifyErr(t, r2, "alice", secret, ErrDisabled)
	if _, _, err := r2.Provision(context.Background(), "sub-1", "alice", "Alice"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Provision after restart err = %v, want ErrDisabled", err)
	}
	p, err := r2.Get("sub-1")
	if err != nil || !p.Disabled {
		t.Fatalf("Get after restart = %+v, %v, want Disabled", p, err)
	}
}

// A principal file from before the flag existed has no "disabled" key. It
// counts as enabled.
func TestFileWithoutDisabledFieldIsEnabled(t *testing.T) {
	f := newFixture(t)
	clock := newFakeClock()
	now := clock.Now()
	old := `{"sub":"sub-1","username":"alice","display_name":"Alice","created_at":"` + now.Format(time.RFC3339Nano) +
		`","updated_at":"` + now.Format(time.RFC3339Nano) + `","defaults_created":true}` + "\n"
	dir := testOwnerDir(f.dataDir)("sub-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, principalFileName), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	r := f.openWith(t, nil, WithClock(clock.Now))
	p, err := r.Get("sub-1")
	if err != nil || p.Disabled {
		t.Fatalf("Get = %+v, %v, want enabled", p, err)
	}
	_, secret := mustIssue(t, r, "sub-1", "Phone", 0)
	mustVerify(t, r, "alice", secret, "sub-1")
	if _, _, err := r.Provision(context.Background(), "sub-1", "alice", "Alice"); err != nil {
		t.Fatalf("Provision: %v", err)
	}
}

func TestEnableRestoresAccess(t *testing.T) {
	f := newAppFixture(t)
	ap, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	mustSetDisabled(t, f.r, "sub-1", true)
	assertVerifyErr(t, f.r, "alice", secret, ErrDisabled)

	p := mustSetDisabled(t, f.r, "sub-1", false)
	if p.Username != "alice" || p.DisplayName != "Alice" {
		t.Fatalf("principal after enable = %+v", p)
	}
	mustVerify(t, f.r, "alice", secret, "sub-1")
	list, err := f.r.ListAppPasswords("sub-1")
	if err != nil || len(list) != 1 || list[0].ID != ap.ID {
		t.Fatalf("ListAppPasswords = %+v, %v, want the password kept", list, err)
	}
	if _, _, err := f.r.Provision(context.Background(), "sub-1", "alice", "Alice"); err != nil {
		t.Fatalf("Provision after enable: %v", err)
	}
	if p, err := f.r.Get("sub-1"); err != nil || p.Disabled {
		t.Fatalf("Get after enable = %+v, %v", p, err)
	}
}

// Disabling and enabling leave updated_at alone: that time decides which of
// two files wins a username, and an admin action must not move a name.
func TestDisableLeavesUpdatedAt(t *testing.T) {
	f := newAppFixture(t)
	before := readRecordFile(t, f.fixture, "sub-1")
	f.clock.Add(time.Hour)

	mustSetDisabled(t, f.r, "sub-1", true)
	rec := readRecordFile(t, f.fixture, "sub-1")
	if !rec.Disabled {
		t.Fatal("file not disabled")
	}
	if !rec.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("disable moved updated_at from %v to %v", before.UpdatedAt, rec.UpdatedAt)
	}
	f.clock.Add(time.Hour)
	mustSetDisabled(t, f.r, "sub-1", false)
	rec = readRecordFile(t, f.fixture, "sub-1")
	if rec.Disabled {
		t.Fatal("file still disabled")
	}
	if !rec.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("enable moved updated_at from %v to %v", before.UpdatedAt, rec.UpdatedAt)
	}
	if rec.Username != before.Username || rec.DisplayName != before.DisplayName ||
		!rec.CreatedAt.Equal(before.CreatedAt) || rec.DefaultsCreated != before.DefaultsCreated {
		t.Errorf("record changed beyond the flag: %+v, was %+v", rec, before)
	}
}

func TestDisableIsIdempotent(t *testing.T) {
	f := newAppFixture(t)
	mustSetDisabled(t, f.r, "sub-1", true)
	p := mustSetDisabled(t, f.r, "sub-1", true)
	if !p.Disabled {
		t.Fatalf("second disable returned %+v", p)
	}
	mustSetDisabled(t, f.r, "sub-1", false)
	p = mustSetDisabled(t, f.r, "sub-1", false)
	if p.Disabled {
		t.Fatalf("second enable returned %+v", p)
	}
}

func TestDisableUnknownSub(t *testing.T) {
	f := newAppFixture(t)
	for _, disabled := range []bool{true, false} {
		if _, err := f.r.SetDisabled(context.Background(), "sub-nobody", disabled); !errors.Is(err, ErrNotFound) {
			t.Fatalf("SetDisabled(unknown, %v) err = %v, want ErrNotFound", disabled, err)
		}
	}
	if _, err := os.Stat(testOwnerDir(f.dataDir)("sub-nobody")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner directory created for an unknown sub: %v", err)
	}
	if _, err := f.r.SetDisabled(context.Background(), "", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetDisabled(\"\") err = %v, want ErrNotFound", err)
	}
}

// A disabled principal's valid app password costs exactly one bcrypt
// compare, like a wrong password, so timing does not tell the two apart.
func TestDisableTimingMatchesWrongPassword(t *testing.T) {
	f := newAppFixture(t)
	_, secret := mustIssue(t, f.r, "sub-1", "Phone", 0)
	wrong := wrongPassword(t, f.r, "sub-1")
	mustSetDisabled(t, f.r, "sub-1", true)
	compares := countCompares(f.r)

	for _, tt := range []struct {
		name, password string
		want           error
	}{
		{"valid password of a disabled principal", secret, ErrDisabled},
		{"wrong password of a disabled principal", wrong, ErrInvalidCredentials},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for range 3 {
				before := compares.Load()
				assertVerifyErr(t, f.r, "alice", tt.password, tt.want)
				if n := compares.Load() - before; n != 1 {
					t.Fatalf("%d compares, want 1", n)
				}
			}
		})
	}
	if n := f.r.cache.len(); n != 0 {
		t.Fatalf("cache has %d entries, want 0", n)
	}
}

func TestDisabledPrincipalCannotManageAppPasswords(t *testing.T) {
	f := newAppFixture(t)
	ap, _ := mustIssue(t, f.r, "sub-1", "Phone", 0)
	mustSetDisabled(t, f.r, "sub-1", true)
	ctx := context.Background()

	if _, _, err := f.r.IssueAppPassword(ctx, "sub-1", "Tablet", 0); !errors.Is(err, ErrDisabled) {
		t.Errorf("IssueAppPassword err = %v, want ErrDisabled", err)
	}
	if _, err := f.r.ListAppPasswords("sub-1"); !errors.Is(err, ErrDisabled) {
		t.Errorf("ListAppPasswords err = %v, want ErrDisabled", err)
	}
	if err := f.r.RevokeAppPassword(ctx, "sub-1", ap.ID); !errors.Is(err, ErrDisabled) {
		t.Errorf("RevokeAppPassword err = %v, want ErrDisabled", err)
	}
	// Nothing was issued or revoked.
	if file := readAppFile(t, f.fixture, "sub-1"); len(file.AppPasswords) != 1 || file.AppPasswords[0].ID != ap.ID {
		t.Errorf("app password file changed: %+v", file.AppPasswords)
	}
}

func TestListPrincipals(t *testing.T) {
	// With a lease, principals that oCIS never confirmed do not hold on to
	// their usernames, which the takeovers below rely on.
	f := newLeaseFixture(t)
	if got := f.r.ListPrincipals(context.Background()); len(got) != 1 || got[0].Sub != "sub-1" {
		t.Fatalf("ListPrincipals = %+v, want alice only", got)
	}

	mustProvision(t, f.r, "sub-3", "carol", "Carol")
	mustProvision(t, f.r, "sub-2", "bob", "Bob")
	mustSetDisabled(t, f.r, "sub-2", true)
	// dave takes carol's name; carol's principal becomes nameless.
	f.clock.Add(time.Minute)
	mustProvision(t, f.r, "sub-4", "carol", "Carol Two")
	// A second nameless principal, to check the order among those.
	mustProvision(t, f.r, "sub-0", "erin", "Erin")
	f.clock.Add(time.Minute)
	mustProvision(t, f.r, "sub-5", "erin", "Erin Two")

	got := f.r.ListPrincipals(context.Background())
	want := []Principal{
		{Sub: "sub-0", Username: "", DisplayName: "Erin"},
		{Sub: "sub-3", Username: "", DisplayName: "Carol"},
		{Sub: "sub-1", Username: "alice", DisplayName: "Alice"},
		{Sub: "sub-2", Username: "bob", DisplayName: "Bob", Disabled: true},
		{Sub: "sub-4", Username: "carol", DisplayName: "Carol Two"},
		{Sub: "sub-5", Username: "erin", DisplayName: "Erin Two"},
	}
	if len(got) != len(want) {
		t.Fatalf("ListPrincipals returned %d principals, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g := got[i]
		if g.Sub != want[i].Sub || g.Username != want[i].Username || g.DisplayName != want[i].DisplayName || g.Disabled != want[i].Disabled {
			t.Errorf("principal %d = %+v, want %+v", i, g, want[i])
		}
		if g.CreatedAt.IsZero() {
			t.Errorf("principal %d has no CreatedAt", i)
		}
	}

	// The list is a copy: changing it does not touch the registry.
	got[0].Username = "hacked"
	if again := f.r.ListPrincipals(context.Background()); again[0].Username != "" {
		t.Errorf("ListPrincipals shares its slice with the caller")
	}
}

func TestEmptyListPrincipals(t *testing.T) {
	f := newFixture(t)
	r := f.open(t, nil)
	if got := r.ListPrincipals(context.Background()); len(got) != 0 {
		t.Fatalf("ListPrincipals on an empty registry = %+v", got)
	}
}
