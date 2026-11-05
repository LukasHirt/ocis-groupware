package principals

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LukasHirt/groupkit/store"
)

// ErrNotDisabled means the principal must be disabled before it can be
// purged.
var ErrNotDisabled = errors.New("principals: principal is not disabled")

// PurgeReport counts what a purge removed, or would remove in a dry run.
// Shares counts the grants on the principal's collections: one for each user
// or group a collection was shared with.
type PurgeReport struct {
	Calendars, AddressBooks, AppPasswords, Shares int
}

// ShareCleaner holds the shares of the collections a principal owns. A
// purge asks it how many there are and has it drop them.
type ShareCleaner interface {
	// Count returns how many grants the owner's collections have.
	Count(owner string) int
	// RemoveOwner drops all shares of the owner and returns how many grants
	// that was.
	RemoveOwner(ctx context.Context, owner string) (int, error)
}

// SetShares tells the registry where a principal's shares are, so that
// Purge removes them too. Call it before the registry is used by more than
// one goroutine; without it purge knows of no shares.
func (r *Registry) SetShares(s ShareCleaner) { r.shares = s }

// Purge deletes a disabled principal with its collections, app passwords and
// owner directory. With dryRun it only counts them. It returns ErrNotDisabled
// for a principal that is still enabled and ErrNotFound for a owner that has
// neither a principal file nor an owner directory.
//
// The collections go first, through the store, then the principal's files
// and its entries in the index, then the owner directory. The principal
// stays in the index, disabled, until its files are gone, so nothing can
// sign in as it while its data is being removed. A purge that fails half
// way can be run again: it finishes what is left.
func (r *Registry) Purge(ctx context.Context, owner string, dryRun bool) (PurgeReport, error) {
	if err := ctx.Err(); err != nil {
		return PurgeReport{}, err
	}
	dir := r.ownerDir(owner)
	if dir == "" {
		return PurgeReport{}, ErrNotFound
	}
	r.mu.RLock()
	rec, ok := r.principals[owner]
	r.mu.RUnlock()
	switch {
	case ok && !rec.Disabled:
		return PurgeReport{}, ErrNotDisabled
	case !ok && !isDir(dir):
		// Without a principal file the directory is an orphan: nothing can
		// sign in as it, so it needs no disabled flag.
		return PurgeReport{}, ErrNotFound
	}

	var (
		report PurgeReport
		cols   []store.Collection
	)
	for _, kind := range []store.Kind{store.KindCalendar, store.KindAddressBook} {
		list, err := r.st.ListCollections(ctx, owner, kind)
		if err != nil {
			return PurgeReport{}, fmt.Errorf("principals: purge: list %ss: %w", kind, err)
		}
		cols = append(cols, list...)
		if kind == store.KindCalendar {
			report.Calendars = len(list)
		} else {
			report.AddressBooks = len(list)
		}
	}
	apps, err := r.appPasswordRecords(owner)
	if err != nil {
		return PurgeReport{}, fmt.Errorf("principals: purge: %w", err)
	}
	report.AppPasswords = len(apps)
	if r.shares != nil {
		report.Shares = r.shares.Count(owner)
	}
	if dryRun {
		return report, nil
	}

	for _, col := range cols {
		err := r.st.DeleteCollection(ctx, col.Ref)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return PurgeReport{}, fmt.Errorf("principals: purge: delete %s %q: %w", col.Ref.Kind, col.Ref.Name, err)
		}
	}

	// The shares go with the collections, before the principal does: a purge
	// that fails here can be run again.
	if r.shares != nil {
		if _, err := r.shares.RemoveOwner(ctx, owner); err != nil {
			return PurgeReport{}, fmt.Errorf("principals: purge: remove shares: %w", err)
		}
	}

	if err := r.forget(owner, dir); err != nil {
		return PurgeReport{}, err
	}
	if r.beforeOwnerDirRemoved != nil {
		if err := r.beforeOwnerDirRemoved(); err != nil {
			return PurgeReport{}, err
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return PurgeReport{}, fmt.Errorf("principals: purge: remove owner directory: %w", err)
	}
	if err := r.syncDir(filepath.Dir(dir)); err != nil {
		r.logger.Warn("could not flush directory after removing an owner directory", "dir", filepath.Dir(dir), "error", err)
	}
	return report, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// forget removes the principal's files in dir and drops it from the index,
// the app password sets and the credential cache. A file that is already
// gone is fine.
func (r *Registry) forget(owner, dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range []string{appPasswordsFileName, principalFileName} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("principals: purge: remove %s: %w", name, err)
		}
	}
	if rec, ok := r.principals[owner]; ok {
		if rec.Username != "" && r.byUsername[rec.Username] == owner {
			delete(r.byUsername, rec.Username)
		}
		if rec.Sub != "" && r.bySub[rec.Sub] == owner {
			delete(r.bySub, rec.Sub)
		}
		delete(r.principals, owner)
	}
	r.appsMu.Lock()
	delete(r.apps, owner)
	r.appsMu.Unlock()
	r.cache.invalidate(owner)
	return nil
}
