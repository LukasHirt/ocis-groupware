// Package sharing lets the owner of a calendar or address book give other
// users access to it. A share names its grantee, a user or a group, by the
// identifier of the directory that holds them, and an access level.
//
// The shares of a collection live in the owner's directory, in shares.json,
// next to the data they describe. The recipient side is an in-memory index
// built from those files when the service starts and kept in step by every
// change, so nothing is stored twice.
package sharing

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
	"unicode/utf8"

	"github.com/LukasHirt/groupkit/store"
)

// FileName is the name of the file that holds an owner's shares, inside the
// owner's directory.
const FileName = "shares.json"

const (
	// MaxMembers is the most grantees one collection can have.
	MaxMembers = 200

	maxIDBytes   = 256
	maxNameBytes = 256

	fileVersion = 1
)

// GranteeType says whether a share names a user or a group.
type GranteeType string

const (
	// User names one user.
	User GranteeType = "user"
	// Group names a group. Its members come from each recipient's own group
	// snapshot, so the service never needs a member list.
	Group GranteeType = "group"
)

// Access is the level a share gives.
type Access string

const (
	// Read lets the grantee read the collection and its objects.
	Read Access = "read"
	// ReadWrite lets the grantee also create, change and delete objects. The
	// collection itself stays with its owner.
	ReadWrite Access = "readwrite"
)

// StoreAccess returns the store access level of a, and false for a value
// that is not an access level.
func (a Access) StoreAccess() (store.Access, bool) {
	switch a {
	case Read:
		return store.AccessRead, true
	case ReadWrite:
		return store.AccessObjects, true
	}
	return 0, false
}

// Member is one grantee of a collection.
type Member struct {
	// Type says whether ID names a user or a group.
	Type GranteeType `json:"type"`
	// ID is the grantee's identifier in the directory.
	ID string `json:"id"`
	// Name is the grantee's display name when the share was made. It is for
	// listings only; nothing decides on it.
	Name string `json:"name"`
	// Access is the level the grantee gets.
	Access Access `json:"access"`
}

var (
	// ErrInvalidMember means a member or a collection id is not acceptable.
	ErrInvalidMember = errors.New("sharing: invalid member")
	// ErrSelfShare means a collection was shared with its owner.
	ErrSelfShare = errors.New("sharing: cannot share with the owner")
	// ErrTooManyMembers means a collection would have more than MaxMembers
	// grantees.
	ErrTooManyMembers = errors.New("sharing: too many members")
	// ErrNotShared means the collection is not shared with that grantee.
	ErrNotShared = errors.New("sharing: not shared with this grantee")
)

// Received is a collection shared with someone, as the recipient's index
// holds it.
type Received struct {
	// Owner is the owner of the collection.
	Owner string
	// Kind and Name address the collection in the owner's data.
	Kind store.Kind
	Name string
	// CollectionID is the id the store gave the collection when it was
	// shared. A collection that was deleted and created again has another
	// id, so a share cannot attach itself to the newcomer.
	CollectionID string
	// Access is the level the recipient has. When a user and a group grant
	// reach the same recipient, the higher one counts.
	Access Access
}

// Ref returns the reference of the collection in its owner's data.
func (r Received) Ref() store.CollectionRef {
	return store.CollectionRef{Owner: r.Owner, Kind: r.Kind, Name: r.Name}
}

// Config holds what Open needs.
type Config struct {
	// OwnerDir returns the directory of an owner's data, or "" for an owner
	// that has none. The file store's OwnerDir does this.
	OwnerDir func(owner string) string
	// Owners lists the owners whose shares are loaded.
	Owners func() []string
	// OCISID returns the directory identifier of the user who is the owner,
	// or "" when it is not known. It is used to refuse a share with oneself.
	OCISID func(owner string) string
	// Logger receives the files that could not be read. Nil discards.
	Logger *slog.Logger
}

// Shares keeps the shares of all owners. It is safe for concurrent use.
type Shares struct {
	cfg    Config
	logger *slog.Logger

	// mu guards the maps. Writers hold it while they write a file, so that
	// the files and the maps change together.
	mu      sync.RWMutex
	byOwner map[string]ownerShares
	// index maps "user:<id>" and "group:<id>" to what that grantee receives.
	index map[string]map[collKey]Received
}

