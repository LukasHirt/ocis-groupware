package main

import (
	"context"
	"log/slog"

	"github.com/LukasHirt/groupkit/store/filestore"

	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// openSharing loads the shares in the data directory and returns them with
// the store view that serves them: the file store with the collections shared
// with each caller added to the caller's own. The oCIS ids and group
// snapshots come from the principals registry.
func openSharing(st *filestore.Store, reg *principals.Registry, logger *slog.Logger) (*sharing.Shares, *sharing.View, error) {
	shares, err := sharing.Open(sharing.Config{
		OwnerDir: st.OwnerDir,
		Owners: func() []string {
			all := reg.ListPrincipals(context.Background())
			subs := make([]string, 0, len(all))
			for _, p := range all {
				subs = append(subs, p.Sub)
			}
			return subs
		},
		OCISID: func(sub string) string {
			id, err := reg.Identity(sub)
			if err != nil {
				return ""
			}
			return id.OCISID
		},
		Logger: logger,
	})
	if err != nil {
		return nil, nil, err
	}
	view, err := sharing.NewView(sharing.ViewConfig{
		Inner:  st,
		Shares: shares,
		Identity: func(sub string) (string, []string) {
			id, err := reg.Identity(sub)
			if err != nil {
				return "", nil
			}
			groups := make([]string, 0, len(id.Groups))
			for _, g := range id.Groups {
				groups = append(groups, g.ID)
			}
			return id.OCISID, groups
		},
		OwnerName: func(sub string) string {
			p, err := reg.Get(sub)
			if err != nil {
				return ""
			}
			if p.DisplayName != "" {
				return p.DisplayName
			}
			return p.Username
		},
		OwnerDir: st.OwnerDir,
		Logger:   logger,
	})
	if err != nil {
		return nil, nil, err
	}
	return shares, view, nil
}
