//go:build stack

package stack

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
)

// graphJSON calls the facade with a bearer token, decodes a JSON answer into
// out when it is not nil, and returns the status.
func graphJSON(t *testing.T, a account, method, path, body string, out any) int {
	t.Helper()
	headers := map[string]string{"Authorization": "Bearer " + a.token}
	var rd *strings.Reader
	if body != "" {
		headers["Content-Type"] = "application/json"
		rd = strings.NewReader(body)
	}
	var status int
	var text string
	if rd != nil {
		status, _, text = do(t, ctxTimeout(t), method, path, headers, rd)
	} else {
		status, _, text = do(t, ctxTimeout(t), method, path, headers, nil)
	}
	if out != nil && status < 300 {
		if err := json.Unmarshal([]byte(text), out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, text)
		}
	}
	return status
}

// davStatus sends a plain request with the account's app password and
// returns the status. The DAV client hides the status of a refusal.
func davStatus(t *testing.T, a account, method, path, body string) int {
	t.Helper()
	headers := basicHeader(a.user, a.appPassword)
	var rd *strings.Reader
	if body != "" {
		headers["Content-Type"] = "text/calendar"
		rd = strings.NewReader(body)
	}
	var status int
	if rd != nil {
		status, _, _ = do(t, ctxTimeout(t), method, path, headers, rd)
	} else {
		status, _, _ = do(t, ctxTimeout(t), method, path, headers, nil)
	}
	return status
}

// TestShareCalendarBetweenUsers has einstein share a calendar with marie
// through the Graph API, read only and then read and write, and watches the
// effect on marie's DAV client: she finds the calendar in her own home, can
// read it, can write only when the share allows it, and loses it when the
// share is revoked.
func TestShareCalendarBetweenUsers(t *testing.T) {
	needsReport(t)
	ctx := ctxTimeout(t)
	einstein := signInWithAppPassword(t, goUser, goPassword)
	marie := signInWithAppPassword(t, pyUser, pyPassword)

	// The user id marie has in oCIS is what a share names her by.
	idClient, err := ocisgraph.New(ocisgraph.Config{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Transport: transport, Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	me, err := idClient.Me(ctx, marie.token)
	if err != nil {
		t.Fatal(err)
	}

	name := "Shared " + randomString(t)[:8]
	cal := mkcalendar(t, ctx, einstein, name)

	var calendars struct {
		Value []struct {
			ID, Name string
			CanEdit  bool
		} `json:"value"`
	}
	if status := graphJSON(t, einstein, http.MethodGet, "/graph/v1.0/me/calendars", "", &calendars); status != 200 {
		t.Fatalf("list calendars: %d", status)
	}
	i := slices.IndexFunc(calendars.Value, func(c struct {
		ID, Name string
		CanEdit  bool
	}) bool {
		return c.Name == name
	})
	if i < 0 {
		t.Fatalf("the new calendar is not in %+v", calendars.Value)
	}
	permissions := "/graph/v1.0/me/calendars/" + calendars.Value[i].ID + "/calendarPermissions"

	// Before the share, marie sees nothing of it.
	mc := calClient(t, marie.basic())
	home := "/dav/calendars/" + pyUser + "/"
	sharedPath := func() string {
		t.Helper()
		cals, err := mc.FindCalendars(ctx, home)
		if err != nil {
			t.Fatalf("FindCalendars: %v", err)
		}
		for _, c := range cals {
			if strings.HasPrefix(c.Name, name) {
				return c.Path
			}
		}
		return ""
	}
	if p := sharedPath(); p != "" {
		t.Fatalf("marie sees %s before the share", p)
	}

	var perm struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	body := `{"role":"read","grantee":{"type":"user","id":"` + me.ID + `"}}`
	if status := graphJSON(t, einstein, http.MethodPost, permissions, body, &perm); status != http.StatusCreated {
		t.Fatalf("share read only: %d", status)
	}

	// Read only: marie finds the calendar and reads it, and a write is refused.
	shared := sharedPath()
	if shared == "" || !strings.HasPrefix(shared, home+"shared-") {
		t.Fatalf("marie's shared calendar is at %q, want one below %sshared-", shared, home)
	}
	ec := calClient(t, einstein.basic())
	if _, err := ec.PutCalendarObject(ctx, cal+"lunch.ics", decodeCal(t, utcEvent("share-1", "20261110T120000Z", "Lunch"))); err != nil {
		t.Fatalf("einstein writes an event: %v", err)
	}
	got, err := mc.GetCalendarObject(ctx, shared+"lunch.ics")
	if err != nil {
		t.Fatalf("marie reads the event: %v", err)
	}
	if eventSummary(t, got.Data) != "Lunch" {
		t.Errorf("marie reads %q", eventSummary(t, got.Data))
	}
	dinner := utcEvent("share-2", "20261110T180000Z", "Dinner")
	if status := davStatus(t, marie, http.MethodPut, shared+"dinner.ics", dinner); status != http.StatusForbidden {
		t.Errorf("marie writes to a read only share: %d, want 403", status)
	}
	if status := davStatus(t, marie, http.MethodDelete, shared+"lunch.ics", ""); status != http.StatusForbidden {
		t.Errorf("marie deletes from a read only share: %d, want 403", status)
	}
	// She cannot manage the share either.
	if status := graphJSON(t, marie, http.MethodGet, permissions, "", nil); status != http.StatusForbidden {
		t.Errorf("marie lists the permissions of a calendar shared with her: %d, want 403", status)
	}

	// Read and write.
	if status := graphJSON(t, einstein, http.MethodPatch, permissions+"/"+perm.ID, `{"role":"write"}`, nil); status != 200 {
		t.Fatalf("change to write: %d", status)
	}
	if status := davStatus(t, marie, http.MethodPut, shared+"dinner.ics", dinner); status != http.StatusCreated {
		t.Fatalf("marie writes to a read and write share: %d, want 201", status)
	}
	found, err := ec.QueryCalendar(ctx, cal, timeQuery(time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{cal + "dinner.ics", cal + "lunch.ics"}; !slices.Equal(calObjPaths(found), want) {
		t.Errorf("einstein sees %v, want %v", calObjPaths(found), want)
	}
	// The calendar itself stays with its owner.
	if status := davStatus(t, marie, http.MethodDelete, shared, ""); status != http.StatusForbidden {
		t.Errorf("marie deleting the shared calendar: %d, want 403", status)
	}

	// Revoked: the calendar is gone from marie's home at once.
	if status := graphJSON(t, einstein, http.MethodDelete, permissions+"/"+perm.ID, "", nil); status != http.StatusNoContent {
		t.Fatalf("revoke: %d", status)
	}
	if p := sharedPath(); p != "" {
		t.Errorf("marie still sees %s after the revoke", p)
	}
	if status := davStatus(t, marie, http.MethodGet, shared+"lunch.ics", ""); status != http.StatusNotFound {
		t.Errorf("marie reads after the revoke: %d, want 404", status)
	}
}
