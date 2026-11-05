// Command load fills a data directory with many principals, large calendars
// and shares, starts a local ocis-groupware binary on it, and reports how it
// answers: latency percentiles, memory and the cold-start time. It signs in
// with app passwords it seeds itself, so no identity provider is needed.
//
// It reports and never fails on a figure. See docs/sizing.md for what it
// measured and how to read it.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() { os.Exit(realMain(os.Args[1:])) }

func realMain(args []string) int {
	cfg, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }
	res, err := run(ctx, cfg, logf)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		return 1
	}
	writeReport(os.Stdout, res)
	if cfg.JSONOut != "" {
		if err := writeJSON(cfg.JSONOut, res); err != nil {
			fmt.Fprintln(os.Stderr, "load:", err)
			return 1
		}
	}
	return 0
}
