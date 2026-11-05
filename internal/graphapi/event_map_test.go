package graphapi

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/ical"
)

var mapNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func loadICS(t *testing.T, name string) *ical.Calendar {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "events", name+".ics"))
	if err != nil {
		t.Fatal(err)
	}
	cal, err := ical.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return cal
}

func readEvent(t *testing.T, cal *ical.Calendar, zone string) eventJSON {
	t.Helper()
	v, aerr := newEventView(cal, zone)
	if aerr != nil {
		t.Fatalf("view: %s", aerr.message)
	}
	master, _ := eventComponents(cal)
	return v.event(master)
}

func marshalIndent(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var goldenCases = []struct{ name, zone, golden string }{
	{"full", "UTC", "full"},
	{"allday", "UTC", "allday"},
	{"zone-iana", "UTC", "zone-iana"},
	{"zone-iana", "Pacific Standard Time", "zone-iana-pst"},
	{"zone-windows", "Europe/Berlin", "zone-windows-berlin"},
	{"daily", "UTC", "daily"},
	{"weekly", "UTC", "weekly"},
	{"monthly-absolute", "UTC", "monthly-absolute"},
	{"monthly-relative", "UTC", "monthly-relative"},
	{"monthly-weekday", "UTC", "monthly-weekday"},
	{"yearly-absolute", "UTC", "yearly-absolute"},
	{"yearly-relative", "UTC", "yearly-relative"},
	{"rrule-unsupported", "UTC", "rrule-unsupported"},
	{"overrides", "UTC", "overrides"},
}

func TestEventMappingGolden(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.golden, func(t *testing.T) {
			got := marshalIndent(t, readEvent(t, loadICS(t, tc.name), tc.zone))
			path := filepath.Join("testdata", "events", tc.golden+".json")
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("mapped event differs from %s:\n%s", path, got)
			}
		})
	}
}

// normalize clears what a round trip is not meant to keep.
func normalize(e eventJSON) eventJSON {
	e.ICalUID, e.CreatedDateTime, e.LastModifiedDateTime = "", "", ""
	e.Organizer, e.RecurrenceEditable = nil, nil
	for i := range e.Attendees {
		e.Attendees[i].Status.Time = ""
	}
	return e
}

func TestEventMappingRoundTrip(t *testing.T) {
	for _, tc := range goldenCases {
		if tc.name == "rrule-unsupported" {
			continue
		}
		t.Run(tc.golden, func(t *testing.T) {
			want := readEvent(t, loadICS(t, tc.name), tc.zone)
			body, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			var p eventPatch
			if err := json.Unmarshal(body, &p); err != nil {
				t.Fatal(err)
			}
			cal, aerr := newEventCalendar(p, "round-trip@example.com", mapNow)
			if aerr != nil {
				t.Fatalf("create: %s", aerr.message)
			}
			// The stored form must be valid and survive a parse.
			data := cal.Encode()
			if _, err := ical.Validate(data, []string{"VEVENT"}); err != nil {
				t.Fatalf("stored form: %v\n%s", err, data)
			}
			again, err := ical.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			got := readEvent(t, again, tc.zone)
			if g, w := string(marshalIndent(t, normalize(got))), string(marshalIndent(t, normalize(want))); g != w {
				t.Errorf("round trip changed the event\n got %s\nwant %s\nical:\n%s", g, w, data)
			}
		})
	}
}

