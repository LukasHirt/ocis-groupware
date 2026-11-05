package migrate

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/migrate/classic"
	"github.com/owncloud/ocis-groupware/internal/migrate/classic/classictest"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// admin is a classic account without an oCIS user, to see it reported.
func fixtureMapper(t *testing.T) Mapper {
	return csvMapper(t, []string{"alice", "bob", "carol"}, []string{"staff", "team"})
}

func TestMigrateTheClassicFixture(t *testing.T) {
	classictest.Each(t, "classic_migrate", func(t *testing.T, db classictest.DB) {
		r, fx := db.Open(t), db.Fixture
		ctx := context.Background()
		e := newEnv(t, fx.Secret)
		rep, err := Run(ctx, e.config(r, fixtureMapper(t)))
		if err != nil {
			t.Fatal(err)
		}

		// alice/work holds 5,000 events byte for byte as classic has them.
		work := store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindCalendar, Name: "work"}
		c, err := e.st.GetCollection(ctx, work)
		if err != nil || c.Props.DisplayName != "Work" || c.Props.Color != "#1d2d44" {
			t.Fatalf("alice/work = %+v, %v", c, err)
		}
		var checked int
		err = r.CalendarObjects(ctx, calendarID(t, r, "alice", "work"), func(o classic.Object) error {
			got, err := e.st.GetObject(ctx, work, o.URI)
			if err != nil {
				return err
			}
			if !bytes.Equal(got.Data, o.Data) {
				t.Errorf("%s differs", o.URI)
			}
			checked++
			return nil
		})
		if err != nil || checked != 5000 {
			t.Fatalf("checked %d objects, %v", checked, err)
		}
		archive, err := e.st.GetCollection(ctx, store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindCalendar, Name: "archive"})
		if err != nil || archive.Props.DisplayName != "Archiv 2024 (ä)" {
			t.Fatalf("archive = %+v, %v", archive, err)
		}
		friends, _ := e.st.ListObjects(ctx, store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindAddressBook, Name: "friends"}, store.ListOptions{})
		contacts, _ := e.st.ListObjects(ctx, store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindAddressBook, Name: "contacts"}, store.ListOptions{})
		if len(friends) != 15 || len(contacts) != 120 {
			t.Fatalf("%d friends, %d contacts", len(friends), len(contacts))
		}

		// Shares: who receives what.
		got := map[string]string{}
		for _, uid := range []string{"bob", "carol", "alice"} {
			id, _ := e.reg.Identity("migrated-ocis-" + uid)
			for _, rc := range e.shares.Received("ocis-"+uid, groupIDs(id)) {
				got[uid+" <- "+rc.Owner+"/"+rc.Name] = string(rc.Access)
			}
		}
		want := map[string]string{
			"bob <- migrated-ocis-alice/work":       "readwrite",
			"bob <- migrated-ocis-alice/friends":    "readwrite",
			"carol <- migrated-ocis-alice/work":     "read",
			"carol <- migrated-ocis-alice/personal": "read",
			"carol <- migrated-ocis-alice/friends":  "read",
			"alice <- migrated-ocis-bob/projects":   "readwrite",
			"alice <- migrated-ocis-bob/contacts":   "read",
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s = %q, want %q", k, got[k], v)
			}
		}

		// The classic app tokens sign in with their old values.
		for _, tk := range fx.AppTokens {
			p, err := e.reg.VerifyBasic(ctx, tk.User, tk.Token)
			if err != nil || p.Username != tk.User {
				t.Errorf("token %q of %s: %+v, %v", tk.Name, tk.User, p, err)
			}
		}
		if _, err := e.reg.VerifyBasic(ctx, "alice", "wrong"); err == nil {
			t.Error("a wrong password signed in")
		}

		// A rerun, after tokens were used, adds nothing and does not import
		// the tokens a second time.
		before := snapshot(t, e.dir)
		e.open(t, fx.Secret)
		again, err := Run(ctx, e.config(r, fixtureMapper(t)))
		if err != nil {
			t.Fatal(err)
		}
		if tt := again.Totals; tt.ObjectsAdded != 0 || tt.Tokens != 0 || tt.Shares != 0 {
			t.Fatalf("rerun totals = %+v", tt)
		}
		if after := snapshot(t, e.dir); after != before {
			t.Fatal("a rerun changed the data directory")
		}
		list, err := e.reg.ListAppPasswords("migrated-ocis-alice")
		if err != nil || len(list) != 2 {
			t.Fatalf("alice has %d app passwords, %v", len(list), err)
		}

		var text bytes.Buffer
		if err := rep.WriteText(&text); err != nil {
			t.Fatal(err)
		}
		golden(t, "report.golden", text.String())
	})
}

func TestDryRunOfTheClassicFixture(t *testing.T) {
	classictest.Each(t, "classic_migrate", func(t *testing.T, db classictest.DB) {
		r, fx := db.Open(t), db.Fixture
		e := newEnv(t, fx.Secret)
		cfg := e.config(r, fixtureMapper(t))
		cfg.DryRun = true
		rep, err := Run(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got := snapshot(t, e.dir); got != "" {
			t.Fatalf("a dry run wrote:\n%s", got)
		}
		var text bytes.Buffer
		if err := rep.WriteText(&text); err != nil {
			t.Fatal(err)
		}
		golden(t, "report_dry_run.golden", text.String())
	})
}

func TestFixtureRunForOneUser(t *testing.T) {
	classictest.Each(t, "classic_migrate", func(t *testing.T, db classictest.DB) {
		r, fx := db.Open(t), db.Fixture
		e := newEnv(t, fx.Secret)
		cfg := e.config(r, fixtureMapper(t))
		cfg.Users = []string{"bob"}
		rep, err := Run(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Users) != 1 || rep.Users[0].Login != "bob" {
			t.Fatalf("users = %+v", rep.Users)
		}
		if got := len(e.reg.ListPrincipals(context.Background())); got != 1 {
			t.Fatalf("%d principals, want only bob", got)
		}
	})
}

func calendarID(t *testing.T, r *classic.Reader, owner, uri string) int64 {
	t.Helper()
	cals, err := r.Calendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cals {
		if c.Owner.Name == owner && c.URI == uri {
			return c.ID
		}
	}
	t.Fatalf("no calendar %s/%s", owner, uri)
	return 0
}

func groupIDs(id principals.Identity) []string {
	var out []string
	for _, g := range id.Groups {
		out = append(out, g.ID)
	}
	return out
}

// golden compares got with testdata/<name>. With -update it rewrites the
// file.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the report:\n%s", path, got)
	}
}
