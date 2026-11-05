package graphapi

import (
	"context"
	"errors"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// sharedCollections finds the collections of one kind a user can reach: the
// user's own and the ones shared with the user, directly or through a group.
type sharedCollections struct {
	st     store.Store
	shares *sharing.Shares
	reg    *principals.Registry
}

// list returns the user's own collections and then the shared ones. An entry's
// Collection keeps the real owner's reference and carries the access level of
// the share. A share whose collection is gone, or was replaced by another
// collection of the same name, is left out.
func (s sharedCollections) list(ctx context.Context, user principals.Principal, kind store.Kind) ([]CalendarEntry, error) {
	own, err := s.st.ListCollections(ctx, user.Owner, kind)
	if err != nil {
		return nil, err
	}
	out := make([]CalendarEntry, 0, len(own))
	for _, c := range own {
		out = append(out, CalendarEntry{Collection: c, OwnerName: ownerName(user)})
	}
	var userID string
	var groups []string
	if id, err := s.reg.Identity(user.Owner); err == nil {
		userID = id.OCISID
		for _, g := range id.Groups {
			groups = append(groups, g.ID)
		}
	}
	for _, r := range s.shares.Received(userID, groups) {
		if r.Owner == user.Owner || r.Kind != kind {
			continue
		}
		col, err := s.st.GetCollection(ctx, r.Ref())
		if errors.Is(err, store.ErrNotFound) || err == nil && col.ID != r.CollectionID {
			continue
		}
		if err != nil {
			return nil, err
		}
		col.Access, _ = r.Access.StoreAccess()
		name := ""
		if owner, err := s.reg.Get(r.Owner); err == nil {
			name = ownerName(owner)
		}
		out = append(out, CalendarEntry{Collection: col, OwnerName: name})
	}
	return out, nil
}

// sharedCalendars is the CalendarSource that adds the calendars shared with
// the user to their own.
type sharedCalendars struct{ sharedCollections }

func (s sharedCalendars) Calendars(ctx context.Context, user principals.Principal) ([]CalendarEntry, error) {
	return s.list(ctx, user, store.KindCalendar)
}
