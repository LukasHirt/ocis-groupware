package graphapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/vcard"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

type contactFolderBody struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	IsDefault   bool   `json:"groupware.isDefault"`
	CanEdit     bool   `json:"groupware.canEdit"`
	CanShare    bool   `json:"groupware.canShare"`
	Owner       struct {
		Name string `json:"name"`
	} `json:"groupware.owner"`
}

type contactFolderList struct {
	Value []contactFolderBody `json:"value"`
}

type contactBody struct {
	ID             string   `json:"id"`
	ETag           string   `json:"@odata.etag"`
	ParentFolderID string   `json:"parentFolderId"`
	DisplayName    string   `json:"displayName"`
	GivenName      string   `json:"givenName"`
	Surname        string   `json:"surname"`
	JobTitle       string   `json:"jobTitle"`
	MobilePhone    string   `json:"mobilePhone"`
	Categories     []string `json:"categories"`
	Emails         []struct {
		Address string `json:"address"`
	} `json:"emailAddresses"`
	Groups []contactGroup `json:"groupware.groups"`
}

type contactList struct {
	Value    []contactBody `json:"value"`
	NextLink string        `json:"@odata.nextLink"`
}

func einsteinBook(t *testing.T, f *fixture) (store.CollectionRef, string) {
	t.Helper()
	rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders", "einstein", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list folders: %d %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[contactFolderList](t, rec).Value
	if len(list) == 0 {
		t.Fatal("no contact folder")
	}
	id, err := ParseCalendarID(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := f.st.ListCollections(context.Background(), "sub-einstein", store.KindAddressBook)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		if c.ID == id {
			return c.Ref, c.ID
		}
	}
	t.Fatal("folder not in the store")
	return store.CollectionRef{}, ""
}

func putCard(t *testing.T, f *fixture, ref store.CollectionRef, name, card string) {
	t.Helper()
	if _, err := f.st.PutObject(context.Background(), ref, name, []byte(strings.ReplaceAll(card, "\n", "\r\n")), store.PutOptions{}); err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
}

