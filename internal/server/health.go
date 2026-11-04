package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// healthzHandler reports readiness. When ready is non-nil it runs on every
// request. A failure is logged and produces a 503 with a generic body, so
// paths and error text do not reach clients.
func healthzHandler(logger *slog.Logger, ready func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(r.Context()); err != nil {
				logger.Error("readiness check failed", "error", err)
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
