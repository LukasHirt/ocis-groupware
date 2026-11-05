package davmount

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/provision"
)

type graphStub struct {
	calls atomic.Int32
	token atomic.Value
}

func (g *graphStub) Me(_ context.Context, token string) (ocisgraph.User, error) {
	g.calls.Add(1)
	g.token.Store(token)
	return ocisgraph.User{ID: "u-alice", Groups: []ocisgraph.Group{{ID: "g-1", Name: "users"}}}, nil
}

// A bearer sign-in records the oCIS identity with the caller's own token. A
// Basic sign-in has no token to ask oCIS with and records nothing.
func TestBearerSignInRecordsTheIdentity(t *testing.T) {
	f := newFixture(t)
	g := &graphStub{}
	dav, _, err := New(Config{Store: f.st, Registry: f.reg, Verifier: f.verifier, Logger: f.logger,
		Identities: provision.NewIdentities(f.reg, g, f.logger)})
	if err != nil {
		t.Fatal(err)
	}
	f.dav = dav
	secret := f.user(t, "sub-basic", "basic")
	f.verifier.tokens["tok-alice"] = auth.Claims{Subject: "sub-alice", PreferredUsername: "alice"}

	if rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", basic("basic", secret), "Depth", "0"); rec.Code != http.StatusMultiStatus {
		t.Fatalf("basic: status %d", rec.Code)
	}
	if n := g.calls.Load(); n != 0 {
		t.Fatalf("%d lookups for a Basic request, want 0", n)
	}

	if rec := f.do(t, "PROPFIND", "/dav/", propfindPrincipal, "Authorization", "Bearer tok-alice", "Depth", "0"); rec.Code != http.StatusMultiStatus {
		t.Fatalf("bearer: status %d", rec.Code)
	}
	if g.token.Load() != "tok-alice" {
		t.Errorf("token = %v", g.token.Load())
	}
	id, err := f.reg.Identity("sub-alice")
	if err != nil || id.OCISID != "u-alice" || len(id.Groups) != 1 {
		t.Fatalf("identity = %+v, %v", id, err)
	}
}
