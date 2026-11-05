package graphapi

import (
	"regexp"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/ical"
)

// graphTimeLayout is the form of a dateTime in a Graph dateTimeTimeZone.
const graphTimeLayout = "2006-01-02T15:04:05.0000000"

type zoneKind int

const (
	zoneUTC zoneKind = iota
	zoneIANA
	zoneWindows
)

var zoneNameRE = regexp.MustCompile(`^[A-Za-z0-9_+\-/ .()',]{1,64}$`)

// windowsZeroOffset are Windows zone names that keep UTC's offset all year,
// which the offset probe in classifyZone cannot tell from an unknown name.
var windowsZeroOffset = []string{"Greenwich Standard Time", "Coordinated Universal Time", "UTC"}

// classifyZone says what kind of time zone name this is. The second result is
// false for a name that is neither UTC, an IANA name nor a Windows name the
// time zone map knows.
func classifyZone(name string) (zoneKind, bool) {
	switch strings.ToLower(name) {
	case "", "utc", "etc/utc", "z", "coordinated universal time":
		return zoneUTC, true
	}
	if !zoneNameRE.MatchString(name) || strings.EqualFold(name, "local") {
		return 0, false
	}
	if _, err := time.LoadLocation(name); err == nil {
		return zoneIANA, true
	}
	res := ical.NewResolver(nil)
	for _, m := range []time.Month{time.January, time.April, time.July, time.October} {
		wall := time.Date(2026, m, 15, 12, 0, 0, 0, time.UTC)
		if !res.Instant(ical.Value{Wall: wall, TZID: name}, nil).Equal(wall) {
			return zoneWindows, true
		}
	}
	for _, z := range windowsZeroOffset {
		if strings.EqualFold(name, z) {
			return zoneWindows, true
		}
	}
	return 0, false
}

// zonedTime is a wall-clock time in a named zone.
type zonedTime struct {
	wall time.Time // fields labelled UTC
	zone string    // "" for UTC
	kind zoneKind
}

// parseDateTimeZone reads a Graph dateTimeTimeZone.
func parseDateTimeZone(d dateTimeZone) (zonedTime, *apiError) {
	kind, ok := classifyZone(d.TimeZone)
	if !ok {
		return zonedTime{}, badRequest("unknown time zone " + clip(d.TimeZone))
	}
	s := strings.TrimSuffix(d.DateTime, "Z")
	wall, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		wall, err = time.Parse("2006-01-02", s)
	}
	if err != nil {
		return zonedTime{}, badRequest("dateTime must look like 2026-11-05T10:00:00")
	}
	z := zonedTime{wall: wall, kind: kind}
	if kind != zoneUTC {
		z.zone = d.TimeZone
	}
	return z, nil
}

func clip(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

func (z zonedTime) value() ical.Value {
	return ical.Value{Wall: z.wall, UTC: z.kind == zoneUTC, TZID: z.zone}
}

// instant returns the moment z stands for.
func instant(res *ical.Resolver, z zonedTime) time.Time {
	return res.Instant(z.value(), nil)
}

// inZone returns the wall time of an instant in the zone of like.
func inZone(res *ical.Resolver, inst time.Time, like zonedTime) zonedTime {
	like.wall = res.WallOf(inst, like.zone, nil)
	return like
}

// timeProp builds a DTSTART or DTEND property. A Windows zone name is kept
// only when keepZone is set, which a recurring event needs so the rule keeps
// its local time across a change of offset. Otherwise the property holds the
// UTC instant, which every client reads the same way.
func timeProp(res *ical.Resolver, name string, z zonedTime, allDay, keepZone bool) ical.Property {
	switch {
	case allDay:
		return ical.Property{Name: name, Params: map[string][]string{"VALUE": {"DATE"}}, Value: z.wall.Format("20060102")}
	case z.kind == zoneUTC:
		return ical.Property{Name: name, Value: z.wall.Format("20060102T150405Z")}
	case z.kind == zoneWindows && !keepZone:
		return ical.Property{Name: name, Value: instant(res, z).Format("20060102T150405Z")}
	}
	return ical.Property{Name: name, Params: map[string][]string{"TZID": {z.zone}}, Value: z.wall.Format("20060102T150405")}
}

// preferredZone returns the zone a Prefer: outlook.timezone="..." header asks
// for, or "UTC" when there is none.
func preferredZone(prefer []string) string {
	for _, h := range prefer {
		for _, part := range strings.Split(h, ",") {
			part = strings.TrimSpace(part)
			if k, v, ok := strings.Cut(part, "="); ok && strings.EqualFold(strings.TrimSpace(k), "outlook.timezone") {
				return strings.Trim(strings.TrimSpace(v), `"`)
			}
		}
	}
	return "UTC"
}
