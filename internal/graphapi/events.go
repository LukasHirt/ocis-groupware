package graphapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/ical"
	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

// maxViewWindow is the longest span a calendarView request may ask for.
const maxViewWindow = 5 * 366 * 24 * time.Hour

// etagHeader is how an object's ETag shows in @odata.etag and in If-Match.
func etagHeader(etag string) string { return `W/"` + etag + `"` }

// unsupportedQuery answers 501 for the OData options the facade does not
// serve, so a client never gets an unfiltered list in place of a filtered one.
func unsupportedQuery(r *http.Request) *apiError {
	q := r.URL.Query()
	for _, k := range []string{"$filter", "$select", "$orderby", "$expand", "$count", "$search"} {
		if q.Has(k) {
			return newError(http.StatusNotImplemented, codeNotImplemented, k+" is not supported")
		}
	}
	return nil
}

// unsupportedSegment is a path segment that stands for an operation the
// facade does not have, where an id would go.
func unsupportedSegment(seg string) *apiError {
	if seg == "delta" || strings.HasPrefix(seg, "$") {
		return newError(http.StatusNotImplemented, codeNotImplemented, "this part of the Graph API is not supported")
	}
	return nil
}

// viewZone returns the zone the request asks events to be shown in, and
// answers Prefer as Graph does.
func viewZone(w http.ResponseWriter, r *http.Request) (string, *apiError) {
	zone := preferredZone(r.Header.Values("Prefer"))
	if _, ok := classifyZone(zone); !ok {
		return "", badRequest("unknown time zone in the Prefer header")
	}
	if zone != "UTC" || len(r.Header.Values("Prefer")) > 0 {
		w.Header().Set("Preference-Applied", `outlook.timezone="`+zone+`"`)
	}
	return zone, nil
}

// storedEvent is one object of a calendar, parsed.
type storedEvent struct {
	entry CalendarEntry
	obj   store.Object
	cal   *ical.Calendar
}

func (s storedEvent) id() string { return EventID(s.entry.Collection.ID, s.obj.Name) }

// loadEvents reads and parses the objects of a calendar. An object that does
// not parse, or has no event, is left out.
func (a *Handler) loadEvents(ctx context.Context, user principals.Principal, e CalendarEntry) ([]storedEvent, error) {
	objs, err := a.cfg.Store.ListObjects(ctx, e.Collection.Ref, store.ListOptions{IncludeData: true})
	if err != nil {
		return nil, err
	}
	out := make([]storedEvent, 0, len(objs))
	for _, o := range objs {
		cal, err := ical.Parse(o.Data)
		if err != nil {
			a.logger.Warn("skipped a stored event that does not parse", "sub", user.Sub, "object", o.Name, "error", err)
			continue
		}
		if len(cal.Comps("VEVENT")) == 0 {
			continue
		}
		out = append(out, storedEvent{entry: e, obj: o, cal: cal})
	}
	return out, nil
}

// withIDs adds what only the store knows to a mapped event.
func (s storedEvent) withIDs(e eventJSON) eventJSON {
	e.ID = s.id()
	e.ETag = etagHeader(s.obj.ETag)
	e.ChangeKey = s.obj.ETag
	return e
}

// seriesView is the event of an object: the master of a series, or the only
// event.
func (s storedEvent) seriesView(v eventView) (eventJSON, bool) {
	master, _ := eventComponents(s.cal)
	if master == nil {
		return eventJSON{}, false
	}
	return s.withIDs(v.event(master)), true
}

// scope resolves the calendar a request is about: the one in the path, or the
// default calendar when id is "" and the path says /me/calendar.
func (a *Handler) scope(ctx context.Context, c *call, def bool) (CalendarEntry, *apiError) {
	if def {
		return a.defaultCalendar(ctx, c.caller)
	}
	e, _, aerr := a.findCalendar(ctx, c.caller, c.params["id"])
	return e, aerr
}

func (a *Handler) listEvents(def bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		if aerr := unsupportedQuery(r); aerr != nil {
			writeError(w, aerr)
			return
		}
		zone, aerr := viewZone(w, r)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		entry, aerr := a.scope(r.Context(), c, def || c.params["id"] == "")
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		stored, err := a.loadEvents(r.Context(), c.caller, entry)
		if err != nil {
			writeError(w, a.storeError("list events", c.caller, err))
			return
		}
		out := []eventJSON{}
		for _, s := range stored {
			v, aerr := newEventView(s.cal, zone)
			if aerr != nil {
				writeError(w, aerr)
				return
			}
			if e, ok := s.seriesView(v); ok {
				out = append(out, e)
			}
		}
		writeCollection(w, r, out)
	}
}

