package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

// maxBody is the largest request body, in bytes. A request names an owner
// and nothing else.
const maxBody = 4 << 10

// Registry is what the admin API needs from the principal registry.
type Registry interface {
	ListPrincipals(ctx context.Context) []principals.Principal
	SetDisabled(ctx context.Context, owner string, disabled bool) (principals.Principal, error)
	Purge(ctx context.Context, owner string, dryRun bool) (principals.PurgeReport, error)
}

// principalJSON is a principal as the API shows it.
type principalJSON struct {
	Owner       string    `json:"owner"`
	Sub         string    `json:"sub"` // empty until the user has signed in
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	Disabled    bool      `json:"disabled"`
	// IdentityAt is when oCIS last confirmed the user. It is left out until
	// that has happened once.
	IdentityAt time.Time `json:"identity_at,omitzero"`
}

func toJSON(p principals.Principal) principalJSON {
	return principalJSON{
		Owner: p.Owner, Sub: p.Sub, Username: p.Username, DisplayName: p.DisplayName,
		CreatedAt: p.CreatedAt, Disabled: p.Disabled, IdentityAt: p.IdentityAt.UTC(),
	}
}

// Handler returns the admin API:
//
//	GET  /v1/principals          every principal
//	POST /v1/principals/disable  {"owner": "..."}
//	POST /v1/principals/enable   {"owner": "..."}
//	POST /v1/principals/purge    {"owner": "...", "dry_run": false}
//
// Answers and errors are JSON. Mutations run one at a time, and each is
// logged at Info with the owner and username, which is the audit trail.
// The handler must only be served on the admin socket: it authenticates
// nobody.
func Handler(reg Registry, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	h := &handler{reg: reg, logger: logger}
	mux := http.NewServeMux()
	mux.Handle("/v1/principals", allow(http.MethodGet, h.list))
	mux.Handle("/v1/principals/disable", allow(http.MethodPost, h.setDisabled(true)))
	mux.Handle("/v1/principals/enable", allow(http.MethodPost, h.setDisabled(false)))
	mux.Handle("/v1/principals/purge", allow(http.MethodPost, h.purge))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}

type handler struct {
	reg    Registry
	logger *slog.Logger
	// mu serializes mutations, so that two admin calls cannot interleave,
	// say, a purge with an enable.
	mu sync.Mutex
}

// allow answers any method but the one given with a JSON 405.
func allow(method string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next(w, r)
	})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	list := h.reg.ListPrincipals(r.Context())
	out := make([]principalJSON, 0, len(list))
	for _, p := range list {
		out = append(out, toJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"principals": out})
}

// ownerRequest is the body of every mutation. The owner is the key of the
// principal's data, which is the subject for a user who signed in on their own.
type ownerRequest struct {
	Owner  string `json:"owner"`
	DryRun bool   `json:"dry_run"`
}

func (h *handler) setDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ownerRequest
		if status, msg := decodeBody(w, r, &req); status != 0 {
			writeError(w, status, msg)
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		p, err := h.reg.SetDisabled(r.Context(), req.Owner, disabled)
		switch {
		case err == nil:
		case errors.Is(err, principals.ErrNotFound):
			writeError(w, http.StatusNotFound, "not found")
			return
		default:
			h.logger.Error("could not change the disabled flag", "owner", req.Owner, "disabled", disabled, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		msg := "principal enabled by admin"
		if disabled {
			msg = "principal disabled by admin"
		}
		h.logger.Info(msg, "owner", p.Owner, "username", p.Username)
		writeJSON(w, http.StatusOK, toJSON(p))
	}
}

func (h *handler) purge(w http.ResponseWriter, r *http.Request) {
	var req ownerRequest
	if status, msg := decodeBody(w, r, &req); status != 0 {
		writeError(w, status, msg)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var username string
	for _, p := range h.reg.ListPrincipals(r.Context()) {
		if p.Owner == req.Owner {
			username = p.Username
		}
	}
	report, err := h.reg.Purge(r.Context(), req.Owner, req.DryRun)
	switch {
	case err == nil:
	case errors.Is(err, principals.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, principals.ErrNotDisabled):
		writeError(w, http.StatusConflict, "principal is not disabled")
		return
	default:
		h.logger.Error("could not purge principal", "owner", req.Owner, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !req.DryRun {
		h.logger.Info("principal purged by admin", "owner", req.Owner, "username", username,
			"calendars", report.Calendars, "addressbooks", report.AddressBooks, "app_passwords", report.AppPasswords, "shares", report.Shares)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"owner":         req.Owner,
		"calendars":     report.Calendars,
		"addressbooks":  report.AddressBooks,
		"app_passwords": report.AppPasswords,
		"shares":        report.Shares,
	})
}

// decodeBody reads one JSON object of at most maxBody bytes into req,
// refusing unknown fields, anything after the object and an empty owner. It
// returns the status and message to answer with, or 0.
func decodeBody(w http.ResponseWriter, r *http.Request, req *ownerRequest) (int, string) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return bodyError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return http.StatusBadRequest, "request body must be a single JSON object"
		}
		return bodyError(err)
	}
	if req.Owner == "" {
		return http.StatusBadRequest, "owner must not be empty"
	}
	return 0, ""
}

func bodyError(err error) (int, string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge, "request body too large"
	}
	return http.StatusBadRequest, `request body must be a JSON object like {"owner": "..."}`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
