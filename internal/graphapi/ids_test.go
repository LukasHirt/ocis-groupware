package graphapi

import (
	"strings"
	"testing"
	"time"
)

func TestCalendarIDRoundTrip(t *testing.T) {
	for _, raw := range []string{"abc", "a/b+c==", "ünïcode", "0123456789abcdef"} {
		id := CalendarID(raw)
		if strings.ContainsAny(id, "+/=.") {
			t.Errorf("CalendarID(%q) = %q, want URL-safe without padding or dots", raw, id)
		}
		got, err := ParseCalendarID(id)
		if err != nil || got != raw {
			t.Errorf("ParseCalendarID(%q) = %q, %v; want %q", id, got, err, raw)
		}
	}
}

func TestParseCalendarIDRejects(t *testing.T) {
	for _, id := range []string{"", "***", "YQ==", "YR", "a.b"} {
		if _, err := ParseCalendarID(id); err == nil {
			t.Errorf("ParseCalendarID(%q) succeeded, want an error", id)
		}
	}
}

func TestEventIDRoundTrip(t *testing.T) {
	id := EventID("coll-1", "a b/c.ics")
	coll, name, err := ParseEventID(id)
	if err != nil || coll != "coll-1" || name != "a b/c.ics" {
		t.Fatalf("ParseEventID(%q) = %q, %q, %v", id, coll, name, err)
	}
	if strings.Count(id, ".") != 1 {
		t.Errorf("event id %q has %d dots, want 1", id, strings.Count(id, "."))
	}
}

func TestOccurrenceIDRoundTrip(t *testing.T) {
	event := EventID("coll", "x.ics")
	at := time.Date(2026, 3, 29, 1, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	id := OccurrenceID(event, at)
	if !strings.HasSuffix(id, ".20260328T233000Z") {
		t.Errorf("OccurrenceID = %q, want the UTC recurrence id as suffix", id)
	}
	gotEvent, gotAt, err := ParseOccurrenceID(id)
	if err != nil || gotEvent != event || !gotAt.Equal(at) || gotAt.Location() != time.UTC {
		t.Errorf("ParseOccurrenceID(%q) = %q, %v, %v", id, gotEvent, gotAt, err)
	}
	if _, _, err := ParseOccurrenceID(event); err == nil {
		t.Error("ParseOccurrenceID of an event id succeeded")
	}
	if _, _, err := ParseOccurrenceID(event + ".2026-03-29"); err == nil {
		t.Error("ParseOccurrenceID accepted a malformed recurrence id")
	}
}

func TestParseEventIDRejects(t *testing.T) {
	for _, id := range []string{"", "abc", "a.b.c", ".", "YQ.", ".YQ", "***.YQ"} {
		if _, _, err := ParseEventID(id); err == nil {
			t.Errorf("ParseEventID(%q) succeeded, want an error", id)
		}
	}
}

func FuzzIDRoundTrip(f *testing.F) {
	f.Add("coll", "name.ics")
	f.Add("", "")
	f.Add("a.b", "c/d")
	f.Fuzz(func(t *testing.T, coll, name string) {
		if coll == "" || name == "" {
			return
		}
		if got, err := ParseCalendarID(CalendarID(coll)); err != nil || got != coll {
			t.Fatalf("calendar id of %q came back as %q, %v", coll, got, err)
		}
		id := EventID(coll, name)
		gc, gn, err := ParseEventID(id)
		if err != nil || gc != coll || gn != name {
			t.Fatalf("event id of %q, %q came back as %q, %q, %v", coll, name, gc, gn, err)
		}
		// Whatever parses must re-encode to the same text.
		if _, _, err := ParseEventID(id); err == nil && EventID(gc, gn) != id {
			t.Fatalf("event id %q is not canonical", id)
		}
	})
}

func FuzzParseID(f *testing.F) {
	f.Add("YQ.Yg")
	f.Add("YQ.Yg.20260101T000000Z")
	f.Fuzz(func(t *testing.T, id string) {
		if c, n, err := ParseEventID(id); err == nil && EventID(c, n) != id {
			t.Fatalf("ParseEventID(%q) = %q, %q, which encodes to %q", id, c, n, EventID(c, n))
		}
		if e, at, err := ParseOccurrenceID(id); err == nil && OccurrenceID(e, at) != id {
			t.Fatalf("ParseOccurrenceID(%q) = %q, %v, which encodes to %q", id, e, at, OccurrenceID(e, at))
		}
	})
}
