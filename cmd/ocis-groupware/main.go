// Command ocis-groupware runs the calendar and contacts service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/LukasHirt/groupkit/store/filestore"

	"github.com/owncloud/ocis-groupware/internal/admin"
	"github.com/owncloud/ocis-groupware/internal/api"
	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/config"
	"github.com/owncloud/ocis-groupware/internal/davmount"
	"github.com/owncloud/ocis-groupware/internal/graphapi"
	"github.com/owncloud/ocis-groupware/internal/logging"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/provision"
	"github.com/owncloud/ocis-groupware/internal/server"
	"github.com/owncloud/ocis-groupware/web"
)

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr))
}

// realMain runs the service, or the admin command when args start with
// "admin", and returns the exit code.
func realMain(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "admin" {
		return runAdmin(args[1:], stdout, stderr)
	}
	if err := run(args, stderr); err != nil {
		if errors.Is(err, errUsage) {
			return 2
		}
		_, _ = fmt.Fprintln(stderr, "ocis-groupware:", err)
		return 1
	}
	return 0
}

// errUsage means the arguments were wrong and the usage has been printed.
var errUsage = errors.New("usage error")

func run(args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("ocis-groupware", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a groupware.yaml config file")
	if err := flags.Parse(args); err != nil {
		return errUsage
	}

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
	// The socket is created once the store's lock is held, so a socket file
	// found there is from a service that is gone.
	adminSocket, err := openAdminSocket(cfg.AdminSocket)
	if err != nil {
		return err
	}

	graph, err := ocisgraph.New(ocisgraph.Config{BaseURL: cfg.OCISURL, CAFile: cfg.OIDCCAFile})
	if err != nil {
		return err
	}
	identities := provision.NewIdentities(registry, graph, logger)

	dav, wellKnown, err := davmount.New(davmount.Config{
		Store:      store,
		Registry:   registry,
		Verifier:   verifier,
		Identities: identities,
		Logger:     logger,
	})
	if err != nil {
		return err
	}

	facade, err := graphapi.New(graphapi.Config{Store: store, Registry: registry, Identities: identities, Logger: logger})
	if err != nil {
		return err
	}

	srv := server.New(cfg.HTTPAddr, logger, auth.Middleware(verifier, logger),
		server.WithReadiness(dataDirWritable(cfg.DataDir)),
		server.WithDAV(dav, wellKnown),
		server.WithAppPasswords(api.AppPasswords(registry, identities, logger)),
		server.WithGraph(facade),
		server.WithWeb(web.Handler()))

	// On shutdown the public server drains first, then the admin server,
	// then the socket file goes, and the store is closed last (its deferred
	// Close runs after this function returns).
	adminCtx, stopAdmin := context.WithCancel(ctx)
	defer stopAdmin()
	adminErr := make(chan error, 1)
	go func() {
		logger.Info("admin socket listening", "path", cfg.AdminSocket)
		adminErr <- serveAdmin(adminCtx, adminSocket, cfg.AdminSocket, registry, logger)
	}()
	err = srv.Run(ctx)
	stopAdmin()
	return errors.Join(err, <-adminErr)
}

// openAdminSocket creates the admin socket at path. The service does not
// start without it: a service that cannot be administered should not run
// quietly. The error names the setting, since the fix is a different path.
func openAdminSocket(path string) (*net.UnixListener, error) {
	l, err := admin.Listen(path)
	if err != nil {
		return nil, fmt.Errorf("%w; set admin_socket (GROUPWARE_ADMIN_SOCKET) to a short path on a local filesystem", err)
	}
	return l, nil
}

// serveAdmin serves the admin API on l until ctx is cancelled, then removes
// the socket file at path.
func serveAdmin(ctx context.Context, l *net.UnixListener, path string, reg *principals.Registry, logger *slog.Logger) error {
	err := admin.Serve(ctx, l, reg, logger)
	if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		logger.Error("removing the admin socket", "path", path, "error", rmErr)
	}
	return err
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
