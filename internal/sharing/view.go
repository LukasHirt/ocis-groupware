package sharing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/LukasHirt/groupkit/store"
)

// SharedPrefix starts the name under which a shared collection appears in a
// recipient's home. The rest is the collection's store id, which is unique
// and never reused, so the name cannot point at another collection later.
const SharedPrefix = "shared-"

// ViewConfig holds what NewView needs.
type ViewConfig struct {
	// Inner is the store that holds the data. It is never told about sharing.
	Inner store.Store
	// Shares says what is shared with whom.
	Shares *Shares
	// Identity returns the oCIS user id and the group ids a principal had
	// when it last signed in with a token, or empty values when there is no
	// snapshot. It is asked on every request, so a revoked share or a
	// changed group takes effect with the next one.
	Identity func(sub string) (ocisID string, groupIDs []string)
	// OwnerName returns the display name of an owner, for the suffix of a
	// shared collection's name. Nil, or an empty answer, leaves the name
	// without a suffix.
	OwnerName func(sub string) string
	// OwnerDir returns the directory of a principal's data, where the
	// recipient's overrides are kept.
	OwnerDir func(sub string) string
	// Logger receives faults. Nil discards them.
	Logger *slog.Logger
}

// View is a store.Store for callers who may see more than their own
// collections. Every method treats ref.Owner as the caller, as the DAV
// engines do. A collection shared with the caller appears among the caller's
// own, as "shared-<id>", reports the access level of the share, and forwards
// to the owner's collection. Everything else goes to the inner store.
type View struct {
	inner     store.Store
	shares    *Shares
	identity  func(string) (string, []string)
	ownerName func(string) string
	overrides *overrides
	logger    *slog.Logger
}

var _ store.Store = (*View)(nil)

