package graphapi

import (
	"strconv"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/ical"
)

// maxAttendees is the most attendees an event takes.
const maxAttendees = 500

// prodID names this service in the objects it writes.
const prodID = "-//ocis-groupware//Graph//EN"

// newEventCalendar builds the stored form of a new event. The start and the
// end are required.
func newEventCalendar(p eventPatch, uid string, now time.Time) (*ical.Calendar, *apiError) {
	if !p.Start.Set || p.Start.Null || !p.End.Set || p.End.Null {
		return nil, badRequest("start and end are required")
	}
	stamp := now.UTC().Format("20060102T150405Z")
	comp := &ical.Component{Name: "VEVENT", Props: []ical.Property{
		{Name: "UID", Value: escapeText(uid)},
		{Name: "DTSTAMP", Value: stamp},
		{Name: "CREATED", Value: stamp},
	}}
	cal := &ical.Calendar{Component: ical.Component{
		Name: "VCALENDAR",
		Props: []ical.Property{
			{Name: "VERSION", Value: "2.0"},
			{Name: "PRODID", Value: prodID},
		},
		Children: []*ical.Component{comp},
	}}
	if aerr := applyEventPatch(cal, comp, p, now); aerr != nil {
		return nil, aerr
	}
	return cal, nil
}

// applyEventPatch changes comp, a VEVENT of cal, by the fields p carries and
// leaves every other property and child as it is. On an error comp may be
// half changed; the caller drops it.
func applyEventPatch(cal *ical.Calendar, comp *ical.Component, p eventPatch, now time.Time) *apiError {
	res := ical.NewResolver(cal)
	w := eventWriter{res: res, cal: cal, comp: comp}

	if p.Subject.Set {
		if aerr := w.text("SUMMARY", p.Subject); aerr != nil {
			return aerr
		}
	}
	if p.Body.Set {
		if !p.Body.Null && p.Body.Val.ContentType != "" && !strings.EqualFold(p.Body.Val.ContentType, "text") {
			return badRequest("only text bodies are supported; send body.contentType text")
		}
		b := opt[string]{Set: true, Null: p.Body.Null, Val: p.Body.Val.Content}
		if aerr := w.text("DESCRIPTION", b); aerr != nil {
			return aerr
		}
	}
	if p.Location.Set {
		l := opt[string]{Set: true, Null: p.Location.Null, Val: p.Location.Val.DisplayName}
		if aerr := w.text("LOCATION", l); aerr != nil {
			return aerr
		}
	}
	if aerr := w.showAs(p); aerr != nil {
		return aerr
	}
	if p.Sensitivity.Set {
		if aerr := w.sensitivity(p.Sensitivity); aerr != nil {
			return aerr
		}
	}
	if p.Categories.Set {
		if aerr := w.categories(p.Categories); aerr != nil {
			return aerr
		}
	}
	if p.Attendees.Set {
		if aerr := w.attendees(p.Attendees); aerr != nil {
			return aerr
		}
	}
	if aerr := w.when(p); aerr != nil {
		return aerr
	}
	if p.IsReminderOn.Set || p.ReminderMinutes.Set {
		if aerr := w.reminder(p); aerr != nil {
			return aerr
		}
	}
	w.set("DTSTAMP", nil, now.UTC().Format("20060102T150405Z"))
	w.set("LAST-MODIFIED", nil, now.UTC().Format("20060102T150405Z"))
	return nil
}

type eventWriter struct {
	res  *ical.Resolver
	cal  *ical.Calendar
	comp *ical.Component
}

// set replaces the first property called name with one that has these
// parameters and value, drops the others of that name, or appends it.
func (w eventWriter) set(name string, params map[string][]string, value string) {
	setProp(w.comp, ical.Property{Name: name, Params: params, Value: value})
}

func setProp(c *ical.Component, np ical.Property) {
	out := c.Props[:0:0]
	done := false
	for _, p := range c.Props {
		if p.Name != np.Name {
			out = append(out, p)
			continue
		}
		if !done {
			out = append(out, np)
			done = true
		}
	}
	if !done {
		out = append(out, np)
	}
	c.Props = out
}

func removeProps(c *ical.Component, names ...string) {
	out := c.Props[:0:0]
	for _, p := range c.Props {
		drop := false
		for _, n := range names {
			drop = drop || p.Name == n
		}
		if !drop {
			out = append(out, p)
		}
	}
	c.Props = out
}

