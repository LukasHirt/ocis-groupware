package graphapi

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

var update = flag.Bool("update", false, "rewrite the contract fixtures")

type calendarBody struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	HexColor          string `json:"hexColor"`
	IsDefaultCalendar bool   `json:"isDefaultCalendar"`
	CanShare          bool   `json:"canShare"`
	CanEdit           bool   `json:"canEdit"`
	Owner             struct {
		Name string `json:"name"`
	} `json:"owner"`
}

type calendarList struct {
	Value    []calendarBody `json:"value"`
	NextLink string         `json:"@odata.nextLink"`
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return v
}

func listCalendarsOf(t *testing.T, f *fixture, token string) []calendarBody {
	t.Helper()
	rec := f.do(http.MethodGet, "/graph/v1.0/me/calendars", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[calendarList](t, rec).Value
}

func TestListAndGetCalendars(t *testing.T) {
	f := newFixture(t)
	list := listCalendarsOf(t, f, "einstein")
	if len(list) != 1 {
		t.Fatalf("got %d calendars, want the default one", len(list))
	}
	cal := list[0]
	if cal.Name != "Personal" || !cal.IsDefaultCalendar || !cal.CanEdit || !cal.CanShare || cal.Owner.Name != "Albert Einstein" {
		t.Errorf("default calendar = %+v", cal)
	}

	rec := f.do(http.MethodGet, "/graph/v1.0/me/calendar", "einstein", "")
	if rec.Code != http.StatusOK || decodeBody[calendarBody](t, rec).ID != cal.ID {
		t.Errorf("/me/calendar: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, "/graph/v1.0/me/calendars/"+cal.ID, "einstein", "")
	if rec.Code != http.StatusOK || decodeBody[calendarBody](t, rec).ID != cal.ID {
		t.Errorf("/me/calendars/{id}: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/calendars/"+CalendarID("nope"), "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: %d, want 404", rec.Code)
	}
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/calendars/***", "einstein", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed id: %d, want 400", rec.Code)
	}
}

func TestCreatePatchDeleteCalendar(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodPost, "/graph/v1.0/me/calendars", "einstein", `{"name":"Lectures","hexColor":"#1A2B3C"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	cal := decodeBody[calendarBody](t, rec)
	if cal.Name != "Lectures" || cal.HexColor != "#1a2b3c" || cal.IsDefaultCalendar || !cal.CanEdit {
		t.Errorf("created = %+v", cal)
	}
	collID, err := ParseCalendarID(cal.ID)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := f.st.ListCollections(context.Background(), "sub-einstein", store.KindCalendar)
	if err != nil || len(cols) != 2 {
		t.Fatalf("store has %d calendars, %v", len(cols), err)
	}
	var stored store.Collection
	for _, c := range cols {
		if c.ID == collID {
			stored = c
		}
	}
	if stored.Props.DisplayName != "Lectures" || stored.Props.Color != "#1a2b3c" || len(stored.Props.Components) != 1 || stored.Props.Components[0] != "VEVENT" {
		t.Errorf("stored props = %+v", stored.Props)
	}
	// The default stays the oldest.
	for _, c := range listCalendarsOf(t, f, "einstein") {
		if c.IsDefaultCalendar != (c.Name == "Personal") {
			t.Errorf("default flag on %q = %v", c.Name, c.IsDefaultCalendar)
		}
	}

	target := "/graph/v1.0/me/calendars/" + cal.ID
	rec = f.do(http.MethodPatch, target, "einstein", `{"name":"Teaching","hexColor":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[calendarBody](t, rec); got.Name != "Teaching" || got.HexColor != "" {
		t.Errorf("patched = %+v", got)
	}
	col, _ := f.st.GetCollection(context.Background(), stored.Ref)
	if col.Props.DisplayName != "Teaching" || col.Props.Color != "" {
		t.Errorf("stored after patch = %+v", col.Props)
	}
	// A body without known fields changes nothing.
	if rec := f.do(http.MethodPatch, target, "einstein", `{"other":1}`); rec.Code != http.StatusOK {
		t.Errorf("empty patch: %d", rec.Code)
	}
	if after, _ := f.st.GetCollection(context.Background(), stored.Ref); after.Revision != col.Revision {
		t.Errorf("empty patch changed the revision")
	}

	if rec := f.do(http.MethodDelete, target, "einstein", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := f.st.GetCollection(context.Background(), stored.Ref); err == nil {
		t.Error("the calendar is still in the store")
	}
	if rec := f.do(http.MethodDelete, target, "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: %d, want 404", rec.Code)
	}
}

func TestPatchDefaultCalendar(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodPatch, "/graph/v1.0/me/calendar", "einstein", `{"name":"Mine"}`)
	if rec.Code != http.StatusOK || decodeBody[calendarBody](t, rec).Name != "Mine" {
		t.Fatalf("patch /me/calendar: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodDelete, "/graph/v1.0/me/calendar", "einstein", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("delete /me/calendar: %d, want 405", rec.Code)
	}
}

func TestCalendarRequestValidation(t *testing.T) {
	f := newFixture(t)
	listCalendarsOf(t, f, "einstein")
	post := func(body, contentType string) int {
		req := httptest.NewRequest(http.MethodPost, "/graph/v1.0/me/calendars", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer einstein")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, tc := range []struct {
		name, body, ct string
		want           int
	}{
		{"no name", `{}`, "application/json", 400},
		{"empty name", `{"name":"  "}`, "application/json", 400},
		{"control character", `{"name":"a\u0000b"}`, "application/json", 400},
		{"long name", `{"name":"` + strings.Repeat("x", 256) + `"}`, "application/json", 400},
		{"bad colour", `{"name":"x","hexColor":"red"}`, "application/json", 400},
		{"not json", `nope`, "application/json", 400},
		{"two values", `{"name":"x"}{"name":"y"}`, "application/json", 400},
		{"wrong type", `{"name":"x"}`, "text/plain", 415},
		{"no type", `{"name":"x"}`, "", 415},
		{"too large", `{"name":"` + strings.Repeat("x", maxJSONBody) + `"}`, "application/json", 413},
		{"charset is fine", `{"name":"x"}`, "application/json; charset=utf-8", 201},
	} {
		if got := post(tc.body, tc.ct); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
	if n := len(listCalendarsOf(t, f, "einstein")); n != 2 {
		t.Errorf("%d calendars after the refused requests, want the default and one created", n)
	}
}

func TestCalendarsArePrivate(t *testing.T) {
	f := newFixture(t)
	mine := listCalendarsOf(t, f, "einstein")[0]
	listCalendarsOf(t, f, "marie")
	for _, m := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		body := ""
		if m == http.MethodPatch {
			body = `{"name":"mine now"}`
		}
		rec := f.do(m, "/graph/v1.0/me/calendars/"+mine.ID, "marie", body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s of another user's calendar: %d, want 404", m, rec.Code)
		}
	}
	if got := listCalendarsOf(t, f, "einstein")[0]; got.Name != "Personal" {
		t.Errorf("einstein's calendar is now %+v", got)
	}
}

func TestCalendarsPaging(t *testing.T) {
	f := newFixture(t)
	for _, n := range []string{"a", "b"} {
		if rec := f.do(http.MethodPost, "/graph/v1.0/me/calendars", "einstein", `{"name":"`+n+`"}`); rec.Code != 201 {
			t.Fatal(rec.Body.String())
		}
	}
	rec := f.do(http.MethodGet, "/graph/v1.0/me/calendars?$top=2", "einstein", "")
	page := decodeBody[calendarList](t, rec)
	if len(page.Value) != 2 || !strings.Contains(page.NextLink, "%24skip=2") {
		t.Fatalf("page = %d items, next %q", len(page.Value), page.NextLink)
	}
	rec = f.do(http.MethodGet, "/graph/v1.0/me/calendars?$top=2&$skip=2", "einstein", "")
	if last := decodeBody[calendarList](t, rec); len(last.Value) != 1 || last.NextLink != "" {
		t.Errorf("last page = %+v", last)
	}
}

// fixedSource shows a user a fixed list of calendars.
type fixedSource struct{ entries []CalendarEntry }

func (s fixedSource) Calendars(context.Context, principals.Principal) ([]CalendarEntry, error) {
	return append([]CalendarEntry(nil), s.entries...), nil
}

// sharedFixture gives einstein his own calendars plus one of marie's at the
// given access level, through a source that knows about sharing.
func sharedFixture(t *testing.T, access store.Access) (*fixture, store.Collection) {
	t.Helper()
	var shared store.Collection
	f := newFixture(t, func(cfg *Config) {
		cfg.Calendars = sharedSource{st: cfg.Store, access: access, shared: &shared}
	})
	listCalendarsOf(t, f, "einstein")
	ref := store.CollectionRef{Owner: "sub-marie", Kind: store.KindCalendar, Name: "team"}
	col, err := f.st.CreateCollection(context.Background(), ref, store.Props{DisplayName: "Team", Components: []string{"VEVENT"}})
	if err != nil {
		t.Fatal(err)
	}
	shared = col
	return f, col
}

type sharedSource struct {
	st     store.Store
	access store.Access
	shared *store.Collection
}

func (s sharedSource) Calendars(ctx context.Context, user principals.Principal) ([]CalendarEntry, error) {
	own, err := ownCalendars{st: s.st}.Calendars(ctx, user)
	if err != nil || s.shared.ID == "" {
		return own, err
	}
	c := *s.shared
	c.Access = s.access
	return append(own, CalendarEntry{Collection: c, OwnerName: "Marie Curie"}), nil
}

func TestSharedCalendars(t *testing.T) {
	for _, tc := range []struct {
		access          store.Access
		canEdit, canShr bool
	}{
		{store.AccessRead, false, false},
		{store.AccessObjects, true, false},
	} {
		f, col := sharedFixture(t, tc.access)
		list := listCalendarsOf(t, f, "einstein")
		if len(list) != 2 {
			t.Fatalf("%v: %d calendars, want own and shared", tc.access, len(list))
		}
		got := list[1]
		if got.ID != CalendarID(col.ID) || got.Name != "Team" || got.Owner.Name != "Marie Curie" ||
			got.CanEdit != tc.canEdit || got.CanShare != tc.canShr || got.IsDefaultCalendar {
			t.Errorf("%v: shared calendar = %+v", tc.access, got)
		}
		if !list[0].IsDefaultCalendar {
			t.Errorf("%v: the own calendar is not the default", tc.access)
		}

		target := "/graph/v1.0/me/calendars/" + got.ID
		if rec := f.do(http.MethodGet, target, "einstein", ""); rec.Code != http.StatusOK {
			t.Errorf("%v: GET: %d", tc.access, rec.Code)
		}
		rec := f.do(http.MethodPatch, target, "einstein", `{"name":"Mine"}`)
		if rec.Code != http.StatusForbidden || decodeError(t, rec).Error.Code != "accessDenied" {
			t.Errorf("%v: PATCH: %d %s", tc.access, rec.Code, rec.Body.String())
		}
		if rec := f.do(http.MethodDelete, target, "einstein", ""); rec.Code != http.StatusForbidden {
			t.Errorf("%v: DELETE: %d, want 403", tc.access, rec.Code)
		}
		after, err := f.st.GetCollection(context.Background(), col.Ref)
		if err != nil || after.Props.DisplayName != "Team" {
			t.Errorf("%v: the owner's calendar changed: %+v, %v", tc.access, after.Props, err)
		}
	}
}

// TestContractFixtures pins the JSON shapes of calendars. The web app's tests
// load the same files, so a change here that the UI does not follow fails
// there. Run with -update to rewrite them.
func TestContractFixtures(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mk := func(id, name, color string, owner string, rev uint64, access store.Access, offset time.Duration) CalendarEntry {
		return CalendarEntry{OwnerName: owner, Collection: store.Collection{
			ID: id, Revision: rev, Created: created.Add(offset), Access: access,
			Ref:   store.CollectionRef{Owner: "sub-" + owner, Kind: store.KindCalendar, Name: id},
			Props: store.Props{DisplayName: name, Color: color},
		}}
	}
	// The own owner is the caller, so the fixture user must match.
	src := fixedSource{entries: []CalendarEntry{
		mk("shared-cal", "Team", "", "Marie Curie", 7, store.AccessRead, 0),
		mk("work-cal", "Work", "#3366ccff", "Albert Einstein", 3, store.AccessFull, time.Hour),
		mk("personal-cal", "Personal", "", "Albert Einstein", 1, store.AccessFull, 0),
	}}
	for i := range src.entries {
		if src.entries[i].OwnerName == "Albert Einstein" {
			src.entries[i].Collection.Ref.Owner = "sub-einstein"
		}
	}
	f := newFixture(t, func(cfg *Config) { cfg.Calendars = src })

	check := func(name, method, target, body string, status int) {
		t.Helper()
		rec := f.do(method, target, "einstein", body)
		if rec.Code != status {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, rec.Body.Bytes(), "", "  "); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		buf.WriteByte('\n')
		path := filepath.Join("testdata", "contract", name+".json")
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
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
	check("calendars", http.MethodGet, "/graph/v1.0/me/calendars", "", 200)
	check("calendar", http.MethodGet, "/graph/v1.0/me/calendar", "", 200)
	check("calendar-shared", http.MethodGet, "/graph/v1.0/me/calendars/"+CalendarID("shared-cal"), "", 200)
	check("calendars-page", http.MethodGet, "/graph/v1.0/me/calendars?$top=1", "", 200)
}
