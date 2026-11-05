package principals

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/LukasHirt/groupkit/store"
)

// ErrUsernameTaken means another principal holds the username that an
// import asked for.
var ErrUsernameTaken = errors.New("principals: username is taken by another principal")

// Import creates a principal for a user who has not signed in yet: it has an
// owner, a username and the oCIS identity, and no subject. The first bearer
// sign-in of the user binds the subject, see Bind. Until then the user can
// sign in with an app password through the username.
//
// The default collections are not created, because the imported data is
// expected to bring its own. If the owner exists already, Import returns it
// and changes nothing, so a repeated import is harmless. A username held by
// another principal is not taken over: Import returns ErrUsernameTaken.
// The identity must carry an oCIS id, since Bind finds the principal by it.
func (r *Registry) Import(ctx context.Context, owner, username, displayName string, id Identity) (Principal, bool, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, false, err
	}
	if err := store.ValidateOwner(owner); err != nil {
		return Principal{}, false, fmt.Errorf("principals: invalid owner: %w", err)
	}
	if err := store.ValidateName(username); err != nil {
		return Principal{}, false, fmt.Errorf("principals: invalid username: %w", err)
	}
	if id.OCISID == "" {
		return Principal{}, false, errors.New("principals: an imported principal needs an oCIS id")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.principals[owner]; ok {
		return rec.principal(), false, nil
	}
	if holder, ok := r.byUsername[username]; ok && holder != owner {
		return Principal{}, false, fmt.Errorf("%w: %q belongs to owner %q", ErrUsernameTaken, username, holder)
	}
	now := r.now().UTC()
	rec := record{
		Owner: owner, Username: username, DisplayName: displayName,
		CreatedAt: now, UpdatedAt: now, DefaultsCreated: true,
		OCISID: id.OCISID, Groups: slices.Clone(id.Groups), IdentityAt: id.At.UTC(),
	}
	if err := r.writeRecord(rec); err != nil {
		return Principal{}, false, err
	}
	r.principals[owner] = rec
	r.byUsername[username] = owner
	return rec.principal(), true, nil
}
