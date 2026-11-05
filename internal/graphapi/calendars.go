package graphapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

// CalendarEntry is one calendar a user sees: their own, or one another user
// shares with them.
type CalendarEntry struct {
	// Collection is the calendar. Its Ref names the real owner's collection,
	// and its Access is what the user may do with it.
	Collection store.Collection
	// OwnerName is the display name of the calendar's owner.
	OwnerName string
}

// CalendarSource decides which calendars a user sees. The default source
// lists the user's own calendars; a source that knows about sharing adds the
// calendars shared with the user.
type CalendarSource interface {
	// Calendars returns the calendars the user sees, in no particular order.
	Calendars(ctx context.Context, user principals.Principal) ([]CalendarEntry, error)
}

// ownCalendars is the CalendarSource that shows a user's own calendars only.
type ownCalendars struct{ st store.Store }

func (o ownCalendars) Calendars(ctx context.Context, user principals.Principal) ([]CalendarEntry, error) {
	cols, err := o.st.ListCollections(ctx, user.Owner, store.KindCalendar)
	if err != nil {
		return nil, err
	}
	out := make([]CalendarEntry, len(cols))
	for i, c := range cols {
		out[i] = CalendarEntry{Collection: c, OwnerName: ownerName(user)}
	}
	return out, nil
}

func ownerName(p principals.Principal) string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Username
}

// calendarJSON is a Graph calendar.
type calendarJSON struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	Color               string       `json:"color"`
	HexColor            string       `json:"hexColor"`
	IsDefaultCalendar   bool         `json:"isDefaultCalendar"`
	ChangeKey           string       `json:"changeKey"`
	CanShare            bool         `json:"canShare"`
	CanViewPrivateItems bool         `json:"canViewPrivateItems"`
	CanEdit             bool         `json:"canEdit"`
	IsRemovable         bool         `json:"isRemovable"`
	Owner               emailAddress `json:"owner"`
}

type emailAddress struct {
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
}

func toCalendar(e CalendarEntry, isDefault bool) calendarJSON {
	c := e.Collection
	name := c.Props.DisplayName
	if name == "" {
		name = c.Ref.Name
	}
	return calendarJSON{
		ID:                  CalendarID(c.ID),
		Name:                name,
		Color:               "auto",
		HexColor:            hexColor(c.Props.Color),
		IsDefaultCalendar:   isDefault,
		ChangeKey:           strconv.FormatUint(c.Revision, 10),
		CanShare:            c.Access.AllowsPropertyWrites(),
		CanViewPrivateItems: true,
		CanEdit:             c.Access.AllowsObjectWrites(),
		IsRemovable:         c.Access.AllowsPropertyWrites(),
		Owner:               emailAddress{Name: e.OwnerName},
	}
}

var (
	hexColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	// storedColorRE matches what DAV clients store: #RRGGBB or #RRGGBBAA.
	storedColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)
)

// hexColor returns the Graph hexColor of a stored colour: "#rrggbb", or ""
// when the stored text is not a colour of that form. The alpha channel of a
// DAV client's colour is dropped.
func hexColor(stored string) string {
	if !storedColorRE.MatchString(stored) {
		return ""
	}
	return strings.ToLower(stored[:7])
}

// myCalendars returns the user's calendars in display order, with the
// default calendar, the oldest one the user owns, flagged.
func (a *Handler) myCalendars(ctx context.Context, user principals.Principal) ([]calendarJSON, []CalendarEntry, int, error) {
	entries, err := a.calendars.Calendars(ctx, user)
	if err != nil {
		return nil, nil, -1, err
	}
	owned := func(e CalendarEntry) bool { return e.Collection.Ref.Owner == user.Owner }
	slices.SortFunc(entries, func(x, y CalendarEntry) int {
		switch {
		case owned(x) != owned(y):
			if owned(x) {
				return -1
			}
			return 1
		case !x.Collection.Created.Equal(y.Collection.Created):
			return x.Collection.Created.Compare(y.Collection.Created)
		}
		return strings.Compare(x.Collection.ID, y.Collection.ID)
	})
	def := -1
	if len(entries) > 0 && owned(entries[0]) {
		def = 0
	}
	out := make([]calendarJSON, len(entries))
	for i, e := range entries {
		out[i] = toCalendar(e, i == def)
	}
	return out, entries, def, nil
}

// findCalendar resolves a Graph calendar id to one of the user's calendars.
func (a *Handler) findCalendar(ctx context.Context, user principals.Principal, id string) (CalendarEntry, calendarJSON, *apiError) {
	collID, err := ParseCalendarID(id)
	if err != nil {
		return CalendarEntry{}, calendarJSON{}, badRequest("the calendar id is malformed")
	}
	list, entries, _, err := a.myCalendars(ctx, user)
	if err != nil {
		a.logger.Error("could not list calendars", "owner", user.Owner, "error", err)
		return CalendarEntry{}, calendarJSON{}, internalError()
	}
	for i, e := range entries {
		if e.Collection.ID == collID {
			return e, list[i], nil
		}
	}
	// The same answer for a calendar that does not exist and one that is not
	// shared with the user.
	return CalendarEntry{}, calendarJSON{}, notFound("the calendar does not exist")
}

func (a *Handler) listCalendars(w http.ResponseWriter, r *http.Request, c *call) {
	list, _, _, err := a.myCalendars(r.Context(), c.caller)
	if err != nil {
		a.logger.Error("could not list calendars", "owner", c.caller.Owner, "error", err)
		writeError(w, internalError())
		return
	}
	writeCollection(w, r, list)
}

func (a *Handler) getCalendar(w http.ResponseWriter, r *http.Request, c *call) {
	_, cal, aerr := a.findCalendar(r.Context(), c.caller, c.params["id"])
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	writeJSON(w, http.StatusOK, cal)
}