func TestUnsupportedRuleIsReadOnly(t *testing.T) {
	cal := loadICS(t, "rrule-unsupported")
	e := readEvent(t, cal, "UTC")
	if e.Recurrence != nil || e.RecurrenceEditable == nil || *e.RecurrenceEditable {
		t.Fatalf("recurrence = %+v, editable = %v", e.Recurrence, e.RecurrenceEditable)
	}
	if e.Type != "seriesMaster" {
		t.Errorf("type = %q", e.Type)
	}
	master, _ := eventComponents(cal)
	// Echoing the event back with recurrence null must not drop the rule.
	var p eventPatch
	if err := json.Unmarshal([]byte(`{"subject":"Renamed","recurrence":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if aerr := applyEventPatch(cal, master, p, mapNow); aerr != nil {
		t.Fatal(aerr.message)
	}
	if got := master.Prop("RRULE"); got == nil || got.Value != "FREQ=WEEKLY;BYDAY=MO;BYHOUR=9,15" {
		t.Errorf("RRULE = %+v", got)
	}
	if got := master.Prop("SUMMARY").Text(); got != "Renamed" {
		t.Errorf("SUMMARY = %q", got)
	}
}

func TestWriteRecurrence(t *testing.T) {
	cases := []struct{ name, recurrence, start, tz, rule string }{
		{"daily", `{"pattern":{"type":"daily","interval":3},"range":{"type":"noEnd"}}`, "2026-11-05T09:00:00", "UTC", "FREQ=DAILY;INTERVAL=3"},
		{"weekly", `{"pattern":{"type":"weekly","interval":2,"daysOfWeek":["monday","friday"],"firstDayOfWeek":"sunday"},"range":{"type":"numbered","numberOfOccurrences":6}}`, "2026-11-05T09:00:00", "UTC", "FREQ=WEEKLY;INTERVAL=2;COUNT=6;BYDAY=MO,FR;WKST=SU"},
		{"weekly default day", `{"pattern":{"type":"weekly","interval":1},"range":{"type":"noEnd"}}`, "2026-11-05T09:00:00", "UTC", "FREQ=WEEKLY;BYDAY=TH"},
		{"absolute monthly", `{"pattern":{"type":"absoluteMonthly","interval":1,"dayOfMonth":31},"range":{"type":"noEnd"}}`, "2026-11-05T09:00:00", "UTC", "FREQ=MONTHLY;BYMONTHDAY=31"},
		{"relative monthly", `{"pattern":{"type":"relativeMonthly","interval":1,"daysOfWeek":["tuesday"],"index":"last"},"range":{"type":"noEnd"}}`, "2026-11-05T09:00:00", "UTC", "FREQ=MONTHLY;BYDAY=-1TU"},
		{"relative monthly weekdays", `{"pattern":{"type":"relativeMonthly","interval":1,"daysOfWeek":["monday","tuesday","wednesday","thursday","friday"],"index":"first"},"range":{"type":"noEnd"}}`, "2026-11-05T09:00:00", "UTC", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=1"},
		{"absolute yearly", `{"pattern":{"type":"absoluteYearly","interval":1,"month":12,"dayOfMonth":24},"range":{"type":"endDate","endDate":"2030-12-24"}}`, "2026-12-24T00:00:00", "UTC", "FREQ=YEARLY;UNTIL=20301224T235959Z;BYMONTH=12;BYMONTHDAY=24"},
		{"relative yearly", `{"pattern":{"type":"relativeYearly","interval":2,"month":11,"daysOfWeek":["thursday"],"index":"fourth"},"range":{"type":"noEnd"}}`, "2026-11-26T09:00:00", "UTC", "FREQ=YEARLY;INTERVAL=2;BYMONTH=11;BYDAY=4TH"},
		{"until in the zone", `{"pattern":{"type":"daily","interval":1},"range":{"type":"endDate","endDate":"2026-12-31"}}`, "2026-11-05T09:00:00", "Europe/Berlin", "FREQ=DAILY;UNTIL=20261231T225959Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"subject":"x","start":{"dateTime":"` + tc.start + `","timeZone":"` + tc.tz + `"},"end":{"dateTime":"` + strings.Replace(tc.start, "T09", "T10", 1) + `","timeZone":"` + tc.tz + `"},"recurrence":` + tc.recurrence + `}`
			var p eventPatch
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatal(err)
			}
			cal, aerr := newEventCalendar(p, "r@example.com", mapNow)
			if aerr != nil {
				t.Fatal(aerr.message)
			}
			master, _ := eventComponents(cal)
			if got := master.Prop("RRULE"); got == nil || got.Value != tc.rule {
				t.Errorf("RRULE = %+v, want %s", got, tc.rule)
			}
			if e := readEvent(t, cal, tc.tz); e.Recurrence == nil {
				t.Errorf("rule %q does not read back", tc.rule)
			}
		})
	}
}

func TestWriteRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"html body":           `{"body":{"contentType":"html","content":"<b>x</b>"},"start":{"dateTime":"2026-11-05T09:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-05T10:00:00","timeZone":"UTC"}}`,
		"unknown zone":        `{"start":{"dateTime":"2026-11-05T09:00:00","timeZone":"Mars/Base"},"end":{"dateTime":"2026-11-05T10:00:00","timeZone":"UTC"}}`,
		"no start":            `{"subject":"x"}`,
		"bad date":            `{"start":{"dateTime":"tomorrow","timeZone":"UTC"},"end":{"dateTime":"2026-11-05T10:00:00","timeZone":"UTC"}}`,
		"end before start":    `{"start":{"dateTime":"2026-11-05T10:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-05T09:00:00","timeZone":"UTC"}}`,
		"bad showAs":          `{"showAs":"sleepy","start":{"dateTime":"2026-11-05T09:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-05T10:00:00","timeZone":"UTC"}}`,
		"all-day with time":   `{"isAllDay":true,"start":{"dateTime":"2026-11-05T09:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-06T00:00:00","timeZone":"UTC"}}`,
		"attendee no address": `{"attendees":[{"type":"required","emailAddress":{"name":"x"}}],"start":{"dateTime":"2026-11-05T09:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-05T10:00:00","timeZone":"UTC"}}`,
		"weekly interval":     `{"recurrence":{"pattern":{"type":"daily","interval":0},"range":{"type":"noEnd"}},"start":{"dateTime":"2026-11-05T09:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-11-05T10:00:00","timeZone":"UTC"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var p eventPatch
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatal(err)
			}
			if _, aerr := newEventCalendar(p, "bad@example.com", mapNow); aerr == nil || aerr.status != 400 {
				t.Errorf("got %+v, want a 400", aerr)
			}
		})
	}
}

func TestWindowsZoneIsStoredAsUTCForSingleEvents(t *testing.T) {
	var p eventPatch
	body := `{"start":{"dateTime":"2026-11-05T10:00:00","timeZone":"W. Europe Standard Time"},"end":{"dateTime":"2026-11-05T11:00:00","timeZone":"W. Europe Standard Time"}}`
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	cal, aerr := newEventCalendar(p, "w@example.com", mapNow)
	if aerr != nil {
		t.Fatal(aerr.message)
	}
	master, _ := eventComponents(cal)
	if got := master.Prop("DTSTART").Value; got != "20261105T090000Z" || master.Prop("DTSTART").Params["TZID"] != nil {
		t.Errorf("DTSTART = %+v", master.Prop("DTSTART"))
	}
}

func TestPatchKeepsWhatItDoesNotMap(t *testing.T) {
	src := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:keep-1\r\nDTSTAMP:20261101T080000Z\r\nDTSTART:20261105T100000Z\r\nDTEND:20261105T110000Z\r\nSUMMARY:Old\r\n" +
		"X-APPLE-STRUCTURED-LOCATION;VALUE=URI;X-ADDRESS=\"Main St 1, Springfield\";X-APPLE-RADIUS=49.9:geo:1.5,2.5\r\n" +
		"X-LONG:" + strings.Repeat("abcdefghij", 20) + "\r\n" +
		"ATTENDEE;RSVP=TRUE;X-NUM-GUESTS=2;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:marie@example.com\r\n" +
		"BEGIN:VALARM\r\nACTION:AUDIO\r\nTRIGGER:-PT5M\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := ical.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	master, _ := eventComponents(cal)
	var p eventPatch
	if err := json.Unmarshal([]byte(`{"subject":"New","attendees":[{"type":"required","status":{"response":"accepted"},"emailAddress":{"name":"Marie","address":"Marie@example.com"}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	if aerr := applyEventPatch(cal, master, p, mapNow); aerr != nil {
		t.Fatal(aerr.message)
	}
	out := strings.ReplaceAll(string(cal.Encode()), "\r\n ", "")
	for _, keep := range []string{
		`X-APPLE-STRUCTURED-LOCATION;VALUE=URI;X-ADDRESS="Main St 1, Springfield";X-APPLE-RADIUS=49.9:geo:1.5,2.5`,
		"ACTION:AUDIO",
		"DTSTART:20261105T100000Z",
		"X-NUM-GUESTS=2",
		"RSVP=TRUE",
		"PARTSTAT=ACCEPTED",
	} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %q in\n%s", keep, out)
		}
	}
	if !strings.Contains(out, "X-LONG:"+strings.Repeat("abcdefghij", 20)) {
		t.Error("lost the long property")
	}
	if strings.Count(out, "ATTENDEE") != 1 {
		t.Errorf("attendees duplicated:\n%s", out)
	}
	if !strings.Contains(out, "SUMMARY:New") {
		t.Errorf("subject not changed:\n%s", out)
	}
}

func TestPreferredZone(t *testing.T) {
	cases := []struct {
		prefer []string
		want   string
	}{
		{nil, "UTC"},
		{[]string{`outlook.timezone="Pacific Standard Time"`}, "Pacific Standard Time"},
		{[]string{`odata.maxpagesize=5, outlook.timezone="Europe/Berlin"`}, "Europe/Berlin"},
		{[]string{`outlook.body-content-type="text"`}, "UTC"},
	}
	for _, tc := range cases {
		if got := preferredZone(tc.prefer); got != tc.want {
			t.Errorf("preferredZone(%q) = %q, want %q", tc.prefer, got, tc.want)
		}
	}
}

func TestOccurrenceMapping(t *testing.T) {
	cal := loadICS(t, "overrides")
	v, aerr := newEventView(cal, "Europe/Berlin")
	if aerr != nil {
		t.Fatal(aerr.message)
	}
	occs, err := cal.Occurrences(v.res, nil, ical.Range{})
	if err != nil {
		t.Fatal(err)
	}
	master, overrides := eventComponents(cal)
	// Nov 5 and Nov 19 (moved to the afternoon); Nov 12 is excluded.
	if len(occs) != 3 {
		t.Fatalf("got %d occurrences, want 3: %+v", len(occs), occs)
	}
	var got []string
	for _, o := range occs {
		base := master
		if o.Override {
			base = overrides[0]
		}
		e := v.occurrence(base, o)
		if e.Recurrence != nil || e.RecurrenceEditable != nil {
			t.Errorf("an occurrence carries a recurrence: %+v", e)
		}
		got = append(got, e.Type+" "+e.Start.DateTime[:16]+" "+e.Subject)
	}
	want := []string{
		"occurrence 2026-11-05T10:00 Series",
		"exception 2026-11-19T14:00 Series, moved",
		"occurrence 2026-11-26T10:00 Series",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