// findEvent resolves an event or occurrence id to the stored object that
// holds it. With in set, the event must be in that calendar.
func (a *Handler) findEvent(ctx context.Context, user principals.Principal, id string, in *CalendarEntry) (storedEvent, time.Time, *apiError) {
	if aerr := unsupportedSegment(id); aerr != nil {
		return storedEvent{}, time.Time{}, aerr
	}
	eventID, rid := id, time.Time{}
	if strings.Count(id, ".") == 2 {
		var err error
		if eventID, rid, err = ParseOccurrenceID(id); err != nil {
			return storedEvent{}, time.Time{}, badRequest("the event id is malformed")
		}
	}
	collID, name, err := ParseEventID(eventID)
	if err != nil {
		return storedEvent{}, time.Time{}, badRequest("the event id is malformed")
	}
	var entry CalendarEntry
	if in != nil {
		if in.Collection.ID != collID {
			return storedEvent{}, time.Time{}, notFound("the event does not exist")
		}
		entry = *in
	} else {
		_, entries, _, err := a.myCalendars(ctx, user)
		if err != nil {
			a.logger.Error("could not list calendars", "sub", user.Sub, "error", err)
			return storedEvent{}, time.Time{}, internalError()
		}
		i := slices.IndexFunc(entries, func(e CalendarEntry) bool { return e.Collection.ID == collID })
		if i < 0 {
			return storedEvent{}, time.Time{}, notFound("the event does not exist")
		}
		entry = entries[i]
	}
	obj, err := a.cfg.Store.GetObject(ctx, entry.Collection.Ref, name)
	if err != nil {
		return storedEvent{}, time.Time{}, a.storeError("read an event", user, err)
	}
	cal, err := ical.Parse(obj.Data)
	if err != nil || len(cal.Comps("VEVENT")) == 0 {
		return storedEvent{}, time.Time{}, notFound("the event does not exist")
	}
	return storedEvent{entry: entry, obj: obj, cal: cal}, rid, nil
}

// overrideAt returns the override of the series whose RECURRENCE-ID is rid.
func overrideAt(res *ical.Resolver, overrides []*ical.Component, rid time.Time) *ical.Component {
	for _, o := range overrides {
		p := o.Prop("RECURRENCE-ID")
		if p == nil {
			continue
		}
		val, err := ical.ParseDateTime(p.Value, p.Params)
		if err == nil && res.Instant(val, nil).Equal(rid) {
			return o
		}
	}
	return nil
}

// findOccurrence returns the occurrence of a series at rid, which is an
// override's own when one replaces it. The component is the override, or the
// master.
func (s storedEvent) findOccurrence(v eventView, rid time.Time) (*ical.Component, ical.Occurrence, bool) {
	master, overrides := eventComponents(s.cal)
	if master == nil {
		return nil, ical.Occurrence{}, false
	}
	if o := overrideAt(v.res, overrides, rid); o != nil {
		sp := v.span(o)
		return o, ical.Occurrence{Start: sp.start, End: sp.end, RecurrenceID: rid, Override: true}, true
	}
	occs, err := s.cal.Occurrences(v.res, nil, ical.Range{Start: rid, End: rid.Add(time.Second)})
	if err != nil {
		return nil, ical.Occurrence{}, false
	}
	i := slices.IndexFunc(occs, func(o ical.Occurrence) bool { return !o.Override && o.RecurrenceID.Equal(rid) })
	if i < 0 {
		return nil, ical.Occurrence{}, false
	}
	return master, occs[i], true
}

// occurrenceAt maps the occurrence of a series at rid.
func (s storedEvent) occurrenceAt(v eventView, rid time.Time) (eventJSON, bool) {
	base, occ, ok := s.findOccurrence(v, rid)
	if !ok {
		return eventJSON{}, false
	}
	e := s.withIDs(v.occurrence(base, occ))
	e.ID = OccurrenceID(s.id(), rid)
	e.SeriesMasterID = s.id()
	return e, true
}

