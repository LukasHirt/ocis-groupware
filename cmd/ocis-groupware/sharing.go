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
// snapshots come from the principals registry, which also learns to remove a
// purged user's shares.
func openSharing(st *filestore.Store, reg *principals.Registry, logger *slog.Logger) (*sharing.Shares, *sharing.View, error) {
	shares, err := openShares(st, reg, logger)
	if err != nil {
		return nil, nil, err
	}
	// A collection deleted while the service was down leaves its shares
	// behind; they could not reach anything, but there is no reason to keep
	// them. A store fault only postpones that to the next start.
	if _, err := shares.Prune(context.Background(), st); err != nil {
		logger.Warn("could not check the shares against the collections", "error", err)
	}
	reg.SetShares(shares)
	view, err := sharing.NewView(sharing.ViewConfig{
		Inner:  st,
		Shares: shares,
		Identity: func(owner string) (string, []string) {
			id, err := reg.Identity(owner)
			if err != nil {
				return "", nil
			}
			groups := make([]string, 0, len(id.Groups))
			for _, g := range id.Groups {
				groups = append(groups, g.ID)
			}
			return id.OCISID, groups
		},
		OwnerName: func(owner string) string {
			p, err := reg.Get(owner)
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

// openShares loads the shares in the data directory. It changes nothing on
// disk besides removing temporary files a crash left behind.
func openShares(st *filestore.Store, reg *principals.Registry, logger *slog.Logger) (*sharing.Shares, error) {
	return sharing.Open(sharing.Config{
		OwnerDir: st.OwnerDir,
		Owners: func() []string {
			all := reg.ListPrincipals(context.Background())
			owners := make([]string, 0, len(all))
			for _, p := range all {
				owners = append(owners, p.Owner)
			}
			return owners
		},
		OCISID: func(owner string) string {
			id, err := reg.Identity(owner)
			if err != nil {
				return ""
			}
			return id.OCISID
		},
		Logger: logger,
	})
}
