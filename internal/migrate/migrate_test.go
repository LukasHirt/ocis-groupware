package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/migrate/classic"
	"github.com/owncloud/ocis-groupware/internal/migrate/idmap"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

func small() *fakeSource {
	return &fakeSource{
		users:  []classic.User{user("alice"), user("bob")},
		groups: []classic.Group{{ID: "team", Members: []string{"alice", "bob"}}},
		cals: []classic.Calendar{
			{ID: 1, Owner: userP("alice"), URI: "work", DisplayName: "Work", Color: "#112233", Order: 3, Timezone: ""},
		},
		objects: map[int64][]classic.Object{1: {
			{URI: "a.ics", Data: ics("a", "A")},
			{URI: "b.ics", Data: ics("b", "B")},
		}},
		books: []classic.AddressBook{{ID: 7, Owner: userP("alice"), URI: "contacts", DisplayName: "Contacts"}},
		cards: map[int64][]classic.Card{7: {{URI: "c.vcf", Data: vcf("c", "Carl")}}},
		shares: []classic.Share{
			{ID: 1, Kind: classic.ResourceCalendar, ResourceID: 1, Target: userP("bob"), Access: classic.ReadWrite},
			{ID: 2, Kind: classic.ResourceCalendar, ResourceID: 1, Target: classic.Principal{Kind: classic.PrincipalGroup, Name: "team"}, Access: classic.ReadOnly},
		},
	}
}

func runIt(t *testing.T, e *env, src Source, mut func(*Config)) *Report {
	t.Helper()
	cfg := e.config(src, csvMapper(t, []string{"alice", "bob"}, []string{"team"}))
	if mut != nil {
		mut(&cfg)
	}
	rep, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

var importedAt = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func TestRunCopiesCollectionsObjectsAndShares(t *testing.T) {
	e := newEnv(t, "s")
	rep := runIt(t, e, small(), func(c *Config) { c.Now = func() time.Time { return importedAt } })

	p, err := e.reg.Get("migrated-ocis-alice")
	if err != nil || p.Username != "alice" || p.Sub != "" || p.DisplayName != "Alice" {
		t.Fatalf("principal = %+v, %v", p, err)
	}
	ref := store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindCalendar, Name: "work"}
	c, err := e.st.GetCollection(context.Background(), ref)
	if err != nil || c.Props.DisplayName != "Work" || c.Props.Color != "#112233" || c.Props.Order != 3 {
		t.Fatalf("collection = %+v, %v", c, err)
	}
	o, err := e.st.GetObject(context.Background(), ref, "b.ics")
	if err != nil || string(o.Data) != string(ics("b", "B")) || o.UID != "b" {
		t.Fatalf("object = %+v, %v", o, err)
	}
	if _, err := e.st.GetObject(context.Background(), store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindAddressBook, Name: "contacts"}, "c.vcf"); err != nil {
		t.Fatal(err)
	}
	// No default "personal" calendar is added next to the migrated ones.
	cals, _ := e.st.ListCollections(context.Background(), "migrated-ocis-alice", store.KindCalendar)
	if len(cals) != 1 {
		t.Fatalf("%d calendars, want only the migrated one", len(cals))
	}

	members := e.shares.Members(ref)
	if len(members) != 2 || members[0] != (sharing.Member{Type: sharing.User, ID: "ocis-bob", Name: "Bob", Access: sharing.ReadWrite}) ||
		members[1] != (sharing.Member{Type: sharing.Group, ID: "ocis-team", Name: "team", Access: sharing.Read}) {
		t.Fatalf("members = %+v", members)
	}
	id, _ := e.reg.Identity("migrated-ocis-bob")
	// bob was in the classic group "team", but that is not carried over.
	if id.OCISID != "ocis-bob" || len(id.Groups) != 0 || !id.At.Equal(importedAt) {
		t.Fatalf("identity = %+v", id)
	}
	if rep.Totals.ObjectsAdded != 3 || rep.Totals.Shares != 2 || rep.Totals.Collections != 2 || rep.Totals.Problems != 0 {
		t.Fatalf("totals = %+v", rep.Totals)
	}
}

func TestRerunChangesNothing(t *testing.T) {
	e := newEnv(t, "s")
	src := small()
	runIt(t, e, src, nil)
	before := snapshot(t, e.dir)

	e.open(t, "s") // a new process
	rep := runIt(t, e, src, nil)
	if after := snapshot(t, e.dir); after != before {
		t.Fatalf("a rerun changed the data directory:\n%s\n---\n%s", before, after)
	}
	tt := rep.Totals
	if tt.ObjectsAdded != 0 || tt.Shares != 0 || tt.Tokens != 0 {
		t.Fatalf("totals = %+v, want nothing new", tt)
	}
	for _, u := range rep.Users {
		if u.Status != StatusExisting {
			t.Errorf("%s: %s", u.Login, u.Status)
		}
	}
}

