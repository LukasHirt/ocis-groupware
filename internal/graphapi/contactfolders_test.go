package graphapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/LukasHirt/groupkit/store"
)

func (sf *shareFixture) einsteinBook(t *testing.T) (id string, ref store.CollectionRef) {
	t.Helper()
	ref = store.CollectionRef{Owner: "sub-einstein", Kind: store.KindAddressBook, Name: "friends"}
	col, err := sf.st.CreateCollection(context.Background(), ref, store.Props{DisplayName: "Friends"})
	if err != nil {
		t.Fatal(err)
	}
	return CalendarID(col.ID), ref
}

func bookPerms(id string, more ...string) string {
	u := "/graph/v1.0/me/contactFolders/" + id + "/permissions"
	for _, m := range more {
		u += m
	}
	return u
}

func TestContactFolderPermissions(t *testing.T) {
	sf := newShareFixture(t)
	id, ref := sf.einsteinBook(t)

	rec := sf.do(http.MethodPost, bookPerms(id), "einstein", grantMarieRead)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	p := decodeBody[permissionBody](t, rec)
	if p.Role != "read" || p.Grantee.ID != "u-marie" || p.EmailAddress.Name != "Marie Curie" {
		t.Errorf("created = %+v", p)
	}
	if m := sf.shares.Members(ref); len(m) != 1 || m[0].Access != "read" {
		t.Fatalf("members = %+v", m)
	}
	compareContract(t, "contactfolder-permission", rec.Body.Bytes())

	// Marie sees the folder among hers, read only.
	rec = sf.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+id, "marie", "")
	if rec.Code != 200 {
		t.Fatalf("recipient reads the shared folder: %d %s", rec.Code, rec.Body.String())
	}
	if f := decodeBody[struct {
		Name string `json:"displayName"`
		Edit bool   `json:"groupware.canEdit"`
	}](t, rec); f.Name != "Friends" || f.Edit {
		t.Errorf("shared folder as marie sees it = %+v", f)
	}

	rec = sf.do(http.MethodGet, bookPerms(id), "einstein", "")
	if rec.Code != 200 || len(decodeBody[permissionList](t, rec).Value) != 1 {
		t.Errorf("list: %d %s", rec.Code, rec.Body.String())
	}
	rec = sf.do(http.MethodPatch, bookPerms(id, "/", p.ID), "einstein", `{"role":"write"}`)
	if rec.Code != 200 || decodeBody[permissionBody](t, rec).Role != "write" {
		t.Errorf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if m := sf.shares.Members(ref); m[0].Access != "readwrite" {
		t.Errorf("access after patch = %q", m[0].Access)
	}

	// The recipient can reach the folder by its id but not manage it.
	for _, c := range []struct{ method, url, body string }{
		{http.MethodGet, bookPerms(id), ""},
		{http.MethodPost, bookPerms(id), `{"role":"read","grantee":{"type":"user","id":"u-newton"}}`},
		{http.MethodPatch, bookPerms(id, "/", p.ID), `{"role":"read"}`},
		{http.MethodDelete, bookPerms(id, "/", p.ID), ""},
	} {
		if rec := sf.do(c.method, c.url, "marie", c.body); rec.Code != http.StatusForbidden {
			t.Errorf("recipient %s: %d, want 403", c.method, rec.Code)
		}
	}

	if rec := sf.do(http.MethodDelete, bookPerms(id, "/", p.ID), "einstein", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if m := sf.shares.Members(ref); len(m) != 0 {
		t.Errorf("members after revoke = %+v", m)
	}
	// Now marie is a stranger.
	if rec := sf.do(http.MethodGet, bookPerms(id), "marie", ""); rec.Code != http.StatusNotFound {
		t.Errorf("stranger: %d, want 404", rec.Code)
	}
	if rec := sf.do(http.MethodGet, bookPerms("!!"), "einstein", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed id: %d, want 400", rec.Code)
	}
	if rec := sf.do(http.MethodGet, bookPerms(CalendarID("nope")), "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown folder: %d, want 404", rec.Code)
	}
}

func TestCalendarIDIsNotAContactFolder(t *testing.T) {
	sf := newShareFixture(t)
	cal := sf.einsteinCalendar(t)
	if rec := sf.do(http.MethodGet, bookPerms(cal), "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("calendar id as a folder: %d, want 404", rec.Code)
	}
}
