package graphapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// fakeDirectory stands in for the oCIS Graph API. It records the tokens it
// was asked with.
type fakeDirectory struct {
	users, groups map[string]string // id to display name
	err           error

	mu     sync.Mutex
	tokens []string
}

func (d *fakeDirectory) User(_ context.Context, token, id string) (ocisgraph.Entity, error) {
	return d.lookup(d.users, token, id)
}

func (d *fakeDirectory) Group(_ context.Context, token, id string) (ocisgraph.Entity, error) {
	return d.lookup(d.groups, token, id)
}

func (d *fakeDirectory) lookup(m map[string]string, token, id string) (ocisgraph.Entity, error) {
	d.mu.Lock()
	d.tokens = append(d.tokens, token)
	d.mu.Unlock()
	if d.err != nil {
		return ocisgraph.Entity{}, d.err
	}
	name, ok := m[id]
	if !ok {
		return ocisgraph.Entity{}, ocisgraph.ErrNotFound
	}
	return ocisgraph.Entity{ID: id, DisplayName: name}, nil
}

type shareFixture struct {
	*fixture
	dir    *fakeDirectory
	shares *sharing.Shares
}

// newShareFixture serves the facade with sharing. Einstein and marie have
// signed in once, so both exist, and marie has an oCIS identity and one group.
func newShareFixture(t *testing.T) *shareFixture {
	t.Helper()
	sf := &shareFixture{dir: &fakeDirectory{
		users:  map[string]string{"u-marie": "Marie Curie", "u-einstein": "Albert Einstein", "u-newton": "Isaac Newton"},
		groups: map[string]string{"g-physics": "physics-lovers"},
	}}
	sf.fixture = newFixture(t, func(cfg *Config) {
		shares, err := sharing.Open(sharing.Config{
			OwnerDir: cfg.Store.(interface{ OwnerDir(string) string }).OwnerDir,
			Owners:   func() []string { return nil },
			OCISID: func(sub string) string {
				id, _ := cfg.Registry.Identity(sub)
				return id.OCISID
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		sf.shares = shares
		cfg.Shares = shares
		cfg.Directory = sf.dir
	})
	for _, u := range []string{"einstein", "marie"} {
		if rec := sf.do(http.MethodGet, "/graph/v1.0/me/calendars", u, ""); rec.Code != 200 {
			t.Fatalf("first sign-in of %s: %d", u, rec.Code)
		}
	}
	set := func(sub, id string, groups ...principals.Group) {
		if err := sf.reg.SetIdentity(context.Background(), sub, principals.Identity{OCISID: id, Groups: groups}); err != nil {
			t.Fatal(err)
		}
	}
	set("sub-einstein", "u-einstein")
	set("sub-marie", "u-marie", principals.Group{ID: "g-physics", Name: "physics-lovers"})
	return sf
}

type permissionBody struct {
	ID           string   `json:"id"`
	Role         string   `json:"role"`
	AllowedRoles []string `json:"allowedRoles"`
	IsRemovable  bool     `json:"isRemovable"`
	EmailAddress struct {
		Name string `json:"name"`
	} `json:"emailAddress"`
	Grantee struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"grantee"`
}

type permissionList struct {
	Value []permissionBody `json:"value"`
}

// einsteinCalendar returns the id of einstein's default calendar.
func (sf *shareFixture) einsteinCalendar(t *testing.T) string {
	t.Helper()
	return listCalendarsOf(t, sf.fixture, "einstein")[0].ID
}

func permsURL(cal string, more ...string) string {
	return "/graph/v1.0/me/calendars/" + cal + "/calendarPermissions" + strings.Join(more, "")
}

func (sf *shareFixture) grant(t *testing.T, cal, body string) permissionBody {
	t.Helper()
	rec := sf.do(http.MethodPost, permsURL(cal), "einstein", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST permission: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[permissionBody](t, rec)
}

const grantMarieRead = `{"role":"read","grantee":{"type":"user","id":"u-marie"}}`

func TestGrantListChangeRevoke(t *testing.T) {
	sf := newShareFixture(t)
	cal := sf.einsteinCalendar(t)

	if rec := sf.do(http.MethodGet, permsURL(cal), "einstein", ""); rec.Code != 200 || len(decodeBody[permissionList](t, rec).Value) != 0 {
		t.Fatalf("empty list: %d %s", rec.Code, rec.Body.String())
	}

	p := sf.grant(t, cal, grantMarieRead)
	if p.ID == "" || p.Role != "read" || p.Grantee.Type != "user" || p.Grantee.ID != "u-marie" || p.EmailAddress.Name != "Marie Curie" || !p.IsRemovable {
		t.Errorf("created = %+v", p)
	}
	if len(p.AllowedRoles) != 2 || p.AllowedRoles[0] != "read" || p.AllowedRoles[1] != "write" {
		t.Errorf("allowedRoles = %v", p.AllowedRoles)
	}
	if tok := sf.dir.tokens; len(tok) != 1 || tok[0] != "einstein" {
		t.Errorf("the directory was asked with %v, want the owner's token", tok)
	}

	// Marie sees the calendar, read only, at once.
	var seen *calendarBody
	for _, c := range listCalendarsOf(t, sf.fixture, "marie") {
		if c.Name == "Personal" && c.Owner.Name == "Albert Einstein" {
			seen = &c
		}
	}
	if seen == nil || seen.CanEdit || seen.CanShare {
		t.Fatalf("marie's view of the shared calendar = %+v", seen)
	}

	rec := sf.do(http.MethodGet, permsURL(cal), "einstein", "")
	if list := decodeBody[permissionList](t, rec).Value; len(list) != 1 || list[0].ID != p.ID {
		t.Errorf("list = %+v", list)
	}
	rec = sf.do(http.MethodGet, permsURL(cal, "/", p.ID), "einstein", "")
	if rec.Code != 200 || decodeBody[permissionBody](t, rec).ID != p.ID {
		t.Errorf("get: %d %s", rec.Code, rec.Body.String())
	}

	rec = sf.do(http.MethodPatch, permsURL(cal, "/", p.ID), "einstein", `{"role":"write"}`)
	if rec.Code != 200 || decodeBody[permissionBody](t, rec).Role != "write" {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range listCalendarsOf(t, sf.fixture, "marie") {
		if c.Owner.Name == "Albert Einstein" && !c.CanEdit {
			t.Errorf("marie cannot edit after the change to write")
		}
	}

	if rec := sf.do(http.MethodDelete, permsURL(cal, "/", p.ID), "einstein", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range listCalendarsOf(t, sf.fixture, "marie") {
		if c.Owner.Name == "Albert Einstein" {
			t.Errorf("marie still sees the calendar after the revoke")
		}
	}
	if rec := sf.do(http.MethodDelete, permsURL(cal, "/", p.ID), "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: %d, want 404", rec.Code)
	}
	if rec := sf.do(http.MethodGet, permsURL(cal, "/", p.ID), "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: %d, want 404", rec.Code)
	}
}

func TestGrantToAGroup(t *testing.T) {
	sf := newShareFixture(t)
	cal := sf.einsteinCalendar(t)
	p := sf.grant(t, cal, `{"role":"write","grantee":{"type":"group","id":"g-physics"}}`)
	if p.Grantee.Type != "group" || p.EmailAddress.Name != "physics-lovers" {
		t.Errorf("created = %+v", p)
	}
	found := false
	for _, c := range listCalendarsOf(t, sf.fixture, "marie") {
		found = found || c.Owner.Name == "Albert Einstein" && c.CanEdit
	}
	if !found {
		t.Error("marie, a member of the group, does not see the calendar")
	}
}

func TestRecipientsAndStrangersCannotManageShares(t *testing.T) {
	sf := newShareFixture(t)
	cal := sf.einsteinCalendar(t)
	p := sf.grant(t, cal, grantMarieRead)

	for _, c := range []struct{ method, url, body string }{
		{http.MethodGet, permsURL(cal), ""},
		{http.MethodPost, permsURL(cal), grantMarieRead},
		{http.MethodGet, permsURL(cal, "/", p.ID), ""},
		{http.MethodPatch, permsURL(cal, "/", p.ID), `{"role":"write"}`},
		{http.MethodDelete, permsURL(cal, "/", p.ID), ""},
	} {
		rec := sf.do(c.method, c.url, "marie", c.body)
		if rec.Code != http.StatusForbidden || decodeError(t, rec).Error.Code != "accessDenied" {
			t.Errorf("recipient %s %s: %d %s, want 403", c.method, c.url, rec.Code, rec.Body.String())
		}
	}
	if n := len(sf.shares.Members(store.CollectionRef{Owner: "sub-einstein", Kind: store.KindCalendar, Name: calName(t, sf, cal)})); n != 1 {
		t.Errorf("the recipient changed the shares: %d members", n)
	}

	// Someone the calendar is not shared with learns nothing about it.
	if err := sf.shares.RemoveCollection(context.Background(), store.CollectionRef{Owner: "sub-einstein", Kind: store.KindCalendar, Name: calName(t, sf, cal)}); err != nil {
		t.Fatal(err)
	}
	if rec := sf.do(http.MethodGet, permsURL(cal), "marie", ""); rec.Code != http.StatusNotFound {
		t.Errorf("stranger: %d, want 404", rec.Code)
	}
}

func calName(t *testing.T, sf *shareFixture, calID string) string {
	t.Helper()
	id, err := ParseCalendarID(calID)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := sf.st.ListCollections(context.Background(), "sub-einstein", store.KindCalendar)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		if c.ID == id {
			return c.Ref.Name
		}
	}
	t.Fatal("calendar not found")
	return ""
}

func TestGrantValidation(t *testing.T) {
	sf := newShareFixture(t)
	cal := sf.einsteinCalendar(t)
	sf.grant(t, cal, grantMarieRead)

	for _, c := range []struct {
		name, body string
		status     int
	}{
		{"unknown role", `{"role":"owner","grantee":{"type":"user","id":"u-newton"}}`, 400},
		{"Graph role that has no meaning here", `{"role":"freeBusyRead","grantee":{"type":"user","id":"u-newton"}}`, 400},
		{"no role", `{"grantee":{"type":"user","id":"u-newton"}}`, 400},
		{"no grantee", `{"role":"read"}`, 400},
		{"bad grantee type", `{"role":"read","grantee":{"type":"team","id":"u-newton"}}`, 400},
		{"no grantee id", `{"role":"read","grantee":{"type":"user"}}`, 400},
		{"user that is not in the directory", `{"role":"read","grantee":{"type":"user","id":"u-nobody"}}`, 400},
		{"group id used as a user", `{"role":"read","grantee":{"type":"user","id":"g-physics"}}`, 400},
		{"the owner", `{"role":"read","grantee":{"type":"user","id":"u-einstein"}}`, 400},
		{"already shared", grantMarieRead, 409},
		{"not json", `nope`, 400},
	} {
		rec := sf.do(http.MethodPost, permsURL(cal), "einstein", c.body)
		if rec.Code != c.status {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body.String(), c.status)
		}
	}
	req := httptest.NewRequest(http.MethodPost, permsURL(cal), strings.NewReader(grantMarieRead))
	req.Header.Set("Authorization", "Bearer einstein")
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	sf.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("wrong content type: %d", rec.Code)
	}

	p := listPermissions(t, sf, cal)[0]
	for _, body := range []string{`{"role":"owner"}`, `{}`, `{"role":""}`} {
		if rec := sf.do(http.MethodPatch, permsURL(cal, "/", p.ID), "einstein", body); rec.Code != 400 {
			t.Errorf("PATCH %s: %d, want 400", body, rec.Code)
		}
	}
	if rec := sf.do(http.MethodGet, permsURL(cal, "/", "!!"), "einstein", ""); rec.Code != 404 {
		t.Errorf("malformed permission id: %d, want 404", rec.Code)
	}
	if rec := sf.do(http.MethodGet, permsURL("!!"), "einstein", ""); rec.Code != 400 {
		t.Errorf("malformed calendar id: %d, want 400", rec.Code)
	}
	if rec := sf.do(http.MethodGet, permsURL(CalendarID("nope")), "einstein", ""); rec.Code != 404 {
		t.Errorf("unknown calendar: %d, want 404", rec.Code)
	}
}

func listPermissions(t *testing.T, sf *shareFixture, cal string) []permissionBody {
	t.Helper()
	rec := sf.do(http.MethodGet, permsURL(cal), "einstein", "")
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[permissionList](t, rec).Value
}

func TestGrantDirectoryFailures(t *testing.T) {
	sf := newShareFixture(t)
	cal := sf.einsteinCalendar(t)

	sf.dir.err = errors.New("connection refused")
	rec := sf.do(http.MethodPost, permsURL(cal), "einstein", grantMarieRead)
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "refused") {
		t.Errorf("unreachable directory: %d %s, want 502 without the cause", rec.Code, rec.Body.String())
	}
	sf.dir.err = ocisgraph.ErrUnauthorized
	if rec := sf.do(http.MethodPost, permsURL(cal), "einstein", grantMarieRead); rec.Code != http.StatusBadGateway {
		t.Errorf("refused token: %d, want 502", rec.Code)
	}
	if got := listPermissions(t, sf, cal); len(got) != 0 {
		t.Errorf("a failed check left shares: %+v", got)
	}
}

func TestPermissionsRoutesWithoutSharing(t *testing.T) {
	f := newFixture(t)
	cal := listCalendarsOf(t, f, "einstein")[0].ID
	if rec := f.do(http.MethodGet, permsURL(cal), "einstein", ""); rec.Code != http.StatusNotImplemented {
		t.Errorf("without sharing: %d, want 501", rec.Code)
	}
}

func TestNewRequiresDirectoryWithShares(t *testing.T) {
	f := newFixture(t)
	shares, err := sharing.Open(sharing.Config{OwnerDir: func(string) string { return "" }, Owners: func() []string { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Store: f.st, Registry: f.reg, Shares: shares}); err == nil {
		t.Error("New accepted shares without a directory")
	}
}

// TestPermissionContract pins the JSON shapes of permissions. The web app's
// tests load the same files. Run with -update to rewrite them.
func TestPermissionContract(t *testing.T) {
	sf := newShareFixture(t)
	cal := sf.einsteinCalendar(t)
	p := sf.grant(t, cal, grantMarieRead)
	sf.grant(t, cal, `{"role":"write","grantee":{"type":"group","id":"g-physics"}}`)

	rec := sf.do(http.MethodGet, permsURL(cal, "/", p.ID), "einstein", "")
	compareContract(t, "calendar-permission", rec.Body.Bytes())
	rec = sf.do(http.MethodGet, permsURL(cal), "einstein", "")
	compareContract(t, "calendar-permissions", rec.Body.Bytes())
}

func TestDeletingACalendarEndsItsShares(t *testing.T) {
	sf := newShareFixture(t)
	rec := sf.do(http.MethodPost, "/graph/v1.0/me/calendars", "einstein", `{"name":"Lectures"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	cal := decodeBody[calendarBody](t, rec).ID
	ref := store.CollectionRef{Owner: "sub-einstein", Kind: store.KindCalendar, Name: calName(t, sf, cal)}
	sf.grant(t, cal, grantMarieRead)
	if n := len(sf.shares.Members(ref)); n != 1 {
		t.Fatalf("members before the delete = %d, want 1", n)
	}

	if rec := sf.do(http.MethodDelete, "/graph/v1.0/me/calendars/"+cal, "einstein", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(sf.shares.Members(ref)); n != 0 {
		t.Errorf("members after the delete = %d, want 0", n)
	}
}

func TestDeletingAContactFolderEndsItsShares(t *testing.T) {
	sf := newShareFixture(t)
	rec := sf.do(http.MethodPost, "/graph/v1.0/me/contactFolders", "einstein", `{"displayName":"Colleagues"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	folder := decodeBody[contactFolderBody](t, rec).ID
	url := "/graph/v1.0/me/contactFolders/" + folder + "/permissions"
	if rec := sf.do(http.MethodPost, url, "einstein", grantMarieRead); rec.Code != http.StatusCreated {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	id, err := ParseCalendarID(folder)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := sf.st.ListCollections(context.Background(), "sub-einstein", store.KindAddressBook)
	if err != nil {
		t.Fatal(err)
	}
	var ref store.CollectionRef
	for _, c := range cols {
		if c.ID == id {
			ref = c.Ref
		}
	}
	if n := len(sf.shares.Members(ref)); n != 1 {
		t.Fatalf("members before the delete = %d, want 1", n)
	}

	if rec := sf.do(http.MethodDelete, "/graph/v1.0/me/contactFolders/"+folder, "einstein", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(sf.shares.Members(ref)); n != 0 {
		t.Errorf("members after the delete = %d, want 0", n)
	}
}
