// Package graphapi serves the calendar and contacts part of the Microsoft
// Graph API below /graph/v1.0. Requests carry an OIDC bearer token; the
// service's authentication middleware verifies it before they get here.
// Everything the facade does not implement answers 501 with a Graph error
// body, so a client can tell "not supported" from "not found".
package graphapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/provision"
)

// Prefix is the URL path the facade is mounted at, without a trailing slash.
const Prefix = "/graph/v1.0"

// Config holds what New needs.
type Config struct {
	// Store holds the calendars and address books.
	Store store.Store
	// Registry provisions the caller.
	Registry *principals.Registry
	// Identities signs the caller in and keeps its oCIS identity snapshot
	// current. Nil provisions the caller without the snapshot.
	Identities *provision.Identities
	// Calendars decides which calendars a user sees. Nil shows each user
	// their own calendars only.
	Calendars CalendarSource
	// Logger receives faults. Nil discards them.
	Logger *slog.Logger
}

// Option customizes the facade.
type Option func(*Handler)

// Handler is the facade.
type Handler struct {
	cfg       Config
	logger    *slog.Logger
	ids       *provision.Identities
	calendars CalendarSource
	routes    []route
}

// call is what a route handler works with: the signed-in caller and the
// values of the route's {name} segments, already unescaped.
type call struct {
	caller principals.Principal
	params map[string]string
}

type route struct {
	method   string
	segments []string
	handle   func(http.ResponseWriter, *http.Request, *call)
}

// withRoute adds a route. The pattern is a path below Prefix without slashes
// at either end; a segment like {id} matches any one segment.
func withRoute(method, pattern string, h func(http.ResponseWriter, *http.Request, *call)) Option {
	return func(a *Handler) {
		a.routes = append(a.routes, route{method: method, segments: strings.Split(pattern, "/"), handle: h})
	}
}

// New returns the facade handler, to mount at Prefix + "/".
func New(cfg Config, opts ...Option) (*Handler, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("graphapi: store is nil")
	case cfg.Registry == nil:
		return nil, errors.New("graphapi: registry is nil")
	}
	a := &Handler{cfg: cfg, logger: cfg.Logger}
	if a.logger == nil {
		a.logger = slog.New(slog.DiscardHandler)
	}
	a.ids = cfg.Identities
	if a.ids == nil {
		a.ids = provision.NewIdentities(cfg.Registry, nil, a.logger)
	}
	a.calendars = cfg.Calendars
	if a.calendars == nil {
		a.calendars = ownCalendars{st: cfg.Store}
	}
	a.routes = []route{
		{http.MethodGet, strings.Split("me/calendar", "/"), a.getDefaultCalendar},
		{http.MethodPatch, strings.Split("me/calendar", "/"), a.patchDefaultCalendar},
		{http.MethodGet, strings.Split("me/calendars", "/"), a.listCalendars},
		{http.MethodPost, strings.Split("me/calendars", "/"), a.createCalendar},
		{http.MethodGet, strings.Split("me/calendars/{id}", "/"), a.getCalendar},
		{http.MethodPatch, strings.Split("me/calendars/{id}", "/"), a.patchCalendar},
		{http.MethodDelete, strings.Split("me/calendars/{id}", "/"), a.deleteCalendar},
		{http.MethodGet, strings.Split("me/events", "/"), a.listEvents(true)},
		{http.MethodGet, strings.Split("me/events/{eid}", "/"), a.getEvent(false, false)},
		{http.MethodGet, strings.Split("me/calendar/events", "/"), a.listEvents(true)},
		{http.MethodGet, strings.Split("me/calendar/events/{eid}", "/"), a.getEvent(true, true)},
		{http.MethodGet, strings.Split("me/calendars/{id}/events", "/"), a.listEvents(false)},
		{http.MethodGet, strings.Split("me/calendars/{id}/events/{eid}", "/"), a.getEvent(false, true)},
		{http.MethodGet, strings.Split("me/calendarView", "/"), a.calendarView("all")},
		{http.MethodGet, strings.Split("me/calendar/calendarView", "/"), a.calendarView("default")},
		{http.MethodGet, strings.Split("me/calendars/{id}/calendarView", "/"), a.calendarView("calendar")},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// ServeHTTP routes the request. The route is found before the caller is
// provisioned, so a request that cannot succeed creates nothing.
func (a *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// The middleware checks the token; this guards against a mount without it.
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, newError(http.StatusUnauthorized, codeUnauthenticated, "unauthorized"))
		return
	}

	segs, ok := splitPath(r.URL.EscapedPath())
	if !ok {
		writeError(w, newError(http.StatusNotImplemented, codeNotImplemented, "this request is not supported"))
		return
	}
	var allowed []string
	for _, rt := range a.routes {
		params, ok := rt.match(segs)
		if !ok {
			continue
		}
		if rt.method != r.Method {
			allowed = append(allowed, rt.method)
			continue
		}
		token, _ := auth.TokenFromContext(r.Context())
		p, status, msg := a.ids.SignIn(r.Context(), claims, token, a.logger)
		if status != 0 {
			code := codeGeneral
			if status == http.StatusForbidden {
				code = codeAccessDenied
			}
			writeError(w, newError(status, code, msg))
			return
		}
		rt.handle(w, r, &call{caller: p, params: params})
		return
	}
	if len(allowed) > 0 {
		slices.Sort(allowed)
		e := newError(http.StatusMethodNotAllowed, codeNotAllowed, "method not allowed")
		e.headers = map[string]string{"Allow": strings.Join(slices.Compact(allowed), ", ")}
		writeError(w, e)
		return
	}
	writeError(w, newError(http.StatusNotImplemented, codeNotImplemented,
		"this part of the Graph API is not supported"))
}

// splitPath returns the unescaped segments of the path below Prefix.
func splitPath(escaped string) ([]string, bool) {
	rest, ok := strings.CutPrefix(escaped, Prefix)
	if !ok {
		return nil, false
	}
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return nil, false
	}
	parts := strings.Split(rest, "/")
	for i, p := range parts {
		s, err := url.PathUnescape(p)
		if err != nil || s == "" {
			return nil, false
		}
		parts[i] = s
	}
	return parts, true
}

func (rt route) match(segs []string) (map[string]string, bool) {
	if len(segs) != len(rt.segments) {
		return nil, false
	}
	var params map[string]string
	for i, want := range rt.segments {
		if len(want) > 2 && want[0] == '{' && want[len(want)-1] == '}' {
			if params == nil {
				params = map[string]string{}
			}
			params[want[1:len(want)-1]] = segs[i]
			continue
		}
		if want != segs[i] {
			return nil, false
		}
	}
	return params, true
}
