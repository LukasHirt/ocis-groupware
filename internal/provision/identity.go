package provision

import (
	"context"
	"errors"
	"log/slog"
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
	attempts map[string]time.Time // by sub
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
func (i *Identities) SignIn(ctx context.Context, claims auth.Claims, token string, logger *slog.Logger) (principals.Principal, int, string) {
	p, status, msg := Principal(ctx, i.reg, claims, logger)
	if status != 0 {
		return p, status, msg
	}
	i.Refresh(ctx, p.Sub, token)
	return p, 0, ""
}

// Refresh looks up the identity of the principal sub with token and stores it,
// unless a snapshot or an attempt is younger than RefreshInterval.
func (i *Identities) Refresh(ctx context.Context, sub, token string) {
	if i == nil || i.graph == nil || token == "" {
		return
	}
	current, err := i.reg.Identity(sub)
	if err != nil {
		return
	}
	now := i.now()
	if !current.At.IsZero() && now.Sub(current.At) < RefreshInterval {
		return
	}
	i.mu.Lock()
	if last, ok := i.attempts[sub]; ok && now.Sub(last) < RefreshInterval {
		i.mu.Unlock()
		return
	}
	i.attempts[sub] = now
	i.mu.Unlock()

	user, err := i.graph.Me(ctx, token)
	if err != nil {
		if errors.Is(err, ocisgraph.ErrUnauthorized) {
			i.logger.Info("oCIS refused the token for the identity lookup, keeping the snapshot", "sub", sub, "error", err)
		} else {
			i.logger.Warn("could not look up the oCIS identity, keeping the snapshot", "sub", sub, "error", err)
		}
		return
	}
	id := principals.Identity{OCISID: user.ID, At: now}
	for _, g := range user.Groups {
		id.Groups = append(id.Groups, principals.Group{ID: g.ID, Name: g.Name})
	}
	if err := i.reg.SetIdentity(ctx, sub, id); err != nil {
		if errors.Is(err, principals.ErrDisabled) {
			i.logger.Debug("identity of a disabled principal left alone", "sub", sub)
			return
		}
		i.logger.Error("could not store the oCIS identity", "sub", sub, "error", err)
	}
}
