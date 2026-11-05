// Package principals keeps the registry of users (principals) that have used
// the service. Each principal is a file, principal.json, in the owner's
// directory of the groupkit file store. The owner is the key of the user's
// data. It is the subject from the identity provider for users who signed in
// themselves, and a name of its own for users who were imported before they
// signed in and have no subject yet. In-memory indexes map usernames and
// subjects to owners.
package principals

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LukasHirt/groupkit"
	"github.com/LukasHirt/groupkit/store"
	"golang.org/x/crypto/bcrypt"
)

// principalFileName is the name of the file that holds a principal, inside
// the owner's directory.
const principalFileName = "principal.json"

// ownersDirName is the directory below the data directory that holds one
// directory per owner. It matches the layout of groupkit's file store.
const ownersDirName = "owners"

// Principal is a user known to the service.
type Principal struct {
	// Owner keys the user's data: the owner directory, the collections, the
	// app passwords and the shares. It never changes. It equals Sub unless the
	// principal was imported before its user signed in.
	Owner string
	// Sub is the subject from the identity provider. It never changes once
	// set. It is empty for an imported principal whose user has not signed
	// in yet.
	Sub string
	// Username is the name that appears in URLs. It is empty when another
	// principal has taken the name over and this one has not logged in since.
	Username    string
	DisplayName string
	CreatedAt   time.Time
	// Disabled means every sign-in of the principal is refused until an
	// administrator enables it again. Its data and app passwords stay.
	Disabled bool
}

var (
	// ErrNotFound means the registry has no principal for the subject.
	ErrNotFound = errors.New("principals: not found")
	// ErrDisabled means the principal exists and the credentials were right,
	// but the principal is disabled.
	ErrDisabled = errors.New("principals: principal is disabled")
	// ErrAmbiguous means more than one unbound principal has the oCIS id.
	ErrAmbiguous = errors.New("principals: more than one principal has the oCIS id")
)

