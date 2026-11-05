// Package server builds and runs ocis-groupware's HTTP server.
package server

import (
	"context"
	"errors"
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

	// debugServer serves /metrics and /healthz on their own address, outside
	// the authentication middleware. It is nil when no address is set.
	debugServer  *http.Server
	debugHandler http.Handler
}

// Option customizes a Server.
type Option func(*options)

type options struct {
	ready          func(context.Context) error
	debugAddr      string
	now            func() time.Time
	dav, wellKnown http.Handler
	compatDAV      http.Handler
	appPasswords   http.Handler
	graph          http.Handler
	groupware      http.Handler
	web            http.Handler
}

// WithReadiness sets the function /healthz calls. Its result is reused for
// ten seconds, so the function runs at most once in that time however many
// requests arrive. A non-nil error turns the response into a 503.
func WithReadiness(fn func(context.Context) error) Option {
	return func(o *options) { o.ready = fn }
}

// WithDebugAddr serves /metrics and /healthz on a listener of their own at
// addr, with no authentication. Bind it to the loopback interface or keep it
// inside the cluster. The main listener answers 404 for /metrics. Empty
// serves no debug listener.
func WithDebugAddr(addr string) Option {
	return func(o *options) { o.debugAddr = addr }
}

// withClock replaces the clock of the readiness cache, for tests.
func withClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
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

// WithCompatDAV mounts the classic-layout DAV handler at /remote.php/dav and
// /remote.php/dav/. Like the handler of WithDAV it must authenticate requests
// itself, and the authn middleware must be built with
// auth.WithDAVPrefix("/remote.php/dav") so that it leaves them to it.
func WithCompatDAV(h http.Handler) Option {
	return func(o *options) { o.compatDAV = h }
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

// WithGroupware serves the endpoints that are not part of Graph: the import and
// export of calendars and address books below /groupware/v1/calendars/ and
// /groupware/v1/addressbooks/, and the account information at /groupware/v1/me. The handler runs behind the authn middleware,
// which requires a bearer token.
func WithGroupware(h http.Handler) Option {
	return func(o *options) { o.groupware = h }
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

	var ready *readyCheck
	if o.ready != nil {
		now := o.now
		if now == nil {
			now = time.Now
		}
		ready = &readyCheck{fn: o.ready, now: now}
	}
	health := healthzHandler(logger, ready)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	if o.dav != nil {
		// Both, because the mux would otherwise answer /dav with a redirect.
		mux.Handle("/dav/", o.dav)
		mux.Handle("/dav", o.dav)
	}
	if o.compatDAV != nil {
		mux.Handle("/remote.php/dav/", o.compatDAV)
		mux.Handle("/remote.php/dav", o.compatDAV)
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

	if o.groupware != nil {
		mux.Handle("/groupware/v1/me", o.groupware)
		mux.Handle("/groupware/v1/calendars/", o.groupware)
		mux.Handle("/groupware/v1/addressbooks/", o.groupware)
	}

	if o.web != nil {
		mux.Handle("/web/", o.web)
	}

	var handler http.Handler = mux
	if authn != nil {
		handler = authn(handler)
	}
	handler = refuseConnect(handler)

	srv := &Server{
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
	if o.debugAddr != "" {
		debug := http.NewServeMux()
		debug.HandleFunc("GET /healthz", health)
		debug.HandleFunc("GET /metrics", metricsHandler())
		srv.debugHandler = withRequestLogging(logger, debug)
		srv.debugServer = &http.Server{
			Addr:              o.debugAddr,
			Handler:           srv.debugHandler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       time.Minute,
			MaxHeaderBytes:    16 << 10,
		}
	}
	return srv
}

// Run starts the server, and the debug listener when there is one, and blocks
// until ctx is canceled or one of them fails, then shuts both down gracefully.
func (s *Server) Run(ctx context.Context) error {
	servers := []*http.Server{s.httpServer}
	if s.debugServer != nil {
		servers = append(servers, s.debugServer)
	}
	errCh := make(chan error, len(servers))
	for _, srv := range servers {
		go func() {
			s.logger.Info("http server listening", "addr", srv.Addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("server: listen and serve %s: %w", srv.Addr, err)
				return
			}
			errCh <- nil
		}()
	}

	var runErr error
	select {
	case runErr = <-errCh:
		if runErr == nil {
			// A server stopped without an error: stop the rest as well.
			runErr = <-errCh
		}
	case <-ctx.Done():
		s.logger.Info("shutting down http server")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var shutErr error
	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			shutErr = errors.Join(shutErr, fmt.Errorf("server: shutdown: %w", err))
		}
	}
	if runErr != nil || shutErr != nil {
		return errors.Join(runErr, shutErr)
	}
	s.logger.Info("http server stopped cleanly")
	return nil
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
