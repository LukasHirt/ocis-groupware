package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/emersion/go-ical"
)

func TestEventIsValidICalendar(t *testing.T) {
	for i := 0; i < 40; i++ {
		data := buildEvent("u", i)
		cal, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
		if err != nil {
			t.Fatalf("event %d does not parse: %v\n%s", i, err, data)
		}
		evs := cal.Events()
		if len(evs) != 1 {
			t.Fatalf("event %d: %d events", i, len(evs))
		}
		if uid, _ := evs[0].Props.Text(ical.PropUID); !strings.HasPrefix(uid, "u-") {
			t.Errorf("event %d: uid %q", i, uid)
		}
	}
}

func TestEventIsDeterministicAndSomeRecur(t *testing.T) {
	if !bytes.Equal(buildEvent("u", 7), buildEvent("u", 7)) {
		t.Error("the same index gives different events")
	}
	recurring := 0
	for i := 0; i < 1000; i++ {
		if bytes.Contains(buildEvent("u", i), []byte("RRULE:")) {
			recurring++
		}
	}
	if recurring < 20 || recurring > 100 {
		t.Errorf("%d of 1000 events recur, want about 5%%", recurring)
	}
}

func TestParseSyncToken(t *testing.T) {
	body := `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:sync-token>http://x/ns/sync/42</d:sync-token></d:multistatus>`
	if got := parseSyncToken([]byte(body)); got != "http://x/ns/sync/42" {
		t.Errorf("token = %q", got)
	}
	if got := parseSyncToken([]byte("<a/>")); got != "" {
		t.Errorf("token = %q, want empty", got)
	}
}
