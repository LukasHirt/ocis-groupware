// Package davmount serves CalDAV and CardDAV under /dav behind the service's
// own authentication: app passwords over HTTP Basic for DAV clients, and OIDC
// bearer tokens for the web and other clients that hold one.
package davmount

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/LukasHirt/groupkit"
	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/provision"
)

const (
	// Prefix is the URL path the DAV server is mounted at.
	Prefix = "/dav"

	// challenge is the WWW-Authenticate value of every 401.
	challenge = `Basic realm="ocis-groupware"`
)

// Config holds what New needs.
type Config struct {
	// Store holds the address books and calendars. Any store.Store works.
	Store store.Store
	// Registry resolves app passwords and provisions principals.
	Registry *principals.Registry
	// Verifier checks bearer tokens.
	Verifier auth.Verifier
	// Identities refreshes the oCIS identity snapshot at bearer sign-ins. Nil
	// signs in without a lookup.
	Identities *provision.Identities
	// Logger receives refusals and faults. Nil discards them.
	Logger *slog.Logger
}

// New returns the DAV handler, to mount at /dav/ and /dav, and the handler for
// /.well-known/caldav and /.well-known/carddav. The DAV handler authenticates
// every request itself; the well-known handler needs no credentials.
func New(cfg Config) (dav, wellKnown http.Handler, err error) {
	switch {
	case cfg.Store == nil:
		return nil, nil, errors.New("davmount: store is nil")
	case cfg.Registry == nil:
		return nil, nil, errors.New("davmount: registry is nil")
	case cfg.Verifier == nil:
		return nil, nil, errors.New("davmount: verifier is nil")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	srv, err := groupkit.NewServer(groupkit.Config{
		Store:       cfg.Store,
		Prefix:      Prefix,
		CurrentUser: currentUser,
		Authorize:   authorize,
		Challenge:   challenge,
		Logger:      logger,
	})
	if err != nil {
		return nil, nil, err
	}
	ids := cfg.Identities
	if ids == nil {
		ids = provision.NewIdentities(cfg.Registry, nil, logger)
	}
	a := &authenticator{reg: cfg.Registry, ids: ids, verifier: cfg.Verifier, logger: logger}
	return a.wrap(srv.Handler()), srv.WellKnown(), nil
}

// identity is the authenticated caller of a request.
type identity struct {
	owner    string
	username string
}

type identityKey struct{}

func identityFrom(ctx context.Context) (identity, bool) {
	id, ok := ctx.Value(identityKey{}).(identity)
	return id, ok && id.owner != "" && id.username != ""
}

// currentUser names the caller by the username in its URLs.
func currentUser(r *http.Request) (string, error) {
	id, ok := identityFrom(r.Context())
	if !ok {
		return "", groupkit.ErrUnauthenticated
	}
	return id.username, nil
}

// authorize lets the caller reach its own data only: the URL user must be the
// caller's username, and the store owner is the principal's owner.
func authorize(r *http.Request, urlUser string) (string, error) {
	id, ok := identityFrom(r.Context())
	if !ok {
		return "", groupkit.ErrUnauthenticated
	}
	if urlUser != id.username {
		return "", groupkit.ErrForbidden
	}
	return id.owner, nil
}

// authenticator resolves the caller of a DAV request before it reaches the
// DAV server.
type authenticator struct {
	reg      *principals.Registry
	ids      *provision.Identities
	verifier auth.Verifier
	logger   *slog.Logger
}

// wrap answers 401 to every request without valid credentials, so that no
// path below /dav, known or not, answers anything else without them. A
// request that passes carries the caller's identity in its context.
func (a *authenticator) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, status := a.authenticate(r)
		if status != 0 {
			refuse(w, status)
			return
		}
		if !cleanPath(r.URL.EscapedPath()) {
			refuse(w, http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}

// authenticate returns the caller, or the status to refuse the request with.
func (a *authenticator) authenticate(r *http.Request) (identity, int) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return identity{}, http.StatusUnauthorized
	}
	if user, password, ok := r.BasicAuth(); ok {
		return a.basic(r.Context(), user, password)
	}
	scheme, token, _ := strings.Cut(header, " ")
	if strings.EqualFold(scheme, "Bearer") {
		if token = strings.TrimSpace(token); token != "" {
			return a.bearer(r.Context(), token)
		}
	}
	return identity{}, http.StatusUnauthorized
}

// basic checks a username and app password. Every refusal is the same 401,
// so the answer does not show whether the user exists or why it failed.
func (a *authenticator) basic(ctx context.Context, username, password string) (identity, int) {
	p, err := a.reg.VerifyBasic(ctx, username, password)
	switch {
	case err == nil:
		return identity{owner: p.Owner, username: p.Username}, 0
	case errors.Is(err, principals.ErrInvalidCredentials):
		a.logger.Debug("app password rejected")
	case errors.Is(err, principals.ErrRateLimited):
		a.logger.Info("sign-in refused by the rate limiter", "username", username)
		return identity{}, http.StatusTooManyRequests
	case errors.Is(err, principals.ErrExpired):
		a.logger.Info("app password expired", "username", username)
	case errors.Is(err, principals.ErrDisabled):
		a.logger.Info("sign-in of a disabled principal refused", "username", username)
	case ctx.Err() != nil:
		// The client went away.
	default:
		a.logger.Error("could not verify app password", "username", username, "error", err)
	}
	return identity{}, http.StatusUnauthorized
}

// bearer verifies an OIDC token and provisions its principal, which creates
// it with its default collections on first use. A refusal shows its status
// only; the body is the same for every refusal with that status.
func (a *authenticator) bearer(ctx context.Context, token string) (identity, int) {
	claims, err := a.verifier.Verify(ctx, token)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrUnavailable):
		a.logger.Error("token verification unavailable", "error", err)
		return identity{}, http.StatusServiceUnavailable
	default:
		a.logger.Debug("token rejected", "error", err)
		return identity{}, http.StatusUnauthorized
	}

	p, status, _ := a.ids.SignIn(ctx, claims, token, a.logger)
	if status != 0 {
		return identity{}, status
	}
	return identity{owner: p.Owner, username: p.Username}, 0
}

// cleanPath reports whether an escaped request path has no empty, "." or ".."
// segment and no encoded slash. The trailing slash of a collection is
// allowed. groupkit refuses such names too; this keeps them from reaching it.
func cleanPath(escaped string) bool {
	segments := strings.Split(strings.TrimPrefix(escaped, "/"), "/")
	for i, seg := range segments {
		if seg == "" && i == len(segments)-1 {
			continue
		}
		name, err := url.PathUnescape(seg)
		if err != nil || name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
			return false
		}
	}
	return true
}

// refuse writes a short plain text answer. The body depends on the status
// only, never on why the request was refused.
func refuse(w http.ResponseWriter, status int) {
	msg := "unauthorized"
	switch status {
	case http.StatusUnauthorized:
		w.Header().Set("WWW-Authenticate", challenge)
	case http.StatusForbidden:
		msg = "forbidden"
	case http.StatusBadRequest:
		msg = "bad request"
	case http.StatusServiceUnavailable:
		msg = "service unavailable"
	default:
		msg = "internal error"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg + "\n"))
}