// escapeText writes s as an iCalendar TEXT value.
func escapeText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return r.Replace(s)
}

// checkText refuses control characters other than tab and line breaks.
func checkText(field, s string) *apiError {
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0x7f {
			return badRequest(field + " must not contain control characters")
		}
	}
	return nil
}

func (w eventWriter) text(name string, v opt[string]) *apiError {
	if v.Null || v.Val == "" {
		removeProps(w.comp, name)
		return nil
	}
	if aerr := checkText(strings.ToLower(name), v.Val); aerr != nil {
		return aerr
	}
	w.set(name, nil, escapeText(v.Val))
	return nil
}

func (w eventWriter) showAs(p eventPatch) *apiError {
	if !p.ShowAs.Set {
		return nil
	}
	v := p.ShowAs.Val
	if p.ShowAs.Null {
		v = "busy"
	}
	status := ""
	if s := w.comp.Prop("STATUS"); s != nil {
		status = strings.ToUpper(strings.TrimSpace(s.Value))
	}
	switch strings.ToLower(v) {
	case "free":
		w.set("TRANSP", nil, "TRANSPARENT")
		if status == "TENTATIVE" {
			removeProps(w.comp, "STATUS")
		}
	case "tentative":
		w.set("TRANSP", nil, "OPAQUE")
		if status != "CANCELLED" {
			w.set("STATUS", nil, "TENTATIVE")
		}
	case "busy", "oof", "workingelsewhere", "unknown":
		w.set("TRANSP", nil, "OPAQUE")
		if status == "TENTATIVE" {
			removeProps(w.comp, "STATUS")
		}
	default:
		return badRequest("showAs must be free, tentative, busy, oof, workingElsewhere or unknown")
	}
	return nil
}

func (w eventWriter) sensitivity(v opt[string]) *apiError {
	val := strings.ToLower(v.Val)
	if v.Null {
		val = "normal"
	}
	switch val {
	case "normal":
		if w.comp.Prop("CLASS") != nil {
			w.set("CLASS", nil, "PUBLIC")
		}
	case "personal", "private":
		w.set("CLASS", nil, "PRIVATE")
	case "confidential":
		w.set("CLASS", nil, "CONFIDENTIAL")
	default:
		return badRequest("sensitivity must be normal, personal, private or confidential")
	}
	return nil
}

func (w eventWriter) categories(v opt[[]string]) *apiError {
	removeProps(w.comp, "CATEGORIES")
	if v.Null || len(v.Val) == 0 {
		return nil
	}
	items := make([]string, 0, len(v.Val))
	for _, c := range v.Val {
		if c == "" {
			continue
		}
		if aerr := checkText("categories", c); aerr != nil {
			return aerr
		}
		items = append(items, escapeText(c))
	}
	if len(items) > 0 {
		w.set("CATEGORIES", nil, strings.Join(items, ","))
	}
	return nil
}

func (w eventWriter) attendees(v opt[[]attendeeJSON]) *apiError {
	list := v.Val
	if v.Null {
		list = nil
	}
	if len(list) > maxAttendees {
		return badRequest("an event takes at most " + strconv.Itoa(maxAttendees) + " attendees")
	}
	old := w.comp.PropAll("ATTENDEE")
	removeProps(w.comp, "ATTENDEE")
	seen := map[string]bool{}
	for _, a := range list {
		addr := a.EmailAddress.Address
		if !validAddress(addr) {
			return badRequest("every attendee needs a valid emailAddress.address")
		}
		if aerr := checkText("attendee name", a.EmailAddress.Name); aerr != nil {
			return aerr
		}
		key := strings.ToLower(addr)
		if seen[key] {
			continue
		}
		seen[key] = true
		np := ical.Property{Name: "ATTENDEE", Params: map[string][]string{}, Value: "mailto:" + addr}
		for _, o := range old {
			if strings.EqualFold(mailAddress(&o).Address, addr) {
				np = ical.Property{Name: "ATTENDEE", Params: cloneParams(o.Params), Value: o.Value}
				break
			}
		}
		if n := a.EmailAddress.Name; n != "" && n != addr {
			np.Params["CN"] = []string{n}
		}
		switch strings.ToLower(a.Type) {
		case "optional":
			np.Params["ROLE"] = []string{"OPT-PARTICIPANT"}
			delete(np.Params, "CUTYPE")
		case "resource":
			np.Params["ROLE"] = []string{"NON-PARTICIPANT"}
			np.Params["CUTYPE"] = []string{"RESOURCE"}
		case "required", "":
			np.Params["ROLE"] = []string{"REQ-PARTICIPANT"}
			delete(np.Params, "CUTYPE")
		default:
			return badRequest("attendee type must be required, optional or resource")
		}
		if r := strings.ToLower(a.Status.Response); r != "" {
			switch r {
			case "accepted":
				np.Params["PARTSTAT"] = []string{"ACCEPTED"}
			case "declined":
				np.Params["PARTSTAT"] = []string{"DECLINED"}
			case "tentativelyaccepted":
				np.Params["PARTSTAT"] = []string{"TENTATIVE"}
			case "notresponded", "none", "organizer":
				np.Params["PARTSTAT"] = []string{"NEEDS-ACTION"}
			default:
				return badRequest("unknown attendee response")
			}
		} else if np.Params["PARTSTAT"] == nil {
			np.Params["PARTSTAT"] = []string{"NEEDS-ACTION"}
		}
		w.comp.Props = append(w.comp.Props, np)
	}
	return nil
}

