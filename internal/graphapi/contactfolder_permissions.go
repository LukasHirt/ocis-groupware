package graphapi

import (
	"context"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

// sharedContactFolders is the ContactFolderSource that adds the address books
// shared with the user to their own.
type sharedContactFolders struct{ sharedCollections }

func (s sharedContactFolders) ContactFolders(ctx context.Context, user principals.Principal) ([]ContactFolderEntry, error) {
	all, err := s.list(ctx, user, store.KindAddressBook)
	if err != nil {
		return nil, err
	}
	out := make([]ContactFolderEntry, len(all))
	for i, e := range all {
		out[i] = ContactFolderEntry(e)
	}
	return out, nil
}

func (a *Handler) contactFolderPermissionRoutes() []route {
	pr := permissionRoutes{a: a, noun: "contact folder",
		find: func(ctx context.Context, user principals.Principal, id string) (CalendarEntry, *apiError) {
			e, _, aerr := a.findContactFolder(ctx, user, id)
			return CalendarEntry(e), aerr
		}}
	return pr.routes("me/contactFolders/{id}/permissions")
}
