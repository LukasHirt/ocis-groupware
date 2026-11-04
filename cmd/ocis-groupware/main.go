// Command ocis-groupware runs the calendar and contacts service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/owncloud/ocis-groupware/internal/config"
	"github.com/owncloud/ocis-groupware/internal/logging"
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

	srv := server.New(cfg.HTTPAddr, logger)
	return srv.Run(ctx)
}
