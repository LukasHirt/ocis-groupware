package graphapi

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/ical"
	"github.com/LukasHirt/groupkit/store"
)

// codePrecondition is the Graph error code for a failed If-Match.
const codePrecondition = "preconditionFailed"

// matchETag reports whether an If-Match header accepts the object's ETag.
// Weak and quoted forms are the same tag, and * accepts any.
func matchETag(header, etag string) bool {
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" {
			return true
		}
		tag = strings.Trim(strings.TrimPrefix(tag, "W/"), `"`)
		if tag != "" && tag == etag {
			return true
		}
	}
	return false
}

func preconditionFailed() *apiError {
	return newError(http.StatusPreconditionFailed, codePrecondition, "the event changed since it was read")
}

// checkPrecondition answers the If-Match header, if the request has one.
func checkPrecondition(r *http.Request, s storedEvent) *apiError {
	if h := r.Header.Get("If-Match"); h != "" && !matchETag(h, s.obj.ETag) {
		return preconditionFailed()
	}
	return nil
}

// saveEvent writes the object back, refusing to overwrite a version the request
// did not see. It checks the data first, as the DAV engine does.
func (a *Handler) saveEvent(r *http.Request, s *storedEvent, c *call) *apiError {
	data := s.cal.Encode()
	if _, err := ical.Validate(data, []string{"VEVENT"}); err != nil {
		return badRequest("the resulting event is not valid")
	}
	obj, err := a.cfg.Store.PutObject(r.Context(), s.entry.Collection.Ref, s.obj.Name, data, store.PutOptions{IfMatch: s.obj.ETag})
	if err != nil {
		return a.storeError("change an event", c.caller, err)
	}
	s.obj = obj
	return nil
}

// writableEvent resolves the event of a PATCH or DELETE and checks that the
// caller may write to its calendar and that If-Match holds.
func (a *Handler) writableEvent(w http.ResponseWriter, r *http.Request, c *call, def, scoped bool) (storedEvent, time.Time, bool) {
	if aerr := unsupportedSegment(c.params["eid"]); aerr != nil {
		writeError(w, aerr)
		return storedEvent{}, time.Time{}, false
	}
	var in *CalendarEntry
	if scoped {
		entry, aerr := a.scope(r.Context(), c, def)
		if aerr != nil {
			writeError(w, aerr)
			return storedEvent{}, time.Time{}, false
		}
		in = &entry
	}
	s, rid, aerr := a.findEvent(r.Context(), c.caller, c.params["eid"], in)
	if aerr == nil && !s.entry.Collection.Access.AllowsObjectWrites() {
		aerr = forbidden("the calendar is read-only for the user")
	}
	if aerr == nil {
		aerr = checkPrecondition(r, s)
	}
	if aerr != nil {
		writeError(w, aerr)
		return storedEvent{}, time.Time{}, false
	}
	return s, rid, true
}

func (a *Handler) createEvent(def bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		zone, aerr := viewZone(w, r)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		entry, aerr := a.scope(r.Context(), c, def || c.params["id"] == "")
		if aerr == nil && !entry.Collection.Access.AllowsObjectWrites() {
			aerr = forbidden("the calendar is read-only for the user")
		}
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		var p eventPatch
		if aerr := decodeJSON(w, r, &p); aerr != nil {
			writeError(w, aerr)
			return
		}
		uid := requestID()
		cal, aerr := newEventCalendar(p, uid, time.Now())
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		data := cal.Encode()
		if _, err := ical.Validate(data, []string{"VEVENT"}); err != nil {
			writeError(w, badRequest("the event is not valid"))
			return
		}
		obj, err := a.cfg.Store.PutObject(r.Context(), entry.Collection.Ref, uid+".ics", data, store.PutOptions{IfNoneMatch: true, UID: uid})
		if err != nil {
			writeError(w, a.storeError("create an event", c.caller, err))
			return
		}
		s := storedEvent{entry: entry, obj: obj, cal: cal}
		v, aerr := newEventView(cal, zone)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		e, _ := s.seriesView(v)
		writeJSON(w, http.StatusCreated, e)
	}
}

