package graphapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/vcard"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

// ContactFolderEntry is one address book a user sees: their own, or one
// another user shares with them.
type ContactFolderEntry struct {
	// Collection is the address book. Its Ref names the real owner's
	// collection, and its Access is what the user may do with it.
	Collection store.Collection
	// OwnerName is the display name of the address book's owner.
	OwnerName string
}

// ContactFolderSource decides which address books a user sees. The default
// source lists the user's own; a source that knows about sharing adds the
// ones shared with the user.
type ContactFolderSource interface {
	// ContactFolders returns the address books the user sees, in no
	// particular order.
	ContactFolders(ctx context.Context, user principals.Principal) ([]ContactFolderEntry, error)
}

// ownContactFolders is the ContactFolderSource that shows a user's own
// address books only.
type ownContactFolders struct{ st store.Store }

func (o ownContactFolders) ContactFolders(ctx context.Context, user principals.Principal) ([]ContactFolderEntry, error) {
	cols, err := o.st.ListCollections(ctx, user.Owner, store.KindAddressBook)
	if err != nil {
		return nil, err
	}
	out := make([]ContactFolderEntry, len(cols))
	for i, c := range cols {
		out[i] = ContactFolderEntry{Collection: c, OwnerName: ownerName(user)}
	}
	return out, nil
}

func (a *Handler) contactRoutes() []route {
	r := func(method, pattern string, h func(http.ResponseWriter, *http.Request, *call)) route {
		return route{method: method, segments: strings.Split(pattern, "/"), handle: h}
	}
	return []route{
		r(http.MethodGet, "me/contactFolders", a.listContactFolders),
		r(http.MethodPost, "me/contactFolders", a.createContactFolder),
		r(http.MethodGet, "me/contactFolders/{id}", a.getContactFolder),
		r(http.MethodPatch, "me/contactFolders/{id}", a.patchContactFolder),
		r(http.MethodDelete, "me/contactFolders/{id}", a.deleteContactFolder),
		r(http.MethodGet, "me/contacts", a.listContacts(true)),
		r(http.MethodPost, "me/contacts", a.createContact(true)),
		r(http.MethodGet, "me/contacts/{cid}", a.getContact(false)),
		r(http.MethodPatch, "me/contacts/{cid}", a.patchContact(false)),
		r(http.MethodDelete, "me/contacts/{cid}", a.deleteContact(false)),
		r(http.MethodGet, "me/contactFolders/{id}/contacts", a.listContacts(false)),
		r(http.MethodPost, "me/contactFolders/{id}/contacts", a.createContact(false)),
		r(http.MethodGet, "me/contactFolders/{id}/contacts/{cid}", a.getContact(true)),
		r(http.MethodPatch, "me/contactFolders/{id}/contacts/{cid}", a.patchContact(true)),
		r(http.MethodDelete, "me/contactFolders/{id}/contacts/{cid}", a.deleteContact(true)),
	}
}

// contactFolderJSON is a Graph contactFolder. The groupware fields say what
// the user may do with it, which Graph has no fields for.
type contactFolderJSON struct {
	ID          string       `json:"id"`
	DisplayName string       `json:"displayName"`
	IsDefault   bool         `json:"groupware.isDefault"`
	CanEdit     bool         `json:"groupware.canEdit"`
	CanShare    bool         `json:"groupware.canShare"`
	Owner       emailAddress `json:"groupware.owner"`
}

func toContactFolder(e ContactFolderEntry, isDefault bool) contactFolderJSON {
	c := e.Collection
	name := c.Props.DisplayName
	if name == "" {
		name = c.Ref.Name
	}
	return contactFolderJSON{
		ID:          CalendarID(c.ID),
		DisplayName: name,
		IsDefault:   isDefault,
		CanEdit:     c.Access.AllowsObjectWrites(),
		CanShare:    c.Access.AllowsPropertyWrites(),
		Owner:       emailAddress{Name: e.OwnerName},
	}
}