func TestContactFolders(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders", "einstein", "")
	list := decodeBody[contactFolderList](t, rec).Value
	if len(list) != 1 || !list[0].IsDefault || !list[0].CanEdit || !list[0].CanShare || list[0].Owner.Name != "Albert Einstein" {
		t.Fatalf("folders = %+v", list)
	}
	def := list[0]
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+def.ID, "einstein", ""); rec.Code != http.StatusOK {
		t.Errorf("GET one: %d", rec.Code)
	}
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+CalendarID("nope"), "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown: %d", rec.Code)
	}
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/***", "einstein", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed: %d", rec.Code)
	}
	// A calendar id is not a contact folder.
	cals := listCalendarsOf(t, f, "einstein")
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+cals[0].ID, "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("calendar id as folder: %d", rec.Code)
	}

	rec = f.do(http.MethodPost, "/graph/v1.0/me/contactFolders", "einstein", `{"displayName":"Friends"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[contactFolderBody](t, rec)
	if created.DisplayName != "Friends" || created.IsDefault {
		t.Errorf("created = %+v", created)
	}
	if rec := f.do(http.MethodPost, "/graph/v1.0/me/contactFolders", "einstein", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("create without a name: %d", rec.Code)
	}
	rec = f.do(http.MethodPatch, "/graph/v1.0/me/contactFolders/"+created.ID, "einstein", `{"displayName":"Colleagues"}`)
	if rec.Code != http.StatusOK || decodeBody[contactFolderBody](t, rec).DisplayName != "Colleagues" {
		t.Errorf("rename: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodDelete, "/graph/v1.0/me/contactFolders/"+created.ID, "einstein", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+created.ID, "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("after delete: %d", rec.Code)
	}
	// Another user does not see the folder.
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+def.ID, "marie", ""); rec.Code != http.StatusNotFound {
		t.Errorf("someone else's folder: %d", rec.Code)
	}
}

// sharedBooks gives einstein his own address books plus one of marie's.
type sharedBooks struct {
	st     store.Store
	access store.Access
	shared *store.Collection
}

func (s sharedBooks) ContactFolders(ctx context.Context, user principals.Principal) ([]ContactFolderEntry, error) {
	own, err := ownContactFolders{st: s.st}.ContactFolders(ctx, user)
	if err != nil || s.shared.ID == "" {
		return own, err
	}
	c := *s.shared
	c.Access = s.access
	return append(own, ContactFolderEntry{Collection: c, OwnerName: "Marie Curie"}), nil
}

func sharedBookFixture(t *testing.T, access store.Access) (*fixture, store.Collection) {
	t.Helper()
	var shared store.Collection
	f := newFixture(t, func(cfg *Config) {
		cfg.ContactFolders = sharedBooks{st: cfg.Store, access: access, shared: &shared}
	})
	einsteinBook(t, f)
	ref := store.CollectionRef{Owner: "sub-marie", Kind: store.KindAddressBook, Name: "team"}
	col, err := f.st.CreateCollection(context.Background(), ref, store.Props{DisplayName: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	shared = col
	return f, col
}

func TestSharedContactFolderIsReadOnlyForRecipients(t *testing.T) {
	f, col := sharedBookFixture(t, store.AccessRead)
	putCard(t, f, col.Ref, "pierre.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:pierre\nFN:Pierre Curie\nEND:VCARD\n")
	base := "/graph/v1.0/me/contactFolders/" + CalendarID(col.ID)
	rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders", "einstein", "")
	var shared contactFolderBody
	for _, b := range decodeBody[contactFolderList](t, rec).Value {
		if b.ID == CalendarID(col.ID) {
			shared = b
		}
	}
	if shared.ID == "" || shared.CanEdit || shared.CanShare || shared.Owner.Name != "Marie Curie" || shared.IsDefault {
		t.Fatalf("shared folder = %+v", shared)
	}
	rec = f.do(http.MethodGet, base+"/contacts", "einstein", "")
	if rec.Code != http.StatusOK || len(decodeBody[contactList](t, rec).Value) != 1 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	id := EventID(col.ID, "pierre.vcf")
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodPost, base + "/contacts", `{"displayName":"X"}`},
		{http.MethodPatch, base + "/contacts/" + id, `{"jobTitle":"X"}`},
		{http.MethodDelete, base + "/contacts/" + id, ""},
		{http.MethodPatch, base, `{"displayName":"Mine"}`},
		{http.MethodDelete, base, ""},
	} {
		rec := f.do(tc.method, tc.target, "einstein", tc.body)
		if rec.Code != http.StatusForbidden || decodeError(t, rec).Error.Code != "accessDenied" {
			t.Errorf("%s %s: %d %s", tc.method, tc.target, rec.Code, rec.Body.String())
		}
	}
	if _, err := f.st.GetObject(context.Background(), col.Ref, "pierre.vcf"); err != nil {
		t.Errorf("the owner's contact changed: %v", err)
	}
}

func TestContactCRUD(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinBook(t, f)

	rec := f.do(http.MethodPost, "/graph/v1.0/me/contacts", "einstein",
		`{"givenName":"Marie","surname":"Curie","jobTitle":"Professor","mobilePhone":"+33 1","emailAddresses":[{"address":"marie@example.org"}],"categories":["Physicists"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[contactBody](t, rec)
	if created.DisplayName != "Marie Curie" || created.ParentFolderID != CalendarID(collID) || created.ETag == "" || created.ID == "" {
		t.Fatalf("created = %+v", created)
	}
	gotColl, name, err := ParseEventID(created.ID)
	if err != nil || gotColl != collID {
		t.Fatalf("id %q: %v", created.ID, err)
	}
	// The card is in the store for DAV clients, as a valid vCard.
	obj, err := f.st.GetObject(context.Background(), ref, name)
	if err != nil {
		t.Fatal(err)
	}
	card, err := vcard.Parse(obj.Data)
	if err != nil || card.Version != "3.0" || card.UID() == "" {
		t.Fatalf("stored card: %v %+v", err, card)
	}

	rec = f.do(http.MethodGet, "/graph/v1.0/me/contacts/"+created.ID, "einstein", "")
	if rec.Code != http.StatusOK || decodeBody[contactBody](t, rec).Surname != "Curie" {
		t.Errorf("get: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+CalendarID(collID)+"/contacts/"+created.ID, "einstein", ""); rec.Code != http.StatusOK {
		t.Errorf("get below the folder: %d", rec.Code)
	}
	// The id is bound to its folder.
	other := f.do(http.MethodPost, "/graph/v1.0/me/contactFolders", "einstein", `{"displayName":"Other"}`)
	otherID := decodeBody[contactFolderBody](t, other).ID
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+otherID+"/contacts/"+created.ID, "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("contact below the wrong folder: %d", rec.Code)
	}
	rec = f.do(http.MethodGet, "/graph/v1.0/me/contacts", "einstein", "")
	if list := decodeBody[contactList](t, rec).Value; rec.Code != http.StatusOK || len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list: %d %s", rec.Code, rec.Body.String())
	}

	// A change keeps what it does not know.
	putCard(t, f, ref, "ada.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:ada\nFN:Ada Lovelace\nN:Lovelace;Ada;;;\nX-CUSTOM:keep me\nitem1.URL:https://ada.example\nitem1.X-ABLabel:Blog\nEND:VCARD\n")
	adaID := EventID(collID, "ada.vcf")
	rec = f.do(http.MethodPatch, "/graph/v1.0/me/contacts/"+adaID, "einstein", `{"jobTitle":"Countess","mobilePhone":"+44 1"}`)
	if rec.Code != http.StatusOK || decodeBody[contactBody](t, rec).JobTitle != "Countess" {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	obj, _ = f.st.GetObject(context.Background(), ref, "ada.vcf")
	for _, want := range []string{"X-CUSTOM:keep me", "ITEM1.URL:https://ada.example", "ITEM1.X-ABLABEL:Blog", "TITLE:Countess", "TEL;TYPE=CELL:+44 1"} {
		if !strings.Contains(string(obj.Data), want) {
			t.Errorf("stored card lost or lacks %q:\n%s", want, obj.Data)
		}
	}
	// A change that changes nothing does not write.
	before := obj.ETag
	if rec := f.do(http.MethodPatch, "/graph/v1.0/me/contacts/"+adaID, "einstein", `{"jobTitle":"Countess"}`); rec.Code != http.StatusOK {
		t.Fatalf("no-op patch: %d", rec.Code)
	}
	if obj, _ = f.st.GetObject(context.Background(), ref, "ada.vcf"); obj.ETag != before {
		t.Error("a patch without a change rewrote the card")
	}

	if rec := f.do(http.MethodDelete, "/graph/v1.0/me/contacts/"+adaID, "einstein", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if _, err := f.st.GetObject(context.Background(), ref, "ada.vcf"); err == nil {
		t.Error("deleted contact is still stored")
	}
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contacts/"+adaID, "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: %d", rec.Code)
	}
	if rec := f.do(http.MethodDelete, "/graph/v1.0/me/contacts/"+adaID, "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: %d", rec.Code)
	}
}

func TestContactIfMatch(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinBook(t, f)
	putCard(t, f, ref, "ada.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:ada\nFN:Ada\nEND:VCARD\n")
	id := EventID(collID, "ada.vcf")
	obj, _ := f.st.GetObject(context.Background(), ref, "ada.vcf")
	stale := httptest.NewRequest(http.MethodPatch, "/graph/v1.0/me/contacts/"+id, strings.NewReader(`{"jobTitle":"x"}`))
	stale.Header.Set("Authorization", "Bearer einstein")
	stale.Header.Set("Content-Type", "application/json")
	stale.Header.Set("If-Match", `W/"nope"`)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, stale)
	if rec.Code != http.StatusPreconditionFailed || decodeError(t, rec).Error.Code != "preconditionFailed" {
		t.Errorf("stale PATCH: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(f, http.MethodDelete, "/graph/v1.0/me/contacts/"+id, "einstein", "", "If-Match", `W/"nope"`); rec.Code != http.StatusPreconditionFailed {
		t.Errorf("stale DELETE: %d", rec.Code)
	}
	if rec := send(f, http.MethodPatch, "/graph/v1.0/me/contacts/"+id, "einstein", `{"jobTitle":"x"}`, "If-Match", etagHeader(obj.ETag)); rec.Code != http.StatusOK {
		t.Errorf("fresh PATCH: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGroupCardsAreHiddenAndListed(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinBook(t, f)
	putCard(t, f, ref, "ada.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:ada\nFN:Ada\nEND:VCARD\n")
	putCard(t, f, ref, "bob.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:bob\nFN:Bob\nEND:VCARD\n")
	putCard(t, f, ref, "club.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:club\nX-ADDRESSBOOKSERVER-KIND:group\nFN:Club\nX-ADDRESSBOOKSERVER-MEMBER:urn:uuid:ada\nEND:VCARD\n")
	rec := f.do(http.MethodGet, "/graph/v1.0/me/contacts", "einstein", "")
	list := decodeBody[contactList](t, rec).Value
	if len(list) != 2 || list[0].DisplayName != "Ada" || list[1].DisplayName != "Bob" {
		t.Fatalf("list = %+v", list)
	}
	if len(list[0].Groups) != 1 || list[0].Groups[0].DisplayName != "Club" || len(list[1].Groups) != 0 {
		t.Errorf("groups = %+v / %+v", list[0].Groups, list[1].Groups)
	}
	rec = f.do(http.MethodGet, "/graph/v1.0/me/contacts/"+EventID(collID, "ada.vcf"), "einstein", "")
	if got := decodeBody[contactBody](t, rec); len(got.Groups) != 1 {
		t.Errorf("single contact groups = %+v", got.Groups)
	}
	club := EventID(collID, "club.vcf")
	for _, m := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		body := ""
		if m == http.MethodPatch {
			body = `{"jobTitle":"x"}`
		}
		if rec := f.do(m, "/graph/v1.0/me/contacts/"+club, "einstein", body); rec.Code != http.StatusNotFound {
			t.Errorf("%s on a group card: %d", m, rec.Code)
		}
	}
	if _, err := f.st.GetObject(context.Background(), ref, "club.vcf"); err != nil {
		t.Errorf("the group card changed: %v", err)
	}
}

func TestContactPaging(t *testing.T) {
	f := newFixture(t)
	ref, _ := einsteinBook(t, f)
	for _, n := range []string{"c", "a", "b"} {
		putCard(t, f, ref, n+".vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:"+n+"\nFN:"+strings.ToUpper(n)+"\nEND:VCARD\n")
	}
	rec := f.do(http.MethodGet, "/graph/v1.0/me/contacts?$top=2", "einstein", "")
	page := decodeBody[contactList](t, rec)
	if len(page.Value) != 2 || page.Value[0].DisplayName != "A" || page.Value[1].DisplayName != "B" || page.NextLink == "" {
		t.Errorf("page 1 = %+v", page)
	}
	rec = f.do(http.MethodGet, "/graph/v1.0/me/contacts?$top=2&$skip=2", "einstein", "")
	if page := decodeBody[contactList](t, rec); len(page.Value) != 1 || page.Value[0].DisplayName != "C" || page.NextLink != "" {
		t.Errorf("page 2 = %+v", page)
	}
}

func TestContactRequestErrors(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinBook(t, f)
	putCard(t, f, ref, "ada.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:ada\nFN:Ada\nEND:VCARD\n")
	id := EventID(collID, "ada.vcf")
	for _, tc := range []struct {
		method, target, body string
		want                 int
	}{
		{http.MethodGet, "/graph/v1.0/me/contacts?$filter=displayName%20eq%20'x'", "", 501},
		{http.MethodGet, "/graph/v1.0/me/contacts?$expand=extensions", "", 501},
		{http.MethodGet, "/graph/v1.0/me/contacts?$search=ada", "", 501},
		{http.MethodGet, "/graph/v1.0/me/contacts/delta", "", 501},
		{http.MethodGet, "/graph/v1.0/me/contacts/" + id + "?$expand=extensions", "", 501},
		{http.MethodGet, "/graph/v1.0/me/contactFolders/" + CalendarID(collID) + "/childFolders", "", 501},
		{http.MethodGet, "/graph/v1.0/me/contacts/not-an-id", "", 400},
		{http.MethodGet, "/graph/v1.0/me/contacts/" + EventID("nope", "ada.vcf"), "", 404},
		{http.MethodGet, "/graph/v1.0/me/contacts/" + EventID(collID, "nope.vcf"), "", 404},
		{http.MethodPost, "/graph/v1.0/me/contacts", `[]`, 400},
		{http.MethodPost, "/graph/v1.0/me/contacts", `{"emailAddresses":[{"address":""}]}`, 400},
		{http.MethodPatch, "/graph/v1.0/me/contacts/" + id, `{"birthday":"someday"}`, 400},
		{http.MethodPut, "/graph/v1.0/me/contacts/" + id, `{}`, 405},
	} {
		rec := f.do(tc.method, tc.target, "einstein", tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d: %s", tc.method, tc.target, rec.Code, tc.want, rec.Body.String())
		}
	}
	// Nothing was created by the failed posts.
	objs, _ := f.st.ListObjects(context.Background(), ref, store.ListOptions{})
	if len(objs) != 1 {
		t.Errorf("%d objects stored, want 1", len(objs))
	}
	if rec := f.do(http.MethodPost, "/graph/v1.0/me/contacts", "einstein", `{"displayName":"x"}`); rec.Code != http.StatusCreated {
		t.Errorf("sanity: %d", rec.Code)
	}
}

type fixedBooks struct{ entries []ContactFolderEntry }

func (s fixedBooks) ContactFolders(context.Context, principals.Principal) ([]ContactFolderEntry, error) {
	return append([]ContactFolderEntry(nil), s.entries...), nil
}

// TestContactContractFixtures pins the JSON shapes of contact folders and
// contacts. The web app's tests load the same files. Run with -update to
// rewrite them.
func TestContactContractFixtures(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ref := store.CollectionRef{Owner: "sub-einstein", Kind: store.KindAddressBook, Name: "contacts"}
	sharedRef := store.CollectionRef{Owner: "sub-marie", Kind: store.KindAddressBook, Name: "team"}
	src := fixedBooks{entries: []ContactFolderEntry{
		{OwnerName: "Marie Curie", Collection: store.Collection{ID: "team-book", Ref: sharedRef, Revision: 7, Created: created, Access: store.AccessRead, Props: store.Props{DisplayName: "Team"}}},
		{OwnerName: "Albert Einstein", Collection: store.Collection{ID: "personal-book", Ref: ref, Revision: 1, Created: created, Access: store.AccessFull, Props: store.Props{DisplayName: "Contacts"}}},
	}}
	f := newFixture(t, func(cfg *Config) { cfg.ContactFolders = src })
	for _, r := range []store.CollectionRef{ref} {
		if _, err := f.st.CreateCollection(context.Background(), r, store.Props{DisplayName: "Contacts"}); err != nil {
			t.Fatal(err)
		}
	}
	for name, file := range map[string]string{"marie.vcf": "apple-v3.vcf", "albert.vcf": "thunderbird-v3.vcf", "group.vcf": "group-v4.vcf"} {
		data, err := os.ReadFile(filepath.Join("testdata", "contacts", file))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.st.PutObject(context.Background(), ref, name, data, store.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(name, target string) {
		t.Helper()
		rec := f.do(http.MethodGet, target, "einstein", "")
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, rec.Body.Bytes(), "", "  "); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		buf.WriteByte('\n')
		path := filepath.Join("testdata", "contract", name+".json")
		if *update {
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, buf.Bytes()) {
			t.Errorf("%s differs from %s; run with -update if the change is intended.\ngot:\n%s\nwant:\n%s", name, path, buf.Bytes(), want)
		}
	}
	check("contact-folders", "/graph/v1.0/me/contactFolders")
	check("contact-folder", "/graph/v1.0/me/contactFolders/"+CalendarID("personal-book"))
	check("contact-folder-shared", "/graph/v1.0/me/contactFolders/"+CalendarID("team-book"))
	check("contacts", "/graph/v1.0/me/contacts")
	check("contact", "/graph/v1.0/me/contacts/"+EventID("personal-book", "marie.vcf"))
}