func TestRerunAddsOnlyWhatIsNew(t *testing.T) {
	e := newEnv(t, "s")
	src := small()
	runIt(t, e, src, nil)
	src.objects[1] = append(src.objects[1], classic.Object{URI: "c.ics", Data: ics("c", "C")})
	rep := runIt(t, e, src, nil)
	if rep.Totals.ObjectsAdded != 1 {
		t.Fatalf("totals = %+v, want one new object", rep.Totals)
	}
	if c := rep.Users[0].Calendars[0]; c.Objects.Unchanged != 2 {
		t.Fatalf("calendar = %+v", c)
	}
}

func TestRerunKeepsEditedObjects(t *testing.T) {
	e := newEnv(t, "s")
	src := small()
	runIt(t, e, src, nil)
	ref := store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindCalendar, Name: "work"}
	edited := ics("a", "edited in the new service")
	if _, err := e.st.PutObject(context.Background(), ref, "a.ics", edited, store.PutOptions{UID: "a"}); err != nil {
		t.Fatal(err)
	}
	rep := runIt(t, e, src, nil)
	o, _ := e.st.GetObject(context.Background(), ref, "a.ics")
	if string(o.Data) != string(edited) {
		t.Fatal("the rerun overwrote an object changed since")
	}
	if c := rep.Users[0].Calendars[0]; c.Objects.Different != 1 || c.Objects.Unchanged != 1 {
		t.Fatalf("calendar = %+v", c)
	}
}

func TestDryRunWritesNothingAndPredictsTheRun(t *testing.T) {
	e := newEnv(t, "s")
	src := small()
	src.tokens = []classic.AppToken{{User: "alice", Name: "Phone", Digest: strings.Repeat("ab", 64)}}
	before := snapshot(t, e.dir)
	dry := runIt(t, e, src, func(c *Config) { c.DryRun = true })
	if after := snapshot(t, e.dir); after != before {
		t.Fatalf("a dry run wrote files:\n%s", after)
	}
	if len(e.reg.ListPrincipals(context.Background())) != 0 {
		t.Fatal("a dry run created a principal")
	}
	real := runIt(t, e, src, nil)
	dry.DryRun = false
	var a, b strings.Builder
	if err := dry.WriteJSON(&a); err != nil {
		t.Fatal(err)
	}
	if err := real.WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatalf("the dry run predicted something else:\n%s\n---\n%s", a.String(), b.String())
	}
}

func TestPerUserRun(t *testing.T) {
	e := newEnv(t, "s")
	src := small()
	src.cals = append(src.cals, classic.Calendar{ID: 2, Owner: userP("bob"), URI: "projects"})
	src.objects[2] = []classic.Object{{URI: "p.ics", Data: ics("p", "P")}}
	src.shares = append(src.shares, classic.Share{ID: 3, Kind: classic.ResourceCalendar, ResourceID: 2, Target: userP("alice"), Access: classic.ReadOnly})
	rep := runIt(t, e, src, func(c *Config) { c.Users = []string{"bob"} })

	if len(rep.Users) != 1 || rep.Users[0].Login != "bob" {
		t.Fatalf("users = %+v", rep.Users)
	}
	if _, err := e.reg.Get("migrated-ocis-alice"); !errors.Is(err, errNotFound) {
		t.Fatalf("alice was migrated in a run for bob: %v", err)
	}
	ref := store.CollectionRef{Owner: "migrated-ocis-bob", Kind: store.KindCalendar, Name: "projects"}
	if m := e.shares.Members(ref); len(m) != 1 || m[0].ID != "ocis-alice" {
		t.Fatalf("members = %+v", m)
	}
	// A second run for alice completes the picture without touching bob.
	runIt(t, e, src, func(c *Config) { c.Users = []string{"alice"} })
	if m := e.shares.Members(store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindCalendar, Name: "work"}); len(m) != 2 {
		t.Fatalf("members = %+v", m)
	}
}