// myContactFolders returns the user's address books in display order, with
// the default one, the oldest the user owns, flagged.
func (a *Handler) myContactFolders(ctx context.Context, user principals.Principal) ([]contactFolderJSON, []ContactFolderEntry, int, error) {
	entries, err := a.contactFolders.ContactFolders(ctx, user)
	if err != nil {
		return nil, nil, -1, err
	}
	owned := func(e ContactFolderEntry) bool { return e.Collection.Ref.Owner == user.Owner }
	slices.SortFunc(entries, func(x, y ContactFolderEntry) int {
		switch {
		case owned(x) != owned(y):
			if owned(x) {
				return -1
			}
			return 1
		case !x.Collection.Created.Equal(y.Collection.Created):
			return x.Collection.Created.Compare(y.Collection.Created)
		}
		return strings.Compare(x.Collection.ID, y.Collection.ID)
	})
	def := -1
	if len(entries) > 0 && owned(entries[0]) {
		def = 0
	}
	out := make([]contactFolderJSON, len(entries))
	for i, e := range entries {
		out[i] = toContactFolder(e, i == def)
	}
	return out, entries, def, nil
}

// findContactFolder resolves a Graph contact folder id to one of the user's
// address books.
func (a *Handler) findContactFolder(ctx context.Context, user principals.Principal, id string) (ContactFolderEntry, contactFolderJSON, *apiError) {
	collID, err := ParseCalendarID(id)
	if err != nil {
		return ContactFolderEntry{}, contactFolderJSON{}, badRequest("the contact folder id is malformed")
	}
	list, entries, _, err := a.myContactFolders(ctx, user)
	if err != nil {
		a.logger.Error("could not list contact folders", "sub", user.Sub, "error", err)
		return ContactFolderEntry{}, contactFolderJSON{}, internalError()
	}
	for i, e := range entries {
		if e.Collection.ID == collID {
			return e, list[i], nil
		}
	}
	// The same answer for a folder that does not exist and one that is not
	// shared with the user.
	return ContactFolderEntry{}, contactFolderJSON{}, notFound("the contact folder does not exist")
}

// defaultContactFolder resolves the folder /me/contacts stands for.
func (a *Handler) defaultContactFolder(ctx context.Context, user principals.Principal) (ContactFolderEntry, *apiError) {
	_, entries, def, err := a.myContactFolders(ctx, user)
	if err != nil {
		a.logger.Error("could not list contact folders", "sub", user.Sub, "error", err)
		return ContactFolderEntry{}, internalError()
	}
	if def < 0 {
		return ContactFolderEntry{}, notFound("the user has no default contact folder")
	}
	return entries[def], nil
}

func (a *Handler) listContactFolders(w http.ResponseWriter, r *http.Request, c *call) {
	list, _, _, err := a.myContactFolders(r.Context(), c.caller)
	if err != nil {
		a.logger.Error("could not list contact folders", "sub", c.caller.Sub, "error", err)
		writeError(w, internalError())
		return
	}
	writeCollection(w, r, list)
}

func (a *Handler) getContactFolder(w http.ResponseWriter, r *http.Request, c *call) {
	_, folder, aerr := a.findContactFolder(r.Context(), c.caller, c.params["id"])
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	writeJSON(w, http.StatusOK, folder)
}

// contactFolderChange is the body of a POST or PATCH of a contact folder.
type contactFolderChange struct {
	DisplayName *string `json:"displayName"`
}

func (a *Handler) createContactFolder(w http.ResponseWriter, r *http.Request, c *call) {
	var ch contactFolderChange
	if aerr := decodeJSON(w, r, &ch); aerr != nil {
		writeError(w, aerr)
		return
	}
	if ch.DisplayName == nil {
		writeError(w, badRequest("displayName is required"))
		return
	}
	if aerr := validCalendarName(*ch.DisplayName); aerr != nil {
		writeError(w, badRequest("displayName must be valid, non-empty text of at most 255 bytes without control characters"))
		return
	}
	ref := store.CollectionRef{Owner: c.caller.Owner, Kind: store.KindAddressBook, Name: newBookName()}
	col, err := a.cfg.Store.CreateCollection(r.Context(), ref, store.Props{DisplayName: *ch.DisplayName})
	if err != nil {
		writeError(w, a.storeError("create a contact folder", c.caller, err))
		return
	}
	writeJSON(w, http.StatusCreated, toContactFolder(ContactFolderEntry{Collection: col, OwnerName: ownerName(c.caller)}, false))
}

