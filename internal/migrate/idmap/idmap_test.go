package idmap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
)

type fakeDirectory struct {
	users  map[string][]ocisgraph.User
	groups map[string][]ocisgraph.Group
	err    error
	calls  int
}

func (f *fakeDirectory) UsersByLogin(_ context.Context, _, login string) ([]ocisgraph.User, error) {
	f.calls++
	return f.users[login], f.err
}

func (f *fakeDirectory) GroupsByName(_ context.Context, _, name string) ([]ocisgraph.Group, error) {
	f.calls++
	return f.groups[name], f.err
}

func TestMapUsersAndGroupsFromGraph(t *testing.T) {
	dir := &fakeDirectory{
		users: map[string][]ocisgraph.User{
			"alice": {{ID: "o-alice", Username: "alice"}},
			"bob":   {{ID: "o-bob"}},
			"twin":  {{ID: "o-1"}, {ID: "o-2"}},
		},
		groups: map[string][]ocisgraph.Group{"team": {{ID: "o-team", Name: "team"}}},
	}
	m := New(WithDirectory(dir, "adm"))
	res, err := m.Map(context.Background(), []string{"alice", "bob", "carol", "twin"}, []string{"team", "staff"})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"alice": "o-alice", "bob": "o-bob"}; !reflect.DeepEqual(res.Users, want) {
		t.Errorf("users = %v, want %v", res.Users, want)
	}
	if want := map[string]string{"team": "o-team"}; !reflect.DeepEqual(res.Groups, want) {
		t.Errorf("groups = %v, want %v", res.Groups, want)
	}
	if want := []string{"team"}; !reflect.DeepEqual(res.GroupsByName, want) {
		t.Errorf("groups by name = %v, want %v", res.GroupsByName, want)
	}
	wantUn := []Unmapped{
		{Kind: User, ID: "carol", Reason: ReasonNotFound},
		{Kind: User, ID: "twin", Reason: ReasonAmbiguous},
		{Kind: Group, ID: "staff", Reason: ReasonNotFound},
	}
	if !reflect.DeepEqual(res.Unmapped, wantUn) {
		t.Errorf("unmapped = %+v, want %+v", res.Unmapped, wantUn)
	}
}

func TestCSVOverridesAndSkipsGraph(t *testing.T) {
	dir := &fakeDirectory{users: map[string][]ocisgraph.User{"alice": {{ID: "from-graph"}}}}
	m := New(WithDirectory(dir, "adm"))
	if err := m.LoadUsersCSV(strings.NewReader("classic_id,ocis_id\nalice,from-csv\n")); err != nil {
		t.Fatal(err)
	}
	res, err := m.Map(context.Background(), []string{"alice"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Users["alice"] != "from-csv" || dir.calls != 0 {
		t.Errorf("users = %v, graph calls = %d", res.Users, dir.calls)
	}
}

func TestCSVOnly(t *testing.T) {
	m := New()
	if err := m.LoadUsersCSV(strings.NewReader("alice,o-alice\n# a comment\n\nbob , o-bob \n")); err != nil {
		t.Fatal(err)
	}
	if err := m.LoadGroupsCSV(strings.NewReader("team,o-team\n")); err != nil {
		t.Fatal(err)
	}
	res, err := m.Map(context.Background(), []string{"alice", "bob", "carol"}, []string{"team", "staff"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Users["bob"] != "o-bob" || res.Groups["team"] != "o-team" || len(res.Unmapped) != 2 {
		t.Errorf("result = %+v", res)
	}
}

func TestCSVErrors(t *testing.T) {
	for name, in := range map[string]string{
		"one column":       "alice\n",
		"three columns":    "alice,o-1,extra\n",
		"empty id":         "alice,\n",
		"duplicate":        "alice,o-1\nalice,o-2\n",
		"unterminated":     "alice,\"o-1\n",
		"duplicate (case)": "alice,o-1\nalice,o-1\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := New().LoadUsersCSV(strings.NewReader(in)); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestDirectoryFailureAborts(t *testing.T) {
	dir := &fakeDirectory{err: ocisgraph.ErrUnauthorized}
	_, err := New(WithDirectory(dir, "bad")).Map(context.Background(), []string{"alice"}, nil)
	if !errors.Is(err, ocisgraph.ErrUnauthorized) {
		t.Fatalf("err = %v, want the refused token to abort the run", err)
	}
}

func TestNoSourceReportsEverythingUnmapped(t *testing.T) {
	res, err := New().Map(context.Background(), []string{"alice"}, []string{"team"})
	if err != nil || len(res.Unmapped) != 2 || len(res.Users) != 0 {
		t.Fatalf("result = %+v, %v", res, err)
	}
}

func TestGraphDirectoryEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer adm" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/graph/v1.0/users":
			if r.URL.Query().Get("$search") == `"alice"` {
				_, _ = w.Write([]byte(`{"value":[{"id":"o-alice","onPremisesSamAccountName":"alice"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"value":[]}`))
		case "/graph/v1.0/groups":
			_, _ = w.Write([]byte(`{"value":[{"id":"o-team","displayName":"team"}]}`))
		}
	}))
	defer srv.Close()
	gc, err := ocisgraph.New(ocisgraph.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	res, err := New(WithDirectory(gc, "adm")).Map(context.Background(), []string{"alice", "zed"}, []string{"team"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Users["alice"] != "o-alice" || res.Groups["team"] != "o-team" || len(res.Unmapped) != 1 || res.Unmapped[0].ID != "zed" {
		t.Errorf("result = %+v", res)
	}
}