type collKey struct {
	owner string
	id    string
}

// ownerShares is the content of one shares.json.
type ownerShares struct {
	Version     int                `json:"version"`
	Collections map[string]*shared `json:"collections"`
}

// shared holds the members of one collection.
type shared struct {
	ID      string   `json:"id"`
	Members []Member `json:"members"`
}

// Open loads the shares of the configured owners. A file it cannot read or
// does not understand is logged and skipped: its owner shares nothing until
// the file is fixed, which fails closed. Temporary files left by a crash
// during a write are removed.
func Open(cfg Config) (*Shares, error) {
	switch {
	case cfg.OwnerDir == nil:
		return nil, errors.New("sharing: owner directory function is nil")
	case cfg.Owners == nil:
		return nil, errors.New("sharing: owner list function is nil")
	}
	s := &Shares{
		cfg:     cfg,
		logger:  cfg.Logger,
		byOwner: make(map[string]ownerShares),
		index:   make(map[string]map[collKey]Received),
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	for _, owner := range cfg.Owners() {
		dir := cfg.OwnerDir(owner)
		if dir == "" {
			continue
		}
		removeTempFiles(dir, FileName, OverridesFileName)
		data, err := os.ReadFile(filepath.Join(dir, FileName))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			s.logger.Warn("cannot read shares, the owner shares nothing", "owner", owner, "error", err)
			continue
		}
		var st ownerShares
		if err := json.Unmarshal(data, &st); err != nil || st.Version != fileVersion {
			s.logger.Warn("cannot understand shares, the owner shares nothing", "owner", owner, "version", st.Version, "error", err)
			continue
		}
		s.byOwner[owner] = st
		s.indexOwner(owner, st)
	}
	return s, nil
}

func key(t GranteeType, id string) string { return string(t) + ":" + id }

func (s *Shares) indexOwner(owner string, st ownerShares) {
	for ref, sh := range st.Collections {
		kind, name, ok := splitRef(ref)
		if !ok {
			continue
		}
		for _, m := range sh.Members {
			k := key(m.Type, m.ID)
			if s.index[k] == nil {
				s.index[k] = make(map[collKey]Received)
			}
			s.index[k][collKey{owner, sh.ID}] = Received{
				Owner: owner, Kind: kind, Name: name, CollectionID: sh.ID, Access: m.Access,
			}
		}
	}
}

func (s *Shares) unindexOwner(owner string, st ownerShares) {
	for _, sh := range st.Collections {
		for _, m := range sh.Members {
			k := key(m.Type, m.ID)
			delete(s.index[k], collKey{owner, sh.ID})
			if len(s.index[k]) == 0 {
				delete(s.index, k)
			}
		}
	}
}

func refKey(ref store.CollectionRef) string { return string(ref.Kind) + "/" + ref.Name }

func splitRef(k string) (store.Kind, string, bool) {
	kind, name, ok := strings.Cut(k, "/")
	if !ok || name == "" || !store.Kind(kind).Valid() {
		return "", "", false
	}
	return store.Kind(kind), name, true
}

// Members returns the grantees of a collection, in the order they were
// added, or nil when it is not shared.
func (s *Shares) Members(ref store.CollectionRef) []Member {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sh := s.byOwner[ref.Owner].Collections[refKey(ref)]
	if sh == nil {
		return nil
	}
	return slices.Clone(sh.Members)
}

// Count returns how many grants the owner's collections have in all.
func (s *Shares) Count(owner string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, sh := range s.byOwner[owner].Collections {
		n += len(sh.Members)
	}
	return n
}

