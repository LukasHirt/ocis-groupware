package graphapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoder DecodeConfig uses
	_ "image/png"  // registers the decoder DecodeConfig uses
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/vcard"
)

// maxPhotoBytes is the largest contact photo, in bytes.
const maxPhotoBytes = 1 << 20

// photoMIME returns the type of an image the facade serves, judging by the
// first bytes, or "" for anything else.
func photoMIME(data []byte) string {
	switch t := http.DetectContentType(data); t {
	case "image/jpeg", "image/png":
		return t
	}
	return ""
}

// photoData returns the base64 text of a PHOTO property that embeds an image:
// vCard 3.0's ENCODING=b and vCard 4.0's data: URI. A link gives "".
func photoData(p vcard.Property) string {
	raw := strings.TrimSpace(p.Raw)
	if len(raw) > 5 && strings.EqualFold(raw[:5], "data:") {
		head, body, ok := strings.Cut(raw[5:], ",")
		if !ok || !strings.HasSuffix(strings.ToLower(head), ";base64") {
			return ""
		}
		return body
	}
	for _, v := range p.Params["ENCODING"] {
		if v = strings.ToLower(v); v == "b" || v == "base64" {
			return raw
		}
	}
	return ""
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}

// cardPhoto returns the first embedded JPEG or PNG photo of the card.
func cardPhoto(c *vcard.Card) (data []byte, mime string, ok bool) {
	for _, p := range c.Find("PHOTO") {
		b64 := photoData(p)
		if b64 == "" {
			continue
		}
		b, err := decodeBase64(b64)
		if err != nil {
			continue
		}
		if m := photoMIME(b); m != "" {
			return b, m, true
		}
	}
	return nil, "", false
}

// hasCardPhoto is cardPhoto without decoding the whole image, for lists.
func hasCardPhoto(c *vcard.Card) bool {
	for _, p := range c.Find("PHOTO") {
		b64 := photoData(p)
		if len(b64) > 64 {
			b64 = b64[:64]
		}
		if b, err := decodeBase64(strings.TrimRight(b64[:len(b64)/4*4], "=")); err == nil && photoMIME(b) != "" {
			return true
		}
	}
	return false
}

// setCardPhoto replaces the photo of the card. vCard 3.0 cards get
// ENCODING=b and vCard 4.0 cards a data: URI.
func setCardPhoto(c *vcard.Card, data []byte, mime string, now time.Time) {
	c.Props = slices.DeleteFunc(c.Props, named("PHOTO"))
	enc := base64.StdEncoding.EncodeToString(data)
	p := vcard.Property{Name: "PHOTO"}
	if c.Version == "4.0" {
		p.Raw = "data:" + mime + ";base64," + enc
	} else {
		p.Params = map[string][]string{"ENCODING": {"b"}, "TYPE": {strings.ToUpper(strings.TrimPrefix(mime, "image/"))}}
		p.Raw = enc
	}
	c.Props = append(c.Props, p)
	setRev(c, now)
}

// removeCardPhoto removes every PHOTO of the card and reports whether there
// was one.
func removeCardPhoto(c *vcard.Card, now time.Time) bool {
	n := len(c.Props)
	c.Props = slices.DeleteFunc(c.Props, named("PHOTO"))
	if len(c.Props) == n {
		return false
	}
	setRev(c, now)
	return true
}

func (a *Handler) photoRoutes() []route {
	r := func(method, pattern string, h func(http.ResponseWriter, *http.Request, *call)) route {
		return route{method: method, segments: strings.Split(pattern, "/"), handle: h}
	}
	var out []route
	for _, base := range []struct {
		prefix string
		scoped bool
	}{{"me/contacts/{cid}/photo", false}, {"me/contactFolders/{id}/contacts/{cid}/photo", true}} {
		out = append(out,
			r(http.MethodGet, base.prefix, a.getPhotoMeta(base.scoped)),
			r(http.MethodDelete, base.prefix, a.deletePhoto(base.scoped)),
			r(http.MethodGet, base.prefix+"/$value", a.getPhoto(base.scoped)),
			r(http.MethodPut, base.prefix+"/$value", a.putPhoto(base.scoped)),
			r(http.MethodDelete, base.prefix+"/$value", a.deletePhoto(base.scoped)),
		)
	}
	return out
}

