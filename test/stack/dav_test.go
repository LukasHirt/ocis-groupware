//go:build stack

package stack

import (
	"context"
	"encoding/xml"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"
)

const (
	davPrefix   = "/dav/"
	goPrincipal = "/dav/principals/" + goUser + "/"
)

// account is a signed-in demo user: a bearer token from the identity
// provider and an app password created with it.
type account struct {
	user, token, appPassword string
}

func signInWithAppPassword(t *testing.T, user, password string) account {
	t.Helper()
	token := signIn(t, user, password)
	return account{user: user, token: token, appPassword: appPassword(t, token, t.Name())}
}

func (a account) basic() webdav.HTTPClient {
	return webdav.HTTPClientWithBasicAuth(newHTTPClient(), a.user, a.appPassword)
}

func (a account) bearer() webdav.HTTPClient {
	return bearerClient{client: newHTTPClient(), token: a.token}
}

func cardClient(t *testing.T, hc webdav.HTTPClient) *carddav.Client {
	t.Helper()
	c, err := carddav.NewClient(hc, baseURL+davPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func calClient(t *testing.T, hc webdav.HTTPClient) *caldav.Client {
	t.Helper()
	c, err := caldav.NewClient(hc, baseURL+davPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestWellKnown checks discovery as in RFC 6764: the well-known paths answer
// without credentials and point at the DAV root, where the client finds its
// principal with Basic auth.
func TestWellKnown(t *testing.T) {
	ctx := ctxTimeout(t)
	for _, p := range []string{"/.well-known/caldav", "/.well-known/carddav"} {
		status, headers, _ := do(t, ctx, http.MethodGet, p, nil, nil)
		if status != http.StatusMovedPermanently || headers.Get("Location") != davPrefix {
			t.Errorf("GET %s = %d, Location %q; want 301 to %s", p, status, headers.Get("Location"), davPrefix)
		}
		status, headers, _ = do(t, ctx, "PROPFIND", p, map[string]string{"Depth": "0"}, nil)
		if status != http.StatusMovedPermanently || headers.Get("Location") != davPrefix {
			t.Errorf("PROPFIND %s = %d, Location %q; want 301 to %s", p, status, headers.Get("Location"), davPrefix)
		}
	}

	a := signInWithAppPassword(t, goUser, goPassword)
	principal, err := cardClient(t, a.basic()).FindCurrentUserPrincipal(ctx)
	if err != nil {
		t.Fatalf("FindCurrentUserPrincipal: %v", err)
	}
	if principal != goPrincipal {
		t.Errorf("principal = %q, want %q", principal, goPrincipal)
	}
}

// TestUnauthenticated checks that nothing under /dav or the app password
// API answers without valid credentials.
func TestUnauthenticated(t *testing.T) {
	ctx := ctxTimeout(t)
	a := signInWithAppPassword(t, goUser, goPassword)
	for _, tc := range []struct {
		name, method, path string
		headers            map[string]string
	}{
		{"no credentials", "PROPFIND", davPrefix, map[string]string{"Depth": "0"}},
		{"wrong app password", "PROPFIND", davPrefix, basicHeader(goUser, "not-the-password")},
		{"unknown user", "PROPFIND", goPrincipal, basicHeader("nobody", a.appPassword)},
		{"bogus bearer token", "PROPFIND", davPrefix, map[string]string{"Authorization": "Bearer not.a.token"}},
		{"app password on the API", http.MethodGet, "/groupware/v1/app-passwords", basicHeader(goUser, a.appPassword)},
		{"no token on the API", http.MethodGet, "/groupware/v1/app-passwords", nil},
	} {
		status, headers, _ := do(t, ctx, tc.method, tc.path, tc.headers, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("%s: %s %s = %d, want 401", tc.name, tc.method, tc.path, status)
		}
		if strings.HasPrefix(tc.path, davPrefix) && !strings.HasPrefix(headers.Get("WWW-Authenticate"), "Basic ") {
			t.Errorf("%s: WWW-Authenticate = %q, want a Basic challenge", tc.name, headers.Get("WWW-Authenticate"))
		}
	}
}

// TestBearerEqualsBasic checks that a bearer token and an app password reach
// the same account: the same principal, the same home sets and the same data.
func TestBearerEqualsBasic(t *testing.T) {
	ctx := ctxTimeout(t)
	a := signInWithAppPassword(t, goUser, goPassword)
	bearer, basic := cardClient(t, a.bearer()), cardClient(t, a.basic())

	for name, c := range map[string]*carddav.Client{"bearer": bearer, "basic": basic} {
		principal, err := c.FindCurrentUserPrincipal(ctx)
		if err != nil {
			t.Fatalf("%s: FindCurrentUserPrincipal: %v", name, err)
		}
		if principal != goPrincipal {
			t.Errorf("%s: principal = %q, want %q", name, principal, goPrincipal)
		}
		home, err := c.FindAddressBookHomeSet(ctx, principal)
		if err != nil {
			t.Fatalf("%s: FindAddressBookHomeSet: %v", name, err)
		}
		if want := "/dav/addressbooks/" + goUser + "/"; home != want {
			t.Errorf("%s: home = %q, want %q", name, home, want)
		}
	}

	book := newAddressBook(t, ctx, a, basic)
	path := book + "same.vcf"
	card := vcard.Card{}
	card.SetValue(vcard.FieldVersion, "4.0")
	card.SetValue(vcard.FieldUID, "same-account")
	card.SetValue(vcard.FieldFormattedName, "Written with a token")
	written, err := bearer.PutAddressObject(ctx, path, card)
	if err != nil {
		t.Fatalf("PutAddressObject with the bearer token: %v", err)
	}
	read, err := basic.GetAddressObject(ctx, path)
	if err != nil {
		t.Fatalf("GetAddressObject with the app password: %v", err)
	}
	if read.ETag != written.ETag || read.Card.PreferredValue(vcard.FieldFormattedName) != "Written with a token" {
		t.Errorf("read back etag %q name %q, want etag %q", read.ETag, read.Card.PreferredValue(vcard.FieldFormattedName), written.ETag)
	}
}

// newAddressBook creates an address book with a unique name in the user's
// home and deletes it when the test ends.
func newAddressBook(t *testing.T, ctx context.Context, a account, c *carddav.Client) string {
	t.Helper()
	path := "/dav/addressbooks/" + goUser + "/stack-" + randomString(t)[:8] + "/"
	if err := c.Mkdir(ctx, path); err != nil {
		t.Fatalf("Mkdir %s: %v", path, err)
	}
	t.Cleanup(func() { deleteCollection(t, a, path) })
	return path
}

// deleteCollection removes a collection a test created. A test may have
// deleted it already, so 404 is fine.
func deleteCollection(t *testing.T, a account, path string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if status, _, resp := do(t, ctx, http.MethodDelete, path, basicHeader(a.user, a.appPassword), nil); status != http.StatusNoContent && status != http.StatusNotFound {
		t.Logf("DELETE %s = %d: %s", path, status, resp)
	}
}

func testCard(uid, name string) vcard.Card {
	c := vcard.Card{}
	c.SetValue(vcard.FieldVersion, "4.0")
	c.SetValue(vcard.FieldUID, uid)
	c.SetValue(vcard.FieldFormattedName, name)
	c.SetName(&vcard.Name{FamilyName: name})
	return c
}

func objPaths(objs []carddav.AddressObject) []string {
	out := []string{}
	for _, o := range objs {
		out = append(out, o.Path)
	}
	slices.Sort(out)
	return out
}

// TestCardDAV runs a CardDAV client through discovery, an address book's
// life cycle, contact CRUD, a query and sync-collection.
func TestCardDAV(t *testing.T) {
	ctx := ctxTimeout(t)
	a := signInWithAppPassword(t, goUser, goPassword)
	c := cardClient(t, a.basic())

	home := "/dav/addressbooks/" + goUser + "/"
	books, err := c.FindAddressBooks(ctx, home)
	if err != nil {
		t.Fatalf("FindAddressBooks: %v", err)
	}
	if !slices.ContainsFunc(books, func(b carddav.AddressBook) bool { return b.Name == "Contacts" }) {
		t.Errorf("the default address book is missing: %+v", books)
	}

	book := newAddressBook(t, ctx, a, c)
	books, err = c.FindAddressBooks(ctx, home)
	if err != nil {
		t.Fatalf("FindAddressBooks: %v", err)
	}
	if !slices.ContainsFunc(books, func(b carddav.AddressBook) bool { return b.Path == book }) {
		t.Fatalf("the new address book %s is not listed: %+v", book, books)
	}

	cards := map[string]vcard.Card{
		book + "ada.vcf":   testCard("uid-ada", "Ada Lovelace"),
		book + "grace.vcf": testCard("uid-grace", "Grace Hopper"),
		book + "alan.vcf":  testCard("uid-alan", "Alan Turing"),
	}
	etags := map[string]string{}
	for p, card := range cards {
		ao, err := c.PutAddressObject(ctx, p, card)
		if err != nil {
			t.Fatalf("PutAddressObject %s: %v", p, err)
		}
		if ao.ETag == "" {
			t.Errorf("PutAddressObject %s: no ETag", p)
		}
		etags[p] = ao.ETag
	}

	ao, err := c.GetAddressObject(ctx, book+"ada.vcf")
	if err != nil {
		t.Fatalf("GetAddressObject: %v", err)
	}
	if ao.Card.PreferredValue(vcard.FieldFormattedName) != "Ada Lovelace" || ao.ETag != etags[book+"ada.vcf"] {
		t.Errorf("GetAddressObject = %q etag %q, want etag %q", ao.Card.PreferredValue(vcard.FieldFormattedName), ao.ETag, etags[book+"ada.vcf"])
	}

	found, err := c.QueryAddressBook(ctx, book, &carddav.AddressBookQuery{
		DataRequest: carddav.AddressDataRequest{AllProp: true},
		PropFilters: []carddav.PropFilter{{
			Name:        vcard.FieldFormattedName,
			TextMatches: []carddav.TextMatch{{Text: "hopper"}},
		}},
	})
	if err != nil {
		t.Fatalf("QueryAddressBook: %v", err)
	}
	if got := objPaths(found); !slices.Equal(got, []string{book + "grace.vcf"}) {
		t.Errorf("QueryAddressBook = %v, want grace.vcf", got)
	}

	first, err := c.SyncCollection(ctx, book, &carddav.SyncQuery{DataRequest: carddav.AddressDataRequest{AllProp: true}})
	if err != nil {
		t.Fatalf("initial SyncCollection: %v", err)
	}
	if got := objPaths(first.Updated); !slices.Equal(got, []string{book + "ada.vcf", book + "alan.vcf", book + "grace.vcf"}) || len(first.Deleted) != 0 || first.SyncToken == "" {
		t.Fatalf("initial sync = %v deleted %v token %q", got, first.Deleted, first.SyncToken)
	}

	edited := testCard("uid-ada", "Ada King")
	if _, err := c.PutAddressObject(ctx, book+"ada.vcf", edited); err != nil {
		t.Fatalf("PutAddressObject (edit): %v", err)
	}
	if err := c.RemoveAll(ctx, book+"alan.vcf"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	next, err := c.SyncCollection(ctx, book, &carddav.SyncQuery{DataRequest: carddav.AddressDataRequest{AllProp: true}, SyncToken: first.SyncToken})
	if err != nil {
		t.Fatalf("delta SyncCollection: %v", err)
	}
	if got := objPaths(next.Updated); !slices.Equal(got, []string{book + "ada.vcf"}) || !slices.Equal(next.Deleted, []string{book + "alan.vcf"}) || next.SyncToken == first.SyncToken {
		t.Errorf("delta sync = %v deleted %v token %q (first %q)", got, next.Deleted, next.SyncToken, first.SyncToken)
	}
	if len(next.Updated) == 1 && (next.Updated[0].ETag == "" || next.Updated[0].ETag == etags[book+"ada.vcf"]) {
		t.Errorf("delta sync etag = %q, want a new one (was %q)", next.Updated[0].ETag, etags[book+"ada.vcf"])
	}

	if err := c.RemoveAll(ctx, book); err != nil {
		t.Fatalf("RemoveAll %s: %v", book, err)
	}
	books, err = c.FindAddressBooks(ctx, home)
	if err != nil {
		t.Fatalf("FindAddressBooks: %v", err)
	}
	if slices.ContainsFunc(books, func(b carddav.AddressBook) bool { return b.Path == book }) {
		t.Errorf("the deleted address book %s is still listed", book)
	}
}

// mkcalendar creates a calendar with MKCALENDAR, which the go-webdav client
// does not offer, and deletes it when the test ends.
func mkcalendar(t *testing.T, ctx context.Context, a account, name string) string {
	t.Helper()
	path := "/dav/calendars/" + goUser + "/stack-" + randomString(t)[:8] + "/"
	body := `<?xml version="1.0"?><C:mkcalendar xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` +
		`<D:set><D:prop><D:displayname>` + name + `</D:displayname>` +
		`<C:supported-calendar-component-set><C:comp name="VEVENT"/></C:supported-calendar-component-set>` +
		`</D:prop></D:set></C:mkcalendar>`
	headers := basicHeader(a.user, a.appPassword)
	headers["Content-Type"] = "application/xml"
	status, _, resp := do(t, ctx, "MKCALENDAR", path, headers, strings.NewReader(body))
	if status != http.StatusCreated {
		t.Fatalf("MKCALENDAR %s = %d: %s", path, status, resp)
	}
	t.Cleanup(func() { deleteCollection(t, a, path) })
	return path
}

func decodeCal(t *testing.T, s string) *ical.Calendar {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(s)).Decode()
	if err != nil {
		t.Fatalf("decoding test calendar: %v\n%s", err, s)
	}
	return cal
}

// utcEvent returns a one-hour event in UTC.
func utcEvent(uid, start, summary string) string {
	s, err := time.Parse("20060102T150405Z", start)
	if err != nil {
		panic(err)
	}
	end := s.Add(time.Hour).Format("20060102T150405Z")
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//stack test//EN\r\nBEGIN:VEVENT\r\nUID:" + uid +
		"\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:" + start + "\r\nDTEND:" + end +
		"\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

// weeklyEvent recurs on four Mondays from 7 December 2026.
const weeklyEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//stack test//EN\r\nBEGIN:VEVENT\r\n" +
	"UID:weekly-1\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20261207T090000Z\r\nDTEND:20261207T100000Z\r\n" +
	"RRULE:FREQ=WEEKLY;COUNT=4\r\nSUMMARY:Weekly\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func timeQuery(start, end time.Time) *caldav.CalendarQuery {
	return &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{Name: "VCALENDAR", AllProps: true, AllComps: true},
		CompFilter: caldav.CompFilter{Name: "VCALENDAR", Comps: []caldav.CompFilter{{
			Name: "VEVENT", Start: start.UTC(), End: end.UTC(),
		}}},
	}
}

func calObjPaths(objs []caldav.CalendarObject) []string {
	out := []string{}
	for _, o := range objs {
		out = append(out, o.Path)
	}
	slices.Sort(out)
	return out
}

func eventSummary(t *testing.T, cal *ical.Calendar) string {
	t.Helper()
	events := cal.Events()
	if len(events) == 0 {
		t.Fatal("calendar has no VEVENT")
	}
	s, err := events[0].Props.Text(ical.PropSummary)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type syncResult struct {
	updated, deleted []string
	token            string
}

// calSync sends a sync-collection REPORT, which the go-webdav CalDAV client
// does not offer, and returns the changed and removed hrefs.
func calSync(t *testing.T, ctx context.Context, a account, href, token string) syncResult {
	t.Helper()
	body := `<?xml version="1.0"?><D:sync-collection xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` +
		`<D:sync-token>` + token + `</D:sync-token><D:sync-level>1</D:sync-level>` +
		`<D:prop><D:getetag/><C:calendar-data/></D:prop></D:sync-collection>`
	headers := basicHeader(a.user, a.appPassword)
	headers["Content-Type"] = "application/xml"
	status, _, resp := do(t, ctx, "REPORT", href, headers, strings.NewReader(body))
	if status != http.StatusMultiStatus {
		t.Fatalf("REPORT sync-collection %s = %d: %s", href, status, resp)
	}
	type propstat struct {
		Status string `xml:"status"`
		ETag   string `xml:"prop>getetag"`
	}
	var ms struct {
		Responses []struct {
			Href      string     `xml:"href"`
			Status    string     `xml:"status"`
			Propstats []propstat `xml:"propstat"`
		} `xml:"response"`
		Token string `xml:"sync-token"`
	}
	if err := xml.Unmarshal([]byte(resp), &ms); err != nil {
		t.Fatalf("sync answer: %v\n%s", err, resp)
	}
	res := syncResult{updated: []string{}, deleted: []string{}, token: ms.Token}
	for _, r := range ms.Responses {
		if strings.Contains(r.Status, "404") {
			res.deleted = append(res.deleted, r.Href)
			continue
		}
		if !slices.ContainsFunc(r.Propstats, func(ps propstat) bool {
			return strings.Contains(ps.Status, "200") && ps.ETag != ""
		}) {
			t.Errorf("sync response for %s lacks an ETag: %+v", r.Href, r.Propstats)
		}
		res.updated = append(res.updated, r.Href)
	}
	slices.Sort(res.updated)
	slices.Sort(res.deleted)
	return res
}

// TestCalDAV runs a CalDAV client through discovery, a calendar's life
// cycle, event CRUD, time-range queries and sync-collection.
func TestCalDAV(t *testing.T) {
	ctx := ctxTimeout(t)
	a := signInWithAppPassword(t, goUser, goPassword)
	c := calClient(t, a.basic())

	principal, err := c.FindCurrentUserPrincipal(ctx)
	if err != nil {
		t.Fatalf("FindCurrentUserPrincipal: %v", err)
	}
	home, err := c.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		t.Fatalf("FindCalendarHomeSet: %v", err)
	}
	if want := "/dav/calendars/" + goUser + "/"; home != want {
		t.Fatalf("calendar home = %q, want %q", home, want)
	}
	cals, err := c.FindCalendars(ctx, home)
	if err != nil {
		t.Fatalf("FindCalendars: %v", err)
	}
	if !slices.ContainsFunc(cals, func(c caldav.Calendar) bool { return c.Name == "Personal" }) {
		t.Errorf("the default calendar is missing: %+v", cals)
	}

	work := mkcalendar(t, ctx, a, "Work")
	cals, err = c.FindCalendars(ctx, home)
	if err != nil {
		t.Fatalf("FindCalendars: %v", err)
	}
	i := slices.IndexFunc(cals, func(c caldav.Calendar) bool { return c.Path == work })
	if i < 0 {
		t.Fatalf("the new calendar %s is not listed: %+v", work, cals)
	}
	if cal := cals[i]; cal.Name != "Work" || !slices.Equal(cal.SupportedComponentSet, []string{"VEVENT"}) {
		t.Errorf("calendar = %+v", cal)
	}

	events := map[string]string{
		work + "lunch.ics":  utcEvent("lunch-1", "20261110T120000Z", "Lunch"),
		work + "weekly.ics": weeklyEvent,
	}
	etags := map[string]string{}
	for p, data := range events {
		co, err := c.PutCalendarObject(ctx, p, decodeCal(t, data))
		if err != nil {
			t.Fatalf("PutCalendarObject %s: %v", p, err)
		}
		if co.ETag == "" {
			t.Errorf("PutCalendarObject %s: no ETag", p)
		}
		etags[p] = co.ETag
	}

	co, err := c.GetCalendarObject(ctx, work+"lunch.ics")
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}
	if eventSummary(t, co.Data) != "Lunch" || co.ETag != etags[work+"lunch.ics"] {
		t.Errorf("GetCalendarObject: summary %q etag %q, want etag %q", eventSummary(t, co.Data), co.ETag, etags[work+"lunch.ics"])
	}

	for _, tt := range []struct {
		name       string
		start, end time.Time
		want       []string
	}{
		{"the lunch", time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC), []string{work + "lunch.ics"}},
		{"the third weekly occurrence", time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 22, 0, 0, 0, 0, time.UTC), []string{work + "weekly.ics"}},
		{"after the last occurrence", time.Date(2027, 1, 4, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC), []string{}},
		{"both", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), []string{work + "lunch.ics", work + "weekly.ics"}},
	} {
		found, err := c.QueryCalendar(ctx, work, timeQuery(tt.start, tt.end))
		if err != nil {
			t.Fatalf("QueryCalendar %s: %v", tt.name, err)
		}
		if got := calObjPaths(found); !slices.Equal(got, tt.want) {
			t.Errorf("QueryCalendar %s = %v, want %v", tt.name, got, tt.want)
		}
	}

	first := calSync(t, ctx, a, work, "")
	if !slices.Equal(first.updated, []string{work + "lunch.ics", work + "weekly.ics"}) || len(first.deleted) != 0 || first.token == "" {
		t.Fatalf("initial sync = %+v", first)
	}
	if _, err := c.PutCalendarObject(ctx, work+"dinner.ics", decodeCal(t, utcEvent("dinner-1", "20261110T180000Z", "Dinner"))); err != nil {
		t.Fatalf("PutCalendarObject: %v", err)
	}
	if err := c.RemoveAll(ctx, work+"weekly.ics"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	next := calSync(t, ctx, a, work, first.token)
	if !slices.Equal(next.updated, []string{work + "dinner.ics"}) || !slices.Equal(next.deleted, []string{work + "weekly.ics"}) || next.token == first.token {
		t.Errorf("delta sync = %+v (first token %q)", next, first.token)
	}

	if status, _, resp := do(t, ctx, http.MethodDelete, work, basicHeader(a.user, a.appPassword), nil); status != http.StatusNoContent {
		t.Fatalf("DELETE %s = %d: %s", work, status, resp)
	}
	cals, err = c.FindCalendars(ctx, home)
	if err != nil {
		t.Fatalf("FindCalendars: %v", err)
	}
	if slices.ContainsFunc(cals, func(c caldav.Calendar) bool { return c.Path == work }) {
		t.Errorf("the deleted calendar %s is still listed", work)
	}
}
