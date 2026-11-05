package graphapi

import (
	"net/http"
	"net/url"
)

// accountJSON is the answer of GET /groupware/v1/me.
type accountJSON struct {
	Username    string  `json:"username"`
	DisplayName string  `json:"displayName"`
	DAV         davJSON `json:"dav"`
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
		Username:    c.caller.Username,
		DisplayName: c.caller.DisplayName,
		DAV:         davAddresses(requestOrigin(r), c.caller.Username),
	})
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
