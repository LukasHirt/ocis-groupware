package graphapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LukasHirt/groupkit/ical"
	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/vcard"
)

type importResultBody struct {
	Index  int    `json:"index"`
	UID    string `json:"uid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type importReportBody struct {
	Total   int                `json:"total"`
	Created int                `json:"created"`
	Updated int                `json:"updated"`
	Failed  int                `json:"failed"`
	Results []importResultBody `json:"results"`
}

// upload sends a file to a groupware endpoint.
func (f *fixture) upload(target, token, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func calImport(id string) string  { return "/groupware/v1/calendars/" + id + "/import" }
func calExport(id string) string  { return "/groupware/v1/calendars/" + id + "/export" }
func bookImport(id string) string { return "/groupware/v1/addressbooks/" + id + "/import" }
func bookExport(id string) string { return "/groupware/v1/addressbooks/" + id + "/export" }

func objectsOf(t *testing.T, f *fixture, ref store.CollectionRef) map[string]store.Object {
	t.Helper()
	objs, err := f.st.ListObjects(context.Background(), ref, store.ListOptions{IncludeData: true})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.Object{}
	for _, o := range objs {
		out[o.UID] = o
	}
	return out
}

const mixedCalendar = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nMETHOD:PUBLISH\r\n" +
	"BEGIN:VEVENT\r\nUID:one\r\nDTSTAMP:20261101T080000Z\r\nDTSTART:20261110T090000Z\r\nDTEND:20261110T100000Z\r\nSUMMARY:First\r\nEND:VEVENT\r\n" +
	"BEGIN:VTODO\r\nUID:todo\r\nDTSTAMP:20261101T080000Z\r\nSUMMARY:Not an event\r\nEND:VTODO\r\n" +
	"BEGIN:VEVENT\r\nUID:two\r\nDTSTAMP:20261101T080000Z\r\nDTSTART:20261111T090000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nSUMMARY:Series\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:two\r\nDTSTAMP:20261101T080000Z\r\nRECURRENCE-ID:20261112T090000Z\r\nDTSTART:20261112T100000Z\r\nSUMMARY:Moved\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestImportCalendarReportsEachObject(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	id := CalendarID(collID)

	rec := f.upload(calImport(id), "einstein", "text/calendar", mixedCalendar)
	if rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	rep := decodeBody[importReportBody](t, rec)
	if rep.Total != 3 || rep.Created != 2 || rep.Updated != 0 || rep.Failed != 1 || len(rep.Results) != 3 {
		t.Fatalf("report = %+v", rep)
	}
	if r := rep.Results[1]; r.Status != "failed" || r.UID != "todo" || r.Error == "" || r.Index != 1 {
		t.Errorf("bad object = %+v", r)
	}
	objs := objectsOf(t, f, ref)
	if len(objs) != 2 || objs["one"].Name == "" || objs["two"].Name == "" {
		t.Fatalf("stored = %v", objs)
	}
	if got := string(objs["two"].Data); strings.Contains(got, "METHOD") || strings.Count(got, "BEGIN:VEVENT") != 2 {
		t.Errorf("series stored as %q", got)
	}
	compareContract(t, "import-report", rec.Body.Bytes())

	// The same file again changes nothing but reports the update.
	rec = f.upload(calImport(id), "einstein", "text/calendar", strings.ReplaceAll(mixedCalendar, "SUMMARY:First", "SUMMARY:Renamed"))
	rep = decodeBody[importReportBody](t, rec)
	if rep.Created != 0 || rep.Updated != 2 || rep.Failed != 1 {
		t.Fatalf("second report = %+v", rep)
	}
	again := objectsOf(t, f, ref)
	if len(again) != 2 || again["one"].Name != objs["one"].Name || !strings.Contains(string(again["one"].Data), "Renamed") {
		t.Errorf("after update: %v", again)
	}
}

func TestImportKeepsGoodObjectsNextToBrokenOnes(t *testing.T) {
	f := newFixture(t)
	_, collID := einsteinBook(t, f)
	id := CalendarID(collID)
	file := "BEGIN:VCARD\nVERSION:3.0\nUID:good-1\nFN:Ada Lovelace\nN:Lovelace;Ada;;;\nEND:VCARD\n" +
		"BEGIN:VCARD\nVERSION:3.0\nFN:Never closed\n" +
		"BEGIN:VCARD\nVERSION:3.0\nFN:No uid\nN:Uid;No;;;\nEND:VCARD\n" +
		"BEGIN:VCARD\nVERSION:3.0\nUID:good-2\nFN:Grace Hopper\nEND:VCARD\n"
	rec := f.upload(bookImport(id), "einstein", "text/vcard", file)
	if rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	rep := decodeBody[importReportBody](t, rec)
	if rep.Total != 4 || rep.Created != 3 || rep.Failed != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if r := rep.Results[1]; r.Status != "failed" || r.Error == "" {
		t.Errorf("broken card = %+v", r)
	}
	if r := rep.Results[2]; r.Status != "created" || r.UID == "" {
		t.Errorf("card without UID = %+v, want a generated UID", r)
	}
	ref, _ := einsteinBook(t, f)
	objs := objectsOf(t, f, ref)
	if len(objs) != 3 || objs["good-1"].Name == "" {
		t.Fatalf("stored = %v", objs)
	}
	for uid, o := range objs {
		if _, err := vcard.Validate(o.Data); err != nil {
			t.Errorf("%s stored invalid: %v", uid, err)
		}
	}

	// A card with a known UID replaces the stored one in place.
	rec = f.upload(bookImport(id), "einstein", "text/vcard", "BEGIN:VCARD\nVERSION:3.0\nUID:good-1\nFN:Ada King\nEND:VCARD\n")
	rep = decodeBody[importReportBody](t, rec)
	if rep.Updated != 1 || rep.Created != 0 {
		t.Fatalf("update report = %+v", rep)
	}
	after := objectsOf(t, f, ref)
	if len(after) != 3 || after["good-1"].Name != objs["good-1"].Name || !strings.Contains(string(after["good-1"].Data), "Ada King") {
		t.Errorf("after update: %v", after)
	}
}

func TestImportRefusesAFileItCannotSplit(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinCalendar(t, f)
	rec := f.upload(calImport(CalendarID(collID)), "einstein", "text/calendar", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x\r\n")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("broken file: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(objectsOf(t, f, ref)); n != 0 {
		t.Errorf("%d objects stored from a broken file", n)
	}
	bref, bcoll := einsteinBook(t, f)
	rec = f.upload(bookImport(CalendarID(bcoll)), "einstein", "text/vcard", "hello\nBEGIN:VCARD\nEND:VCARD\n")
	if rec.Code != http.StatusBadRequest || len(objectsOf(t, f, bref)) != 0 {
		t.Errorf("stray text: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportLimitsAndAccess(t *testing.T) {
	f, shared := sharedFixture(t, store.AccessRead)
	sid := CalendarID(shared.ID)
	body := mixedCalendar
	if rec := f.upload(calImport(sid), "einstein", "text/calendar", body); rec.Code != http.StatusForbidden {
		t.Errorf("read-only calendar: %d, want 403", rec.Code)
	}
	if rec := f.upload(calImport(CalendarID("nope")), "einstein", "text/calendar", body); rec.Code != http.StatusNotFound {
		t.Errorf("unknown calendar: %d, want 404", rec.Code)
	}
	if rec := f.upload(calImport("***"), "einstein", "text/calendar", body); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed id: %d, want 400", rec.Code)
	}
	if rec := f.upload(calImport(sid), "", "text/calendar", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", rec.Code)
	}
	if rec := f.do(http.MethodGet, calImport(sid), "einstein", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET import: %d, want 405", rec.Code)
	}

	_, collID := einsteinCalendar(t, f)
	big := strings.Repeat("X-FILL:"+strings.Repeat("a", 60)+"\r\n", maxImportBytes/60)
	rec := f.upload(calImport(CalendarID(collID)), "einstein", "text/calendar", "BEGIN:VCALENDAR\r\n"+big+"END:VCALENDAR\r\n")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized file: %d, want 413", rec.Code)
	}
}

func TestImportIntoSharedAddressBookNeedsWriteAccess(t *testing.T) {
	sf := newShareFixture(t)
	id, _ := sf.einsteinBook(t)
	if rec := sf.do(http.MethodPost, bookPerms(id), "einstein", grantMarieRead); rec.Code != http.StatusCreated {
		t.Fatalf("grant: %d", rec.Code)
	}
	card := "BEGIN:VCARD\nVERSION:3.0\nUID:z\nFN:Z\nEND:VCARD\n"
	if rec := sf.upload(bookImport(id), "marie", "text/vcard", card); rec.Code != http.StatusForbidden {
		t.Errorf("read-only recipient: %d, want 403", rec.Code)
	}
	if rec := sf.do(http.MethodGet, bookExport(id), "marie", ""); rec.Code != http.StatusOK {
		t.Errorf("read-only recipient may export: %d", rec.Code)
	}
	if rec := sf.upload(bookImport(id), "einstein", "text/vcard", card); rec.Code != http.StatusOK {
		t.Errorf("owner: %d", rec.Code)
	}
}

func TestExportCalendarRoundTrips(t *testing.T) {
	f := newFixture(t)
	_, collID := einsteinCalendar(t, f)
	id := CalendarID(collID)
	withZone := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID:Europe/Prague\r\nBEGIN:STANDARD\r\nDTSTART:19701025T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
		"BEGIN:VEVENT\r\nUID:zoned\r\nDTSTAMP:20261101T080000Z\r\nDTSTART;TZID=Europe/Prague:20261120T090000\r\nSUMMARY:Zoned\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	for _, file := range []string{mixedCalendar, withZone} {
		if rec := f.upload(calImport(id), "einstein", "text/calendar", file); rec.Code != 200 {
			t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
		}
	}

	rec := f.do(http.MethodGet, calExport(id), "einstein", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
		t.Errorf("content type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, ".ics") {
		t.Errorf("content disposition = %q", cd)
	}
	out := rec.Body.String()
	if _, err := ical.Parse([]byte(out)); err != nil {
		t.Fatalf("export does not parse: %v\n%s", err, out)
	}
	if strings.Count(out, "BEGIN:VCALENDAR") != 1 || strings.Count(out, "BEGIN:VTIMEZONE") != 1 || strings.Count(out, "BEGIN:VEVENT") != 4 {
		t.Errorf("export shape:\n%s", out)
	}

	// Importing the export into an empty calendar gives the same objects.
	rec = f.do(http.MethodPost, "/graph/v1.0/me/calendars", "einstein", `{"name":"Copy"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create calendar: %d %s", rec.Code, rec.Body.String())
	}
	copyID := decodeBody[calendarBody](t, rec).ID
	rep := decodeBody[importReportBody](t, f.upload(calImport(copyID), "einstein", "text/calendar", out))
	if rep.Total != 3 || rep.Created != 3 || rep.Failed != 0 {
		t.Fatalf("round trip report = %+v", rep)
	}
	collCopy, _ := ParseCalendarID(copyID)
	cols, _ := f.st.ListCollections(context.Background(), "sub-einstein", store.KindCalendar)
	for _, c := range cols {
		if c.ID == collCopy {
			if got := objectsOf(t, f, c.Ref); len(got) != 3 || got["zoned"].Name == "" || !strings.Contains(string(got["zoned"].Data), "TZID:Europe/Prague") {
				t.Errorf("copy holds %v", got)
			}
		}
	}
}

