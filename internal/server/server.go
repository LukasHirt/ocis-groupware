// Package server builds and runs ocis-groupware's HTTP server.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/owncloud/ocis-groupware/internal/api"
)

// Server wraps the HTTP server serving ocis-groupware's endpoints.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
	addr       string
}

// Option customizes a Server.
type Option func(*options)

type options struct {
	ready          func(context.Context) error
	dav, wellKnown http.Handler
	appPasswords   http.Handler
	graph          http.Handler
	web            http.Handler
}

// WithReadiness sets the function /healthz calls on every request. A non-nil
// error turns the response into a 503.
func WithReadiness(fn func(context.Context) error) Option {
	return func(o *options) { o.ready = fn }
}

// WithDAV mounts the DAV handler at /dav and /dav/, and the discovery handler
// at /.well-known/caldav and /.well-known/carddav. The DAV handler must
// authenticate requests itself: the authn middleware passes /dav through.
func WithDAV(dav, wellKnown http.Handler) Option {
	return func(o *options) {
		o.dav = dav
		o.wellKnown = wellKnown
	}
}

// WithAppPasswords serves the app password API at /groupware/v1/app-passwords.
// The handler runs behind the authn middleware, which requires a bearer token.
func WithAppPasswords(h http.Handler) Option {
	return func(o *options) { o.appPasswords = h }
}

// WithGraph serves the Graph facade below /graph/v1.0/. The handler runs
// behind the authn middleware, which requires a bearer token.
func WithGraph(h http.Handler) Option {
	return func(o *options) { o.graph = h }
}

// WithWeb serves the web application's files below /web/. The authn
// middleware treats that prefix as public, so the handler must serve nothing
// but static files.
func WithWeb(h http.Handler) Option {
	return func(o *options) { o.web = h }
}

// New builds a Server listening on addr, logging through logger. When authn is
// non-nil, every request passes through it before reaching the router, so
// paths that aren't registered yet fail closed instead of returning 404.
func New(addr string, logger *slog.Logger, authn func(http.Handler) http.Handler, opts ...Option) *Server {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler(logger, o.ready))
	mux.HandleFunc("GET /metrics", metricsHandler())
	if o.dav != nil {
		// Both, because the mux would otherwise answer /dav with a redirect.
		mux.Handle("/dav/", o.dav)
		mux.Handle("/dav", o.dav)
	}
	if o.wellKnown != nil {
		mux.Handle("/.well-known/caldav", o.wellKnown)
		mux.Handle("/.well-known/carddav", o.wellKnown)
	}

	if o.appPasswords != nil {
		// No method in the patterns: the handler answers a wrong method with
		// a JSON 405 and an Allow header, and the catch-all answers a JSON
		// 404, both only after authentication.
		mux.Handle("/groupware/v1/app-passwords", o.appPasswords)
		mux.Handle("/groupware/v1/app-passwords/{id}", o.appPasswords)
		mux.Handle("/groupware/", api.NotFound())
		mux.Handle("/groupware", api.NotFound())
	}

	if o.graph != nil {
		mux.Handle("/graph/v1.0/", o.graph)
	}

	if o.web != nil {
		mux.Handle("/web/", o.web)
	}

	var handler http.Handler = mux
	if authn != nil {
		handler = authn(handler)
	}
	handler = refuseConnect(handler)

	return &Server{
		httpServer: &http.Server{
			Addr:    addr,
			Handler: withRequestLogging(logger, handler),
			// Without these, a client that opens a connection and sends nothing, or
			// trickles a request, holds the connection and its goroutine forever.
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       2 * time.Minute,
			WriteTimeout:      5 * time.Minute,
			IdleTimeout:       2 * time.Minute,
			MaxHeaderBytes:    64 << 10,
		},
		logger: logger,
		addr:   addr,
	}
}

// Run starts the server and blocks until ctx is canceled, then shuts down
// gracefully.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", "addr", s.addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("server: listen and serve: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutting down http server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server: shutdown: %w", err)
		}
		s.logger.Info("http server stopped cleanly")
		return nil
	}
}

// allowedMethods is the Allow header of the 405 that answers CONNECT: the
// methods the service's HTTP and DAV endpoints use.
const allowedMethods = "OPTIONS, GET, HEAD, POST, PUT, DELETE, PROPFIND, PROPPATCH, REPORT, MKCOL, MKCALENDAR"

// refuseConnect answers every CONNECT with 405 before it reaches the
// authentication middleware or the router. The service never needs CONNECT,
// and ServeMux neither cleans nor redirects CONNECT paths: it would route
// "/graph/../dav/x" by the raw path while the middleware classifies the
// cleaned one.
func refuseConnect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			w.Header().Set("Allow", allowedMethods)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withRequestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		level := slog.LevelInfo
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			level = slog.LevelDebug
		}
		logger.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap returns the wrapped writer, so that http.ResponseController can
// flush streamed DAV responses.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
