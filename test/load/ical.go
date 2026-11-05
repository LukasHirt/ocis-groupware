package main

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"
)

// The events spread over 2026. About one in twenty recurs weekly, which is
// the part of a calendar a range query has to expand.
var (
	eventsStart = time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	eventsSpan  = 360 * 24 * time.Hour
)

// buildEvent returns event i of the calendar called prefix. The same prefix
// and index always give the same bytes.
func buildEvent(prefix string, i int) []byte {
	rng := rand.New(rand.NewSource(int64(i)*7919 + int64(len(prefix))))
	start := eventsStart.Add(time.Duration(rng.Int63n(int64(eventsSpan)))).Truncate(15 * time.Minute)
	end := start.Add(time.Duration(1+rng.Intn(4)) * 30 * time.Minute)
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//load//EN\r\nBEGIN:VEVENT\r\n")
	fmt.Fprintf(&b, "UID:%s-%d@load.example\r\n", prefix, i)
	b.WriteString("DTSTAMP:20260101T000000Z\r\n")
	fmt.Fprintf(&b, "DTSTART:%s\r\nDTEND:%s\r\n", start.Format("20060102T150405Z"), end.Format("20060102T150405Z"))
	if rng.Intn(20) == 0 {
		fmt.Fprintf(&b, "RRULE:FREQ=WEEKLY;COUNT=%d\r\n", 10+rng.Intn(40))
	}
	fmt.Fprintf(&b, "SUMMARY:Meeting %d with the %s team\r\n", i, prefix)
	b.WriteString("LOCATION:Room " + fmt.Sprint(1+rng.Intn(40)) + "\r\n")
	b.WriteString("DESCRIPTION:Agenda for the regular sync. Please read the notes ahead of the meeting and bring your questions.\r\n")
	b.WriteString("END:VEVENT\r\nEND:VCALENDAR\r\n")
	return []byte(b.String())
}

var syncTokenRE = regexp.MustCompile(`<(?:\w+:)?sync-token>([^<]*)</(?:\w+:)?sync-token>`)

// parseSyncToken returns the sync token of a multistatus body, or "".
func parseSyncToken(body []byte) string {
	m := syncTokenRE.FindSubmatch(body)
	if m == nil {
		return ""
	}
	return string(m[1])
}