func TestExportAddressBook(t *testing.T) {
	f := newFixture(t)
	_, collID := einsteinBook(t, f)
	id := CalendarID(collID)
	file := "BEGIN:VCARD\nVERSION:3.0\nUID:a\nFN:Ada\nEND:VCARD\nBEGIN:VCARD\nVERSION:4.0\nUID:b\nFN:Bob\nEND:VCARD\n"
	if rec := f.upload(bookImport(id), "einstein", "text/vcard", file); rec.Code != 200 {
		t.Fatalf("seed: %d", rec.Code)
	}
	rec := f.do(http.MethodGet, bookExport(id), "einstein", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/vcard") {
		t.Errorf("content type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, ".vcf") {
		t.Errorf("content disposition = %q", cd)
	}
	cards, err := vcard.Split(rec.Body.Bytes())
	if err != nil || len(cards) != 2 || cards[0].Err != nil || cards[1].Err != nil {
		t.Fatalf("export = %q: %v", rec.Body.String(), err)
	}
	if !strings.Contains(rec.Body.String(), "\r\n") || strings.Contains(strings.ReplaceAll(rec.Body.String(), "\r\n", ""), "\n") {
		t.Error("export lines do not all end in CRLF")
	}
	if rec := f.do(http.MethodGet, bookExport(CalendarID("nope")), "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown book: %d", rec.Code)
	}
}

func TestImportContactsReportContract(t *testing.T) {
	f := newFixture(t)
	_, collID := einsteinBook(t, f)
	file := "BEGIN:VCARD\nVERSION:3.0\nUID:ada\nFN:Ada Lovelace\nEND:VCARD\n" +
		"BEGIN:VCARD\nVERSION:3.0\nFN:Never closed\n" +
		"BEGIN:VCARD\nVERSION:3.0\nUID:grace\nFN:Grace Hopper\nEND:VCARD\n"
	rec := f.upload(bookImport(CalendarID(collID)), "einstein", "text/vcard", file)
	if rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	compareContract(t, "import-report-contacts", rec.Body.Bytes())
}