// defaultCalendar resolves /me/calendar.
func (a *Handler) defaultCalendar(ctx context.Context, user principals.Principal) (CalendarEntry, *apiError) {
	_, entries, def, err := a.myCalendars(ctx, user)
	if err != nil {
		a.logger.Error("could not list calendars", "owner", user.Owner, "error", err)
		return CalendarEntry{}, internalError()
	}
	if def < 0 {
		return CalendarEntry{}, notFound("the user has no default calendar")
	}
	return entries[def], nil
}

func (a *Handler) getDefaultCalendar(w http.ResponseWriter, r *http.Request, c *call) {
	e, aerr := a.defaultCalendar(r.Context(), c.caller)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	writeJSON(w, http.StatusOK, toCalendar(e, true))
}

// calendarChange is the body of a POST or PATCH. A nil field is absent.
type calendarChange struct {
	Name     *string `json:"name"`
	HexColor *string `json:"hexColor"`
}

// validate checks the fields and returns the update they stand for.
func (ch calendarChange) validate() (store.PropsUpdate, *apiError) {
	var u store.PropsUpdate
	if ch.Name != nil {
		if err := validCalendarName(*ch.Name); err != nil {
			return u, err
		}
		u.DisplayName = ch.Name
	}
	if ch.HexColor != nil {
		if *ch.HexColor != "" && !hexColorRE.MatchString(*ch.HexColor) {
			return u, badRequest("hexColor must be empty or look like #1a2b3c")
		}
		col := strings.ToLower(*ch.HexColor)
		u.Color = &col
	}
	return u, nil
}

func validCalendarName(name string) *apiError {
	switch {
	case strings.TrimSpace(name) == "":
		return badRequest("name must not be empty")
	case len(name) > 255 || !utf8.ValidString(name):
		return badRequest("name must be valid text of at most 255 bytes")
	case strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return badRequest("name must not contain control characters")
	}
	return nil
}

func (a *Handler) createCalendar(w http.ResponseWriter, r *http.Request, c *call) {
	var ch calendarChange
	if aerr := decodeJSON(w, r, &ch); aerr != nil {
		writeError(w, aerr)
		return
	}
	if ch.Name == nil {
		writeError(w, badRequest("name is required"))
		return
	}
	u, aerr := ch.validate()
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	props := store.Props{DisplayName: *u.DisplayName, Components: []string{"VEVENT"}}
	if u.Color != nil {
		props.Color = *u.Color
	}
	ref := store.CollectionRef{Owner: c.caller.Owner, Kind: store.KindCalendar, Name: newCollectionName()}
	col, err := a.cfg.Store.CreateCollection(r.Context(), ref, props)
	if err != nil {
		writeError(w, a.storeError("create a calendar", c.caller, err))
		return
	}
	writeJSON(w, http.StatusCreated, toCalendar(CalendarEntry{Collection: col, OwnerName: ownerName(c.caller)}, false))
}

// newCollectionName returns a name for a collection made through the facade.
// It shows in the DAV URL.
func newCollectionName() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "cal-" + hex.EncodeToString(b[:])
}

func (a *Handler) patchCalendar(w http.ResponseWriter, r *http.Request, c *call) {
	a.updateCalendar(w, r, c, c.params["id"])
}

func (a *Handler) patchDefaultCalendar(w http.ResponseWriter, r *http.Request, c *call) {
	e, aerr := a.defaultCalendar(r.Context(), c.caller)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	a.updateCalendar(w, r, c, CalendarID(e.Collection.ID))
}

// updateCalendar applies the body of a PATCH to the calendar with id.
func (a *Handler) updateCalendar(w http.ResponseWriter, r *http.Request, c *call, id string) {
	e, cal, aerr := a.findCalendar(r.Context(), c.caller, id)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	if !e.Collection.Access.AllowsPropertyWrites() {
		writeError(w, forbidden("only the owner can change a calendar"))
		return
	}
	var ch calendarChange
	if aerr := decodeJSON(w, r, &ch); aerr != nil {
		writeError(w, aerr)
		return
	}
	u, aerr := ch.validate()
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	if u.DisplayName != nil || u.Color != nil {
		col, err := a.cfg.Store.UpdateCollection(r.Context(), e.Collection.Ref, u)
		if err != nil {
			writeError(w, a.storeError("update a calendar", c.caller, err))
			return
		}
		e.Collection = col
		cal = toCalendar(e, cal.IsDefaultCalendar)
	}
	writeJSON(w, http.StatusOK, cal)
}

func (a *Handler) deleteCalendar(w http.ResponseWriter, r *http.Request, c *call) {
	e, _, aerr := a.findCalendar(r.Context(), c.caller, c.params["id"])
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	if !e.Collection.Access.AllowsPropertyWrites() {
		writeError(w, forbidden("only the owner can delete a calendar"))
		return
	}
	if err := a.cfg.Store.DeleteCollection(r.Context(), e.Collection.Ref); err != nil {
		writeError(w, a.storeError("delete a calendar", c.caller, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// storeError maps an error of the store to the answer. Faults are logged and
// never shown.
func (a *Handler) storeError(action string, user principals.Principal, err error) *apiError {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return notFound("the item does not exist")
	case errors.Is(err, store.ErrPermission):
		return forbidden("the user may not do this")
	case errors.Is(err, store.ErrPreconditionFailed):
		return preconditionFailed()
	case errors.Is(err, store.ErrExists), errors.Is(err, store.ErrUIDConflict):
		return newError(http.StatusConflict, codeNameExists, "the item already exists")
	case errors.Is(err, store.ErrInvalidName):
		return badRequest("the name is not valid")
	}
	a.logger.Error("could not "+action, "owner", user.Owner, "error", err)
	return internalError()
}
