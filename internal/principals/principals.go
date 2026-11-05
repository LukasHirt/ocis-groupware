// Package principals keeps the registry of users (principals) that have used
// the service. Each principal is a file, principal.json, in the owner's
// directory of the groupkit file store. An in-memory index maps usernames to
// subjects.
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
	// Sub is the subject from the identity provider. It never changes and
	// owns the user's data.
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
)

// record is the content of principal.json.
type record struct {
	Sub         string    `json:"sub"`
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
}

func (rec record) principal() Principal {
	return Principal{
		Sub: rec.Sub, Username: rec.Username, DisplayName: rec.DisplayName,
		CreatedAt: rec.CreatedAt, Disabled: rec.Disabled,
	}
}

// tempPrefixes start the names of files that are being written.
var tempPrefixes = []string{
	"." + principalFileName + ".tmp-",
	"." + appPasswordsFileName + ".tmp-",
}

// Registry keeps principals in <ownerDir(sub)>/principal.json and a username
// index in memory. It is safe for concurrent use.
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
	principals map[string]record // by sub
	byUsername map[string]string // username to sub

	// App passwords, see apppasswords.go.
	bcryptCost int
	compare    func(hash, password []byte) error
	cache      *credentialCache
	// dummyHash is compared against when a check has no stored hash, so
	// that it costs the same as checking a wrong password.
	dummyHash []byte
	appsMu    sync.Mutex
	apps      map[string]*appSet // by sub
	// beforeCachePut, if set, runs in VerifyBasic between the last check of
	// the app password and storing it in the cache. Tests use it to revoke
	// the password in exactly that gap.
	beforeCachePut func()
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
		bcryptCost: defaultBcryptCost,
		compare:    bcrypt.CompareHashAndPassword,
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
		return cmp.Compare(a.Sub, b.Sub)
	})
	for _, rec := range recs {
		if rec.Username != "" {
			if prev, ok := r.byUsername[rec.Username]; ok {
				r.logger.Warn("username claimed by more than one principal, the newer claim wins",
					"username", rec.Username, "sub", rec.Sub, "previous_sub", prev)
				released := r.principals[prev]
				released.Username = ""
				if err := r.writeRecord(released); err != nil {
					r.logger.Error("could not release username in principal file", "sub", prev, "error", err)
				}
				r.principals[prev] = released
			}
			r.byUsername[rec.Username] = rec.Sub
		}
		r.principals[rec.Sub] = rec
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
	if err := store.ValidateOwner(rec.Sub); err != nil {
		return record{}, fmt.Errorf("sub: %w", err)
	}
	if rec.Username != "" {
		if err := store.ValidateName(rec.Username); err != nil {
			return record{}, fmt.Errorf("username: %w", err)
		}
	}
	if filepath.Clean(r.ownerDir(rec.Sub)) != filepath.Clean(dir) {
		return record{}, fmt.Errorf("file for sub %q is not in its owner directory", rec.Sub)
	}
	return rec, nil
}

// Provision returns the principal for sub and whether this call created it.
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
	rec, ok := r.principals[sub]
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
		if err := groupkit.EnsureDefaultCollections(ctx, r.st, sub); err != nil {
			return Principal{}, false, fmt.Errorf("principals: default collections: %w", err)
		}
		r.markDefaultsCreated(sub)
	}
	return rec.principal(), created, nil
}

// save writes the principal if it is new or its names changed, and updates
// the index. It returns the current record.
func (r *Registry) save(sub, username, displayName string) (record, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	old, exists := r.principals[sub]
	if exists && old.Disabled {
		// Checked again under the lock: the principal may have been disabled
		// since Provision looked.
		return record{}, false, ErrDisabled
	}
	if exists && old.Username == username && old.DisplayName == displayName {
		return old, false, nil
	}
	now := r.now().UTC()
	rec := record{Sub: sub, CreatedAt: now}
	if exists {
		rec = old
	}
	rec.Username = username
	rec.DisplayName = displayName
	rec.UpdatedAt = now
	if err := r.writeRecord(rec); err != nil {
		return record{}, false, err
	}

	if exists && old.Username != username && r.byUsername[old.Username] == sub {
		delete(r.byUsername, old.Username)
	}
	if holder, ok := r.byUsername[username]; ok && holder != sub {
		r.release(holder, username, now)
	}
	r.byUsername[username] = sub
	r.principals[sub] = rec
	return rec, !exists, nil
}

// markDefaultsCreated saves the flag on the current record. It re-reads the
// record under the lock, because a takeover may have released the username
// since the caller looked; writing an older copy would hand the name back.
// A failed write is only logged: the flag stays false, so the next Provision
// ensures again (a no-op once the collections exist) and retries the write.
func (r *Registry) markDefaultsCreated(sub string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.principals[sub]
	if !ok || rec.DefaultsCreated {
		return
	}
	rec.DefaultsCreated = true
	if err := r.writeRecord(rec); err != nil {
		r.logger.Warn("could not save that the default collections exist", "sub", sub, "error", err)
		return
	}
	r.principals[sub] = rec
}

// release takes username away from holder after another principal claimed
// it. The caller holds r.mu. If the file cannot be written, the newer claim
// still wins after a restart, so the error is only logged.
func (r *Registry) release(holder, username string, now time.Time) {
	r.logger.Warn("username moved to another principal", "username", username, "previous_sub", holder)
	rec := r.principals[holder]
	rec.Username = ""
	rec.UpdatedAt = now
	if err := r.writeRecord(rec); err != nil {
		r.logger.Error("could not release username in principal file", "sub", holder, "error", err)
	}
	r.principals[holder] = rec
}

// writeRecord replaces the principal file atomically. It creates the owner
// directory when needed.
func (r *Registry) writeRecord(rec record) error {
	dir := r.ownerDir(rec.Sub)
	if dir == "" {
		return fmt.Errorf("principals: no owner directory for sub %q", rec.Sub)
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

// Get returns the principal for sub, or ErrNotFound.
func (r *Registry) Get(sub string) (Principal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.principals[sub]
	if !ok {
		return Principal{}, ErrNotFound
	}
	return rec.principal(), nil
}

// SetDisabled disables or enables the principal and returns it. It returns
// ErrNotFound for an unknown sub. The flag is the only thing it writes:
// updated_at stays, so the action cannot move a username between
// principals, and the app passwords are kept, so enabling restores every
// device without pairing it again. Credentials of the principal that were
// verified recently are forgotten, so the next request after a disable is
// refused.
func (r *Registry) SetDisabled(ctx context.Context, sub string, disabled bool) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.principals[sub]
	if !ok {
		return Principal{}, ErrNotFound
	}
	if rec.Disabled != disabled {
		rec.Disabled = disabled
		if err := r.writeRecord(rec); err != nil {
			return Principal{}, err
		}
		r.principals[sub] = rec
	}
	r.cache.invalidate(sub)
	return rec.principal(), nil
}

// ListPrincipals returns every principal, disabled and nameless ones
// included, sorted by username and then by sub. The nameless come first.
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
		return cmp.Compare(a.Sub, b.Sub)
	})
	return list
}

// LookupUsername returns the sub of the principal that holds username.
func (r *Registry) LookupUsername(username string) (string, bool) {
	if username == "" {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	sub, ok := r.byUsername[username]
	return sub, ok
}
