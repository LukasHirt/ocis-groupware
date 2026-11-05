package classic

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// The tests that read a database load the dumps from test/classic/dumps into
// the databases named here. Without them the tests are skipped, unless
// CLASSIC_REQUIRE_DB is set, which CI does.
//
//	CLASSIC_TEST_MYSQL_DSN      user:pass@tcp(127.0.0.1:3306)/classic
//	CLASSIC_TEST_POSTGRES_URL   postgres://user:pass@127.0.0.1:5432/classic
const (
	envMySQL    = "CLASSIC_TEST_MYSQL_DSN"
	envPostgres = "CLASSIC_TEST_POSTGRES_URL"
	envRequire  = "CLASSIC_REQUIRE_DB"
)

type target struct {
	name   string
	driver Driver
	dsn    string
	dump   string
}

var (
	loadMu sync.Mutex
	loaded = map[string]error{}
)

// eachDatabase runs fn once for every configured database with its dump
// loaded. The load happens once per test binary.
func eachDatabase(t *testing.T, fn func(t *testing.T, r *Reader, fx fixtureInfo)) {
	t.Helper()
	targets := []target{
		{"mariadb", MySQL, os.Getenv(envMySQL), "mariadb"},
		{"postgres", Postgres, os.Getenv(envPostgres), "postgres"},
	}
	for _, tg := range targets {
		t.Run(tg.name, func(t *testing.T) {
			if tg.dsn == "" {
				if os.Getenv(envRequire) != "" {
					t.Fatalf("%s is required but not set", map[Driver]string{MySQL: envMySQL, Postgres: envPostgres}[tg.driver])
				}
				t.Skip("no database configured")
			}
			loadDump(t, tg)
			r, err := Open(context.Background(), tg.driver, tg.dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			fn(t, r, readFixture(t, tg.dump))
		})
	}
}

func loadDump(t *testing.T, tg target) {
	t.Helper()
	loadMu.Lock()
	defer loadMu.Unlock()
	if err, done := loaded[tg.name]; done {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	err := loadInto(tg)
	loaded[tg.name] = err
	if err != nil {
		t.Fatal(err)
	}
}

func loadInto(tg target) error {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "classic", "dumps", tg.dump+".sql"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch tg.driver {
	case MySQL:
		cfg, err := mysql.ParseDSN(tg.dsn)
		if err != nil {
			return err
		}
		cfg.MultiStatements = true
		c, err := mysql.NewConnector(cfg)
		if err != nil {
			return err
		}
		db := sql.OpenDB(c)
		defer func() { _ = db.Close() }()
		for _, table := range dropOrder {
			if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS `"+table+"`"); err != nil {
				return err
			}
		}
		_, err = db.ExecContext(ctx, string(raw))
		return err
	default:
		conn, err := pgconn.Connect(ctx, tg.dsn)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(ctx) }()
		if _, err := conn.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public").ReadAll(); err != nil {
			return err
		}
		_, err = conn.Exec(ctx, strings.ReplaceAll(string(raw), "\r\n", "\n")).ReadAll()
		return err
	}
}

var dropOrder = []string{
	"oc_users", "oc_accounts", "oc_groups", "oc_group_user", "oc_calendars",
	"oc_calendarobjects", "oc_calendarchanges", "oc_calendarsubscriptions",
	"oc_schedulingobjects", "oc_dav_shares", "oc_dav_properties",
	"oc_addressbooks", "oc_cards", "oc_cards_properties",
	"oc_addressbookchanges", "oc_authtoken",
}
