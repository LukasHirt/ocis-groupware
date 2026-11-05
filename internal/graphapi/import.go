package graphapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/LukasHirt/groupkit/ical"
	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/vcard"
)

const (
	// maxImportBytes is the largest file an import accepts.
	maxImportBytes = 20 << 20
	// maxImportObject is the largest single object an import stores. It is
	// the size the DAV endpoints accept as well.
	maxImportObject = 10 << 20
)

// Import result states.
const (
	importCreated = "created"
	importUpdated = "updated"
	importFailed  = "failed"
)

// importResult is what happened to one object of the file.
type importResult struct {
	// Index is the position of the object in the file, from 0.
	Index int `json:"index"`
	// UID is the UID of the object. It is empty when the object could not be
	// read far enough to tell.
	UID string `json:"uid"`
	// Name is the name the object has in the collection, which is part of its
	// DAV URL. It is absent for a failed object.
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	// Error says why the object failed.
	Error string `json:"error,omitempty"`
}

// importReport is the answer to an import.
type importReport struct {
	Total   int            `json:"total"`
	Created int            `json:"created"`
	Updated int            `json:"updated"`
	Failed  int            `json:"failed"`
	Results []importResult `json:"results"`
}

// importItem is one object cut out of the file, ready to store or already
// known to be broken.
type importItem struct {
	uid  string
	data []byte
	err  string
}

func (a *Handler) importExportRoutes() []route {
	r := func(method, pattern string, h func(http.ResponseWriter, *http.Request, *call)) route {
		return route{method: method, segments: strings.Split(pattern, "/"), handle: h}
	}
	return []route{
		r(http.MethodGet, "me", a.accountInfo),
		r(http.MethodPost, "calendars/{id}/import", a.importFile(store.KindCalendar)),
		r(http.MethodGet, "calendars/{id}/export", a.exportFile(store.KindCalendar)),
		r(http.MethodPost, "addressbooks/{id}/import", a.importFile(store.KindAddressBook)),
		r(http.MethodGet, "addressbooks/{id}/export", a.exportFile(store.KindAddressBook)),
	}
}

// collectionOf resolves the id of a calendar or an address book the user sees.
func (a *Handler) collectionOf(ctx context.Context, c *call, kind store.Kind) (store.Collection, *apiError) {
	if kind == store.KindCalendar {
		e, _, aerr := a.findCalendar(ctx, c.caller, c.params["id"])
		return e.Collection, aerr
	}
	e, _, aerr := a.findContactFolder(ctx, c.caller, c.params["id"])
	return e.Collection, aerr
}

func (a *Handler) importFile(kind store.Kind) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		col, aerr := a.collectionOf(r.Context(), c, kind)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		if !col.Access.AllowsObjectWrites() {
			writeError(w, forbidden("the collection is read-only for the user"))
			return
		}
		// The file is read whole and parsed, so only a few imports may run at
		// once, and one per user.
		release, ok := a.imports.acquire(c.caller.Owner)
		if !ok {
			writeError(w, tooManyImports())
			return
		}
		defer release()
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, newError(http.StatusRequestEntityTooLarge, codeTooLarge,
					fmt.Sprintf("the file is larger than %d MB", maxImportBytes>>20)))
				return
			}
			writeError(w, badRequest("the file could not be read"))
			return
		}
		var items []importItem
		if kind == store.KindCalendar {
			items, err = splitCalendar(data)
		} else {
			items, err = splitCards(data)
		}
		if err != nil {
			writeError(w, badRequest("the file is not valid: "+err.Error()))
			return
		}
		rep, err := a.storeItems(r.Context(), col, kind, items)
		if err != nil {
			a.logger.Error("could not import a file", "owner", c.caller.Owner, "error", err)
			writeError(w, internalError())
			return
		}
		writeJSON(w, http.StatusOK, rep)
	}
}