func (a *Handler) patchEvent(def, scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		zone, aerr := viewZone(w, r)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		s, rid, ok := a.writableEvent(w, r, c, def, scoped)
		if !ok {
			return
		}
		var p eventPatch
		if aerr := decodeJSON(w, r, &p); aerr != nil {
			writeError(w, aerr)
			return
		}
		master, _ := eventComponents(s.cal)
		if master == nil {
			writeError(w, notFound("the event does not exist"))
			return
		}
		target := master
		if !rid.IsZero() {
			v, aerr := newEventView(s.cal, "UTC")
			if aerr != nil {
				writeError(w, aerr)
				return
			}
			base, occ, found := s.findOccurrence(v, rid)
			if !found {
				writeError(w, notFound("the occurrence does not exist"))
				return
			}
			target = base
			if base == master {
				ov, aerr := newOverride(v.res, master, occ)
				if aerr != nil {
					writeError(w, aerr)
					return
				}
				s.cal.Children = append(s.cal.Children, ov)
				target = ov
			}
		}
		if aerr := applyEventPatch(s.cal, target, p, time.Now()); aerr != nil {
			writeError(w, aerr)
			return
		}
		if aerr := a.saveEvent(r, &s, c); aerr != nil {
			writeError(w, aerr)
			return
		}
		v, aerr := newEventView(s.cal, zone)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		var (
			e   eventJSON
			ok2 bool
		)
		if rid.IsZero() {
			e, ok2 = s.seriesView(v)
		} else {
			e, ok2 = s.occurrenceAt(v, rid)
		}
		if !ok2 {
			a.logger.Error("a changed event cannot be read back", "sub", c.caller.Sub, "object", s.obj.Name)
			writeError(w, internalError())
			return
		}
		writeJSON(w, http.StatusOK, e)
	}
}

func (a *Handler) deleteEvent(def, scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		s, rid, ok := a.writableEvent(w, r, c, def, scoped)
		if !ok {
			return
		}
		if rid.IsZero() {
			err := a.cfg.Store.DeleteObject(r.Context(), s.entry.Collection.Ref, s.obj.Name, store.DeleteOptions{IfMatch: s.obj.ETag})
			if err != nil {
				writeError(w, a.storeError("delete an event", c.caller, err))
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		v, aerr := newEventView(s.cal, "UTC")
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		if _, _, found := s.findOccurrence(v, rid); !found {
			writeError(w, notFound("the occurrence does not exist"))
			return
		}
		master, _ := eventComponents(s.cal)
		start, allDay, haveStart := (eventWriter{res: v.res, comp: master}).current()
		if !haveStart {
			writeError(w, badRequest("the series has no valid start"))
			return
		}
		// The override of the occurrence goes, and the date is excluded.
		s.cal.Children = slices.DeleteFunc(s.cal.Children, func(k *ical.Component) bool {
			return k.Name == "VEVENT" && k != master && overrideAt(v.res, []*ical.Component{k}, rid) != nil
		})
		ex := timeProp(v.res, "EXDATE", inZone(v.res, rid, start), allDay, true)
		master.Props = append(master.Props, ex)
		now := time.Now().UTC().Format("20060102T150405Z")
		setProp(master, ical.Property{Name: "DTSTAMP", Value: now})
		setProp(master, ical.Property{Name: "LAST-MODIFIED", Value: now})
		if aerr := a.saveEvent(r, &s, c); aerr != nil {
			writeError(w, aerr)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// newOverride makes the override that stands for one occurrence of a series:
// a copy of the master without its rules, marked with the occurrence's
// RECURRENCE-ID and placed at the occurrence's own time.
func newOverride(res *ical.Resolver, master *ical.Component, occ ical.Occurrence) (*ical.Component, *apiError) {
	start, allDay, ok := (eventWriter{res: res, comp: master}).current()
	if !ok {
		return nil, badRequest("the series has no valid start")
	}
	ov := cloneComponent(master)
	removeProps(ov, "RRULE", "RDATE", "EXDATE", "EXRULE", "RECURRENCE-ID", "DURATION")
	ov.Props = replaceProp(ov.Props, timeProp(res, "DTSTART", inZone(res, occ.Start, start), allDay, true))
	ov.Props = replaceProp(ov.Props, timeProp(res, "DTEND", inZone(res, occ.End, start), allDay, true))
	ov.Props = append(ov.Props, timeProp(res, "RECURRENCE-ID", inZone(res, occ.RecurrenceID, start), allDay, true))
	return ov, nil
}

func cloneComponent(c *ical.Component) *ical.Component {
	out := &ical.Component{Name: c.Name, Props: make([]ical.Property, len(c.Props))}
	for i, p := range c.Props {
		out.Props[i] = ical.Property{Name: p.Name, Params: cloneParams(p.Params), Value: p.Value}
	}
	for _, k := range c.Children {
		out.Children = append(out.Children, cloneComponent(k))
	}
	return out
}
