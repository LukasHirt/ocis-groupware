package graphapi

import (
	"net/http"
	"slices"
	"testing"
)

func TestEventFilterGrammar(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	putEvent(t, f, ref, collID, "a.ics", ics(vevent("a", "DTSTART:20261105T090000Z", "DTEND:20261105T100000Z", "SUMMARY:Team standup")))
	putEvent(t, f, ref, collID, "b.ics", ics(vevent("b", "DTSTART:20261110T090000Z", "DTEND:20261110T100000Z", "SUMMARY:Dentist")))
	putEvent(t, f, ref, collID, "c.ics", ics(vevent("c", "DTSTART:20261120T090000Z", "DTEND:20261120T100000Z", "SUMMARY:It's a Standup")))
	putEvent(t, f, ref, collID, "d.ics", ics(vevent("d", "DTSTART;TZID=Europe/Prague:20261110T100000", "DTEND;TZID=Europe/Prague:20261110T110000", "SUMMARY:Prague call")))

	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"start/dateTime ge '2026-11-10T00:00:00Z'", []string{"Dentist", "It's a Standup", "Prague call"}},
		{"start/dateTime gt '2026-11-10T09:00:00Z'", []string{"It's a Standup"}},
		{"start/dateTime ge 2026-11-10T00:00:00Z and start/dateTime lt 2026-11-11T00:00:00Z", []string{"Dentist", "Prague call"}},
		{"start/dateTime le '2026-11-05T09:00:00Z'", []string{"Team standup"}},
		{"start/dateTime lt '2026-11-05T09:00:00Z'", nil},
		{"end/dateTime gt '2026-11-10T10:00:00Z'", []string{"It's a Standup"}},
		{"end/dateTime le '2026-11-10T10:00:00Z'", []string{"Team standup", "Dentist", "Prague call"}},
		{"end/dateTime ge '2026-11-10T00:00:00+01:00'", []string{"Dentist", "It's a Standup", "Prague call"}},
		{"start/dateTime ge '2026-11-10'", []string{"Dentist", "It's a Standup", "Prague call"}},
		{"subject eq 'Dentist'", []string{"Dentist"}},
		{"subject eq 'dentist'", []string{"Dentist"}},
		{"subject eq 'It''s a Standup'", []string{"It's a Standup"}},
		{"startswith(subject,'team')", []string{"Team standup"}},
		{"startswith(subject, 'Team')", []string{"Team standup"}},
		{"contains(subject,'standup')", []string{"Team standup", "It's a Standup"}},
		{"contains(subject,'standup') and start/dateTime ge '2026-11-06T00:00:00Z'", []string{"It's a Standup"}},
		{"CONTAINS(subject,'call') AND subject eq 'Prague call'", []string{"Prague call"}},
		{"contains(subject,'')", []string{"Team standup", "Dentist", "It's a Standup", "Prague call"}},
	} {
		rec := f.do(http.MethodGet, q("/graph/v1.0/me/events", "$filter", tc.filter, "$orderby", "start/dateTime"), "einstein", "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", tc.filter, rec.Code, rec.Body.String())
			continue
		}
		got := subjects(decodeBody[rawList](t, rec))
		slices.Sort(got)
		want := slices.Clone(tc.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", tc.filter, got, want)
		}
	}
}