// splitCalendar cuts a calendar file into events. Components of other types
// than VEVENT come back as failed items, and so do events the stored form
// would refuse.
func splitCalendar(data []byte) ([]importItem, error) {
	objs, err := ical.Split(data)
	if err != nil {
		return nil, err
	}
	items := make([]importItem, len(objs))
	for i, o := range objs {
		cal := o.Calendar
		if o.UID == "" {
			// Without a UID the object cannot be matched against what is stored,
			// so it gets one and is always created.
			uid := requestID()
			for _, k := range cal.Children {
				if k.Name != "VTIMEZONE" {
					k.Props = append(k.Props, ical.Property{Name: "UID", Value: uid})
				}
			}
		}
		enc := cal.Encode()
		info, err := ical.Validate(enc, []string{"VEVENT"})
		if err != nil {
			items[i] = importItem{uid: o.UID, err: "not an event the calendar can store: " + err.Error()}
			continue
		}
		items[i] = importItem{uid: info.UID, data: enc}
	}
	return items, nil
}

// splitCards cuts an address book file into cards. A card without a UID gets
// one.
func splitCards(data []byte) ([]importItem, error) {
	objs, err := vcard.Split(data)
	if err != nil {
		return nil, err
	}
	items := make([]importItem, len(objs))
	for i, o := range objs {
		if o.Err != nil {
			items[i] = importItem{err: "not a valid card: " + o.Err.Error()}
			continue
		}
		uid, card := o.Card.UID(), o.Data
		if uid == "" {
			uid = requestID()
			card = withCardUID(card, uid)
		}
		if _, err := vcard.Validate(card); err != nil {
			items[i] = importItem{uid: uid, err: "not a valid card: " + err.Error()}
			continue
		}
		items[i] = importItem{uid: uid, data: card}
	}
	return items, nil
}

// withCardUID adds a UID line before the END line of a card. Split ends every
// card with an END:VCARD line and CRLF.
func withCardUID(card []byte, uid string) []byte {
	i := bytes.LastIndex(bytes.ToUpper(card), []byte("END:VCARD"))
	if i < 0 {
		return card
	}
	out := make([]byte, 0, len(card)+len(uid)+8)
	out = append(out, card[:i]...)
	out = append(out, "UID:"...)
	out = append(out, uid...)
	out = append(out, '\r', '\n')
	return append(out, card[i:]...)
}

// objectName returns the name an imported object gets: its UID plus the
// extension, or a hash of the UID when the UID is not usable as a name.
func objectName(uid, ext string) string {
	if name := uid + ext; store.ValidateName(name) == nil && !strings.HasPrefix(name, ".") {
		return name
	}
	sum := sha256.Sum256([]byte(uid))
	return hex.EncodeToString(sum[:16]) + ext
}

// storeItems writes the items into the collection one by one. An object whose
// UID the collection already holds replaces that object under its own name; the
// others are created. A failed object never stops the ones after it. The error
// is for faults of the request as a whole.
func (a *Handler) storeItems(ctx context.Context, col store.Collection, kind store.Kind, items []importItem) (importReport, error) {
	existing := map[string]string{}
	objs, err := a.cfg.Store.ListObjects(ctx, col.Ref, store.ListOptions{})
	if err != nil {
		return importReport{}, err
	}
	for _, o := range objs {
		if o.UID != "" {
			existing[o.UID] = o.Name
		}
	}
	ext := ".ics"
	if kind == store.KindAddressBook {
		ext = ".vcf"
	}
	rep := importReport{Total: len(items), Results: make([]importResult, 0, len(items))}
	for i, it := range items {
		if err := ctx.Err(); err != nil {
			return importReport{}, err
		}
		res := importResult{Index: i, UID: it.uid}
		switch {
		case it.err != "":
			res.Status, res.Error = importFailed, it.err
		case len(it.data) > maxImportObject:
			res.Status, res.Error = importFailed, fmt.Sprintf("the object is larger than %d MB", maxImportObject>>20)
		default:
			name, found := existing[it.uid]
			opts := store.PutOptions{UID: it.uid}
			res.Status = importUpdated
			if !found {
				name, opts.IfNoneMatch, res.Status = objectName(it.uid, ext), true, importCreated
			}
			if _, err := a.cfg.Store.PutObject(ctx, col.Ref, name, it.data, opts); err != nil {
				res.Status, res.Error = importFailed, a.importStoreError(err)
			} else {
				res.Name = name
				existing[it.uid] = name
			}
		}
		switch res.Status {
		case importCreated:
			rep.Created++
		case importUpdated:
			rep.Updated++
		default:
			rep.Failed++
		}
		rep.Results = append(rep.Results, res)
	}
	return rep, nil
}