// Received returns what a user receives: the collections shared with the
// user's own id or with any of the groups, without the user's own
// collections. When several grants reach one collection the highest access
// counts. The result is sorted by owner, kind and name.
func (s *Shares) Received(userID string, groupIDs []string) []Received {
	s.mu.RLock()
	defer s.mu.RUnlock()
	got := map[collKey]Received{}
	add := func(k string) {
		for ck, r := range s.index[k] {
			if cur, ok := got[ck]; !ok || r.Access == ReadWrite && cur.Access != ReadWrite {
				got[ck] = r
			}
		}
	}
	if userID != "" {
		add(key(User, userID))
	}
	for _, g := range groupIDs {
		if g != "" {
			add(key(Group, g))
		}
	}
	out := make([]Received, 0, len(got))
	for _, r := range got {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Received) int {
		return cmp.Or(cmp.Compare(a.Owner, b.Owner), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	return out
}

// Grant shares the collection with the member, or changes the member's name
// and access when the collection is already shared with that user or group.
// collectionID is the store's id of the collection.
func (s *Shares) Grant(ctx context.Context, ref store.CollectionRef, collectionID string, m Member) error {
	if err := s.check(ref, collectionID, m); err != nil {
		return err
	}
	return s.change(ctx, ref, func(st *ownerShares) error {
		sh := st.entry(ref, collectionID)
		if i := slices.IndexFunc(sh.Members, func(x Member) bool { return x.Type == m.Type && x.ID == m.ID }); i >= 0 {
			sh.Members[i] = m
			return nil
		}
		if len(sh.Members) >= MaxMembers {
			return fmt.Errorf("%w: at most %d", ErrTooManyMembers, MaxMembers)
		}
		sh.Members = append(sh.Members, m)
		return nil
	})
}

// Set replaces all members of the collection. An empty list stops sharing it.
func (s *Shares) Set(ctx context.Context, ref store.CollectionRef, collectionID string, members []Member) error {
	if len(members) > MaxMembers {
		return fmt.Errorf("%w: at most %d", ErrTooManyMembers, MaxMembers)
	}
	seen := map[string]bool{}
	for _, m := range members {
		if err := s.check(ref, collectionID, m); err != nil {
			return err
		}
		k := key(m.Type, m.ID)
		if seen[k] {
			return fmt.Errorf("%w: %s listed twice", ErrInvalidMember, k)
		}
		seen[k] = true
	}
	return s.change(ctx, ref, func(st *ownerShares) error {
		delete(st.Collections, refKey(ref))
		if len(members) > 0 {
			st.entry(ref, collectionID).Members = slices.Clone(members)
		}
		return nil
	})
}

// Revoke stops sharing the collection with a user or group. It returns
// ErrNotShared when the collection was not shared with it.
func (s *Shares) Revoke(ctx context.Context, ref store.CollectionRef, t GranteeType, id string) error {
	return s.change(ctx, ref, func(st *ownerShares) error {
		sh := st.Collections[refKey(ref)]
		if sh == nil {
			return ErrNotShared
		}
		i := slices.IndexFunc(sh.Members, func(x Member) bool { return x.Type == t && x.ID == id })
		if i < 0 {
			return ErrNotShared
		}
		sh.Members = slices.Delete(sh.Members, i, i+1)
		if len(sh.Members) == 0 {
			delete(st.Collections, refKey(ref))
		}
		return nil
	})
}

// RemoveCollection drops all shares of a collection, for a collection that
// was deleted. A collection that was not shared is not an error.
func (s *Shares) RemoveCollection(ctx context.Context, ref store.CollectionRef) error {
	return s.change(ctx, ref, func(st *ownerShares) error {
		delete(st.Collections, refKey(ref))
		return nil
	})
}

// RemoveOwner drops all shares of an owner, for an owner that was purged. It
// returns how many grants it removed and deletes the owner's shares file.
func (s *Shares) RemoveOwner(ctx context.Context, owner string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.byOwner[owner]
	n := 0
	for _, sh := range old.Collections {
		n += len(sh.Members)
	}
	if dir := s.cfg.OwnerDir(owner); dir != "" {
		if err := os.Remove(filepath.Join(dir, FileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("sharing: remove %s: %w", FileName, err)
		}
	}
	s.unindexOwner(owner, old)
	delete(s.byOwner, owner)
	return n, nil
}

// Prune drops the shares of collections that no longer exist in st, or that
// exist under the same name with another id, and returns how many it
// dropped. A collection can disappear without its shares knowing, for
// example when the service stops between deleting the collection and the
// shares. It stops at the first store error other than "not found" and
// keeps every share it has not checked.
func (s *Shares) Prune(ctx context.Context, st store.Store) (int, error) {
	type entry struct {
		ref store.CollectionRef
		id  string
	}
	s.mu.RLock()
	var all []entry
	for owner, shares := range s.byOwner {
		for k, sh := range shares.Collections {
			if kind, name, ok := splitRef(k); ok {
				all = append(all, entry{store.CollectionRef{Owner: owner, Kind: kind, Name: name}, sh.ID})
			}
		}
	}
	s.mu.RUnlock()

	var stale []entry
	for _, e := range all {
		c, err := st.GetCollection(ctx, e.ref)
		switch {
		case err == nil && c.ID == e.id:
		case err == nil || errors.Is(err, store.ErrNotFound):
			stale = append(stale, e)
		default:
			return 0, fmt.Errorf("sharing: check %s %q of %q: %w", e.ref.Kind, e.ref.Name, e.ref.Owner, err)
		}
	}
	n := 0
	for _, e := range stale {
		if err := s.RemoveCollection(ctx, e.ref); err != nil {
			return n, err
		}
		s.logger.Info("dropped the shares of a collection that is gone", "owner", e.ref.Owner, "kind", e.ref.Kind, "name", e.ref.Name)
		n++
	}
	return n, nil
}

func (st *ownerShares) entry(ref store.CollectionRef, collectionID string) *shared {
	if st.Collections == nil {
		st.Collections = map[string]*shared{}
	}
	sh := st.Collections[refKey(ref)]
	if sh == nil || sh.ID != collectionID {
		sh = &shared{ID: collectionID}
		st.Collections[refKey(ref)] = sh
	}
	return sh
}

// change applies mutate to a copy of the owner's shares, writes the copy,
// and only then makes it the current state and updates the index. A refused
// or failed change leaves everything as it was.
func (s *Shares) change(ctx context.Context, ref store.CollectionRef, mutate func(*ownerShares) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.ValidateRef(ref); err != nil {
		return err
	}
	dir := s.cfg.OwnerDir(ref.Owner)
	if dir == "" {
		return fmt.Errorf("sharing: no owner directory for %q", ref.Owner)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.byOwner[ref.Owner]
	next := old.clone()
	if err := mutate(&next); err != nil {
		return err
	}
	if len(next.Collections) == 0 {
		if err := os.Remove(filepath.Join(dir, FileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("sharing: remove %s: %w", FileName, err)
		}
		s.unindexOwner(ref.Owner, old)
		delete(s.byOwner, ref.Owner)
		return nil
	}
	next.Version = fileVersion
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("sharing: encode: %w", err)
	}
	if err := writeFileAtomic(dir, FileName, append(data, '\n'), s.logger); err != nil {
		return fmt.Errorf("sharing: write %s: %w", FileName, err)
	}
	s.unindexOwner(ref.Owner, old)
	s.byOwner[ref.Owner] = next
	s.indexOwner(ref.Owner, next)
	return nil
}

func (st ownerShares) clone() ownerShares {
	out := ownerShares{Version: st.Version, Collections: make(map[string]*shared, len(st.Collections))}
	for k, sh := range st.Collections {
		out.Collections[k] = &shared{ID: sh.ID, Members: slices.Clone(sh.Members)}
	}
	return out
}

func (s *Shares) check(ref store.CollectionRef, collectionID string, m Member) error {
	if err := store.ValidateRef(ref); err != nil {
		return err
	}
	switch {
	case collectionID == "":
		return fmt.Errorf("%w: empty collection id", ErrInvalidMember)
	case m.Type != User && m.Type != Group:
		return fmt.Errorf("%w: type %q is not user or group", ErrInvalidMember, m.Type)
	case m.ID == "" || len(m.ID) > maxIDBytes || !validText(m.ID):
		return fmt.Errorf("%w: id is empty, too long or not text", ErrInvalidMember)
	case len(m.Name) > maxNameBytes || !validText(m.Name):
		return fmt.Errorf("%w: name is too long or not text", ErrInvalidMember)
	}
	if _, ok := m.Access.StoreAccess(); !ok {
		return fmt.Errorf("%w: access %q is not read or readwrite", ErrInvalidMember, m.Access)
	}
	if m.Type == User && s.cfg.OCISID != nil {
		if own := s.cfg.OCISID(ref.Owner); own != "" && own == m.ID {
			return ErrSelfShare
		}
	}
	return nil
}

func validText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