func TestEventFilterRejects(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		filter string
		want   int
	}{
		// Valid OData that the facade does not serve.
		{"subject eq 'a' or subject eq 'b'", 501},
		{"not contains(subject,'a')", 501},
		{"(subject eq 'a')", 501},
		{"subject ne 'a'", 501},
		{"subject gt 'a'", 501},
		{"start/dateTime eq '2026-11-10T00:00:00Z'", 501},
		{"start/dateTime ne '2026-11-10T00:00:00Z'", 501},
		{"isAllDay eq true", 501},
		{"showAs eq 'free'", 501},
		{"categories/any(c:c eq 'x')", 501},
		{"start/timeZone eq 'UTC'", 501},
		{"endswith(subject,'x')", 501},
		{"tolower(subject) eq 'x'", 501},
		{"contains(location/displayName,'x')", 501},
		// Not valid.
		{"", 400},
		{"subject", 400},
		{"subject eq", 400},
		{"subject eq Dentist", 400},
		{"subject eq 'unterminated", 400},
		{"start/dateTime ge 'yesterday'", 400},
		{"start/dateTime ge 5", 400},
		{"subject eq 'a' and", 400},
		{"and subject eq 'a'", 400},
		{"subject eq 'a' subject eq 'b'", 400},
		{"contains(subject)", 400},
		{"contains(subject,'a'", 400},
		{"contains('a',subject)", 400},
		{"contains(subject,x)", 400},
	} {
		rec := f.do(http.MethodGet, q("/graph/v1.0/me/events", "$filter", tc.filter), "einstein", "")
		if rec.Code != tc.want {
			t.Errorf("%q = %d, want %d: %s", tc.filter, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestFilterOnCalendarViewAndPaging(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	putEvent(t, f, ref, collID, "s.ics", ics(vevent("s", "DTSTART:20261105T090000Z", "DTEND:20261105T100000Z", "RRULE:FREQ=DAILY;COUNT=5", "SUMMARY:Daily")))
	putEvent(t, f, ref, collID, "o.ics", ics(vevent("o", "DTSTART:20261106T090000Z", "DTEND:20261106T100000Z", "SUMMARY:Other")))
	rec := f.do(http.MethodGet, q("/graph/v1.0/me/calendarView",
		"startDateTime", "2026-11-01T00:00:00Z", "endDateTime", "2026-12-01T00:00:00Z",
		"$filter", "subject eq 'daily' and start/dateTime ge '2026-11-07T00:00:00Z'", "$top", "2"), "einstein", "")
	l := decodeBody[rawList](t, rec)
	if len(l.Value) != 2 || l.NextLink == "" {
		t.Fatalf("page = %d items, next %q: %s", len(l.Value), l.NextLink, rec.Body.String())
	}
	rec = f.do(http.MethodGet, q("/graph/v1.0/me/calendarView",
		"startDateTime", "2026-11-01T00:00:00Z", "endDateTime", "2026-12-01T00:00:00Z",
		"$filter", "subject eq 'daily' and start/dateTime ge '2026-11-07T00:00:00Z'", "$top", "2", "$skip", "2"), "einstein", "")
	if l := decodeBody[rawList](t, rec); len(l.Value) != 1 || l.NextLink != "" {
		t.Errorf("last page = %d items", len(l.Value))
	}
}

func TestFilterTimesFollowThePreferredZone(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	putEvent(t, f, ref, collID, "a.ics", ics(vevent("a", "DTSTART:20261110T080000Z", "DTEND:20261110T090000Z", "SUMMARY:Morning")))
	// 08:00 UTC is 09:00 in Prague. The literal is an instant, so the zone
	// the events are shown in does not change the answer.
	count := func(filter, zone string) int {
		rec, l := getEvents(t, f, q("/graph/v1.0/me/events", "$filter", filter), "Prefer", `outlook.timezone="`+zone+`"`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", filter, rec.Code, rec.Body.String())
		}
		return len(l.Value)
	}
	for _, zone := range []string{"UTC", "Europe/Prague"} {
		if n := count("start/dateTime ge '2026-11-10T08:00:00Z'", zone); n != 1 {
			t.Errorf("%s ge: %d events", zone, n)
		}
		if n := count("start/dateTime gt '2026-11-10T08:00:00Z'", zone); n != 0 {
			t.Errorf("%s gt: %d events", zone, n)
		}
	}
}

func TestFilterStillRefusedOnContactsAndOtherLists(t *testing.T) {
	f := newFixture(t)
	for _, target := range []string{
		q("/graph/v1.0/me/contacts", "$filter", "displayName eq 'x'"),
		q("/graph/v1.0/me/calendars", "$filter", "name eq 'x'"),
		q("/graph/v1.0/me/contactFolders", "$filter", "displayName eq 'x'"),
	} {
		if rec := f.do(http.MethodGet, target, "einstein", ""); rec.Code != http.StatusNotImplemented {
			t.Errorf("%s = %d, want 501", target, rec.Code)
		}
	}
}