func (a *Handler) getEvent(def, scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		if aerr := unsupportedSegment(c.params["eid"]); aerr != nil {
			writeError(w, aerr)
			return
		}
		if aerr := unsupportedQuery(r); aerr != nil {
			writeError(w, aerr)
			return
		}
		zone, aerr := viewZone(w, r)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		var in *CalendarEntry
		if scoped {
			entry, aerr := a.scope(r.Context(), c, def)
			if aerr != nil {
				writeError(w, aerr)
				return
			}
			in = &entry
		}
		s, rid, aerr := a.findEvent(r.Context(), c.caller, c.params["eid"], in)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		v, aerr := newEventView(s.cal, zone)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		var (
			e  eventJSON
			ok bool
		)
		if rid.IsZero() {
			e, ok = s.seriesView(v)
		} else {
			e, ok = s.occurrenceAt(v, rid)
		}
		if !ok {
			writeError(w, notFound("the event does not exist"))
			return
		}
		writeJSON(w, http.StatusOK, e)
	}
}

// parseWindowTime reads the startDateTime or endDateTime of a calendarView.
// A time without an offset is UTC.
func parseWindowTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

type viewItem struct {
	event eventJSON
	start time.Time
}

func (a *Handler) calendarView(mode string) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		if aerr := unsupportedQuery(r); aerr != nil {
			writeError(w, aerr)
			return
		}
		zone, aerr := viewZone(w, r)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		q := r.URL.Query()
		from, ok1 := parseWindowTime(q.Get("startDateTime"))
		to, ok2 := parseWindowTime(q.Get("endDateTime"))
		switch {
		case !ok1 || !ok2:
			writeError(w, badRequest("startDateTime and endDateTime are required and must look like 2026-11-01T00:00:00Z"))
			return
		case !to.After(from):
			writeError(w, badRequest("endDateTime must be after startDateTime"))
			return
		case to.Sub(from) > maxViewWindow:
			writeError(w, badRequest("the window is longer than five years"))
			return
		}

		var entries []CalendarEntry
		switch mode {
		case "all":
			_, all, _, err := a.myCalendars(r.Context(), c.caller)
			if err != nil {
				a.logger.Error("could not list calendars", "sub", c.caller.Sub, "error", err)
				writeError(w, internalError())
				return
			}
			entries = all
		default:
			e, aerr := a.scope(r.Context(), c, mode == "default")
			if aerr != nil {
				writeError(w, aerr)
				return
			}
			entries = []CalendarEntry{e}
		}

		var items []viewItem
		for _, entry := range entries {
			stored, err := a.loadEvents(r.Context(), c.caller, entry)
			if err != nil {
				writeError(w, a.storeError("read calendar view", c.caller, err))
				return
			}
			for _, s := range stored {
				its, aerr := a.expand(s, zone, ical.Range{Start: from, End: to})
				if aerr != nil {
					writeError(w, aerr)
					return
				}
				items = append(items, its...)
			}
		}
		slices.SortStableFunc(items, func(x, y viewItem) int {
			if c := x.start.Compare(y.start); c != 0 {
				return c
			}
			return strings.Compare(x.event.ID, y.event.ID)
		})
		out := make([]eventJSON, len(items))
		for i, it := range items {
			out[i] = it.event
		}
		writeCollection(w, r, out)
	}
}

// expand returns the occurrences of one object that overlap the window.
func (a *Handler) expand(s storedEvent, zone string, win ical.Range) ([]viewItem, *apiError) {
	v, aerr := newEventView(s.cal, zone)
	if aerr != nil {
		return nil, aerr
	}
	master, overrides := eventComponents(s.cal)
	if master == nil {
		return nil, nil
	}
	occs, err := s.cal.Occurrences(v.res, nil, win)
	if err != nil {
		a.logger.Warn("skipped an event that cannot be expanded", "object", s.obj.Name, "error", err)
		return nil, nil
	}
	recurring := master.Prop("RRULE") != nil || master.Prop("RDATE") != nil
	var out []viewItem
	for _, occ := range occs {
		var e eventJSON
		switch {
		case !recurring && !occ.Override:
			e = s.withIDs(v.event(master))
		default:
			base := master
			if occ.Override {
				if o := overrideAt(v.res, overrides, occ.RecurrenceID); o != nil {
					base = o
				}
			}
			e = s.withIDs(v.occurrence(base, occ))
			e.ID = OccurrenceID(s.id(), occ.RecurrenceID)
			e.SeriesMasterID = s.id()
		}
		out = append(out, viewItem{event: e, start: occ.Start})
	}
	return out, nil
}
