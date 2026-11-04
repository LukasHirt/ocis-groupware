// Package provision turns verified OIDC claims into a principal. The DAV
// handler and the REST API both sign callers in with a bearer token, and they
// must provision in exactly the same way: a different username or display name
// would make the principal file change on alternating requests.
package provision

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

// Principal provisions the principal of verified claims and returns it, which
// creates it with its default collections on first use. The display name is the
// one in the claims, or the username when the token has none.
//
// On failure it returns the HTTP status to refuse the request with: 403 for a
// name that cannot be served, 500 for anything else. The faults are logged.
func Principal(ctx context.Context, reg *principals.Registry, claims auth.Claims, logger *slog.Logger) (principals.Principal, int) {
	username := claims.PreferredUsername
	displayName := claims.DisplayName
	if displayName == "" {
		displayName = username
	}
	p, _, err := reg.Provision(ctx, claims.Subject, username, displayName)
	switch {
	case err == nil:
		return p, 0
	case errors.Is(err, store.ErrInvalidName):
		logger.Warn("token names a user that cannot be served", "sub", claims.Subject, "error", err)
		return principals.Principal{}, http.StatusForbidden
	default:
		logger.Error("could not provision principal", "sub", claims.Subject, "error", err)
		return principals.Principal{}, http.StatusInternalServerError
	}
}
