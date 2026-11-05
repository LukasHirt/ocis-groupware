// Package api serves ocis-groupware's REST endpoints under /groupware/v1.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/provision"
)

// maxBody is the largest request body, in bytes. A request names an app
// password and nothing else.
const maxBody = 4 << 10

// appPasswordJSON is an app password as the API shows it. The hash is never
// part of it.
type appPasswordJSON struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func toJSON(ap principals.AppPassword) appPasswordJSON {
	return appPasswordJSON{
		ID: ap.ID, Name: ap.Name, CreatedAt: ap.CreatedAt,
		ExpiresAt: ap.ExpiresAt, LastUsedAt: ap.LastUsedAt,
	}
}

// issuedJSON is the answer to a creation: the only time the secret is shown.
type issuedJSON struct {
	appPasswordJSON
	Password string `json:"password"`
}

// AppPasswords returns the handler for GET and POST /groupware/v1/app-passwords
// and DELETE /groupware/v1/app-passwords/{id}. Register it for those two paths
// without a method, so that it answers every other method with a JSON 405 and
// an Allow header itself; it reads the id from the request's path value. It
// must run behind auth.Middleware: the caller is the principal of the verified claims,
// which are provisioned like the DAV handler does it. A principal reaches only
// its own app passwords.
func AppPasswords(reg *principals.Registry, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	h := &appPasswords{reg: reg, logger: logger}
	return http.HandlerFunc(h.serve)
}

// NotFound returns the handler for every other path under /groupware: a JSON
// 404. Like AppPasswords it must run behind auth.Middleware, so that an
// unauthenticated request never gets this far.
func NotFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeError(w, http.StatusNotFound, "not found")
	})
}

type appPasswords struct {
	reg    *principals.Registry
	logger *slog.Logger
}

func (h *appPasswords) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// The method is checked before the caller is provisioned, so a request
	// that cannot succeed creates nothing.
	id := r.PathValue("id")
	allowed := []string{http.MethodGet, http.MethodPost}
	if id != "" {
		allowed = []string{http.MethodDelete}
	}
	if !slices.Contains(allowed, r.Method) {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, status, msg := provision.Principal(r.Context(), h.reg, claims, h.logger)
	if status != 0 {
		writeError(w, status, msg)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.list(w, p)
	case http.MethodPost:
		h.create(w, r, p)
	default:
		h.revoke(w, r, p, id)
	}
}

func (h *appPasswords) list(w http.ResponseWriter, p principals.Principal) {
	list, err := h.reg.ListAppPasswords(p.Sub)
	if err != nil {
		h.logger.Error("could not list app passwords", "sub", p.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]appPasswordJSON, 0, len(list))
	for _, ap := range list {
		out = append(out, toJSON(ap))
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_passwords": out})
}

func (h *appPasswords) create(w http.ResponseWriter, r *http.Request, p principals.Principal) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if status, msg := decodeBody(w, r, &req); status != 0 {
		writeError(w, status, msg)
		return
	}
	ap, secret, err := h.reg.IssueAppPassword(r.Context(), p.Sub, req.Name, 0)
	switch {
	case err == nil:
	case errors.Is(err, principals.ErrInvalidAppPasswordName):
		writeError(w, http.StatusBadRequest, "invalid name: a name has 1 to 100 bytes of text without control characters")
		return
	case errors.Is(err, principals.ErrTooManyAppPasswords):
		writeError(w, http.StatusConflict, "too many app passwords: revoke one first")
		return
	default:
		h.logger.Error("could not issue app password", "sub", p.Sub, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.logger.Info("app password issued", "sub", p.Sub, "id", ap.ID)
	writeJSON(w, http.StatusCreated, issuedJSON{appPasswordJSON: toJSON(ap), Password: secret})
}

func (h *appPasswords) revoke(w http.ResponseWriter, r *http.Request, p principals.Principal, id string) {
	err := h.reg.RevokeAppPassword(r.Context(), p.Sub, id)
	switch {
	case err == nil:
		h.logger.Info("app password revoked", "sub", p.Sub, "id", clip(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, principals.ErrNotFound):
		// The same answer for an unknown id and for another principal's.
		writeError(w, http.StatusNotFound, "not found")
	default:
		h.logger.Error("could not revoke app password", "sub", p.Sub, "id", clip(id), "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// decodeBody reads one JSON value of at most maxBody bytes into v, refusing
// unknown fields and anything after the value. Keys match without regard to
// case (encoding/json does that) and a key that occurs twice takes its last
// value. It returns the status and
// message to answer with, or 0.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) (int, string) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return bodyError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return http.StatusBadRequest, "request body must be a single JSON object"
		}
		return bodyError(err)
	}
	return 0, ""
}

func bodyError(err error) (int, string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge, "request body too large"
	}
	return http.StatusBadRequest, `request body must be a JSON object like {"name": "..."}`
}

// maxLoggedID is the most bytes of a caller-supplied id that a log line holds.
const maxLoggedID = 64

// clip shortens a caller-supplied value for a log line.
func clip(s string) string {
	if len(s) <= maxLoggedID {
		return s
	}
	return s[:maxLoggedID] + "...(truncated)"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
