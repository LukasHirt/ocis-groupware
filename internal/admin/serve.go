package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Serve serves the admin API on l until ctx is cancelled, then shuts the
// server down and returns. It takes a unix listener and nothing else, so a
// network listener cannot be handed to it by mistake. Closing the listener
// does not remove the socket file; the caller does that.
func Serve(ctx context.Context, l *net.UnixListener, reg Registry, logger *slog.Logger) error {
	if l == nil {
		return errors.New("admin: no listener")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	srv := &http.Server{
		Handler: Handler(reg, logger),
		// A client that connects and sends nothing, or trickles a request,
		// must not hold the service's shutdown.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("admin: serve: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("admin: shutdown: %w", err)
		}
		return nil
	}
}
