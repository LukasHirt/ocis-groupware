package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/LukasHirt/groupkit/store/filestore"

	"github.com/owncloud/ocis-groupware/internal/config"
	"github.com/owncloud/ocis-groupware/internal/migrate"
	"github.com/owncloud/ocis-groupware/internal/migrate/classic"
	"github.com/owncloud/ocis-groupware/internal/migrate/idmap"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

const migrateUsage = `       ocis-groupware admin [--config <file>] migrate classic --db-type <mysql|postgres>
                    [--db-dsn <dsn>] [--table-prefix <prefix>] [--user <login>]...
                    [--users <file>] [--groups <file>] [--dry-run] [--report-json <file>]

migrate classic copies the calendars, address books, shares and app tokens of a
classic installation into the data directory. It reads the classic database
and writes the data directory itself, so the service must be stopped first:
the data directory allows one process at a time.

  --db-type         mysql (also MariaDB) or postgres
  --db-dsn          mysql: user:pass@tcp(host:3306)/database
                    postgres: postgres://user:pass@host:5432/database
                    Defaults to GROUPWARE_MIGRATE_DB_DSN, which keeps the
                    password out of the process list.
  --table-prefix    the classic dbtableprefix, default oc_
  --user            migrate only this classic user; repeat for several
  --users, --groups CSV files with two columns, classic id and oCIS id, that
                    take precedence over the oCIS lookup
  --dry-run         read everything, write nothing, print what would happen
  --report-json     also write the report as JSON to this file

Classic ids are mapped to oCIS ids with the CSV files and, for the rest, with
the oCIS Graph API using the administrator access token in
GROUPWARE_MIGRATE_OCIS_TOKEN (the address is ocis_url). Running it again
copies only what is new.
`

// migrateOptions is a parsed "migrate classic" command line.
type migrateOptions struct {
	dbType      string
	dsn         string
	tablePrefix string
	users       []string
	usersCSV    string
	groupsCSV   string
	dryRun      bool
	reportJSON  string
}

// stringList is a flag that can be given more than once.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// parseMigrate parses what follows "migrate".
func parseMigrate(args []string) (*migrateOptions, error) {
	if len(args) == 0 || args[0] != "classic" {
		return nil, errors.New(`expected "migrate classic"`)
	}
	var o migrateOptions
	var users stringList
	fs := flag.NewFlagSet("migrate classic", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.dbType, "db-type", "", "")
	fs.StringVar(&o.dsn, "db-dsn", "", "")
	fs.StringVar(&o.tablePrefix, "table-prefix", classic.DefaultTablePrefix, "")
	fs.Var(&users, "user", "")
	fs.StringVar(&o.usersCSV, "users", "", "")
	fs.StringVar(&o.groupsCSV, "groups", "", "")
	fs.BoolVar(&o.dryRun, "dry-run", false, "")
	fs.StringVar(&o.reportJSON, "report-json", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	o.users = users
	if o.dbType == "" {
		return nil, errors.New("--db-type is required")
	}
	if _, err := classic.ParseDriver(o.dbType); err != nil {
		return nil, err
	}
	if o.dsn == "" {
		o.dsn = os.Getenv("GROUPWARE_MIGRATE_DB_DSN")
	}
	if o.dsn == "" {
		return nil, errors.New("give the database with --db-dsn or GROUPWARE_MIGRATE_DB_DSN")
	}
	for _, u := range o.users {
		if strings.TrimSpace(u) == "" {
			return nil, errors.New("--user must not be empty")
		}
	}
	return &o, nil
}

// errMigrateProblems is a run that finished with problems in the report. The
// report already names them.
var errMigrateProblems = errors.New("the migration finished with problems, see the report")

// run performs the migration against the data directory of the configuration.
func (o *migrateOptions) run(ctx context.Context, cfg config.Config, stdout, stderr io.Writer) error {
	if cfg.DataDir == "" {
		return errors.New("config: data_dir must be set (GROUPWARE_DATA_DIR)")
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	mapper, err := o.mapper(cfg)
	if err != nil {
		return err
	}

	// The data directory first: a service that is still running is the
	// likeliest mistake, and it should show before anything else happens.
	st, err := filestore.Open(cfg.DataDir, filestore.WithLogger(logger))
	if err != nil {
		if errors.Is(err, filestore.ErrLocked) {
			return fmt.Errorf("the data directory %s is in use by another process: stop the ocis-groupware service before migrating, since the data directory allows one process at a time (%w)", cfg.DataDir, err)
		}
		return fmt.Errorf("opening data directory %s: %w", cfg.DataDir, err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("closing the file store", "error", err)
		}
	}()

	driver, err := classic.ParseDriver(o.dbType)
	if err != nil {
		return err
	}
	src, err := classic.Open(ctx, driver, o.dsn, classic.WithTablePrefix(o.tablePrefix))
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	reg, err := principals.NewRegistry(cfg.DataDir, st, st.OwnerDir, logger,
		principals.WithLegacyTokens(cfg.LegacyTokenSecret, cfg.LegacyTokenTTL))
	if err != nil {
		return err
	}
	shares, err := openShares(st, reg, logger)
	if err != nil {
		return err
	}
	reg.SetShares(shares)

	rep, err := migrate.Run(ctx, migrate.Config{
		Source: src, Mapper: mapper, Store: st, Registry: reg, Shares: shares,
		Users: o.users, DryRun: o.dryRun, LegacyTokensEnabled: cfg.LegacyTokenSecret != "",
		Logger: logger,
	})
	if err != nil {
		return err
	}
	if err := rep.WriteText(stdout); err != nil {
		return err
	}
	if o.reportJSON != "" {
		f, err := os.OpenFile(o.reportJSON, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if err := rep.WriteJSON(f); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	if rep.Totals.Problems > 0 {
		return errMigrateProblems
	}
	return nil
}

// mapper builds the id mapper from the CSV files and, when the token is set,
// the oCIS directory.
func (o *migrateOptions) mapper(cfg config.Config) (*idmap.Mapper, error) {
	var opts []idmap.Option
	token := os.Getenv("GROUPWARE_MIGRATE_OCIS_TOKEN")
	if token != "" {
		if cfg.OCISURL == "" {
			return nil, errors.New("config: ocis_url (GROUPWARE_OCIS_URL) must be set to look users up in oCIS")
		}
		graph, err := ocisgraph.New(ocisgraph.Config{BaseURL: cfg.OCISURL, CAFile: cfg.OIDCCAFile})
		if err != nil {
			return nil, err
		}
		opts = append(opts, idmap.WithDirectory(graph, token))
	}
	m := idmap.New(opts...)
	for _, f := range []struct {
		path string
		load func(io.Reader) error
	}{{o.usersCSV, m.LoadUsersCSV}, {o.groupsCSV, m.LoadGroupsCSV}} {
		if f.path == "" {
			continue
		}
		file, err := os.Open(f.path)
		if err != nil {
			return nil, err
		}
		err = f.load(file)
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.path, err)
		}
	}
	if token == "" && o.usersCSV == "" {
		return nil, errors.New("classic users cannot be mapped to oCIS: set GROUPWARE_MIGRATE_OCIS_TOKEN to an administrator access token, or give --users with a CSV file")
	}
	return m, nil
}

// runMigrate runs the parsed command and returns the exit code.
func runMigrate(configPath string, o *migrateOptions, stdout, stderr io.Writer) int {
	cfg, err := config.Load(configPath)
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = o.run(ctx, cfg, stdout, stderr)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ocis-groupware:", err)
		return 1
	}
	return 0
}
