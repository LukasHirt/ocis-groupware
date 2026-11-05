package graphapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/provision"
)

type accountBody struct {
	Username               string     `json:"username"`
	DisplayName            string     `json:"displayName"`
	AppPasswordsValidUntil *time.Time `json:"app_passwords_valid_until"`
	DAV                    struct {
		URL                string `json:"url"`
		CalendarHomeURL    string `json:"calendarHomeUrl"`
		AddressBookHomeURL string `json:"addressBookHomeUrl"`
	} `json:"dav"`
}

func TestAccountInfo(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequest(http.MethodGet, "http://ocis.example/groupware/v1/me", nil)
	req.Header.Set("Authorization", "Bearer einstein")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET me: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[accountBody](t, rec)
	if got.Username != "einstein" || got.DisplayName != "Albert Einstein" ||
		got.DAV.URL != "https://ocis.example/dav/" ||
		got.DAV.CalendarHomeURL != "https://ocis.example/dav/calendars/einstein/" ||
		got.DAV.AddressBookHomeURL != "https://ocis.example/dav/addressbooks/einstein/" {
		t.Errorf("account = %+v", got)
	}
	if got.AppPasswordsValidUntil != nil {
		t.Errorf("app_passwords_valid_until = %v, want null with the lease off", got.AppPasswordsValidUntil)
	}
	compareContract(t, "account", rec.Body.Bytes())

	if rec := f.do(http.MethodGet, "/groupware/v1/me", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/groupware/v1/me", "einstein", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d, want 405", rec.Code)
	}
}

func TestAccountInfoEscapesTheUsername(t *testing.T) {
	got := davAddresses("", "ann/o'brien x")
	if !strings.HasSuffix(got.CalendarHomeURL, "/dav/calendars/ann%2Fo%27brien%20x/") {
		t.Errorf("calendar home = %q", got.CalendarHomeURL)
	}
	if got := davAddresses("https://h", ""); got.CalendarHomeURL != "" || got.AddressBookHomeURL != "" || got.URL != "https://h/dav/" {
		t.Errorf("without a username: %+v", got)
	}
}

type meGraph struct{}

func (meGraph) Me(context.Context, string) (ocisgraph.User, error) {
	return ocisgraph.User{ID: "u-einstein"}, nil
}

// With an identity lease, the answer says until when the user's devices stay
// signed in: the time oCIS last confirmed the user plus the lease.
func TestAccountInfoShowsWhenAppPasswordsStopWorking(t *testing.T) {
	var ids *provision.Identities
	f := newFixtureWith(t, []principals.Option{principals.WithIdentityMaxAge(720 * time.Hour)}, func(c *Config) {
		ids = provision.NewIdentities(c.Registry, meGraph{}, nil)
		c.Identities = ids
	})
	before := time.Now()
	rec := f.do(http.MethodGet, "/groupware/v1/me", "einstein", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET me: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[accountBody](t, rec)
	if got.AppPasswordsValidUntil == nil {
		t.Fatalf("app_passwords_valid_until is null: %s", rec.Body.String())
	}
	if d := got.AppPasswordsValidUntil.Sub(before); d < 720*time.Hour-time.Minute || d > 720*time.Hour+time.Minute {
		t.Errorf("valid until %v is %v from now, want 720h", got.AppPasswordsValidUntil, d)
	}
}