// newBookName returns a name for an address book made through the facade. It
// shows in the DAV URL.
func newBookName() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "book-" + hex.EncodeToString(b[:])
}

func (a *Handler) patchContactFolder(w http.ResponseWriter, r *http.Request, c *call) {
	e, folder, aerr := a.findContactFolder(r.Context(), c.caller, c.params["id"])
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	if !e.Collection.Access.AllowsPropertyWrites() {
		writeError(w, forbidden("only the owner can change a contact folder"))
		return
	}
	var ch contactFolderChange
	if aerr := decodeJSON(w, r, &ch); aerr != nil {
		writeError(w, aerr)
		return
	}
	if ch.DisplayName != nil {
		if aerr := validCalendarName(*ch.DisplayName); aerr != nil {
			writeError(w, badRequest("displayName must be valid, non-empty text of at most 255 bytes without control characters"))
			return
		}
		col, err := a.cfg.Store.UpdateCollection(r.Context(), e.Collection.Ref, store.PropsUpdate{DisplayName: ch.DisplayName})
		if err != nil {
			writeError(w, a.storeError("update a contact folder", c.caller, err))
			return
		}
		e.Collection = col
		folder = toContactFolder(e, folder.IsDefault)
	}
	writeJSON(w, http.StatusOK, folder)
}

