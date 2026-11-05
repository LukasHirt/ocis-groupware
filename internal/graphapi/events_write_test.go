package graphapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LukasHirt/groupkit/ical"
	"github.com/LukasHirt/groupkit/store"
)

const newMeeting = `{
  "subject": "Kick-off",
  "body": {"contentType": "text", "content": "Bring notes"},
  "start": {"dateTime": "2026-11-10T09:00:00", "timeZone": "Europe/Berlin"},
  "end": {"dateTime": "2026-11-10T10:00:00", "timeZone": "Europe/Berlin"},
  "location": {"displayName": "Room 1"},
  "attendees": [{"type": "required", "emailAddress": {"name": "Marie", "address": "marie@example.com"}}],
  "isReminderOn": true,
  "reminderMinutesBeforeStart": 10
}`

func send(f *fixture, method, target, token, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func storedCal(t *testing.T, f *fixture, ref store.CollectionRef, name string) (*ical.Calendar, store.Object) {
	t.Helper()
	obj, err := f.st.GetObject(context.Background(), ref, name)
	if err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	cal, err := ical.Parse(obj.Data)
	if err != nil {
		t.Fatal(err)
	}
	return cal, obj
}

func TestCreateEvent(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	rev := currentRevision(t, f, ref)

	for _, target := range []string{"/graph/v1.0/me/events", "/graph/v1.0/me/calendar/events", "/graph/v1.0/me/calendars/" + CalendarID(collID) + "/events"} {
		rec := send(f, http.MethodPost, target, "einstein", newMeeting)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST %s: %d %s", target, rec.Code, rec.Body.String())
		}
		e := decodeBody[eventJSON](t, rec)
		_, name, err := ParseEventID(e.ID)
		if err != nil || e.Subject != "Kick-off" || e.ETag == "" || e.Type != "singleInstance" || e.Start.DateTime != "2026-11-10T08:00:00.0000000" {
			t.Fatalf("created = %+v (%v)", e, err)
		}
		cal, obj := storedCal(t, f, ref, name)
		if _, err := ical.Validate(obj.Data, []string{"VEVENT"}); err != nil {
			t.Errorf("stored object is not valid: %v", err)
		}
		master, _ := eventComponents(cal)
		if got := master.Prop("DTSTART"); got.Value != "20261110T090000" || got.Params["TZID"][0] != "Europe/Berlin" {
			t.Errorf("DTSTART = %+v", got)
		}
		if rec := f.do(http.MethodGet, "/graph/v1.0/me/events/"+e.ID, "einstein", ""); rec.Code != 200 {
			t.Errorf("GET after create: %d", rec.Code)
		}
	}

	// A DAV client that syncs from the old revision sees the three objects.
	ch, err := f.st.Changes(context.Background(), ref, rev)
	if err != nil || len(ch.Changes) != 3 {
		t.Errorf("changes since %d: %+v, %v", rev, ch, err)
	}
}

