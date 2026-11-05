package ocisgraph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestUsersByLogin(t *testing.T) {
	var gotAuth, gotPath, gotFilter, gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotFilter, gotRaw = r.Header.Get("Authorization"), r.URL.Path, r.URL.Query().Get("$filter"), r.URL.RawQuery
		_, _ = w.Write([]byte(`{"value":[{"id":"u-9","displayName":"O'Neil","mail":"o@example.org","onPremisesSamAccountName":"o'neil"}]}`))
	}))
	defer srv.Close()

	users, err := newClient(t, srv.URL).UsersByLogin(context.Background(), "adm", "o'neil")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer adm" || gotPath != "/graph/v1.0/users" {
		t.Errorf("request: %q %q", gotAuth, gotPath)
	}
	// A quote in the login is doubled so it cannot end the string.
	if want := `onPremisesSamAccountName eq 'o''neil'`; gotFilter != want {
		t.Errorf("filter = %q, want %q (raw %q)", gotFilter, want, gotRaw)
	}
	want := []User{{ID: "u-9", Username: "o'neil", DisplayName: "O'Neil", Mail: "o@example.org"}}
	if !reflect.DeepEqual(users, want) {
		t.Errorf("users = %+v, want %+v", users, want)
	}
}

func TestUsersByLoginNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":[]}`))
	}))
	defer srv.Close()
	users, err := newClient(t, srv.URL).UsersByLogin(context.Background(), "t", "nobody")
	if err != nil || len(users) != 0 {
		t.Fatalf("users = %+v, %v", users, err)
	}
}

func TestGroupsByNameKeepsExactMatches(t *testing.T) {
	var gotSearch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSearch = r.URL.Query().Get("$search")
		_, _ = w.Write([]byte(`{"value":[
			{"id":"g-1","displayName":"team"},
			{"id":"g-2","displayName":"team-leads"},
			{"id":"g-3","displayName":"Team"},
			{"displayName":"team"}]}`))
	}))
	defer srv.Close()
	groups, err := newClient(t, srv.URL).GroupsByName(context.Background(), "adm", "team")
	if err != nil {
		t.Fatal(err)
	}
	if gotSearch != `"team"` {
		t.Errorf("search = %q", gotSearch)
	}
	if want := []Group{{ID: "g-1", Name: "team"}}; !reflect.DeepEqual(groups, want) {
		t.Errorf("groups = %+v, want %+v", groups, want)
	}
}

func TestGroupsByNameFollowsPages(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"value":[{"id":"g-2","displayName":"team"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":[{"id":"g-1","displayName":"team"}],"@odata.nextLink":"` + srv.URL + `/graph/v1.0/groups?page=2"}`))
	}))
	defer srv.Close()
	groups, err := newClient(t, srv.URL).GroupsByName(context.Background(), "adm", "team")
	if err != nil || len(groups) != 2 {
		t.Fatalf("groups = %+v, %v", groups, err)
	}
}

func TestLookupRefusedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	if _, err := c.UsersByLogin(context.Background(), "bad", "a"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("users: %v", err)
	}
	if _, err := c.GroupsByName(context.Background(), "bad", "a"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("groups: %v", err)
	}
}
