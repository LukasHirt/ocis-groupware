package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// readyCacheTTL is how long the result of the readiness check is reused. The
// check writes a file, so it must not run once per request on a public path.
const readyCacheTTL = 10 * time.Second

// readyCheck runs the readiness function at most once per readyCacheTTL, however
// many requests ask. Concurrent callers wait for the run in progress. The
// result, including a failure, is reused until it is older than the TTL.
type readyCheck struct {
	fn  func(context.Context) error
	now func() time.Time

	mu      sync.Mutex
	checked bool
	at      time.Time
	err     error
}

func (c *readyCheck) check(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checked && c.now().Sub(c.at) < readyCacheTTL {
		return c.err
	}
	// The caller going away must not turn into a cached failure.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	c.err = c.fn(ctx)
	c.at, c.checked = c.now(), true
	return c.err
}

// healthzHandler reports readiness. When ready is non-nil its result is
// cached for readyCacheTTL. A failure is logged and produces a 503 with a
// generic body, so paths and error text do not reach clients.
func healthzHandler(logger *slog.Logger, ready *readyCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready.check(r.Context()); err != nil {
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
