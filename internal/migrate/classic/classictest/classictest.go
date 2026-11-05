// Package classictest loads the classic database dumps from test/classic into
// real MariaDB and PostgreSQL databases for tests of code that reads a classic
// installation.
//
// The databases come from the environment, as for the reader's own tests:
//
//	CLASSIC_TEST_MYSQL_DSN      user:pass@tcp(127.0.0.1:3306)/classic
//	CLASSIC_TEST_POSTGRES_URL   postgres://user:pass@127.0.0.1:5432/classic
//
// Tests of different packages run at the same time, so each caller names a
// scratch database, which is created next to the configured one and filled
// from the dump. The database user must be allowed to create databases.
// Without a configured database the tests are skipped, unless
// CLASSIC_REQUIRE_DB is set, which CI does.
package classictest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/owncloud/ocis-groupware/internal/migrate/classic"
)

const (
	envMySQL    = "CLASSIC_TEST_MYSQL_DSN"
	envPostgres = "CLASSIC_TEST_POSTGRES_URL"
	envRequire  = "CLASSIC_REQUIRE_DB"
)

// Fixture is the description that is kept next to a dump.
type Fixture struct {
	Secret    string `json:"secret"`
	AppTokens []struct {
		User   string `json:"user"`
		Name   string `json:"name"`
		Token  string `json:"token"`
		Digest string `json:"sha512"`
	} `json:"app_tokens"`
}

// DB is a database with a dump loaded.
type DB struct {
	// Name is "mariadb" or "postgres", like the dump files.
	Name string
	// Type is the value for a --db-type flag.
	Type    string
	Driver  classic.Driver
	DSN     string
	Fixture Fixture
}

// Open connects a reader to the database. It is closed with the test.
func (d DB) Open(t *testing.T) *classic.Reader {
	t.Helper()
	r, err := classic.Open(context.Background(), d.Driver, d.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

var scratchName = regexp.MustCompile(`^[a-z0-9_]+$`)

// Each runs fn once for every configured database with its dump loaded into
// the scratch database of that name.
func Each(t *testing.T, scratch string, fn func(t *testing.T, db DB)) {
	t.Helper()
	if !scratchName.MatchString(scratch) {
		t.Fatalf("classictest: bad scratch database name %q", scratch)
	}
	for _, tg := range []struct {
		name, typ, env string
		driver         classic.Driver
	}{
		{"mariadb", "mysql", envMySQL, classic.MySQL},
		{"postgres", "postgres", envPostgres, classic.Postgres},
	} {
		t.Run(tg.name, func(t *testing.T) {
			base := os.Getenv(tg.env)
			if base == "" {
				if os.Getenv(envRequire) != "" {
					t.Fatalf("%s is required but not set", tg.env)
				}
				t.Skip("no database configured")
			}
			dsn, err := load(tg.driver, base, scratch, tg.name)
			if err != nil {
				t.Fatal(err)
			}
			db := DB{Name: tg.name, Type: tg.typ, Driver: tg.driver, DSN: dsn}
			raw, err := os.ReadFile(filepath.Join(dumps(), tg.name+".json"))
			if err == nil {
				err = json.Unmarshal(raw, &db.Fixture)
			}
			if err != nil {
				t.Fatal(err)
			}
			fn(t, db)
		})
	}
}

func dumps() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "test", "classic", "dumps")
}

var dropOrder = []string{
	"oc_users", "oc_accounts", "oc_groups", "oc_group_user", "oc_calendars",
	"oc_calendarobjects", "oc_calendarchanges", "oc_calendarsubscriptions",
	"oc_schedulingobjects", "oc_dav_shares", "oc_dav_properties",
	"oc_addressbooks", "oc_cards", "oc_cards_properties",
	"oc_addressbookchanges", "oc_authtoken",
}

// load creates the scratch database next to the one base names, fills it from
// the dump and returns its DSN.
func load(driver classic.Driver, base, scratch, dump string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dumps(), dump+".sql"))
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if driver == classic.MySQL {
		cfg, err := mysql.ParseDSN(base)
		if err != nil {
			return "", err
		}
		cfg.MultiStatements = true
		admin := cfg.Clone()
		admin.DBName = ""
		if err := execMySQL(ctx, admin, "CREATE DATABASE IF NOT EXISTS `"+scratch+"`"); err != nil {
			return "", fmt.Errorf("classictest: create database: %w", err)
		}
		cfg.DBName = scratch
		c, err := mysql.NewConnector(cfg)
		if err != nil {
			return "", err
		}
		db := sql.OpenDB(c)
		defer func() { _ = db.Close() }()
		for _, table := range dropOrder {
			if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS `"+table+"`"); err != nil {
				return "", err
			}
		}
		if _, err := db.ExecContext(ctx, string(raw)); err != nil {
			return "", err
		}
		cfg.MultiStatements = false
		return cfg.FormatDSN(), nil
	}

	conn, err := pgconn.Connect(ctx, base)
	if err != nil {
		return "", err
	}
	var exists bool
	row := conn.ExecParams(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", [][]byte{[]byte(scratch)}, nil, nil, nil).Read()
	if row.Err == nil && len(row.Rows) == 1 {
		exists = string(row.Rows[0][0]) == "t"
	}
	if !exists {
		if _, err := conn.Exec(ctx, `CREATE DATABASE "`+scratch+`"`).ReadAll(); err != nil {
			_ = conn.Close(ctx)
			return "", fmt.Errorf("classictest: create database: %w", err)
		}
	}
	_ = conn.Close(ctx)
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = "/" + scratch
	dsn := u.String()
	conn, err = pgconn.Connect(ctx, dsn)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public").ReadAll(); err != nil {
		return "", err
	}
	if _, err := conn.Exec(ctx, strings.ReplaceAll(string(raw), "\r\n", "\n")).ReadAll(); err != nil {
		return "", err
	}
	return dsn, nil
}

func execMySQL(ctx context.Context, cfg *mysql.Config, stmt string) error {
	c, err := mysql.NewConnector(cfg)
	if err != nil {
		return err
	}
	db := sql.OpenDB(c)
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, stmt)
	return err
}
