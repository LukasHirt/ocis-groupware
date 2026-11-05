package graphapi

import (
	"net/http"
	"net/url"
	"time"
)

// accountJSON is the answer of GET /groupware/v1/me.
type accountJSON struct {
	Username    string  `json:"username"`
	DisplayName string  `json:"displayName"`
	DAV         davJSON `json:"dav"`
	// UsernameConflict is true when the user's oCIS username is held by
	// another account that can still use it. The username is then empty, and
	// DAV and app passwords do not work until an administrator frees the name.
	UsernameConflict bool `json:"username_conflict"`
	// AppPasswordsValidUntil is when the user's app passwords stop working
	// unless oCIS confirms the user again: the time of the last
	// confirmation plus the identity lease. It is null when the lease is
	// off, or when oCIS has not confirmed the user yet.
	AppPasswordsValidUntil *time.Time `json:"app_passwords_valid_until"`
}

// davJSON holds the addresses a CalDAV or CardDAV client needs.
type davJSON struct {
	// URL is the root of the DAV endpoint, the address to enter in a client
	// that discovers the rest itself.
	URL string `json:"url"`
	// CalendarHomeURL and AddressBookHomeURL hold the user's calendars and
	// address books. They are empty when the user has no username.
	CalendarHomeURL    string `json:"calendarHomeUrl"`
	AddressBookHomeURL string `json:"addressBookHomeUrl"`
}

func (a *Handler) accountInfo(w http.ResponseWriter, r *http.Request, c *call) {
	writeJSON(w, http.StatusOK, accountJSON{
		Username:               c.caller.Username,
		DisplayName:            c.caller.DisplayName,
		DAV:                    davAddresses(requestOrigin(r), c.caller.Username),
		UsernameConflict:       c.caller.UsernameConflict,
		AppPasswordsValidUntil: a.appPasswordsValidUntil(c.caller.Owner),
	})
}

// appPasswordsValidUntil returns the end of the owner's identity lease, or nil.
// The snapshot is read again here, because signing in has just renewed it.
func (a *Handler) appPasswordsValidUntil(owner string) *time.Time {
	maxAge := a.cfg.Registry.IdentityMaxAge()
	if maxAge <= 0 {
		return nil
	}
	id, err := a.cfg.Registry.Identity(owner)
	if err != nil || id.At.IsZero() {
		return nil
	}
	end := id.At.Add(maxAge).UTC()
	return &end
}

// davAddresses returns the DAV addresses of a user below origin, which is
// "scheme://host" or empty for addresses without a host.
func davAddresses(origin, username string) davJSON {
	out := davJSON{URL: origin + "/dav/"}
	if username != "" {
		name := url.PathEscape(username)
		out.CalendarHomeURL = origin + "/dav/calendars/" + name + "/"
		out.AddressBookHomeURL = origin + "/dav/addressbooks/" + name + "/"
	}
	return out
}

// requestOrigin is the scheme and host the client used, or "" when the request
// has no host.
func requestOrigin(r *http.Request) string {
	if r.Host == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
