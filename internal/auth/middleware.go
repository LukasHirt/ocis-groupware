// Package auth authenticates requests to ocis-groupware using OIDC bearer tokens issued
// by the oCIS identity provider.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

var (
	// ErrInvalidToken is returned by a Verifier when the token is malformed,
	// expired, signed by an unknown key, or issued for another audience.
	ErrInvalidToken = errors.New("invalid token")

	// ErrUnavailable is returned by a Verifier when the identity provider
	// can't be reached, so the token could not be checked either way.
	ErrUnavailable = errors.New("identity provider unavailable")
)

// Claims is the identity extracted from a verified token.
type Claims struct {
	// Subject is the stable user identifier (the "sub" claim). All ownership
	// keys on it.
	Subject string
	// PreferredUsername is the login name: the preferred_username claim, or for
	// oCIS access tokens the login name in the lg.i claim. Empty when the token
	// carries neither.
	PreferredUsername string
	// DisplayName is the name to show for the user: the name claim, or for oCIS
	// access tokens the display name in the lg.i claim. It can be empty.
	DisplayName string
	// Client is the OAuth client the token was issued to, for the audit
	// log: the azp claim, else the audience entry that matched a configured
	// client, else the first audience. It can be empty.
	Client string
}

// Verifier checks a raw bearer token and returns the identity it carries.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (Claims, error)
}

type claimsKey struct{}

type tokenKey struct{}

// TokenFromContext returns the raw bearer token that Middleware verified. It
// is for calls made on the caller's behalf; it must never be logged.
func TokenFromContext(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tokenKey{}).(string)
	return t, ok
}

// ClaimsFromContext returns the claims that Middleware attached to ctx.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(Claims)
	return c, ok
}

// Option changes what Middleware lets through.
type Option func(*middlewareOptions)

type middlewareOptions struct{ davPrefixes []string }

// WithDAVPrefix treats the paths below prefix like the ones below /dav: they
// pass through untouched, because a DAV handler mounted there authenticates
// them itself. Use it only for a prefix that has such a handler.
func WithDAVPrefix(prefix string) Option {
	return func(o *middlewareOptions) { o.davPrefixes = append(o.davPrefixes, path.Clean(prefix)) }
}

// Middleware requires a valid "Authorization: Bearer" token on every request
// except the public paths, and stores the verified claims in the request
// context. Requests below /dav pass through untouched: the DAV handler
// authenticates them itself, with an app password or a bearer token.
func Middleware(v Verifier, logger *slog.Logger, opts ...Option) func(http.Handler) http.Handler {
	var o middlewareOptions
	for _, opt := range opts {
		opt(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p := routedPath(r); isPublic(p) || isDAV(p, o.davPrefixes) {
				next.ServeHTTP(w, r)
				return
			}

			raw, ok := bearerToken(r)
			if !ok {
				unauthorized(w, "")
				return
			}

			claims, err := v.Verify(r.Context(), raw)
			switch {
			case err == nil:
				ctx := context.WithValue(r.Context(), claimsKey{}, claims)
				ctx = context.WithValue(ctx, tokenKey{}, raw)
				next.ServeHTTP(w, r.WithContext(ctx))
			case errors.Is(err, ErrUnavailable):
				logger.Error("token verification unavailable", "error", err)
				writeError(w, http.StatusServiceUnavailable, "identity provider unavailable")
			default:
				logger.Debug("token rejected", "error", err)
				unauthorized(w, "invalid_token")
			}
		})
	}
}

// routedPath returns the path the router picks a handler by: the escaped
// path, cleaned. The router matches escaped segments, so "/graph/..%2Fweb/x"
// goes to the handler of /graph/, and the decoded path must not decide here.
// A path with dot segments or double slashes is redirected to its clean form
// by the router, so "/web/../dav/..." can't borrow a public prefix either.
// CONNECT is the exception: the router neither cleans nor redirects it, so
// the server refuses CONNECT before this middleware runs.
func routedPath(r *http.Request) string {
	return path.Clean(r.URL.EscapedPath())
}

// isPublic reports whether the cleaned path p is served without
// authentication.
func isPublic(p string) bool {
	switch p {
	case "/healthz", "/metrics":
		return true
	}
	return strings.HasPrefix(p, "/.well-known/") || strings.HasPrefix(p, "/web/")
}

// isDAV reports whether the cleaned path p belongs to a DAV handler: the one
// at /dav, or one at an extra prefix.
func isDAV(p string, extra []string) bool {
	for _, prefix := range append([]string{"/dav"}, extra...) {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func unauthorized(w http.ResponseWriter, errCode string) {
	challenge := `Bearer realm="ocis-groupware"`
	if errCode != "" {
		challenge += `, error="` + errCode + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeError(w, http.StatusUnauthorized, "unauthorized")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