func (a *Handler) deleteContactFolder(w http.ResponseWriter, r *http.Request, c *call) {
	e, _, aerr := a.findContactFolder(r.Context(), c.caller, c.params["id"])
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	if !e.Collection.Access.AllowsPropertyWrites() {
		writeError(w, forbidden("only the owner can delete a contact folder"))
		return
	}
	if err := a.cfg.Store.DeleteCollection(r.Context(), e.Collection.Ref); err != nil {
		writeError(w, a.storeError("delete a contact folder", c.caller, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Contacts

// storedContact is one object of an address book, parsed.
type storedContact struct {
	entry ContactFolderEntry
	obj   store.Object
	card  *vcard.Card
}

func (s storedContact) id() string { return EventID(s.entry.Collection.ID, s.obj.Name) }

// view maps the contact, adding what only the store knows.
func (s storedContact) view(groups []groupCard) contactJSON {
	out := contactFromCard(s.card)
	out.ID = s.id()
	out.ETag = etagHeader(s.obj.ETag)
	out.ChangeKey = s.obj.ETag
	out.ParentFolderID = CalendarID(s.entry.Collection.ID)
	out.LastModifiedDateTime = lastModified(s.card, s.obj.ModTime)
	out.Groups = memberships(s.card.UID(), groups)
	out.HasPhoto = hasCardPhoto(s.card)
	return out
}

// lastModified is the REV of the card, or the time the object was written.
func lastModified(c *vcard.Card, modTime time.Time) string {
	if p := c.Find("REV"); len(p) > 0 {
		rev := strings.TrimSpace(p[0].Raw)
		for _, layout := range []string{"20060102T150405Z", "2006-01-02T15:04:05Z", "2006-01-02T15:04:05.999999999Z"} {
			if t, err := time.Parse(layout, rev); err == nil {
				return t.UTC().Format("2006-01-02T15:04:05Z")
			}
		}
	}
	if modTime.IsZero() {
		return ""
	}
	return modTime.UTC().Format("2006-01-02T15:04:05Z")
}

// loadContacts reads and parses the objects of an address book. A card that
// does not parse is left out. Group cards come back apart from the contacts.
func (a *Handler) loadContacts(ctx context.Context, user principals.Principal, e ContactFolderEntry) ([]storedContact, []groupCard, error) {
	objs, err := a.cfg.Store.ListObjects(ctx, e.Collection.Ref, store.ListOptions{IncludeData: true})
	if err != nil {
		return nil, nil, err
	}
	var (
		contacts []storedContact
		groups   []groupCard
	)
	for _, o := range objs {
		card, err := vcard.Parse(o.Data)
		if err != nil {
			a.logger.Warn("skipped a stored contact that does not parse", "sub", user.Sub, "object", o.Name, "error", err)
			continue
		}
		if isGroup, members := groupCardInfo(card); isGroup {
			groups = append(groups, groupCard{UID: card.UID(), Name: text(card, "FN"), Members: members})
			continue
		}
		contacts = append(contacts, storedContact{entry: e, obj: o, card: card})
	}
	return contacts, groups, nil
}

// scopeContacts resolves the address book a request is about: the one in the
// path, or the default one for /me/contacts.
func (a *Handler) scopeContacts(ctx context.Context, c *call, def bool) (ContactFolderEntry, *apiError) {
	if def {
		return a.defaultContactFolder(ctx, c.caller)
	}
	e, _, aerr := a.findContactFolder(ctx, c.caller, c.params["id"])
	return e, aerr
}

// contactSpec is what contact lists accept besides $top, $skip and $select.
// Contacts have no $filter.
var contactSpec = &listSpec[contactJSON]{
	order: map[string]func(a, b contactJSON) int{
		"displayName": func(a, b contactJSON) int { return compareText(a.DisplayName, b.DisplayName, a.ID, b.ID) },
		"givenName":   func(a, b contactJSON) int { return compareText(a.GivenName, b.GivenName, a.ID, b.ID) },
		"surname":     func(a, b contactJSON) int { return compareText(a.Surname, b.Surname, a.ID, b.ID) },
		"companyName": func(a, b contactJSON) int { return compareText(a.CompanyName, b.CompanyName, a.ID, b.ID) },
		"lastModifiedDateTime": func(a, b contactJSON) int {
			return compareText(a.LastModifiedDateTime, b.LastModifiedDateTime, a.ID, b.ID)
		},
	},
}

func (a *Handler) listContacts(def bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		if aerr := checkOptions(r); aerr != nil {
			writeError(w, aerr)
			return
		}
		entry, aerr := a.scopeContacts(r.Context(), c, def)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		contacts, groups, err := a.loadContacts(r.Context(), c.caller, entry)
		if err != nil {
			writeError(w, a.storeError("list contacts", c.caller, err))
			return
		}
		out := make([]contactJSON, len(contacts))
		for i, s := range contacts {
			out[i] = s.view(groups)
		}
		slices.SortStableFunc(out, contactSpec.order["displayName"])
		writeList(w, r, out, contactSpec)
	}
}

// findContact resolves a contact id to the stored card. With in set, the
// contact must be in that address book. A group card is never found.
func (a *Handler) findContact(ctx context.Context, user principals.Principal, id string, in *ContactFolderEntry) (storedContact, *apiError) {
	if aerr := unsupportedSegment(id); aerr != nil {
		return storedContact{}, aerr
	}
	collID, name, err := ParseEventID(id)
	if err != nil {
		return storedContact{}, badRequest("the contact id is malformed")
	}
	var entry ContactFolderEntry
	if in != nil {
		if in.Collection.ID != collID {
			return storedContact{}, notFound("the contact does not exist")
		}
		entry = *in
	} else {
		_, entries, _, err := a.myContactFolders(ctx, user)
		if err != nil {
			a.logger.Error("could not list contact folders", "sub", user.Sub, "error", err)
			return storedContact{}, internalError()
		}
		i := slices.IndexFunc(entries, func(e ContactFolderEntry) bool { return e.Collection.ID == collID })
		if i < 0 {
			return storedContact{}, notFound("the contact does not exist")
		}
		entry = entries[i]
	}
	obj, err := a.cfg.Store.GetObject(ctx, entry.Collection.Ref, name)
	if err != nil {
		return storedContact{}, a.storeError("read a contact", user, err)
	}
	card, err := vcard.Parse(obj.Data)
	if err != nil {
		return storedContact{}, notFound("the contact does not exist")
	}
	if isGroup, _ := groupCardInfo(card); isGroup {
		return storedContact{}, notFound("the contact does not exist")
	}
	return storedContact{entry: entry, obj: obj, card: card}, nil
}

// groupsOf returns the group cards of the contact's address book.
func (a *Handler) groupsOf(ctx context.Context, user principals.Principal, e ContactFolderEntry) []groupCard {
	_, groups, err := a.loadContacts(ctx, user, e)
	if err != nil {
		a.logger.Warn("could not read the groups of a contact", "sub", user.Sub, "error", err)
		return nil
	}
	return groups
}

// resolveContact finds the contact of a request, either below a folder in the
// path (scoped) or by its own id.
func (a *Handler) resolveContact(r *http.Request, c *call, scoped bool) (storedContact, *apiError) {
	var in *ContactFolderEntry
	if scoped {
		e, _, aerr := a.findContactFolder(r.Context(), c.caller, c.params["id"])
		if aerr != nil {
			return storedContact{}, aerr
		}
		in = &e
	}
	return a.findContact(r.Context(), c.caller, c.params["cid"], in)
}

func (a *Handler) getContact(scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		if aerr := unsupportedSegment(c.params["cid"]); aerr != nil {
			writeError(w, aerr)
			return
		}
		if aerr := checkOptions(r); aerr != nil {
			writeError(w, aerr)
			return
		}
		s, aerr := a.resolveContact(r, c, scoped)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		writeItem(w, r, http.StatusOK, s.view(a.groupsOf(r.Context(), c.caller, s.entry)))
	}
}

func contactPreconditionFailed() *apiError {
	return newError(http.StatusPreconditionFailed, codePrecondition, "the contact changed since it was read")
}

// writableContact resolves the contact of a PATCH or DELETE, and checks that
// the caller may write to its address book and that If-Match holds.
func (a *Handler) writableContact(w http.ResponseWriter, r *http.Request, c *call, scoped bool) (storedContact, bool) {
	s, aerr := a.resolveContact(r, c, scoped)
	if aerr == nil && !s.entry.Collection.Access.AllowsObjectWrites() {
		aerr = forbidden("the contact folder is read-only for the user")
	}
	if aerr == nil {
		if h := r.Header.Get("If-Match"); h != "" && !matchETag(h, s.obj.ETag) {
			aerr = contactPreconditionFailed()
		}
	}
	if aerr != nil {
		writeError(w, aerr)
		return storedContact{}, false
	}
	return s, true
}

func (a *Handler) createContact(def bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		entry, aerr := a.scopeContacts(r.Context(), c, def)
		if aerr == nil && !entry.Collection.Access.AllowsObjectWrites() {
			aerr = forbidden("the contact folder is read-only for the user")
		}
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		var p contactPatch
		if aerr := decodeJSON(w, r, &p); aerr != nil {
			writeError(w, aerr)
			return
		}
		uid := requestID()
		card, aerr := newCard(p, uid, time.Now())
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		data := encodeCard(card)
		if _, err := vcard.Validate(data); err != nil {
			writeError(w, badRequest("the contact is not valid"))
			return
		}
		obj, err := a.cfg.Store.PutObject(r.Context(), entry.Collection.Ref, uid+".vcf", data, store.PutOptions{IfNoneMatch: true, UID: uid})
		if err != nil {
			writeError(w, a.storeError("create a contact", c.caller, err))
			return
		}
		s := storedContact{entry: entry, obj: obj, card: card}
		writeJSON(w, http.StatusCreated, s.view(nil))
	}
}

func (a *Handler) patchContact(scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		s, ok := a.writableContact(w, r, c, scoped)
		if !ok {
			return
		}
		var p contactPatch
		if aerr := decodeJSON(w, r, &p); aerr != nil {
			writeError(w, aerr)
			return
		}
		changed, aerr := applyContact(s.card, p, time.Now())
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		if changed {
			if aerr := a.saveContact(r, &s, c); aerr != nil {
				writeError(w, aerr)
				return
			}
		}
		writeJSON(w, http.StatusOK, s.view(a.groupsOf(r.Context(), c.caller, s.entry)))
	}
}

func (a *Handler) deleteContact(scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		s, ok := a.writableContact(w, r, c, scoped)
		if !ok {
			return
		}
		if err := a.cfg.Store.DeleteObject(r.Context(), s.entry.Collection.Ref, s.obj.Name, store.DeleteOptions{IfMatch: s.obj.ETag}); err != nil {
			writeError(w, a.storeError("delete a contact", c.caller, err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