// NewView returns a View over cfg.Inner.
func NewView(cfg ViewConfig) (*View, error) {
	switch {
	case cfg.Inner == nil:
		return nil, errors.New("sharing: inner store is nil")
	case cfg.Shares == nil:
		return nil, errors.New("sharing: shares is nil")
	case cfg.Identity == nil:
		return nil, errors.New("sharing: identity function is nil")
	case cfg.OwnerDir == nil:
		return nil, errors.New("sharing: owner directory function is nil")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &View{
		inner: cfg.Inner, shares: cfg.Shares, identity: cfg.Identity, ownerName: cfg.OwnerName,
		overrides: newOverrides(cfg.OwnerDir, logger), logger: logger,
	}, nil
}

// received lists what is shared with the caller, without the caller's own
// collections.
func (v *View) received(sub string) []Received {
	id, groups := v.identity(sub)
	all := v.shares.Received(id, groups)
	return slices.DeleteFunc(all, func(r Received) bool { return r.Owner == sub })
}

// find returns the share that ref names for its caller, or false when ref
// is not the name of a shared collection or nothing is shared under it.
func (v *View) find(ref store.CollectionRef) (Received, bool) {
	id, ok := strings.CutPrefix(ref.Name, SharedPrefix)
	if !ok || id == "" {
		return Received{}, false
	}
	for _, r := range v.received(ref.Owner) {
		if r.CollectionID == id && r.Kind == ref.Kind {
			return r, true
		}
	}
	return Received{}, false
}

// target is a collection of the owner that was reached through a share.
type target struct {
	recv Received
	col  store.Collection
	// access is the store access level of the share.
	access store.Access
}

// resolve returns the shared collection that ref names, or false when ref
// is not a shared name. A share whose collection is gone, or was replaced
// by another one of the same name, is store.ErrNotFound.
func (v *View) resolve(ctx context.Context, ref store.CollectionRef) (target, bool, error) {
	recv, ok := v.find(ref)
	if !ok {
		return target{}, false, nil
	}
	col, err := v.inner.GetCollection(ctx, recv.Ref())
	if err != nil {
		return target{}, true, err
	}
	if col.ID != recv.CollectionID {
		return target{}, true, fmt.Errorf("%w: the shared collection was replaced", store.ErrNotFound)
	}
	acc, _ := recv.Access.StoreAccess()
	return target{recv: recv, col: col, access: acc}, true, nil
}

// present shows a shared collection as the caller sees it.
func (v *View) present(caller string, t target) store.Collection {
	c := t.col
	props := c.Props
	name := props.DisplayName
	if name == "" {
		name = c.Ref.Name
	}
	if owner := v.owner(t.recv.Owner); owner != "" {
		name += " (" + owner + ")"
	}
	props.DisplayName = name
	ov := v.overrides.get(caller, t.recv.CollectionID)
	if ov.DisplayName != nil {
		props.DisplayName = *ov.DisplayName
	}
	if ov.Color != nil {
		props.Color = *ov.Color
	}
	if ov.Order != nil {
		props.Order = *ov.Order
	}
	return store.Collection{
		Ref:      store.CollectionRef{Owner: caller, Kind: t.recv.Kind, Name: SharedPrefix + t.recv.CollectionID},
		ID:       c.ID,
		Props:    props,
		Revision: c.Revision,
		Created:  c.Created,
		Access:   t.access,
	}
}

func (v *View) owner(sub string) string {
	if v.ownerName == nil {
		return ""
	}
	return v.ownerName(sub)
}

func denied(op string, what string) error {
	return fmt.Errorf("%w: %s on %s", store.ErrPermission, op, what)
}

// ListCollections returns the caller's own collections of one kind and the
// ones shared with the caller, sorted by name. Collections the caller
// hid are left out.
func (v *View) ListCollections(ctx context.Context, owner string, kind store.Kind) ([]store.Collection, error) {
	cols, err := v.inner.ListCollections(ctx, owner, kind)
	if err != nil {
		return nil, err
	}
	for _, r := range v.received(owner) {
		if r.Kind != kind {
			continue
		}
		if v.overrides.get(owner, r.CollectionID).Hidden {
			continue
		}
		t, _, err := v.resolve(ctx, store.CollectionRef{Owner: owner, Kind: kind, Name: SharedPrefix + r.CollectionID})
		switch {
		case err == nil:
			cols = append(cols, v.present(owner, t))
		case errors.Is(err, store.ErrNotFound):
			// Deleted or replaced behind the share's back; the share is stale.
			v.logger.Debug("share points at a collection that is gone", "owner", r.Owner, "collection", r.CollectionID)
		default:
			return nil, err
		}
	}
	slices.SortFunc(cols, func(a, b store.Collection) int { return strings.Compare(a.Ref.Name, b.Ref.Name) })
	return cols, nil
}

// GetCollection returns an own or a shared collection.
func (v *View) GetCollection(ctx context.Context, ref store.CollectionRef) (store.Collection, error) {
	t, shared, err := v.resolve(ctx, ref)
	if !shared {
		return v.inner.GetCollection(ctx, ref)
	}
	if err != nil {
		return store.Collection{}, err
	}
	return v.present(ref.Owner, t), nil
}

// CreateCollection makes an own collection. The name of a collection that is
// shared with the caller is taken.
func (v *View) CreateCollection(ctx context.Context, ref store.CollectionRef, props store.Props) (store.Collection, error) {
	if _, ok := v.find(ref); ok {
		return store.Collection{}, store.ErrExists
	}
	return v.inner.CreateCollection(ctx, ref, props)
}

// UpdateCollection changes an own collection. On a shared one the caller may
// change the display name, colour and order, which are kept for the caller
// alone; an empty name or colour removes the change. Anything else belongs
// to the owner.
func (v *View) UpdateCollection(ctx context.Context, ref store.CollectionRef, update store.PropsUpdate) (store.Collection, error) {
	t, shared, err := v.resolve(ctx, ref)
	if !shared {
		return v.inner.UpdateCollection(ctx, ref, update)
	}
	if err != nil {
		return store.Collection{}, err
	}
	if update.Description != nil || update.Timezone != nil || update.Components != nil {
		return store.Collection{}, denied("UpdateCollection", "a shared collection's description, time zone or components")
	}
	_, err = v.overrides.update(ref.Owner, t.recv.CollectionID, func(o *override) {
		if update.DisplayName != nil {
			o.DisplayName = nonEmpty(*update.DisplayName)
		}
		if update.Color != nil {
			o.Color = nonEmpty(*update.Color)
		}
		if update.Order != nil {
			n := *update.Order
			o.Order = &n
		}
	})
	if err != nil {
		return store.Collection{}, err
	}
	return v.present(ref.Owner, t), nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// SetHidden hides a shared collection from the recipient's listings, or
// shows it again. A hidden collection stays reachable by its name.
func (v *View) SetHidden(ctx context.Context, recipient, collectionID string, hidden bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := v.overrides.update(recipient, collectionID, func(o *override) { o.Hidden = hidden })
	return err
}

// DeleteCollection deletes an own collection and ends its shares. A recipient
// cannot delete a shared collection.
func (v *View) DeleteCollection(ctx context.Context, ref store.CollectionRef) error {
	if _, shared, err := v.resolve(ctx, ref); shared {
		if err != nil {
			return err
		}
		return denied("DeleteCollection", "a shared collection")
	}
	if err := v.inner.DeleteCollection(ctx, ref); err != nil {
		return err
	}
	if err := v.shares.RemoveCollection(ctx, ref); err != nil {
		// The collection is gone, and a share keeps the id of the collection
		// it was made for, so it cannot reach a newcomer. Startup removes it.
		v.logger.Error("could not remove the shares of a deleted collection", "owner", ref.Owner, "kind", ref.Kind, "name", ref.Name, "error", err)
	}
	return nil
}

// route returns the collection that holds the objects ref addresses, and
// the access the caller has to it.
func (v *View) route(ctx context.Context, ref store.CollectionRef) (store.CollectionRef, store.Access, error) {
	t, shared, err := v.resolve(ctx, ref)
	if !shared {
		return ref, store.AccessFull, nil
	}
	if err != nil {
		return store.CollectionRef{}, 0, err
	}
	return t.recv.Ref(), t.access, nil
}

// GetObject reads an object of an own or a shared collection.
func (v *View) GetObject(ctx context.Context, ref store.CollectionRef, name string) (store.Object, error) {
	real, _, err := v.route(ctx, ref)
	if err != nil {
		return store.Object{}, err
	}
	return v.inner.GetObject(ctx, real, name)
}

// ListObjects lists the objects of an own or a shared collection.
func (v *View) ListObjects(ctx context.Context, ref store.CollectionRef, opts store.ListOptions) ([]store.Object, error) {
	real, _, err := v.route(ctx, ref)
	if err != nil {
		return nil, err
	}
	return v.inner.ListObjects(ctx, real, opts)
}

// PutObject writes an object, if the caller's access allows it.
func (v *View) PutObject(ctx context.Context, ref store.CollectionRef, name string, data []byte, opts store.PutOptions) (store.Object, error) {
	real, acc, err := v.route(ctx, ref)
	if err != nil {
		return store.Object{}, err
	}
	if !acc.AllowsObjectWrites() {
		return store.Object{}, denied("PutObject", "a read-only shared collection")
	}
	return v.inner.PutObject(ctx, real, name, data, opts)
}

// DeleteObject deletes an object, if the caller's access allows it.
func (v *View) DeleteObject(ctx context.Context, ref store.CollectionRef, name string, opts store.DeleteOptions) error {
	real, acc, err := v.route(ctx, ref)
	if err != nil {
		return err
	}
	if !acc.AllowsObjectWrites() {
		return denied("DeleteObject", "a read-only shared collection")
	}
	return v.inner.DeleteObject(ctx, real, name, opts)
}

// Changes lists what changed in an own or a shared collection. A shared
// collection counts revisions as its owner's does, so a sync token from the
// owner's side is as good on the recipient's.
func (v *View) Changes(ctx context.Context, ref store.CollectionRef, since uint64) (store.Changes, error) {
	real, _, err := v.route(ctx, ref)
	if err != nil {
		return store.Changes{}, err
	}
	return v.inner.Changes(ctx, real, since)
}