func (a *Handler) importStoreError(err error) string {
	switch {
	case errors.Is(err, store.ErrExists):
		return "another object already has this name"
	case errors.Is(err, store.ErrUIDConflict):
		return "another object already has this UID"
	case errors.Is(err, store.ErrPermission):
		return "the user may not write to the collection"
	case errors.Is(err, store.ErrInvalidName):
		return "the name of the object is not valid"
	}
	a.logger.Error("could not store an imported object", "error", err)
	return "the object could not be stored"
}

func (a *Handler) exportFile(kind store.Kind) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		col, aerr := a.collectionOf(r.Context(), c, kind)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		objs, err := a.cfg.Store.ListObjects(r.Context(), col.Ref, store.ListOptions{IncludeData: true})
		if err != nil {
			writeError(w, a.storeError("export a collection", c.caller, err))
			return
		}
		slices.SortFunc(objs, func(x, y store.Object) int { return strings.Compare(x.Name, y.Name) })
		var (
			body []byte
			ctyp string
			ext  string
		)
		if kind == store.KindCalendar {
			body, ctyp, ext = a.mergeCalendars(c, objs), "text/calendar; charset=utf-8", ".ics"
		} else {
			body, ctyp, ext = joinCards(objs), "text/vcard; charset=utf-8", ".vcf"
		}
		w.Header().Set("Content-Type", ctyp)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": exportName(col, ext)}))
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// exportName is the file name of an export: the collection's name, with
// characters that file systems and headers dislike replaced.
func exportName(col store.Collection, ext string) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|;`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(col.Props.DisplayName))
	if name == "" || name == "." || name == ".." {
		name = "export"
	}
	if len(name) > 100 {
		name = strings.ToValidUTF8(name[:100], "")
	}
	return name + ext
}

// mergeCalendars puts the stored objects into one calendar file: the top-level
// properties of the first object, each time zone once, and the events in the
// order of the objects. An object that does not parse is left out and logged.
func (a *Handler) mergeCalendars(c *call, objs []store.Object) []byte {
	out := &ical.Calendar{Component: ical.Component{Name: "VCALENDAR"}}
	var (
		zones  []*ical.Component
		events []*ical.Component
		seen   = map[string]bool{}
	)
	for _, o := range objs {
		cal, err := ical.Parse(o.Data)
		if err != nil {
			a.logger.Warn("skipped a stored event that does not parse", "owner", c.caller.Owner, "object", o.Name, "error", err)
			continue
		}
		if out.Props == nil {
			out.Props = cal.Props
		}
		for _, k := range cal.Children {
			if k.Name != "VTIMEZONE" {
				events = append(events, k)
				continue
			}
			if p := k.Prop("TZID"); p != nil && !seen[p.Value] {
				seen[p.Value] = true
				zones = append(zones, k)
			}
		}
	}
	if out.Props == nil {
		out.Props = []ical.Property{{Name: "VERSION", Value: "2.0"}, {Name: "PRODID", Value: "-//ocis-groupware//export//EN"}}
	}
	out.Children = append(zones, events...)
	return out.Encode()
}

// joinCards concatenates the stored cards, each ending in CRLF.
func joinCards(objs []store.Object) []byte {
	var b bytes.Buffer
	for _, o := range objs {
		b.Write(o.Data)
		if !bytes.HasSuffix(o.Data, []byte("\r\n")) {
			b.WriteString("\r\n")
		}
	}
	return b.Bytes()
}