func currentRevision(t *testing.T, f *fixture, ref store.CollectionRef) uint64 {
	t.Helper()
	c, err := f.st.GetCollection(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return c.Revision
}

func TestCreateEventRejectsBadBodies(t *testing.T) {
	f := newFixture(t)
	for name, body := range map[string]string{
		"no times":   `{"subject":"x"}`,
		"html":       `{"body":{"contentType":"html","content":"<p>"},"start":{"dateTime":"2026-11-10T09:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-10T10:00:00","timeZone":"UTC"}}`,
		"bad zone":   `{"start":{"dateTime":"2026-11-10T09:00:00","timeZone":"Nowhere"},"end":{"dateTime":"2026-11-10T10:00:00","timeZone":"UTC"}}`,
		"not json":   `[1]`,
		"bad attend": `{"attendees":[{"emailAddress":{"address":"nope"}}],"start":{"dateTime":"2026-11-10T09:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-10T10:00:00","timeZone":"UTC"}}`,
	} {
		if rec := send(f, http.MethodPost, "/graph/v1.0/me/events", "einstein", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/graph/v1.0/me/events", strings.NewReader(newMeeting))
	req.Header.Set("Authorization", "Bearer einstein")
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("content type: %d", rec.Code)
	}
}

// original has properties the mapping does not know, a series, an
// alarm and an attendee with extra parameters.
func originalSeries() []byte {
	return ics(vevent("orig@example.com",
		"DTSTART:20261105T090000Z", "DTEND:20261105T100000Z", "RRULE:FREQ=WEEKLY;COUNT=4", "EXDATE:20261112T090000Z",
		"SUMMARY:Old", "SEQUENCE:3", "X-CUSTOM;X-P=1:keep, me", "URL:https://example.com/m",
		"ATTENDEE;RSVP=TRUE;X-NUM-GUESTS=2;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:marie@example.com",
		"BEGIN:VALARM\r\nACTION:AUDIO\r\nTRIGGER:-PT5M\r\nEND:VALARM"))
}

// sameProps reports the properties of a that b does not have, ignoring the
// named ones.
func missingProps(a, b *ical.Component, ignore ...string) []string {
	var out []string
	for _, p := range a.Props {
		skip := false
		for _, n := range ignore {
			skip = skip || p.Name == n
		}
		if skip {
			continue
		}
		found := false
		for _, q := range b.Props {
			if q.Name == p.Name && q.Value == p.Value && len(q.Params) == len(p.Params) {
				found = true
				for k, v := range p.Params {
					found = found && strings.Join(q.Params[k], ",") == strings.Join(v, ",")
				}
			}
		}
		if !found {
			out = append(out, p.Name+":"+p.Value)
		}
	}
	return out
}

func TestPatchEventKeepsWhatItDoesNotMap(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := putEvent(t, f, ref, collID, "orig.ics", originalSeries())
	before, _ := storedCal(t, f, ref, "orig.ics")
	rev := currentRevision(t, f, ref)

	rec := send(f, http.MethodPatch, "/graph/v1.0/me/events/"+id, "einstein", `{"subject":"New","location":{"displayName":"Hall"},"categories":["a","b"]}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if e := decodeBody[eventJSON](t, rec); e.Subject != "New" || e.Location.DisplayName != "Hall" || e.Recurrence == nil {
		t.Errorf("answer = %+v", e)
	}
	after, _ := storedCal(t, f, ref, "orig.ics")
	bm, _ := eventComponents(before)
	am, _ := eventComponents(after)
	if miss := missingProps(bm, am, "SUMMARY", "DTSTAMP", "LAST-MODIFIED"); len(miss) > 0 {
		t.Errorf("properties lost or changed: %v", miss)
	}
	if len(am.Comps("VALARM")) != 1 || am.Comps("VALARM")[0].Prop("ACTION").Value != "AUDIO" {
		t.Errorf("alarm changed: %+v", am.Children)
	}
	if got := am.Prop("SUMMARY").Value; got != "New" {
		t.Errorf("SUMMARY = %q", got)
	}
	ch, err := f.st.Changes(context.Background(), ref, rev)
	if err != nil || len(ch.Changes) != 1 || ch.Changes[0].Name != "orig.ics" || ch.Changes[0].Deleted {
		t.Errorf("sync sees %+v, %v", ch, err)
	}
}

func TestEchoingAnEventBackChangesNothing(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := putEvent(t, f, ref, collID, "e.ics", ics(vevent("e", "DTSTART:20261105T090000Z", "DTEND:20261105T100000Z", "RRULE:FREQ=WEEKLY;BYDAY=MO;BYHOUR=9,15", "SUMMARY:Odd rule", "CATEGORIES:x")))
	before, _ := storedCal(t, f, ref, "e.ics")
	rec := f.do(http.MethodGet, "/graph/v1.0/me/events/"+id, "einstein", "")
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if rec := send(f, http.MethodPatch, "/graph/v1.0/me/events/"+id, "einstein", rec.Body.String()); rec.Code != 200 {
		t.Fatalf("echo: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := storedCal(t, f, ref, "e.ics")
	bm, _ := eventComponents(before)
	am, _ := eventComponents(after)
	if miss := missingProps(bm, am, "DTSTAMP", "LAST-MODIFIED"); len(miss) > 0 {
		t.Errorf("an echo changed %v", miss)
	}
}

func TestPatchEventIfMatch(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := putEvent(t, f, ref, collID, "m.ics", ics(vevent("m", "DTSTART:20261105T090000Z", "DTEND:20261105T100000Z", "SUMMARY:Old")))
	rec := f.do(http.MethodGet, "/graph/v1.0/me/events/"+id, "einstein", "")
	etag := decodeBody[eventJSON](t, rec).ETag
	target := "/graph/v1.0/me/events/" + id

	rec = send(f, http.MethodPatch, target, "einstein", `{"subject":"A"}`, "If-Match", `W/"stale"`)
	if rec.Code != http.StatusPreconditionFailed || decodeError(t, rec).Error.Code != "preconditionFailed" {
		t.Errorf("stale: %d %s", rec.Code, rec.Body.String())
	}
	if cal, _ := storedCal(t, f, ref, "m.ics"); textOf(firstEvent(cal), "SUMMARY") != "Old" {
		t.Error("a refused patch changed the event")
	}
	rec = send(f, http.MethodPatch, target, "einstein", `{"subject":"B"}`, "If-Match", etag)
	if rec.Code != 200 {
		t.Fatalf("matching: %d %s", rec.Code, rec.Body.String())
	}
	if now := decodeBody[eventJSON](t, rec).ETag; now == etag {
		t.Error("the ETag did not change")
	}
	// The old tag is now stale; a bare tag and * work.
	if rec := send(f, http.MethodPatch, target, "einstein", `{"subject":"C"}`, "If-Match", etag); rec.Code != 412 {
		t.Errorf("old tag: %d", rec.Code)
	}
	if rec := send(f, http.MethodPatch, target, "einstein", `{"subject":"D"}`, "If-Match", "*"); rec.Code != 200 {
		t.Errorf("*: %d", rec.Code)
	}
	if rec := send(f, http.MethodDelete, target, "einstein", "", "If-Match", etag); rec.Code != 412 {
		t.Errorf("delete with a stale tag: %d", rec.Code)
	}
	if _, err := f.st.GetObject(context.Background(), ref, "m.ics"); err != nil {
		t.Errorf("a refused delete removed the event: %v", err)
	}
}

func firstEvent(cal *ical.Calendar) *ical.Component {
	m, _ := eventComponents(cal)
	return m
}

func TestDeleteEvent(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := putEvent(t, f, ref, collID, "d.ics", ics(vevent("d", "DTSTART:20261105T090000Z", "DTEND:20261105T100000Z")))
	rev := currentRevision(t, f, ref)
	if rec := send(f, http.MethodDelete, "/graph/v1.0/me/events/"+id, "einstein", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	ch, err := f.st.Changes(context.Background(), ref, rev)
	if err != nil || len(ch.Changes) != 1 || !ch.Changes[0].Deleted {
		t.Errorf("sync sees %+v, %v", ch, err)
	}
	if rec := send(f, http.MethodDelete, "/graph/v1.0/me/events/"+id, "einstein", ""); rec.Code != 404 {
		t.Errorf("second delete: %d, want 404", rec.Code)
	}
}

func TestPatchAndDeleteOccurrences(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := putEvent(t, f, ref, collID, "orig.ics", originalSeries())
	occ := func(rid string) string { return "/graph/v1.0/me/events/" + id + "." + rid }

	// "This event" of a series: an override is written, the master stays.
	rec := send(f, http.MethodPatch, occ("20261119T090000Z"), "einstein",
		`{"subject":"Moved","start":{"dateTime":"2026-11-19T13:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-19T14:00:00","timeZone":"UTC"}}`)
	if rec.Code != 200 {
		t.Fatalf("patch occurrence: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[eventJSON](t, rec)
	if got.Type != "exception" || got.Subject != "Moved" || got.Start.DateTime != "2026-11-19T13:00:00.0000000" || got.ID != id+".20261119T090000Z" {
		t.Errorf("answer = %+v", got)
	}
	cal, _ := storedCal(t, f, ref, "orig.ics")
	master, overrides := eventComponents(cal)
	if len(overrides) != 1 || master.Prop("RRULE").Value != "FREQ=WEEKLY;COUNT=4" || textOf(master, "SUMMARY") != "Old" {
		t.Fatalf("master changed or override missing: %s", cal.Encode())
	}
	ov := overrides[0]
	if ov.Prop("RECURRENCE-ID").Value != "20261119T090000Z" || ov.Prop("RRULE") != nil || ov.Prop("EXDATE") != nil || ov.Prop("UID").Value != "orig@example.com" {
		t.Errorf("override = %+v", ov.Props)
	}
	if ov.Prop("X-CUSTOM") == nil || len(ov.Comps("VALARM")) != 1 {
		t.Errorf("override lost what the series had: %+v", ov)
	}

	// A second patch changes the same override; a different one by name only.
	if rec := send(f, http.MethodPatch, occ("20261119T090000Z"), "einstein", `{"location":{"displayName":"Hall"}}`); rec.Code != 200 {
		t.Fatalf("second patch: %d", rec.Code)
	}
	cal, _ = storedCal(t, f, ref, "orig.ics")
	if _, overrides = eventComponents(cal); len(overrides) != 1 || textOf(overrides[0], "LOCATION") != "Hall" || textOf(overrides[0], "SUMMARY") != "Moved" {
		t.Errorf("overrides = %d: %s", len(overrides), cal.Encode())
	}
	view := "/graph/v1.0/me/calendarView?startDateTime=2026-11-01T00:00:00Z&endDateTime=2026-12-31T00:00:00Z"
	_, list := getEvents(t, f, view)
	var starts []string
	for _, e := range list.Value {
		starts = append(starts, e.Start.DateTime[:13])
	}
	if strings.Join(starts, " ") != "2026-11-05T09 2026-11-19T13 2026-11-26T09" {
		t.Errorf("view after patch: %v", starts)
	}

	// Occurrence rules: a rule on an occurrence is refused, and so are dates
	// outside the series and the excluded one.
	if rec := send(f, http.MethodPatch, occ("20261126T090000Z"), "einstein", `{"recurrence":{"pattern":{"type":"daily","interval":1},"range":{"type":"noEnd"}}}`); rec.Code != 400 {
		t.Errorf("recurrence on an occurrence: %d", rec.Code)
	}
	for _, rid := range []string{"20261112T090000Z", "20261107T090000Z"} {
		if rec := send(f, http.MethodPatch, occ(rid), "einstein", `{"subject":"x"}`); rec.Code != 404 {
			t.Errorf("patch %s: %d, want 404", rid, rec.Code)
		}
		if rec := send(f, http.MethodDelete, occ(rid), "einstein", ""); rec.Code != 404 {
			t.Errorf("delete %s: %d, want 404", rid, rec.Code)
		}
	}

	// Deleting an occurrence adds an EXDATE; deleting the overridden one also
	// drops the override.
	if rec := send(f, http.MethodDelete, occ("20261126T090000Z"), "einstein", ""); rec.Code != 204 {
		t.Fatalf("delete occurrence: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(f, http.MethodDelete, occ("20261119T090000Z"), "einstein", ""); rec.Code != 204 {
		t.Fatalf("delete override: %d %s", rec.Code, rec.Body.String())
	}
	cal, _ = storedCal(t, f, ref, "orig.ics")
	master, overrides = eventComponents(cal)
	var ex []string
	for _, p := range master.PropAll("EXDATE") {
		ex = append(ex, p.Value)
	}
	if len(overrides) != 0 || strings.Join(ex, ",") != "20261112T090000Z,20261126T090000Z,20261119T090000Z" {
		t.Errorf("EXDATEs %v, %d overrides\n%s", ex, len(overrides), cal.Encode())
	}
	_, list = getEvents(t, f, view)
	if len(list.Value) != 1 || list.Value[0].Start.DateTime[:10] != "2026-11-05" {
		t.Errorf("view after deletes: %+v", list.Value)
	}
	if rec := f.do(http.MethodGet, occ("20261126T090000Z"), "einstein", ""); rec.Code != 404 {
		t.Errorf("deleted occurrence: %d", rec.Code)
	}
}

func TestAllDayOccurrenceDeleteUsesDates(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := putEvent(t, f, ref, collID, "a.ics", ics(vevent("a", "DTSTART;VALUE=DATE:20261105", "DTEND;VALUE=DATE:20261106", "RRULE:FREQ=DAILY;COUNT=3")))
	if rec := send(f, http.MethodDelete, "/graph/v1.0/me/events/"+id+".20261106T000000Z", "einstein", ""); rec.Code != 204 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	cal, _ := storedCal(t, f, ref, "a.ics")
	m, _ := eventComponents(cal)
	if p := m.Prop("EXDATE"); p == nil || p.Value != "20261106" || p.Params["VALUE"][0] != "DATE" {
		t.Errorf("EXDATE = %+v", p)
	}
}

func TestReadOnlyCalendarRefusesWrites(t *testing.T) {
	for _, access := range []store.Access{store.AccessRead, store.AccessObjects} {
		f, col := sharedFixture(t, access)
		id := putEvent(t, f, col.Ref, col.ID, "t.ics", ics(vevent("t", "DTSTART:20261105T100000Z", "DTEND:20261105T110000Z", "RRULE:FREQ=DAILY;COUNT=3", "SUMMARY:Team")))
		calURL := "/graph/v1.0/me/calendars/" + CalendarID(col.ID)
		type req struct{ method, target, body string }
		writes := []req{
			{http.MethodPost, calURL + "/events", newMeeting},
			{http.MethodPatch, "/graph/v1.0/me/events/" + id, `{"subject":"x"}`},
			{http.MethodPatch, "/graph/v1.0/me/events/" + id + ".20261106T100000Z", `{"subject":"x"}`},
			{http.MethodDelete, "/graph/v1.0/me/events/" + id + ".20261106T100000Z", ""},
			{http.MethodDelete, calURL + "/events/" + id, ""},
		}
		for _, w := range writes {
			rec := send(f, w.method, w.target, "einstein", w.body)
			if access == store.AccessRead {
				if rec.Code != 403 || decodeError(t, rec).Error.Code != "accessDenied" {
					t.Errorf("%v %s %s: %d %s, want 403", access, w.method, w.target, rec.Code, rec.Body.String())
				}
				continue
			}
			if rec.Code >= 400 {
				t.Errorf("%v %s %s: %d %s, want success", access, w.method, w.target, rec.Code, rec.Body.String())
			}
		}
		if access == store.AccessRead {
			cal, _ := storedCal(t, f, col.Ref, "t.ics")
			if textOf(firstEvent(cal), "SUMMARY") != "Team" {
				t.Error("a refused write changed the event")
			}
			objs, _ := f.st.ListObjects(context.Background(), col.Ref, store.ListOptions{})
			if len(objs) != 1 {
				t.Errorf("a refused create left %d objects", len(objs))
			}
		}
	}
}

func TestWriteEndpointsDoNotLeakOtherUsersEvents(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := putEvent(t, f, ref, collID, "p.ics", ics(vevent("p", "DTSTART:20261105T100000Z", "DTEND:20261105T110000Z", "SUMMARY:Private")))
	listCalendarsOf(t, f, "marie")
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		body := ""
		if method == http.MethodPatch {
			body = `{"subject":"x"}`
		}
		if rec := send(f, method, "/graph/v1.0/me/events/"+id, "marie", body); rec.Code != 404 {
			t.Errorf("%s by another user: %d, want 404", method, rec.Code)
		}
	}
	if _, err := f.st.GetObject(context.Background(), ref, "p.ics"); err != nil {
		t.Error(err)
	}
}

func TestPatchEventBody(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := putEvent(t, f, ref, collID, "b.ics", ics(vevent("b", "DTSTART:20261105T100000Z", "DTEND:20261105T110000Z", "SUMMARY:x")))
	for body, want := range map[string]int{
		`{"showAs":"sleepy"}`: 400,
		`{"start":null}`:      400,
		`{"start":{"dateTime":"nope","timeZone":"UTC"}}`: 400,
		`{"isAllDay":true}`: 400,
		`{"end":{"dateTime":"2026-11-05T09:00:00","timeZone":"UTC"}}`:   400,
		`{"start":{"dateTime":"2026-11-05T12:00:00","timeZone":"UTC"}}`: 200,
		`[]`: 400,
	} {
		if rec := send(f, http.MethodPatch, "/graph/v1.0/me/events/"+id, "einstein", body); rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", body, rec.Code, want, rec.Body.String())
		}
	}
	// Moving the start keeps the length.
	cal, _ := storedCal(t, f, ref, "b.ics")
	m, _ := eventComponents(cal)
	if m.Prop("DTSTART").Value != "20261105T120000Z" || m.Prop("DTEND").Value != "20261105T130000Z" {
		t.Errorf("times = %s %s", m.Prop("DTSTART").Value, m.Prop("DTEND").Value)
	}
}
