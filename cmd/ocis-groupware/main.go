// Command ocis-groupware runs the calendar and contacts service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/LukasHirt/groupkit/store/filestore"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/config"
	"github.com/owncloud/ocis-groupware/internal/davmount"
	"github.com/owncloud/ocis-groupware/internal/logging"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ocis-groupware:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to a groupware.yaml config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	logger, err := logging.New(cfg.LogLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	verifier, err := auth.NewOIDCVerifier(auth.OIDCConfig{
		Issuer:   cfg.OIDCIssuer,
		Audience: cfg.OIDCAudience,
		CAFile:   cfg.OIDCCAFile,
	})
	if err != nil {
		return err
	}

	store, err := openStore(cfg.DataDir, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("closing the file store", "error", err)
		}
	}()

	registry, err := principals.NewRegistry(cfg.DataDir, store, store.OwnerDir, logger)
	if err != nil {
		return err
	}
	dav, wellKnown, err := davmount.New(davmount.Config{
		Store:    store,
		Registry: registry,
		Verifier: verifier,
		Logger:   logger,
	})
	if err != nil {
		return err
	}

	srv := server.New(cfg.HTTPAddr, logger, auth.Middleware(verifier, logger),
		server.WithReadiness(dataDirWritable(cfg.DataDir)),
		server.WithDAV(dav, wellKnown))
	return srv.Run(ctx)
}

// openStore opens the file store in dir. The error from a directory held by
// another process already names the lock file, so the path is not repeated.
func openStore(dir string, logger *slog.Logger) (*filestore.Store, error) {
	store, err := filestore.Open(dir, filestore.WithLogger(logger))
	if err != nil {
		if errors.Is(err, filestore.ErrLocked) {
			return nil, fmt.Errorf("data directory in use by another process: %w", err)
		}
		return nil, fmt.Errorf("opening data directory %s: %w", dir, err)
	}
	return store, nil
}

// dataDirWritable returns a readiness check that creates and removes a temp
// file in dir, which fails when the directory is gone, full or read-only.
func dataDirWritable(dir string) func(context.Context) error {
	return func(context.Context) error {
		f, err := os.CreateTemp(dir, ".ready-*")
		if err != nil {
			return fmt.Errorf("data directory not writable: %w", err)
		}
		name := f.Name()
		closeErr := f.Close()
		if err := os.Remove(name); err != nil {
			return fmt.Errorf("data directory not writable: %w", err)
		}
		if closeErr != nil {
			return fmt.Errorf("data directory not writable: %w", closeErr)
		}
		return nil
	}
}
