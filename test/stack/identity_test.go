//go:build stack

package stack

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
)

// The service learns a user's oCIS id and groups from the Graph API with the
// user's own token. This runs the service's Graph client against oCIS the way
// a sign-in does: einstein is a member of four demo groups.
func TestOCISIdentityOfTheCaller(t *testing.T) {
	token := signIn(t, goUser, goPassword)
	client, err := ocisgraph.New(ocisgraph.Config{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Transport: transport, Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err := client.Me(ctxTimeout(t), token)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" || user.Username != goUser {
		t.Errorf("user = %+v, want an id and the login %q", user, goUser)
	}
	var names []string
	for _, g := range user.Groups {
		if g.ID == "" {
			t.Errorf("group without an id: %+v", g)
		}
		names = append(names, g.Name)
	}
	slices.Sort(names)
	want := []string{"physics-lovers", "sailing-lovers", "users", "violin-haters"}
	if !slices.Equal(names, want) {
		t.Errorf("groups = %v, want %v", names, want)
	}

	// A token oCIS does not know is refused.
	if _, err := client.Me(ctxTimeout(t), "not-a-token"); err == nil {
		t.Error("a made-up token was accepted")
	}
}
