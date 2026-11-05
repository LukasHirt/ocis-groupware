package graphapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"
)

type eventList struct {
	Value    []eventJSON `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

// ics wraps VEVENT lines into a stored object.
func ics(lines ...string) []byte {
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCALENDAR\r\n")
}

func vevent(uid string, props ...string) string {
	return "BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20261101T080000Z\r\n" + strings.Join(props, "\r\n") + "\r\nEND:VEVENT"
}

// putEvent stores an object in a calendar and returns its Graph event id.
func putEvent(t *testing.T, f *fixture, ref store.CollectionRef, collID, name string, data []byte) string {
	t.Helper()
	if _, err := f.st.PutObject(context.Background(), ref, name, data, store.PutOptions{}); err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
	return EventID(collID, name)
}

// einsteinCalendar returns the default calendar of einstein.
func einsteinCalendar(t *testing.T, f *fixture) (store.CollectionRef, string) {
	t.Helper()
	cals := listCalendarsOf(t, f, "einstein")
	id, err := ParseCalendarID(cals[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := f.st.ListCollections(context.Background(), "sub-einstein", store.KindCalendar)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		if c.ID == id {
			return c.Ref, c.ID
		}
	}
	t.Fatal("default calendar not in the store")
	return store.CollectionRef{}, ""
}

func getEvents(t *testing.T, f *fixture, target string, hdr ...string) (*httptest.ResponseRecorder, eventList) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer einstein")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	var l eventList
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), `"value"`) {
		l = decodeBody[eventList](t, rec)
	}
	return rec, l
}

func TestListEvents(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id1 := putEvent(t, f, ref, collID, "a.ics", ics(vevent("a", "DTSTART:20261105T100000Z", "DTEND:20261105T110000Z", "SUMMARY:First")))
	putEvent(t, f, ref, collID, "b.ics", ics(vevent("b", "DTSTART:20261106T100000Z", "DTEND:20261106T110000Z", "RRULE:FREQ=DAILY;COUNT=3", "SUMMARY:Series")))
	// An event of another calendar of the same user is not in /me/events.
	rec := f.do(http.MethodPost, "/graph/v1.0/me/calendars", "einstein", `{"name":"Other"}`)
	other := decodeBody[calendarBody](t, rec)
	otherID, _ := ParseCalendarID(other.ID)
	otherRef := store.CollectionRef{Owner: "sub-einstein", Kind: store.KindCalendar, Name: ""}
	cols, _ := f.st.ListCollections(context.Background(), "sub-einstein", store.KindCalendar)
	for _, c := range cols {
		if c.ID == otherID {
			otherRef = c.Ref
		}
	}
	putEvent(t, f, otherRef, otherID, "c.ics", ics(vevent("c", "DTSTART:20261105T100000Z", "DTEND:20261105T110000Z", "SUMMARY:Elsewhere")))

	for _, target := range []string{"/graph/v1.0/me/events", "/graph/v1.0/me/calendar/events", "/graph/v1.0/me/calendars/" + CalendarID(collID) + "/events"} {
		rec, list := getEvents(t, f, target)
		if rec.Code != http.StatusOK || len(list.Value) != 2 {
			t.Fatalf("%s: %d %s", target, rec.Code, rec.Body.String())
		}
		first, series := list.Value[0], list.Value[1]
		if first.ID != id1 || first.Subject != "First" || first.Type != "singleInstance" || first.ETag == "" || first.ICalUID != "a" {
			t.Errorf("%s: first = %+v", target, first)
		}
		if series.Type != "seriesMaster" || series.Recurrence == nil || series.Recurrence.Range.NumberOfOccurrences != 3 {
			t.Errorf("%s: series = %+v", target, series)
		}
	}
	rec, list := getEvents(t, f, "/graph/v1.0/me/calendars/"+other.ID+"/events")
	if rec.Code != http.StatusOK || len(list.Value) != 1 || list.Value[0].Subject != "Elsewhere" {
		t.Errorf("other calendar: %d %s", rec.Code, rec.Body.String())
	}
	rec, list = getEvents(t, f, "/graph/v1.0/me/events?$top=1")
	if len(list.Value) != 1 || list.NextLink == "" {
		t.Errorf("paging: %s", rec.Body.String())
	}
	// A calendar of someone else is not there.
	if rec, _ := getEvents(t, f, "/graph/v1.0/me/calendars/"+CalendarID("nope")+"/events"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown calendar: %d", rec.Code)
	}
}

func TestUnsupportedODataQueryOptions(t *testing.T) {
	f := newFixture(t)
	for _, q := range []string{"$filter=subject ne 'x'", "$expand=attachments", "$count=true", "$search=x"} {
		target := "/graph/v1.0/me/events?" + strings.Replace(url.QueryEscape(q), "%3D", "=", 1)
		if rec, _ := getEvents(t, f, target); rec.Code != http.StatusNotImplemented {
			t.Errorf("%s: %d, want 501", q, rec.Code)
		}
	}
}

func TestGetEventAndOccurrence(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	data := ics(
		vevent("s", "DTSTART:20261105T090000Z", "DTEND:20261105T100000Z", "RRULE:FREQ=WEEKLY;COUNT=4", "EXDATE:20261112T090000Z", "SUMMARY:Series"),
		vevent("s", "RECURRENCE-ID:20261119T090000Z", "DTSTART:20261119T130000Z", "DTEND:20261119T140000Z", "SUMMARY:Moved"),
	)
	id := putEvent(t, f, ref, collID, "s.ics", data)

	rec, _ := getEvents(t, f, "/graph/v1.0/me/events/"+id)
	e := decodeBody[eventJSON](t, rec)
	if rec.Code != 200 || e.ID != id || e.Type != "seriesMaster" || e.Subject != "Series" {
		t.Fatalf("event: %d %+v", rec.Code, e)
	}
	// Also through the calendar, and not through a calendar it is not in.
	if rec, _ := getEvents(t, f, "/graph/v1.0/me/calendars/"+CalendarID(collID)+"/events/"+id); rec.Code != 200 {
		t.Errorf("through its calendar: %d", rec.Code)
	}
	if rec, _ := getEvents(t, f, "/graph/v1.0/me/calendars/"+CalendarID("other")+"/events/"+id); rec.Code != 404 {
		t.Errorf("through another calendar: %d, want 404", rec.Code)
	}

	// A generated occurrence, an overridden one, an excluded date, and a date
	// that is not in the series.
	for _, tc := range []struct {
		rid     string
		status  int
		subject string
		typ     string
		start   string
	}{
		{"20261105T090000Z", 200, "Series", "occurrence", "2026-11-05T09:00"},
		{"20261126T090000Z", 200, "Series", "occurrence", "2026-11-26T09:00"},
		{"20261119T090000Z", 200, "Moved", "exception", "2026-11-19T13:00"},
		{"20261112T090000Z", 404, "", "", ""},
		{"20261107T090000Z", 404, "", "", ""},
		{"20261203T090000Z", 404, "", "", ""},
	} {
		rec, _ := getEvents(t, f, "/graph/v1.0/me/events/"+id+"."+tc.rid)
		if rec.Code != tc.status {
			t.Errorf("%s: %d, want %d: %s", tc.rid, rec.Code, tc.status, rec.Body.String())
			continue
		}
		if tc.status != 200 {
			continue
		}
		o := decodeBody[eventJSON](t, rec)
		if o.Subject != tc.subject || o.Type != tc.typ || !strings.HasPrefix(o.Start.DateTime, tc.start) || o.SeriesMasterID != id ||
			o.ID != id+"."+tc.rid || o.Recurrence != nil {
			t.Errorf("%s: %+v", tc.rid, o)
		}
	}

	for target, want := range map[string]int{
		"/graph/v1.0/me/events/***":                            400,
		"/graph/v1.0/me/events/" + EventID("x", "y"):           404,
		"/graph/v1.0/me/events/" + EventID(collID, "gone.ics"): 404,
		"/graph/v1.0/me/events/" + id + ".2026-11-05":          400,
		"/graph/v1.0/me/events/delta":                          501,
		"/graph/v1.0/me/events/" + id + "/instances":           501,
	} {
		if rec, _ := getEvents(t, f, target); rec.Code != want {
			t.Errorf("GET %s = %d, want %d", target, rec.Code, want)
		}
	}
}

func TestCalendarViewWindow(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	put := func(name string, props ...string) {
		putEvent(t, f, ref, collID, name+".ics", ics(vevent(name, props...)))
	}
	put("before", "DTSTART:20261104T100000Z", "DTEND:20261104T110000Z", "SUMMARY:ends at the start of the window")
	// Ends exactly where the window starts: out. Starts exactly where it ends: out.
	put("ends-at-start", "DTSTART:20261104T230000Z", "DTEND:20261105T000000Z", "SUMMARY:ends at start")
	put("starts-at-end", "DTSTART:20261106T000000Z", "DTEND:20261106T010000Z", "SUMMARY:starts at end")
	put("inside", "DTSTART:20261105T100000Z", "DTEND:20261105T110000Z", "SUMMARY:inside")
	put("spans", "DTSTART:20261104T200000Z", "DTEND:20261106T040000Z", "SUMMARY:spans")
	put("edge-in", "DTSTART:20261105T235959Z", "DTEND:20261106T003000Z", "SUMMARY:last second")
	put("allday", "DTSTART;VALUE=DATE:20261105", "DTEND;VALUE=DATE:20261106", "SUMMARY:all day")
	put("series", "DTSTART:20261029T090000Z", "DTEND:20261029T100000Z", "RRULE:FREQ=WEEKLY;COUNT=6", "EXDATE:20261112T090000Z", "SUMMARY:series")

	rec, list := getEvents(t, f, "/graph/v1.0/me/calendarView?startDateTime=2026-11-05T00:00:00Z&endDateTime=2026-11-06T00:00:00Z")
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var got []string
	for _, e := range list.Value {
		got = append(got, e.Subject)
	}
	// 2026-11-05 is a Thursday, so the series has an occurrence that day.
	want := []string{"spans", "all day", "series", "inside", "last second"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("window 5th: got %q, want %q", got, want)
	}
	for _, e := range list.Value {
		if e.Subject == "series" && (e.Type != "occurrence" || e.SeriesMasterID != EventID(collID, "series.ics") || !strings.HasSuffix(e.ID, ".20261105T090000Z")) {
			t.Errorf("series occurrence = %+v", e)
		}
		if e.Subject == "inside" && (e.Type != "singleInstance" || e.ID != EventID(collID, "inside.ics")) {
			t.Errorf("single event = %+v", e)
		}
	}

	// The excluded date is gone and the next week is there.
	_, list = getEvents(t, f, "/graph/v1.0/me/calendarView?startDateTime=2026-11-12T00:00:00Z&endDateTime=2026-11-13T00:00:00Z")
	if len(list.Value) != 0 {
		t.Errorf("excluded date shows: %+v", list.Value)
	}
	_, list = getEvents(t, f, "/graph/v1.0/me/calendarView?startDateTime=2026-11-19T00:00:00Z&endDateTime=2026-11-20T00:00:00Z")
	if len(list.Value) != 1 || list.Value[0].Subject != "series" {
		t.Errorf("next week: %+v", list.Value)
	}

	// Times without an offset are UTC, offsets are honoured.
	_, list = getEvents(t, f, "/graph/v1.0/me/calendarView?startDateTime=2026-11-05T11:30:00&endDateTime=2026-11-05T23:00:00")
	if len(list.Value) != 2 || list.Value[0].Subject != "spans" || list.Value[1].Subject != "all day" {
		t.Errorf("later window: %+v", list.Value)
	}

	for _, q := range []string{
		"", "startDateTime=2026-11-05T00:00:00Z", "startDateTime=x&endDateTime=y",
		"startDateTime=2026-11-06T00:00:00Z&endDateTime=2026-11-05T00:00:00Z",
		"startDateTime=2020-01-01T00:00:00Z&endDateTime=2030-01-01T00:00:00Z",
	} {
		if rec, _ := getEvents(t, f, "/graph/v1.0/me/calendarView?"+q); rec.Code != 400 {
			t.Errorf("%q: %d, want 400", q, rec.Code)
		}
	}
}

func TestCalendarViewOverrideAndZone(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	putEvent(t, f, ref, collID, "s.ics", ics(
		vevent("s", "DTSTART;TZID=Europe/Berlin:20261105T090000", "DTEND;TZID=Europe/Berlin:20261105T100000", "RRULE:FREQ=WEEKLY;COUNT=3", "SUMMARY:Series"),
		vevent("s", "RECURRENCE-ID;TZID=Europe/Berlin:20261112T090000", "DTSTART;TZID=Europe/Berlin:20261112T150000", "DTEND;TZID=Europe/Berlin:20261112T160000", "SUMMARY:Moved"),
	))
	rec, list := getEvents(t, f, "/graph/v1.0/me/calendar/calendarView?startDateTime=2026-11-05T00:00:00Z&endDateTime=2026-11-20T00:00:00Z",
		"Prefer", `outlook.timezone="Pacific Standard Time"`)
	if rec.Code != 200 || len(list.Value) != 3 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Preference-Applied"); got != `outlook.timezone="Pacific Standard Time"` {
		t.Errorf("Preference-Applied = %q", got)
	}
	e := list.Value[0]
	if e.Start.TimeZone != "Pacific Standard Time" || e.Start.DateTime != "2026-11-05T00:00:00.0000000" {
		t.Errorf("first start = %+v", e.Start)
	}
	moved := list.Value[1]
	if moved.Subject != "Moved" || moved.Type != "exception" || moved.Start.DateTime != "2026-11-12T06:00:00.0000000" {
		t.Errorf("override = %+v", moved)
	}
	if rec, _ := getEvents(t, f, "/graph/v1.0/me/events", "Prefer", `outlook.timezone="Mars/Base"`); rec.Code != 400 {
		t.Errorf("unknown Prefer zone: %d, want 400", rec.Code)
	}
}

func TestSharedCalendarEventsAreVisible(t *testing.T) {
	f, col := sharedFixture(t, store.AccessRead)
	putEvent(t, f, col.Ref, col.ID, "t.ics", ics(vevent("t", "DTSTART:20261105T100000Z", "DTEND:20261105T110000Z", "SUMMARY:Team meeting")))
	ref, collID := einsteinCalendar(t, f)
	putEvent(t, f, ref, collID, "m.ics", ics(vevent("m", "DTSTART:20261105T120000Z", "DTEND:20261105T130000Z", "SUMMARY:Mine")))

	view := "?startDateTime=2026-11-05T00:00:00Z&endDateTime=2026-11-06T00:00:00Z"
	_, list := getEvents(t, f, "/graph/v1.0/me/calendarView"+view)
	if len(list.Value) != 2 || list.Value[0].Subject != "Team meeting" || list.Value[1].Subject != "Mine" {
		t.Fatalf("/me/calendarView: %+v", list.Value)
	}
	// /me/events means the default calendar only.
	_, list = getEvents(t, f, "/graph/v1.0/me/events")
	if len(list.Value) != 1 || list.Value[0].Subject != "Mine" {
		t.Errorf("/me/events: %+v", list.Value)
	}
	_, list = getEvents(t, f, "/graph/v1.0/me/calendars/"+CalendarID(col.ID)+"/events")
	if len(list.Value) != 1 || list.Value[0].Subject != "Team meeting" {
		t.Errorf("shared events: %+v", list.Value)
	}
	if rec, _ := getEvents(t, f, "/graph/v1.0/me/events/"+EventID(col.ID, "t.ics")); rec.Code != 200 {
		t.Errorf("shared event by id: %d", rec.Code)
	}
}

// TestEventContractFixtures pins the JSON shapes of events. The web app's
// tests load the same files. Run with -update to rewrite them.
func TestEventContractFixtures(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ref := store.CollectionRef{Owner: "sub-einstein", Kind: store.KindCalendar, Name: "personal"}
	src := fixedSource{entries: []CalendarEntry{{OwnerName: "Albert Einstein", Collection: store.Collection{
		ID: "personal-cal", Ref: ref, Revision: 1, Created: created,
		Props: store.Props{DisplayName: "Personal"},
	}}}}
	f := newFixture(t, func(cfg *Config) { cfg.Calendars = src })
	if _, err := f.st.CreateCollection(context.Background(), ref, store.Props{DisplayName: "Personal", Components: []string{"VEVENT"}}); err != nil {
		t.Fatal(err)
	}
	put := func(name string, data []byte) {
		if _, err := f.st.PutObject(context.Background(), ref, name, data, store.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	put("meeting.ics", ics(vevent("meeting-1@example.com", "CREATED:20261101T080000Z", "LAST-MODIFIED:20261102T090000Z",
		"DTSTART;TZID=Europe/Berlin:20261105T100000", "DTEND;TZID=Europe/Berlin:20261105T113000",
		"SUMMARY:Planning", "DESCRIPTION:Agenda", "LOCATION:Room 4", "CATEGORIES:Work",
		"ORGANIZER;CN=Albert Einstein:mailto:einstein@example.com",
		"ATTENDEE;CN=Marie Curie;ROLE=REQ-PARTICIPANT;PARTSTAT=ACCEPTED:mailto:marie@example.com",
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Reminder\r\nTRIGGER:-PT15M\r\nEND:VALARM")))
	put("series.ics", ics(
		vevent("series-1@example.com", "DTSTART:20261106T090000Z", "DTEND:20261106T100000Z", "RRULE:FREQ=WEEKLY;COUNT=3", "SUMMARY:Standup"),
		vevent("series-1@example.com", "RECURRENCE-ID:20261113T090000Z", "DTSTART:20261113T110000Z", "DTEND:20261113T120000Z", "SUMMARY:Standup, later"),
	))
	collID := "personal-cal"
	check := func(name, target string, hdr ...string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer einstein")
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
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
	check("events", "/graph/v1.0/me/events")
	check("event", "/graph/v1.0/me/events/"+EventID(collID, "meeting.ics"))
	check("event-zone", "/graph/v1.0/me/events/"+EventID(collID, "meeting.ics"), "Prefer", `outlook.timezone="Pacific Standard Time"`)
	check("event-series", "/graph/v1.0/me/events/"+EventID(collID, "series.ics"))
	check("event-occurrence", "/graph/v1.0/me/events/"+OccurrenceID(EventID(collID, "series.ics"), time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)))
	check("calendarview", "/graph/v1.0/me/calendarView?startDateTime=2026-11-05T00:00:00Z&endDateTime=2026-11-21T00:00:00Z")
}
