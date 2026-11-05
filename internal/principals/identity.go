package principals

import (
	"context"
	"slices"
	"time"
)

// Group is a group of the oCIS user, as the identity provider's directory
// names it.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Identity is the snapshot of a principal's oCIS identity: the user's oCIS id
// and the groups the user belonged to when the snapshot was taken. The
// snapshot is refreshed when the user signs in with a token, so it can lag
// behind changes in oCIS.
type Identity struct {
	// OCISID is the oCIS user id. It is empty until a snapshot was taken.
	OCISID string
	Groups []Group
	// At is when the snapshot was taken. It is the zero time when there is
	// none.
	At time.Time
}

// Identity returns the identity snapshot of the principal, or ErrNotFound.
// The result is a copy.
func (r *Registry) Identity(owner string) (Identity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.principals[owner]
	if !ok {
		return Identity{}, ErrNotFound
	}
	return Identity{OCISID: rec.OCISID, Groups: slices.Clone(rec.Groups), At: rec.IdentityAt}, nil
}

// SetIdentity replaces the identity snapshot of the principal and writes it.
// Nothing else in the principal file changes, in particular not updated_at, so
// a snapshot cannot move a username between principals. It returns
// ErrNotFound for an unknown owner and ErrDisabled, with nothing written, for a
// disabled principal.
func (r *Registry) SetIdentity(ctx context.Context, owner string, id Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.principals[owner]
	if !ok {
		return ErrNotFound
	}
	if rec.Disabled {
		return ErrDisabled
	}
	rec.OCISID = id.OCISID
	rec.Groups = slices.Clone(id.Groups)
	rec.IdentityAt = id.At.UTC()
	if err := r.writeRecord(rec); err != nil {
		return err
	}
	r.principals[owner] = rec
	return nil
}