// record is the content of principal.json.
type record struct {
	// Owner keys the data of the principal. A file without it counts as
	// owned by its subject.
	Owner       string    `json:"owner,omitempty"`
	Sub         string    `json:"sub,omitempty"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	// UpdatedAt is when the username or display name was last written. When
	// two files claim one username, the newer claim wins. It comes from the
	// wall clock, so a clock that steps back between two logins can let the
	// older claim win after a crash; the next login of either user fixes it.
	UpdatedAt time.Time `json:"updated_at"`
	// DefaultsCreated is set once the default address book and calendar were
	// ensured for the principal. A file without the field counts as false:
	// that principal is ensured once more and the flag is then saved.
	DefaultsCreated bool `json:"defaults_created"`
	// Disabled refuses every sign-in of the principal, with app passwords and
	// with bearer tokens alike, until an administrator enables it again. A
	// file without the field counts as enabled.
	Disabled bool `json:"disabled"`
	// OCISID, Groups and IdentityAt are the snapshot of the user's oCIS
	// identity, see Identity. A file without them has no snapshot yet.
	OCISID     string    `json:"ocis_id,omitempty"`
	Groups     []Group   `json:"groups,omitempty"`
	IdentityAt time.Time `json:"identity_at,omitzero"`
}

func (rec record) principal() Principal {
	return Principal{
		Owner: rec.Owner, Sub: rec.Sub, Username: rec.Username, DisplayName: rec.DisplayName,
		CreatedAt: rec.CreatedAt, Disabled: rec.Disabled,
	}
}

// tempPrefixes start the names of files that are being written.
var tempPrefixes = []string{
	"." + principalFileName + ".tmp-",
	"." + appPasswordsFileName + ".tmp-",
}

// Registry keeps principals in <ownerDir(owner)>/principal.json and username
// and subject indexes in memory. It is safe for concurrent use.
type Registry struct {
	dataDir  string
	st       store.Store
	ownerDir func(owner string) string
	logger   *slog.Logger
	now      func() time.Time
	syncDir  func(dir string) error

	// mu guards the maps below. Writers hold it while they write a file, so
	// that the files and the maps change together. Readers therefore wait
	// for a login that changed a name or created a principal; logins that
	// change nothing take only the read lock.
	mu         sync.RWMutex
	principals map[string]record // by owner
	byUsername map[string]string // username to owner
	bySub      map[string]string // subject to owner

	// shares holds the shares of the principals' collections, see SetShares.
	shares ShareCleaner

	// App passwords, see apppasswords.go.
	bcryptCost int
	compare    func(hash, password []byte) error
	hash       func(password []byte, cost int) ([]byte, error)
	cache      *credentialCache
	// dummyHash is compared against when a check has no stored hash, so
	// that it costs the same as checking a wrong password.
	dummyHash []byte
	// Legacy tokens, see legacy.go.
	legacySecret string
	legacyTTL    time.Duration
	legacyGate   func(ctx context.Context, username string) error
	appsMu       sync.Mutex
	apps         map[string]*appSet // by owner
	// beforeCachePut, if set, runs in VerifyBasic between the last check of
	// the app password and storing it in the cache. Tests use it to revoke
	// the password in exactly that gap.
	beforeCachePut func()
	// beforeOwnerDirRemoved, if set, runs in Purge after the principal's
	// files are removed and before its owner directory is. Tests use it to
	// fail a purge in exactly that gap.
	beforeOwnerDirRemoved func() error
}

// Option configures a Registry.
type Option func(*Registry)

// NewRegistry returns a registry for the principals below dataDir. st is the
// store that holds the principals' collections, and ownerDir returns the
// directory of an owner's data, which is filestore.Store.OwnerDir for the
// file store. A nil logger discards log output.
//
// NewRegistry reads <dataDir>/owners/*/principal.json to build the username
// index. It skips files it cannot read or that do not belong to the
// directory they are in, and logs a warning for each. It removes temporary
// files left by a crash during a write.
func NewRegistry(dataDir string, st store.Store, ownerDir func(owner string) string, logger *slog.Logger, opts ...Option) (*Registry, error) {
	switch {
	case dataDir == "":
		return nil, errors.New("principals: data directory is empty")
	case st == nil:
		return nil, errors.New("principals: store is nil")
	case ownerDir == nil:
		return nil, errors.New("principals: owner directory function is nil")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	r := &Registry{
		dataDir:    dataDir,
		st:         st,
		ownerDir:   ownerDir,
		logger:     logger,
		now:        time.Now,
		syncDir:    syncDir,
		principals: make(map[string]record),
		byUsername: make(map[string]string),
		bySub:      make(map[string]string),
		bcryptCost: defaultBcryptCost,
		compare:    bcrypt.CompareHashAndPassword,
		hash:       bcrypt.GenerateFromPassword,
		legacyTTL:  defaultLegacyTokenLifetime,
		cache:      newCredentialCache(cacheTTL),
		apps:       make(map[string]*appSet),
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.bcryptCost < bcrypt.MinCost || r.bcryptCost > bcrypt.MaxCost {
		return nil, fmt.Errorf("principals: bcrypt cost %d is outside %d to %d", r.bcryptCost, bcrypt.MinCost, bcrypt.MaxCost)
	}
	dummy, err := newDummyHash(r.bcryptCost)
	if err != nil {
		return nil, err
	}
	r.dummyHash = dummy
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

// load reads every principal file and builds the index.
func (r *Registry) load() error {
	owners := filepath.Join(r.dataDir, ownersDirName)
	entries, err := os.ReadDir(owners)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("principals: read %s: %w", owners, err)
	}
	var recs []record
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(owners, e.Name())
		r.removeTempFiles(dir)
		rec, err := r.readRecord(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			r.logger.Warn("skipping unreadable principal file", "dir", dir, "error", err)
			continue
		}
		recs = append(recs, rec)
	}

	// Oldest first, so that the newest claim on a username wins.
	slices.SortFunc(recs, func(a, b record) int {
		if c := a.UpdatedAt.Compare(b.UpdatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Owner, b.Owner)
	})
	for _, rec := range recs {
		if rec.Username != "" {
			if prev, ok := r.byUsername[rec.Username]; ok {
				r.logger.Warn("username claimed by more than one principal, the newer claim wins",
					"username", rec.Username, "owner", rec.Owner, "previous_owner", prev)
				released := r.principals[prev]
				released.Username = ""
				if err := r.writeRecord(released); err != nil {
					r.logger.Error("could not release username in principal file", "owner", prev, "error", err)
				}
				r.principals[prev] = released
			}
			r.byUsername[rec.Username] = rec.Owner
		}
		if prev, ok := r.bySub[rec.Sub]; rec.Sub != "" && ok {
			r.logger.Warn("subject claimed by more than one principal file, the first stays bound",
				"sub", rec.Sub, "owner", rec.Owner, "bound_owner", prev)
		} else if rec.Sub != "" {
			r.bySub[rec.Sub] = rec.Owner
		}
		r.principals[rec.Owner] = rec
	}
	return nil
}

// removeTempFiles deletes temporary principal and app password files in
// dir. Only a crash during a write leaves them behind.
func (r *Registry) removeTempFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		r.logger.Warn("could not read owner directory", "dir", dir, "error", err)
		return
	}
	for _, e := range entries {
		if !slices.ContainsFunc(tempPrefixes, func(p string) bool { return strings.HasPrefix(e.Name(), p) }) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			r.logger.Warn("could not remove temporary file", "path", path, "error", err)
			continue
		}
		r.logger.Info("removed temporary file left by a crash", "path", path)
	}
}

// readRecord reads and checks the principal file in dir.
func (r *Registry) readRecord(dir string) (record, error) {
	data, err := os.ReadFile(filepath.Join(dir, principalFileName))
	if err != nil {
		return record{}, err
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		return record{}, fmt.Errorf("decode: %w", err)
	}
	if rec.Owner == "" {
		rec.Owner = rec.Sub
	}
	if err := store.ValidateOwner(rec.Owner); err != nil {
		return record{}, fmt.Errorf("owner: %w", err)
	}
	if rec.Sub != "" {
		if err := store.ValidateOwner(rec.Sub); err != nil {
			return record{}, fmt.Errorf("sub: %w", err)
		}
	}
	if rec.Username != "" {
		if err := store.ValidateName(rec.Username); err != nil {
			return record{}, fmt.Errorf("username: %w", err)
		}
	}
	if filepath.Clean(r.ownerDir(rec.Owner)) != filepath.Clean(dir) {
		return record{}, fmt.Errorf("file for owner %q is not in its owner directory", rec.Owner)
	}
	return rec, nil
}

// Provision returns the principal for sub and whether this call created it.
// A subject that is not bound to a principal gets a new one that owns its data
// under the subject as owner. A principal that was bound to the subject
// earlier is found through it, whatever its owner is.
// A new principal gets its file and the default address book and calendar.
// For an existing principal, a changed username or display name is written
// to its file.
//
// The username is the most recent login's: if another principal holds it,
// that principal loses it (its Username becomes empty) and a warning is
// logged.
//
// The default collections are ensured until that succeeded once, which the
// principal file records. A crash between writing the principal file and
// creating the collections therefore heals on the next Provision, and a
// default the user deleted later stays deleted. If ensuring them fails,
// Provision returns the error; the principal file exists by then, so the next
// call reports created as false.
//
// A disabled principal gets ErrDisabled before anything is written: a changed
// name is not recorded either.
func (r *Registry) Provision(ctx context.Context, sub, username, displayName string) (Principal, bool, error) {
	if err := store.ValidateOwner(sub); err != nil {
		return Principal{}, false, fmt.Errorf("principals: invalid sub: %w", err)
	}
	if err := store.ValidateName(username); err != nil {
		return Principal{}, false, fmt.Errorf("principals: invalid username: %w", err)
	}

	r.mu.RLock()
	rec, ok := r.principals[r.bySub[sub]]
	r.mu.RUnlock()
	if ok && rec.Disabled {
		return Principal{}, false, ErrDisabled
	}
	if ok && rec.DefaultsCreated && rec.Username == username && rec.DisplayName == displayName {
		return rec.principal(), false, nil
	}

	rec, created, err := r.save(sub, username, displayName)
	if err != nil {
		return Principal{}, false, err
	}
	if !rec.DefaultsCreated {
		// Concurrent first calls may both get here; groupkit makes that safe.
		if err := groupkit.EnsureDefaultCollections(ctx, r.st, rec.Owner); err != nil {
			return Principal{}, false, fmt.Errorf("principals: default collections: %w", err)
		}
		r.markDefaultsCreated(rec.Owner)
	}
	return rec.principal(), created, nil
}

// save writes the principal if it is new or its names changed, and updates
// the index. It returns the current record.
func (r *Registry) save(sub, username, displayName string) (record, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	owner, exists := r.bySub[sub]
	if !exists {
		owner = sub
		if _, taken := r.principals[owner]; taken {
			return record{}, false, fmt.Errorf("principals: %q is the owner of another principal", sub)
		}
	}
	old := r.principals[owner]
	if exists && old.Disabled {
		// Checked again under the lock: the principal may have been disabled
		// since Provision looked.
		return record{}, false, ErrDisabled
	}
	if exists && old.Username == username && old.DisplayName == displayName {
		return old, false, nil
	}
	now := r.now().UTC()
	rec := record{Owner: owner, Sub: sub, CreatedAt: now}
	if exists {
		rec = old
	}
	rec.Username = username
	rec.DisplayName = displayName
	rec.UpdatedAt = now
	if err := r.writeRecord(rec); err != nil {
		return record{}, false, err
	}

	if exists && old.Username != username && r.byUsername[old.Username] == owner {
		delete(r.byUsername, old.Username)
	}
	if holder, ok := r.byUsername[username]; ok && holder != owner {
		r.release(holder, username, now)
	}
	r.byUsername[username] = owner
	r.bySub[sub] = owner
	r.principals[owner] = rec
	return rec, !exists, nil
}

// markDefaultsCreated saves the flag on the current record. It re-reads the
// record under the lock, because a takeover may have released the username
// since the caller looked; writing an older copy would hand the name back.
// A failed write is only logged: the flag stays false, so the next Provision
// ensures again (a no-op once the collections exist) and retries the write.
func (r *Registry) markDefaultsCreated(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.principals[owner]
	if !ok || rec.DefaultsCreated {
		return
	}
	rec.DefaultsCreated = true
	if err := r.writeRecord(rec); err != nil {
		r.logger.Warn("could not save that the default collections exist", "owner", owner, "error", err)
		return
	}
	r.principals[owner] = rec
}

// release takes username away from holder after another principal claimed
// it. The caller holds r.mu. If the file cannot be written, the newer claim
// still wins after a restart, so the error is only logged.
func (r *Registry) release(holder, username string, now time.Time) {
	r.logger.Warn("username moved to another principal", "username", username, "previous_owner", holder)
	rec := r.principals[holder]
	rec.Username = ""
	rec.UpdatedAt = now
	if err := r.writeRecord(rec); err != nil {
		r.logger.Error("could not release username in principal file", "owner", holder, "error", err)
	}
	r.principals[holder] = rec
}

// writeRecord replaces the principal file atomically. It creates the owner
// directory when needed.
func (r *Registry) writeRecord(rec record) error {
	dir := r.ownerDir(rec.Owner)
	if dir == "" {
		return fmt.Errorf("principals: no owner directory for owner %q", rec.Owner)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("principals: encode: %w", err)
	}
	data = append(data, '\n')
	if err := r.mkdirAll(dir); err != nil {
		return fmt.Errorf("principals: create owner directory: %w", err)
	}
	if err := r.writeFileAtomic(dir, principalFileName, data); err != nil {
		return fmt.Errorf("principals: write principal file: %w", err)
	}
	return nil
}

// mkdirAll creates dir and its missing parents, mode 0700, and flushes the
// parent of each directory it creates, so that a new directory survives a
// power loss together with the file written into it.
func (r *Registry) mkdirAll(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(dir)
	if parent != dir {
		if err := r.mkdirAll(parent); err != nil {
			return err
		}
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	if err := r.syncDir(parent); err != nil {
		// Remove the directory, so that a retry creates and flushes it again.
		_ = os.Remove(dir)
		return err
	}
	return nil
}

// writeFileAtomic replaces dir/name with data, mode 0600: it writes a
// temporary file in dir, flushes it and renames it over the old file.
// Readers see either the old or the new content, never a part of it.
//
// Once the rename is done, the new content is what every reader sees. A
// failure to flush the directory after it is logged and not returned, so
// that the caller's view matches the file.
func (r *Registry) writeFileAtomic(dir, name string, data []byte) (err error) {
	tmp, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	if syncErr := r.syncDir(dir); syncErr != nil {
		r.logger.Warn("could not flush directory after writing a file", "dir", dir, "error", syncErr)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

// Get returns the principal that owns owner's data, or ErrNotFound.
func (r *Registry) Get(owner string) (Principal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.principals[owner]
	if !ok {
		return Principal{}, ErrNotFound
	}
	return rec.principal(), nil
}

// BySub returns the principal that is bound to the subject sub, or
// ErrNotFound.
func (r *Registry) BySub(sub string) (Principal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	owner, ok := r.bySub[sub]
	if !ok {
		return Principal{}, ErrNotFound
	}
	return r.principals[owner].principal(), nil
}

// SetDisabled disables or enables the principal and returns it. It returns
// ErrNotFound for an unknown owner. The flag is the only thing it writes:
// updated_at stays, so the action cannot move a username between
// principals, and the app passwords are kept, so enabling restores every
// device without pairing it again. Credentials of the principal that were
// verified recently are forgotten, so the next request after a disable is
// refused.
func (r *Registry) SetDisabled(ctx context.Context, owner string, disabled bool) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.principals[owner]
	if !ok {
		return Principal{}, ErrNotFound
	}
	if rec.Disabled != disabled {
		rec.Disabled = disabled
		if err := r.writeRecord(rec); err != nil {
			return Principal{}, err
		}
		r.principals[owner] = rec
	}
	r.cache.invalidate(owner)
	return rec.principal(), nil
}

// ListPrincipals returns every principal, disabled and nameless ones
// included, sorted by username and then by owner. The nameless come first.
func (r *Registry) ListPrincipals(ctx context.Context) []Principal {
	if ctx.Err() != nil {
		return nil
	}
	r.mu.RLock()
	list := make([]Principal, 0, len(r.principals))
	for _, rec := range r.principals {
		list = append(list, rec.principal())
	}
	r.mu.RUnlock()
	slices.SortFunc(list, func(a, b Principal) int {
		if c := cmp.Compare(a.Username, b.Username); c != 0 {
			return c
		}
		return cmp.Compare(a.Owner, b.Owner)
	})
	return list
}

// LookupUsername returns the owner of the principal that holds username.
func (r *Registry) LookupUsername(username string) (string, bool) {
	if username == "" {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	owner, ok := r.byUsername[username]
	return owner, ok
}

// HasUnbound reports whether some principal has no subject yet.
func (r *Registry) HasUnbound() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rec := range r.principals {
		if rec.Sub == "" {
			return true
		}
	}
	return false
}

// Bind gives the principal that has no subject yet, and whose oCIS id is
// ocisID, the subject sub, and returns it. The second result is true when
// this call made the binding.
//
// A principal that already has a subject is never taken over: it does not
// match, whatever its oCIS id. If sub already belongs to a principal, that
// principal is returned and nothing is bound. Bind returns ErrNotFound when
// no unbound principal has the id (or the id is empty), ErrAmbiguous when
// several have it, and ErrDisabled, with nothing written, when the match is
// disabled. Only the subject changes in the file; updated_at stays, so the
// binding cannot move a username between principals.
func (r *Registry) Bind(ctx context.Context, ocisID, sub string) (Principal, bool, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, false, err
	}
	if err := store.ValidateOwner(sub); err != nil {
		return Principal{}, false, fmt.Errorf("principals: invalid sub: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner, ok := r.bySub[sub]; ok {
		return r.principals[owner].principal(), false, nil
	}
	if ocisID == "" {
		return Principal{}, false, ErrNotFound
	}
	var match []string
	for owner, rec := range r.principals {
		if rec.Sub == "" && rec.OCISID == ocisID {
			match = append(match, owner)
		}
	}
	switch len(match) {
	case 0:
		return Principal{}, false, ErrNotFound
	case 1:
	default:
		return Principal{}, false, ErrAmbiguous
	}
	rec := r.principals[match[0]]
	if rec.Disabled {
		return Principal{}, false, ErrDisabled
	}
	if _, taken := r.principals[sub]; taken {
		return Principal{}, false, fmt.Errorf("principals: %q is the owner of another principal", sub)
	}
	rec.Sub = sub
	if err := r.writeRecord(rec); err != nil {
		return Principal{}, false, err
	}
	r.principals[rec.Owner] = rec
	r.bySub[sub] = rec.Owner
	return rec.principal(), true, nil
}
