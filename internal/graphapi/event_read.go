package graphapi

import (
	"net/mail"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/ical"
)

// eventView turns the components of one calendar object into Graph events,
// with times shown in one zone.
type eventView struct {
	res  *ical.Resolver
	zone string
}

// newEventView returns a view of cal that shows times in zone: a time zone
// name as Prefer: outlook.timezone carries it, or "" for UTC.
func newEventView(cal *ical.Calendar, zone string) (eventView, *apiError) {
	kind, ok := classifyZone(zone)
	if !ok {
		return eventView{}, badRequest("unknown time zone in the Prefer header")
	}
	if kind == zoneUTC {
		zone = "UTC"
	}
	return eventView{res: ical.NewResolver(cal), zone: zone}, nil
}

// eventComponents returns the master VEVENT of an object, which is the one
// without a RECURRENCE-ID (the first one when every one has an override
// mark), and the overrides.
func eventComponents(cal *ical.Calendar) (master *ical.Component, overrides []*ical.Component) {
	for _, c := range cal.Comps("VEVENT") {
		if c.Prop("RECURRENCE-ID") == nil && master == nil {
			master = c
			continue
		}
		overrides = append(overrides, c)
	}
	if master == nil && len(overrides) > 0 {
		master, overrides = overrides[0], overrides[1:]
	}
	return master, overrides
}

// span is when an event starts and ends, as instants. For an all-day event
// they are midnight UTC of the first day and of the day after the last.
type span struct {
	start, end time.Time
	allDay     bool
}

func (v eventView) span(c *ical.Component) span {
	p := c.Prop("DTSTART")
	if p == nil {
		return span{}
	}
	val, err := ical.ParseDateTime(p.Value, p.Params)
	if err != nil {
		return span{}
	}
	s := span{start: v.res.Instant(val, nil), allDay: val.Date}
	s.end = s.start
	if s.allDay {
		s.end = s.start.Add(24 * time.Hour)
	}
	if e := c.Prop("DTEND"); e != nil {
		if ev, err := ical.ParseDateTime(e.Value, e.Params); err == nil {
			s.end = v.res.Instant(ev, nil)
		}
	} else if d := c.Prop("DURATION"); d != nil {
		if dur, err := ical.ParseDuration(d.Value); err == nil {
			s.end = s.start.Add(dur)
		}
	}
	return s
}

func (v eventView) format(t time.Time, allDay bool) dateTimeZone {
	wall := t.UTC()
	if !allDay {
		wall = v.res.WallOf(t, v.zone, nil)
	}
	return dateTimeZone{DateTime: wall.Format(graphTimeLayout), TimeZone: v.zone}
}

// event maps one component as it is stored: a single event, a series master
// or an override.
func (v eventView) event(c *ical.Component) eventJSON {
	sp := v.span(c)
	e := eventJSON{
		Type:        "singleInstance",
		Subject:     textOf(c, "SUMMARY"),
		Body:        bodyJSON{ContentType: "text", Content: textOf(c, "DESCRIPTION")},
		Start:       v.format(sp.start, sp.allDay),
		End:         v.format(sp.end, sp.allDay),
		IsAllDay:    sp.allDay,
		ShowAs:      "busy",
		Sensitivity: "normal",
		Categories:  categoriesOf(c),
		Attendees:   []attendeeJSON{},
		Location:    locationJSON{DisplayName: textOf(c, "LOCATION")},
	}
	e.ICalUID = textOf(c, "UID")
	if p := c.Prop("STATUS"); p != nil {
		switch strings.ToUpper(strings.TrimSpace(p.Value)) {
		case "CANCELLED":
			e.IsCancelled = true
		case "TENTATIVE":
			e.ShowAs = "tentative"
		}
	}
	if p := c.Prop("TRANSP"); p != nil && strings.EqualFold(strings.TrimSpace(p.Value), "TRANSPARENT") {
		e.ShowAs = "free"
	}
	if p := c.Prop("CLASS"); p != nil {
		switch strings.ToUpper(strings.TrimSpace(p.Value)) {
		case "PRIVATE":
			e.Sensitivity = "private"
		case "CONFIDENTIAL":
			e.Sensitivity = "confidential"
		}
	}
	e.CreatedDateTime = utcStamp(c, "CREATED")
	e.LastModifiedDateTime = utcStamp(c, "LAST-MODIFIED")
	for _, p := range c.PropAll("ATTENDEE") {
		e.Attendees = append(e.Attendees, attendeeOf(p))
	}
	if p := c.Prop("ORGANIZER"); p != nil {
		a := mailAddress(p)
		e.Organizer = &recipientJSON{EmailAddress: a}
	}
	if a := firstReminder(c); a != nil {
		e.IsReminderOn, e.ReminderMinutes = true, a.minutes
	}
	switch {
	case c.Prop("RECURRENCE-ID") != nil:
		e.Type = "exception"
	case len(c.PropAll("RRULE")) > 0 || len(c.PropAll("RDATE")) > 0 || c.Prop("EXRULE") != nil:
		e.Type = "seriesMaster"
		e.Recurrence, e.RecurrenceEditable = v.recurrence(c)
	}
	return e
}

