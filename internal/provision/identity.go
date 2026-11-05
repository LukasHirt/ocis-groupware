package provision

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

// RefreshInterval is how long a principal's oCIS identity snapshot is reused
// before a sign-in looks it up again.
const RefreshInterval = 5 * time.Minute

// Looker asks oCIS who the holder of a token is. *ocisgraph.Client is one.
type Looker interface {
	Me(ctx context.Context, token string) (ocisgraph.User, error)
}

// Identities keeps the oCIS identity snapshots of principals up to date. At a
// sign-in with a bearer token it asks oCIS, with that token, for the user's id
// and groups and stores them on the principal, at most once per
// RefreshInterval per principal. A failed lookup keeps the old snapshot and is
// logged; it never fails the sign-in, and it counts as an attempt, so that an
// unreachable oCIS is not asked again on every request.
type Identities struct {
	reg    *principals.Registry
	graph  Looker
	logger *slog.Logger
	now    func() time.Time

	mu       sync.Mutex
	attempts map[string]time.Time // by owner
}

// IdentitiesOption configures Identities.
type IdentitiesOption func(*Identities)

// WithClock sets the clock, for tests.
func WithClock(now func() time.Time) IdentitiesOption {
	return func(i *Identities) { i.now = now }
}

// NewIdentities returns the updater for reg's principals. A nil graph turns
// the lookup off: sign-ins then only provision. A nil logger discards logs.
func NewIdentities(reg *principals.Registry, graph Looker, logger *slog.Logger, opts ...IdentitiesOption) *Identities {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	i := &Identities{reg: reg, graph: graph, logger: logger, now: time.Now, attempts: make(map[string]time.Time)}
	for _, o := range opts {
		o(i)
	}
	return i
}

// SignIn provisions the principal of the claims like Principal does and, when
// that succeeds, refreshes its identity snapshot with token. The return values
// are those of Principal.
//
// A subject that no principal is bound to yet is first looked up in oCIS with
// token. If an imported principal without a subject has the user's oCIS id,
// the subject is bound to it, so the user finds the data that was imported
// for them. A lookup that fails then ends the sign-in with 503, as long as
// such principals exist: provisioning anyway would give the user an empty
// second principal. The next request tries again. A disabled imported
// principal refuses the sign-in with 403.
func (i *Identities) SignIn(ctx context.Context, claims auth.Claims, token string, logger *slog.Logger) (principals.Principal, int, string) {
	if status, msg := i.bind(ctx, claims.Subject, token, logger); status != 0 {
		return principals.Principal{}, status, msg
	}
	p, status, msg := Principal(ctx, i.reg, claims, logger)
	if status != 0 {
		return p, status, msg
	}
	i.Refresh(ctx, p.Owner, token)
	return p, 0, ""
}

// bind binds sub to an imported principal that has the caller's oCIS id, if
// there is one. It returns the status to refuse the request with, or 0.
func (i *Identities) bind(ctx context.Context, sub, token string, logger *slog.Logger) (int, string) {
	if i.graph == nil || token == "" || !i.reg.HasUnbound() {
		return 0, ""
	}
	if _, err := i.reg.BySub(sub); err == nil {
		return 0, ""
	}
	user, err := i.graph.Me(ctx, token)
	if err != nil {
		logger.Warn("could not look up the oCIS identity to bind an imported user", "sub", sub, "error", err)
		return http.StatusServiceUnavailable, "identity lookup failed"
	}
	p, bound, err := i.reg.Bind(ctx, user.ID, sub)
	switch {
	case err == nil:
		if bound {
			logger.Info("bound imported user to the subject", "owner", p.Owner, "sub", sub)
			i.store(ctx, p.Owner, user, i.now())
		}
		return 0, ""
	case errors.Is(err, principals.ErrNotFound):
		return 0, ""
	case errors.Is(err, principals.ErrDisabled):
		logger.Info("sign-in of a disabled imported user refused", "sub", sub)
		return http.StatusForbidden, "account disabled"
	default:
		logger.Error("could not bind imported user", "sub", sub, "error", err)
		return http.StatusInternalServerError, "internal error"
	}
}

// Refresh looks up the identity of the principal that owns owner with token and stores it,
// unless a snapshot or an attempt is younger than RefreshInterval.
func (i *Identities) Refresh(ctx context.Context, owner, token string) {
	if i == nil || i.graph == nil || token == "" {
		return
	}
	current, err := i.reg.Identity(owner)
	if err != nil {
		return
	}
	now := i.now()
	if !current.At.IsZero() && now.Sub(current.At) < RefreshInterval {
		return
	}
	i.mu.Lock()
	if last, ok := i.attempts[owner]; ok && now.Sub(last) < RefreshInterval {
		i.mu.Unlock()
		return
	}
	i.attempts[owner] = now
	i.mu.Unlock()

	user, err := i.graph.Me(ctx, token)
	if err != nil {
		if errors.Is(err, ocisgraph.ErrUnauthorized) {
			i.logger.Info("oCIS refused the token for the identity lookup, keeping the snapshot", "owner", owner, "error", err)
		} else {
			i.logger.Warn("could not look up the oCIS identity, keeping the snapshot", "owner", owner, "error", err)
		}
		return
	}
	i.store(ctx, owner, user, now)
}

// store saves the snapshot of user as the identity of the principal owner.
func (i *Identities) store(ctx context.Context, owner string, user ocisgraph.User, now time.Time) {
	id := principals.Identity{OCISID: user.ID, At: now}
	for _, g := range user.Groups {
		id.Groups = append(id.Groups, principals.Group{ID: g.ID, Name: g.Name})
	}
	if err := i.reg.SetIdentity(ctx, owner, id); err != nil {
		if errors.Is(err, principals.ErrDisabled) {
			i.logger.Debug("identity of a disabled principal left alone", "owner", owner)
			return
		}
		i.logger.Error("could not store the oCIS identity", "owner", owner, "error", err)
	}
}
