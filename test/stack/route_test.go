//go:build stack

package stack

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

// The ways a request can reach the service, chosen with STACK_ROUTE.
const (
	// routeIngress: the ingress in front of oCIS sends the groupware paths
	// straight to the service. This is docker-compose.yml as it is.
	routeIngress = "ingress"
	// routeOCISProxy: the ingress only knows oCIS, and the proxy inside
	// oCIS forwards the groupware paths with deploy/ocis/proxy.yaml. This
	// is docker-compose.yml with deploy/compose/ocis-proxy.yml on top.
	routeOCISProxy = "ocis-proxy"
)

// needsReport skips a test on the oCIS proxy route, where REPORT requests
// never reach the service. TestOCISProxyRoute keeps track of that.
func needsReport(t *testing.T) {
	t.Helper()
	if route == routeOCISProxy {
		t.Skip("REPORT does not reach the service through the oCIS proxy; see TestOCISProxyRoute")
	}
}

// answeredByService tells from a 401 which side produced it: the service
// names itself in the challenge, oCIS uses the host.
func answeredByService(h http.Header) bool {
	return strings.Contains(h.Get("WWW-Authenticate"), `realm="ocis-groupware"`)
}

// TestOCISProxyRoute records what the oCIS proxy route can do, as of oCIS
// 8.2.2. The proxy tries regex routes before prefix routes, so regex routes
// from additional_policies get to the groupware paths ahead of oCIS's own
// /dav/ and /graph/ prefixes, and credentials pass through an unprotected
// route untouched. REPORT is the exception: oCIS sends every REPORT whose
// path contains /dav to its search service, and nothing in
// additional_policies can come before that route. Without REPORT there is no
// calendar-query, addressbook-query, multiget or sync-collection, so CalDAV
// and CardDAV clients cannot work on this route.
//
// If the REPORT check fails one day, the proxy has changed: update the
// README's stack tests section and drop needsReport.
func TestOCISProxyRoute(t *testing.T) {
	if route != routeOCISProxy {
		t.Skipf("STACK_ROUTE is %q, not %q", route, routeOCISProxy)
	}
	ctx := ctxTimeout(t)

	// Without credentials every path answers 401; the challenge says who.
	for _, tc := range []struct {
		method, path string
		service      bool
	}{
		{"PROPFIND", "/dav", true},
		{"PROPFIND", "/dav/", true},
		{"PROPFIND", "/dav/calendars/" + goUser + "/", true},
		{"PROPFIND", "/dav/addressbooks/" + goUser + "/", true},
		{"PROPFIND", goPrincipal, true},
		{http.MethodGet, "/graph/v1.0/me/calendars", true},
		{http.MethodGet, "/graph/v1.0/me/events", true},
		{http.MethodGet, "/graph/v1.0/me/contacts", true},
		{http.MethodGet, "/groupware/v1/app-passwords", true},
		{"PROPFIND", "/dav/spaces/", false},
		{"PROPFIND", "/dav/files/" + goUser + "/", false},
		{http.MethodGet, "/graph/v1.0/me", false},
		{http.MethodGet, "/graph/v1.0/users", false},
	} {
		status, headers, _ := do(t, ctx, tc.method, tc.path, map[string]string{"Depth": "0"}, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", tc.method, tc.path, status)
			continue
		}
		if got := answeredByService(headers); got != tc.service {
			t.Errorf("%s %s answered by the service = %v, want %v (WWW-Authenticate %q)", tc.method, tc.path, got, tc.service, headers.Get("WWW-Authenticate"))
		}
	}

	// With an app password, everything but REPORT works: the token API, the
	// discovery PROPFINDs, MKCALENDAR, PUT, GET and DELETE.
	a := signInWithAppPassword(t, goUser, goPassword)
	c := calClient(t, a.basic())
	home := "/dav/calendars/" + goUser + "/"
	cal := mkcalendar(t, ctx, a, "Through the proxy")
	calendars, err := c.FindCalendars(ctx, home)
	if err != nil {
		t.Fatalf("FindCalendars: %v", err)
	}
	if !slices.ContainsFunc(calendars, func(c caldav.Calendar) bool { return c.Path == cal }) {
		t.Fatalf("the new calendar %s is not listed: %+v", cal, calendars)
	}
	path := cal + "proxied.ics"
	written, err := c.PutCalendarObject(ctx, path, decodeCal(t, utcEvent("proxied", "20260302T090000Z", "Proxied")))
	if err != nil {
		t.Fatalf("PutCalendarObject: %v", err)
	}
	read, err := c.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}
	if read.ETag != written.ETag || read.Data.Events()[0].Props.Get(ical.PropSummary).Value != "Proxied" {
		t.Errorf("read back etag %q, want %q", read.ETag, written.ETag)
	}

	body := `<?xml version="1.0"?><D:sync-collection xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` +
		`<D:sync-token/><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`
	headers := basicHeader(a.user, a.appPassword)
	headers["Content-Type"] = "application/xml"
	status, h, resp := do(t, ctx, "REPORT", cal, headers, strings.NewReader(body))
	if status == http.StatusMultiStatus || answeredByService(h) {
		t.Fatalf("REPORT %s reached the service (status %d): the oCIS proxy route has changed, update the README and drop needsReport\n%s", cal, status, resp)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("REPORT %s = %d (WWW-Authenticate %q), want oCIS's 401", cal, status, h.Get("WWW-Authenticate"))
	}

	if err := c.RemoveAll(ctx, path); err != nil {
		t.Fatalf("RemoveAll %s: %v", path, err)
	}
	if _, err := c.GetCalendarObject(ctx, path); err == nil {
		t.Errorf("GetCalendarObject %s after DELETE: no error", path)
	}
}