func cloneParams(m map[string][]string) map[string][]string {
	out := make(map[string][]string, len(m)+3)
	for k, v := range m {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (w eventWriter) reminder(p eventPatch) *apiError {
	cur := firstReminder(w.comp)
	on := cur != nil
	if p.IsReminderOn.Set {
		on = !p.IsReminderOn.Null && p.IsReminderOn.Val
	}
	minutes := 15
	if cur != nil {
		minutes = cur.minutes
	}
	if p.ReminderMinutes.Set && !p.ReminderMinutes.Null {
		minutes = p.ReminderMinutes.Val
		if minutes < 0 || minutes > 40320*2 {
			return badRequest("reminderMinutesBeforeStart must be from 0 to 80640")
		}
	}
	trigger := "-PT" + strconv.Itoa(minutes) + "M"
	switch {
	case !on && cur != nil:
		kids := w.comp.Children[:0:0]
		for _, k := range w.comp.Children {
			if k != cur.comp {
				kids = append(kids, k)
			}
		}
		w.comp.Children = kids
	case on && cur != nil:
		setProp(cur.comp, ical.Property{Name: "TRIGGER", Value: trigger})
	case on:
		w.comp.Children = append(w.comp.Children, &ical.Component{Name: "VALARM", Props: []ical.Property{
			{Name: "ACTION", Value: "DISPLAY"},
			{Name: "DESCRIPTION", Value: "Reminder"},
			{Name: "TRIGGER", Value: trigger},
		}})
	}
	return nil
}

// current describes the stored start of the event as a zonedTime.
func (w eventWriter) current() (zonedTime, bool, bool) {
	p := w.comp.Prop("DTSTART")
	if p == nil {
		return zonedTime{}, false, false
	}
	val, err := ical.ParseDateTime(p.Value, p.Params)
	if err != nil {
		return zonedTime{}, false, false
	}
	z := zonedTime{wall: val.Wall, zone: val.TZID}
	if val.UTC || val.TZID == "" {
		z.zone = ""
	} else if k, ok := classifyZone(val.TZID); ok {
		z.kind = k
	} else {
		z.kind = zoneIANA
	}
	return z, val.Date, true
}

// when handles start, end, isAllDay and recurrence, which depend on each
// other.
func (w eventWriter) when(p eventPatch) *apiError {
	curStart, curAllDay, haveCur := w.current()
	allDay := curAllDay
	if p.IsAllDay.Set {
		allDay = !p.IsAllDay.Null && p.IsAllDay.Val
	}
	startSet := p.Start.Set && !p.Start.Null
	endSet := p.End.Set && !p.End.Null
	if (p.Start.Set && p.Start.Null) || (p.End.Set && p.End.Null) {
		return badRequest("start and end cannot be null")
	}
	if haveCur && allDay != curAllDay && (!startSet || !endSet) {
		return badRequest("changing isAllDay needs start and end")
	}
	rec := p.Recurrence.Set && !p.Recurrence.Null

	var sz, ez zonedTime
	var sInst, eInst time.Time
	var cur span
	if haveCur {
		cur = eventView{res: w.res}.span(w.comp)
	}
	if startSet {
		var aerr *apiError
		if sz, aerr = parseDateTimeZone(p.Start.Val); aerr != nil {
			return aerr
		}
		if rec && p.Recurrence.Val.Range.RecurrenceTimeZone != "" {
			rz, aerr := parseDateTimeZone(dateTimeZone{DateTime: p.Start.Val.DateTime, TimeZone: p.Recurrence.Val.Range.RecurrenceTimeZone})
			if aerr != nil {
				return aerr
			}
			if rz.zone != sz.zone {
				sInst = instant(w.res, sz)
				sz = inZone(w.res, sInst, rz)
			}
		}
		sInst = instant(w.res, sz)
	} else {
		sz, sInst = curStart, cur.start
	}
	switch {
	case endSet:
		var aerr *apiError
		if ez, aerr = parseDateTimeZone(p.End.Val); aerr != nil {
			return aerr
		}
		if startSet && rec && ez.zone != sz.zone && p.Recurrence.Val.Range.RecurrenceTimeZone != "" {
			ez = inZone(w.res, instant(w.res, ez), sz)
		}
		eInst = instant(w.res, ez)
	case startSet && haveCur:
		eInst = sInst.Add(cur.end.Sub(cur.start))
		ez = inZone(w.res, eInst, sz)
	case startSet:
		return badRequest("end is required")
	}

	if allDay && (startSet || endSet) {
		for _, z := range []zonedTime{sz, ez} {
			if (z != zonedTime{}) && (z.wall.Hour() != 0 || z.wall.Minute() != 0 || z.wall.Second() != 0) {
				return badRequest("an all-day event must start and end at midnight")
			}
		}
	}
	if startSet || endSet {
		if !eInst.After(sInst) && (allDay || eInst.Before(sInst)) {
			return badRequest("end must be after start")
		}
	}

	recurring := len(w.comp.PropAll("RRULE")) > 0
	if p.Recurrence.Set {
		if aerr := w.recurrence(p.Recurrence, sz, allDay, &recurring); aerr != nil {
			return aerr
		}
	}
	if startSet || (allDay != curAllDay) {
		w.comp.Props = replaceProp(w.comp.Props, timeProp(w.res, "DTSTART", sz, allDay, recurring))
	}
	if startSet || endSet {
		w.comp.Props = replaceProp(w.comp.Props, timeProp(w.res, "DTEND", ez, allDay, recurring))
		removeProps(w.comp, "DURATION")
	}
	return nil
}

func replaceProp(props []ical.Property, np ical.Property) []ical.Property {
	for i, p := range props {
		if p.Name == np.Name {
			props[i] = np
			return props
		}
	}
	return append(props, np)
}

// recurrence applies the recurrence field. A series whose rule the pattern
// cannot express keeps it when the client sends null, since the client only
// echoes what it read.
func (w eventWriter) recurrence(v opt[recurrenceJSON], start zonedTime, allDay bool, recurring *bool) *apiError {
	if v.Null {
		if len(w.comp.PropAll("RRULE")) == 0 && len(w.comp.PropAll("RDATE")) == 0 {
			return nil
		}
		if _, editable := (eventView{res: w.res}).recurrence(w.comp); editable != nil {
			return nil
		}
		removeProps(w.comp, "RRULE", "RDATE", "EXDATE")
		if w.comp.Prop("RECURRENCE-ID") == nil {
			// Overrides of a series that no longer exists go with it.
			var keep []*ical.Component
			for _, k := range w.cal.Children {
				if k.Name != "VEVENT" || k == w.comp || k.Prop("RECURRENCE-ID") == nil {
					keep = append(keep, k)
				}
			}
			w.cal.Children = keep
		}
		*recurring = false
		return nil
	}
	if w.comp.Prop("RECURRENCE-ID") != nil {
		return badRequest("an occurrence cannot have a recurrence of its own")
	}
	rule, aerr := buildRule(w.res, v.Val, start, allDay)
	if aerr != nil {
		return aerr
	}
	removeProps(w.comp, "RRULE")
	w.comp.Props = append(w.comp.Props, ical.Property{Name: "RRULE", Value: rule})
	*recurring = true
	return nil
}
