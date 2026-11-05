package graphapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// rawList decodes a list answer without a typed item, so a test sees exactly
// which fields are present.
type rawList struct {
	Value    []map[string]json.RawMessage `json:"value"`
	NextLink string                       `json:"@odata.nextLink"`
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// q builds a request target with the options encoded.
func q(path string, opts ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(opts); i += 2 {
		v.Set(opts[i], opts[i+1])
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

func seedEvents(t *testing.T, f *fixture) {
	t.Helper()
	ref, collID := einsteinCalendar(t, f)
	putEvent(t, f, ref, collID, "b.ics", ics(vevent("b", "DTSTART:20261106T090000Z", "DTEND:20261106T100000Z", "SUMMARY:Bravo")))
	putEvent(t, f, ref, collID, "a.ics", ics(vevent("a", "DTSTART:20261107T090000Z", "DTEND:20261107T100000Z", "SUMMARY:alpha")))
	putEvent(t, f, ref, collID, "c.ics", ics(vevent("c", "DTSTART:20261105T090000Z", "DTEND:20261108T120000Z", "SUMMARY:Charlie")))
}

func subjects(l rawList) []string {
	var out []string
	for _, it := range l.Value {
		var s string
		_ = json.Unmarshal(it["subject"], &s)
		out = append(out, s)
	}
	return out
}

func TestSelectOnEvents(t *testing.T) {
	f := newFixture(t)
	seedEvents(t, f)

	rec := f.do(http.MethodGet, q("/graph/v1.0/me/events", "$select", "subject,isAllDay"), "einstein", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	l := decodeBody[rawList](t, rec)
	if len(l.Value) != 3 {
		t.Fatalf("items = %d", len(l.Value))
	}
	want := []string{"@odata.etag", "id", "isAllDay", "subject"}
	if got := keysOf(l.Value[0]); !slices.Equal(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}

	// One item is selected the same way.
	var id string
	_ = json.Unmarshal(l.Value[0]["id"], &id)
	rec = f.do(http.MethodGet, q("/graph/v1.0/me/events/"+id, "$select", "subject"), "einstein", "")
	one := decodeBody[map[string]json.RawMessage](t, rec)
	if got := keysOf(one); !slices.Equal(got, []string{"@odata.etag", "id", "subject"}) {
		t.Errorf("single item fields = %v", got)
	}

	// Without $select nothing is dropped.
	rec = f.do(http.MethodGet, "/graph/v1.0/me/events", "einstein", "")
	if full := decodeBody[rawList](t, rec); len(full.Value[0]) < 15 {
		t.Errorf("full event has %d fields", len(full.Value[0]))
	}
}

func TestSelectErrors(t *testing.T) {
	f := newFixture(t)
	seedEvents(t, f)
	for _, tc := range []struct {
		target string
		want   int
	}{
		{q("/graph/v1.0/me/events", "$select", "nope"), 400},
		{q("/graph/v1.0/me/events", "$select", "subject,,body"), 400},
		{q("/graph/v1.0/me/events", "$select", ""), 400},
		{q("/graph/v1.0/me/events", "$select", "start/dateTime"), 501},
		{q("/graph/v1.0/me/events", "$select", "Subject"), 400},
		{q("/graph/v1.0/me/calendars", "$select", "name"), 200},
		{q("/graph/v1.0/me/calendars", "$orderby", "name"), 501},
	} {
		if rec := f.do(http.MethodGet, tc.target, "einstein", ""); rec.Code != tc.want {
			t.Errorf("%s = %d, want %d: %s", tc.target, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestOrderByEvents(t *testing.T) {
	f := newFixture(t)
	seedEvents(t, f)
	for _, tc := range []struct {
		orderby string
		want    []string
	}{
		{"subject", []string{"alpha", "Bravo", "Charlie"}},
		{"subject desc", []string{"Charlie", "Bravo", "alpha"}},
		{"start/dateTime", []string{"Charlie", "Bravo", "alpha"}},
		{"start/dateTime desc", []string{"alpha", "Bravo", "Charlie"}},
		{"end/dateTime asc", []string{"Bravo", "alpha", "Charlie"}},
	} {
		rec := f.do(http.MethodGet, q("/graph/v1.0/me/events", "$orderby", tc.orderby), "einstein", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.orderby, rec.Code, rec.Body.String())
		}
		if got := subjects(decodeBody[rawList](t, rec)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.orderby, got, tc.want)
		}
	}
	for _, tc := range []struct {
		orderby string
		want    int
	}{
		{"nope", 400},
		{"subject,start/dateTime", 501},
		{"subject sideways", 400},
		{"", 400},
		{"body/content", 501},
	} {
		if rec := f.do(http.MethodGet, q("/graph/v1.0/me/events", "$orderby", tc.orderby), "einstein", ""); rec.Code != tc.want {
			t.Errorf("$orderby=%q = %d, want %d: %s", tc.orderby, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestOrderThenPageThenSelect(t *testing.T) {
	f := newFixture(t)
	seedEvents(t, f)
	rec := f.do(http.MethodGet, q("/graph/v1.0/me/events", "$orderby", "subject desc", "$top", "2", "$select", "subject"), "einstein", "")
	l := decodeBody[rawList](t, rec)
	if got := subjects(l); !slices.Equal(got, []string{"Charlie", "Bravo"}) {
		t.Errorf("page 1 = %v", got)
	}
	for _, k := range []string{"%24orderby", "%24select", "%24skip=2"} {
		if !strings.Contains(l.NextLink, k) {
			t.Errorf("nextLink %q lacks %s", l.NextLink, k)
		}
	}
	rec = f.do(http.MethodGet, q("/graph/v1.0/me/events", "$orderby", "subject desc", "$top", "2", "$skip", "2"), "einstein", "")
	if got := subjects(decodeBody[rawList](t, rec)); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("page 2 = %v", got)
	}
}

func TestOrderByAndSelectOnCalendarView(t *testing.T) {
	f := newFixture(t)
	seedEvents(t, f)
	rec := f.do(http.MethodGet, q("/graph/v1.0/me/calendarView",
		"startDateTime", "2026-11-01T00:00:00Z", "endDateTime", "2026-12-01T00:00:00Z",
		"$orderby", "subject", "$select", "subject"), "einstein", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := subjects(decodeBody[rawList](t, rec)); !slices.Equal(got, []string{"alpha", "Bravo", "Charlie"}) {
		t.Errorf("order = %v", got)
	}
}

func TestSelectAndOrderByContacts(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinBook(t, f)
	putCard(t, f, ref, "1.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:1\nFN:Zed Adams\nN:Adams;Zed;;;\nORG:Initech\nEND:VCARD\n")
	putCard(t, f, ref, "2.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:2\nFN:Ann Zimmer\nN:Zimmer;Ann;;;\nORG:Acme\nEND:VCARD\n")
	id := EventID(collID, "1.vcf")

	get := func(opts ...string) (int, rawList) {
		rec := f.do(http.MethodGet, q("/graph/v1.0/me/contacts", opts...), "einstein", "")
		if rec.Code != http.StatusOK {
			return rec.Code, rawList{}
		}
		return rec.Code, decodeBody[rawList](t, rec)
	}
	display := func(l rawList) []string {
		var out []string
		for _, it := range l.Value {
			var s string
			_ = json.Unmarshal(it["displayName"], &s)
			out = append(out, s)
		}
		return out
	}
	_, l := get("$orderby", "surname desc")
	if got := display(l); !slices.Equal(got, []string{"Ann Zimmer", "Zed Adams"}) {
		t.Errorf("surname desc = %v", got)
	}
	_, l = get("$orderby", "companyName")
	if got := display(l); !slices.Equal(got, []string{"Ann Zimmer", "Zed Adams"}) {
		t.Errorf("companyName = %v", got)
	}
	_, l = get("$select", "displayName,groupware.hasPhoto")
	if got := keysOf(l.Value[0]); !slices.Equal(got, []string{"@odata.etag", "displayName", "groupware.hasPhoto", "id"}) {
		t.Errorf("fields = %v", got)
	}
	rec := f.do(http.MethodGet, q("/graph/v1.0/me/contacts/"+id, "$select", "givenName"), "einstein", "")
	if got := keysOf(decodeBody[map[string]json.RawMessage](t, rec)); !slices.Equal(got, []string{"@odata.etag", "givenName", "id"}) {
		t.Errorf("single = %v", got)
	}
	if code, _ := get("$select", "nope"); code != 400 {
		t.Errorf("unknown field = %d", code)
	}
	if code, _ := get("$orderby", "nope"); code != 400 {
		t.Errorf("unknown key = %d", code)
	}
	if code, _ := get("$orderby", "homeAddress"); code != 501 {
		t.Errorf("unlisted key = %d", code)
	}
}