func TestUnknownUserIsAnError(t *testing.T) {
	e := newEnv(t, "s")
	_, err := Run(context.Background(), func() Config {
		c := e.config(small(), csvMapper(t, nil, nil))
		c.Users = []string{"nobody"}
		return c
	}())
	if err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnmappedDisabledAndTakenUsersAreSkipped(t *testing.T) {
	e := newEnv(t, "s")
	if _, _, err := e.reg.Provision(context.Background(), "sub-x", "dave", "Dave"); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{
		users: []classic.User{user("alice"), user("carol"), user("dave"), {ID: "erin", Enabled: false}, user("frank")},
		cals: []classic.Calendar{
			{ID: 1, Owner: userP("carol"), URI: "c"}, {ID: 2, Owner: userP("dave"), URI: "d"},
		},
		objects: map[int64][]classic.Object{1: {{URI: "x.ics", Data: ics("x", "X")}}, 2: {{URI: "y.ics", Data: ics("y", "Y")}}},
	}
	// carol and frank are unknown to oCIS; dave's name is held by a user who
	// signed in already (with another oCIS id).
	m := csvMapper(t, []string{"alice", "dave"}, nil)
	rep, err := Run(context.Background(), e.config(src, m))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, u := range rep.Users {
		got[u.Login] = u.Status + ": " + u.Reason
	}
	for login, want := range map[string]string{"carol": "no oCIS user", "erin": "disabled", "dave": "belongs to another user", "frank": "no oCIS user"} {
		if g := got[login]; !strings.Contains(g, want) || !strings.HasPrefix(g, StatusSkipped) {
			t.Errorf("%s: %q, want skipped with %q", login, g, want)
		}
	}
	if len(rep.Unmapped) != 2 || rep.Unmapped[0].ID != "carol" || rep.Unmapped[1].ID != "frank" {
		t.Fatalf("unmapped = %+v", rep.Unmapped)
	}
	if rep.Totals.ObjectsAdded != 0 {
		t.Fatalf("objects were copied for skipped users: %+v", rep.Totals)
	}
}

func TestSignedInUserKeepsTheirOwnerAndData(t *testing.T) {
	e := newEnv(t, "s")
	ctx := context.Background()
	if _, _, err := e.reg.Provision(ctx, "sub-alice", "alice", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := e.reg.SetIdentity(ctx, "sub-alice", principalsIdentity("ocis-alice")); err != nil {
		t.Fatal(err)
	}
	rep := runIt(t, e, small(), nil)
	if rep.Users[0].Owner != "sub-alice" || rep.Users[0].Status != StatusExisting {
		t.Fatalf("alice = %+v", rep.Users[0])
	}
	if _, err := e.st.GetObject(ctx, store.CollectionRef{Owner: "sub-alice", Kind: store.KindCalendar, Name: "work"}, "a.ics"); err != nil {
		t.Fatal(err)
	}
}

func TestObjectsTheEngineWouldRefuse(t *testing.T) {
	e := newEnv(t, "s")
	src := small()
	todo := []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:t\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:t\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	src.objects[1] = []classic.Object{
		{URI: "ok.ics", Data: ics("u1", "ok")},
		{URI: "todo.ics", Data: todo},
		{URI: "junk.ics", Data: []byte("not a calendar")},
		{URI: "twin.ics", Data: ics("u1", "same uid as ok")},
	}
	rep := runIt(t, e, src, nil)
	c := rep.Users[0].Calendars[0]
	if c.Objects.Added != 2 || c.Objects.Skipped != 1 || c.Objects.Failed != 1 {
		t.Fatalf("counts = %+v, problems %v", c.Objects, c.Problems)
	}
	if rep.Totals.Problems != 1 {
		t.Fatalf("totals = %+v", rep.Totals)
	}
	if _, err := e.st.GetObject(context.Background(), store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindCalendar, Name: "work"}, "twin.ics"); err != nil {
		t.Fatalf("an object with a duplicate UID was dropped: %v", err)
	}
}

func TestSharesToUnmappedRecipientsAndPublicLinks(t *testing.T) {
	e := newEnv(t, "s")
	src := small()
	src.shares = append(src.shares,
		classic.Share{ID: 5, Kind: classic.ResourceCalendar, ResourceID: 1, Target: userP("zed"), Access: classic.ReadOnly},
		classic.Share{ID: 6, Kind: classic.ResourceCalendar, ResourceID: 1, Target: userP("alice"), Access: classic.ReadOnly, PublicURI: "abc"},
		classic.Share{ID: 7, Kind: classic.ResourceCalendar, ResourceID: 1, Target: userP("alice"), Access: classic.ReadOnly},
	)
	src.subs = []classic.Subscription{{Owner: userP("alice"), URI: "holidays", DisplayName: "Holidays", Source: "https://example.org/h.ics"}}
	src.cals = append(src.cals,
		classic.Calendar{ID: 9, Owner: classic.Principal{Kind: classic.PrincipalSystem, Name: "system"}, URI: birthdayCalendar},
		classic.Calendar{ID: 10, Owner: userP("alice"), URI: birthdayCalendar})
	rep := runIt(t, e, src, nil)

	status := map[string]string{}
	for _, s := range rep.Shares {
		status[s.Grantee] = s.Status + " " + s.Reason
	}
	if !strings.HasPrefix(status["user zed"], StatusSkipped) || !strings.HasPrefix(status["user alice"], StatusSkipped) {
		t.Fatalf("shares = %+v", rep.Shares)
	}
	if !strings.Contains(status["user alice"], "owner") {
		t.Fatalf("share with the owner: %q", status["user alice"])
	}
	kinds := map[string]int{}
	for _, n := range rep.NotMigrated {
		kinds[n.What]++
	}
	if kinds["public link"] != 1 || kinds["subscription"] != 1 || kinds["birthday calendar"] != 2 {
		t.Fatalf("not migrated = %+v", rep.NotMigrated)
	}
	// A run for one user lists only that user's items.
	rep = runIt(t, e, src, func(c *Config) { c.Users = []string{"bob"} })
	if len(rep.NotMigrated) != 0 {
		t.Fatalf("not migrated for bob = %+v", rep.NotMigrated)
	}
}

func TestImportsTokensOnceByName(t *testing.T) {
	e := newEnv(t, "s")
	src := small()
	src.tokens = []classic.AppToken{
		{User: "alice", Name: "Phone", Digest: strings.Repeat("ab", 64)},
		{User: "alice", Name: "Phone", Digest: strings.Repeat("cd", 64)},
		{User: "alice", Name: "Laptop", Digest: strings.Repeat("ef", 64)},
		{User: "alice", Name: "Broken", Digest: "xyz"},
	}
	rep := runIt(t, e, src, nil)
	if tk := rep.Users[0].Tokens; tk.Imported != 3 || tk.Failed != 1 {
		t.Fatalf("tokens = %+v", tk)
	}
	list, _ := e.reg.ListAppPasswords("migrated-ocis-alice")
	if len(list) != 3 {
		t.Fatalf("%d app passwords", len(list))
	}
	// The digests are gone once a token was used; a rerun finds them by name.
	rep = runIt(t, e, src, nil)
	if tk := rep.Users[0].Tokens; tk.Imported != 0 || tk.Existing != 3 {
		t.Fatalf("rerun tokens = %+v", tk)
	}
}

func TestWarnsWhenTokensCannotWork(t *testing.T) {
	e := newEnv(t, "")
	src := small()
	src.tokens = []classic.AppToken{{User: "alice", Name: "Phone", Digest: strings.Repeat("ab", 64)}}
	rep := runIt(t, e, src, func(c *Config) { c.LegacyTokensEnabled = false })
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "GROUPWARE_LEGACY_TOKEN_SECRET") {
		t.Fatalf("warnings = %v", rep.Warnings)
	}
}