// photoJSON is a Graph profilePhoto.
type photoJSON struct {
	MediaContentType string `json:"@odata.mediaContentType"`
	ID               string `json:"id"`
	Height           int    `json:"height"`
	Width            int    `json:"width"`
}

// photoInfo describes an image, or says why it cannot be used.
func photoInfo(data []byte, mime string) (photoJSON, error) {
	cfg, _, err := image.DecodeConfig(strings.NewReader(string(data)))
	if err != nil {
		return photoJSON{}, err
	}
	return photoJSON{MediaContentType: mime, ID: fmt.Sprintf("%dX%d", cfg.Width, cfg.Height), Height: cfg.Height, Width: cfg.Width}, nil
}

func noPhoto() *apiError { return notFound("the contact has no photo") }

func (a *Handler) getPhoto(scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		s, aerr := a.resolveContact(r, c, scoped)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		data, mime, ok := cardPhoto(s.card)
		if !ok {
			writeError(w, noPhoto())
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

func (a *Handler) getPhotoMeta(scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		s, aerr := a.resolveContact(r, c, scoped)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		data, mime, ok := cardPhoto(s.card)
		if !ok {
			writeError(w, noPhoto())
			return
		}
		info, err := photoInfo(data, mime)
		if err != nil {
			writeError(w, noPhoto())
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func (a *Handler) putPhoto(scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		s, ok := a.writableContact(w, r, c, scoped)
		if !ok {
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPhotoBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, newError(http.StatusRequestEntityTooLarge, codeTooLarge, "the photo must be at most 1 MB"))
				return
			}
			writeError(w, badRequest("the photo could not be read"))
			return
		}
		if len(data) == 0 {
			writeError(w, badRequest("the request has no photo"))
			return
		}
		mime := photoMIME(data)
		if mime == "" {
			writeError(w, newError(http.StatusUnsupportedMediaType, codeUnsupported, "the photo must be a JPEG or PNG image"))
			return
		}
		info, err := photoInfo(data, mime)
		if err != nil {
			writeError(w, badRequest("the photo is not a valid "+strings.ToUpper(strings.TrimPrefix(mime, "image/"))+" image"))
			return
		}
		setCardPhoto(s.card, data, mime, time.Now())
		if aerr := a.saveContact(r, &s, c); aerr != nil {
			writeError(w, aerr)
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func (a *Handler) deletePhoto(scoped bool) func(http.ResponseWriter, *http.Request, *call) {
	return func(w http.ResponseWriter, r *http.Request, c *call) {
		s, ok := a.writableContact(w, r, c, scoped)
		if !ok {
			return
		}
		if _, _, has := cardPhoto(s.card); !has {
			writeError(w, noPhoto())
			return
		}
		removeCardPhoto(s.card, time.Now())
		if aerr := a.saveContact(r, &s, c); aerr != nil {
			writeError(w, aerr)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// saveContact writes the changed card back, refusing to overwrite a version
// the request did not see.
func (a *Handler) saveContact(r *http.Request, s *storedContact, c *call) *apiError {
	data := encodeCard(s.card)
	if _, err := vcard.Validate(data); err != nil {
		return badRequest("the resulting contact is not valid")
	}
	obj, err := a.cfg.Store.PutObject(r.Context(), s.entry.Collection.Ref, s.obj.Name, data, store.PutOptions{IfMatch: s.obj.ETag})
	if err != nil {
		return a.storeError("change a contact", c.caller, err)
	}
	s.obj = obj
	return nil
}