// occurrence maps one instance of a series. base is the override that
// replaces the instance, or the master.
func (v eventView) occurrence(base *ical.Component, occ ical.Occurrence) eventJSON {
	e := v.event(base)
	e.Type = "occurrence"
	if occ.Override {
		e.Type = "exception"
	}
	e.Recurrence, e.RecurrenceEditable = nil, nil
	all := e.IsAllDay
	e.Start = v.format(occ.Start, all)
	e.End = v.format(occ.End, all)
	return e
}

func textOf(c *ical.Component, name string) string {
	if p := c.Prop(name); p != nil {
		return p.Text()
	}
	return ""
}

func utcStamp(c *ical.Component, name string) string {
	p := c.Prop(name)
	if p == nil {
		return ""
	}
	val, err := ical.ParseDateTime(p.Value, p.Params)
	if err != nil || !val.UTC {
		return ""
	}
	return val.Wall.Format("2006-01-02T15:04:05.0000000Z")
}

// categoriesOf returns the items of every CATEGORIES property.
func categoriesOf(c *ical.Component) []string {
	out := []string{}
	for _, p := range c.PropAll("CATEGORIES") {
		start := 0
		for i := 0; i <= len(p.Value); i++ {
			switch {
			case i < len(p.Value) && p.Value[i] == '\\':
				i++
			case i == len(p.Value) || p.Value[i] == ',':
				if item := (ical.Property{Value: p.Value[start:i]}).Text(); item != "" {
					out = append(out, item)
				}
				start = i + 1
			}
		}
	}
	return out
}

func mailAddress(p *ical.Property) emailAddress {
	addr := strings.TrimSpace(p.Value)
	if len(addr) >= 7 && strings.EqualFold(addr[:7], "mailto:") {
		addr = addr[7:]
	}
	name := ""
	if cn := p.Params["CN"]; len(cn) > 0 {
		name = cn[0]
	}
	if name == "" {
		name = addr
	}
	return emailAddress{Name: name, Address: addr}
}

func attendeeOf(p ical.Property) attendeeJSON {
	a := attendeeJSON{Type: "required", EmailAddress: mailAddress(&p)}
	if r := p.Params["ROLE"]; len(r) > 0 && strings.EqualFold(r[0], "OPT-PARTICIPANT") {
		a.Type = "optional"
	}
	if c := p.Params["CUTYPE"]; len(c) > 0 && (strings.EqualFold(c[0], "RESOURCE") || strings.EqualFold(c[0], "ROOM")) {
		a.Type = "resource"
	}
	a.Status.Response = "notResponded"
	if s := p.Params["PARTSTAT"]; len(s) > 0 {
		switch strings.ToUpper(s[0]) {
		case "ACCEPTED":
			a.Status.Response = "accepted"
		case "DECLINED":
			a.Status.Response = "declined"
		case "TENTATIVE":
			a.Status.Response = "tentativelyAccepted"
		}
	}
	return a
}

type alarm struct {
	comp    *ical.Component
	minutes int
}

// firstReminder returns the first display alarm that fires a fixed time
// before the start.
func firstReminder(c *ical.Component) *alarm {
	for _, a := range c.Comps("VALARM") {
		if act := a.Prop("ACTION"); act == nil || !strings.EqualFold(strings.TrimSpace(act.Value), "DISPLAY") {
			continue
		}
		t := a.Prop("TRIGGER")
		if t == nil {
			continue
		}
		if rel := t.Params["RELATED"]; len(rel) > 0 && !strings.EqualFold(rel[0], "START") {
			continue
		}
		if vt := t.Params["VALUE"]; len(vt) > 0 && !strings.EqualFold(vt[0], "DURATION") {
			continue
		}
		d, err := ical.ParseDuration(strings.TrimSpace(t.Value))
		if err != nil || d > 0 {
			continue
		}
		return &alarm{comp: a, minutes: int(-d / time.Minute)}
	}
	return nil
}

// validAddress reports whether s can stand as an e-mail address in a mailto
// URI.
func validAddress(s string) bool {
	if len(s) > 254 || strings.ContainsAny(s, " \t\r\n;,<>\"") {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}