// byNameMapper reports the given groups as matched by display name.
type byNameMapper struct {
	Mapper
	groups []string
}

func (m byNameMapper) Map(ctx context.Context, users, groups []string) (idmap.Result, error) {
	res, err := m.Mapper.Map(ctx, users, groups)
	res.GroupsByName = m.groups
	return res, err
}

func TestReportListsGroupsMatchedByName(t *testing.T) {
	e := newEnv(t, "s")
	m := byNameMapper{Mapper: csvMapper(t, []string{"alice", "bob"}, []string{"team"}), groups: []string{"team"}}
	rep, err := Run(context.Background(), e.config(small(), m))
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	if err := rep.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	if want := "Groups matched to oCIS by display name\n  team\n"; !strings.Contains(text.String(), want) {
		t.Fatalf("report lacks %q:\n%s", want, text.String())
	}
	// The share to the group is still written, and nobody gets its members.
	members := e.shares.Members(store.CollectionRef{Owner: "migrated-ocis-alice", Kind: store.KindCalendar, Name: "work"})
	if len(members) != 2 {
		t.Fatalf("members = %+v", members)
	}
	for _, owner := range []string{"migrated-ocis-alice", "migrated-ocis-bob"} {
		if id, _ := e.reg.Identity(owner); len(id.Groups) != 0 {
			t.Fatalf("%s has groups %+v", owner, id.Groups)
		}
	}
}
