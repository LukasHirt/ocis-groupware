package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukasHirt/groupkit/store/filestore"

	"github.com/owncloud/ocis-groupware/internal/migrate/classic/classictest"
)

func migrateEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GROUPWARE_DATA_DIR", dir)
	t.Setenv("GROUPWARE_ADMIN_SOCKET", "")
	t.Setenv("GROUPWARE_OIDC_ISSUER", "")
	t.Setenv("GROUPWARE_OCIS_URL", "")
	t.Setenv("GROUPWARE_MIGRATE_OCIS_TOKEN", "")
	t.Setenv("GROUPWARE_MIGRATE_DB_DSN", "")
	t.Setenv("GROUPWARE_LEGACY_TOKEN_SECRET", "")
	return dir
}

func TestMigrateUsageErrors(t *testing.T) {
	migrateEnv(t)
	for name, args := range map[string][]string{
		"no noun":        {"admin", "migrate"},
		"unknown source": {"admin", "migrate", "oracle"},
		"no db type":     {"admin", "migrate", "classic", "--db-dsn", "x"},
		"bad db type":    {"admin", "migrate", "classic", "--db-type", "sqlite", "--db-dsn", "x"},
		"no dsn":         {"admin", "migrate", "classic", "--db-type", "mysql"},
		"stray argument": {"admin", "migrate", "classic", "--db-type", "mysql", "--db-dsn", "x", "alice"},
		"empty user":     {"admin", "migrate", "classic", "--db-type", "mysql", "--db-dsn", "x", "--user", " "},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := runMain(t, args...)
			if code != 2 || !strings.Contains(stderr, "usage:") {
				t.Fatalf("code %d, stderr %q", code, stderr)
			}
		})
	}
}

func TestMigrateNeedsAWayToMapUsers(t *testing.T) {
	migrateEnv(t)
	code, _, stderr := runMain(t, "admin", "migrate", "classic", "--db-type", "mysql", "--db-dsn", "x")
	if code != 1 || !strings.Contains(stderr, "GROUPWARE_MIGRATE_OCIS_TOKEN") || !strings.Contains(stderr, "--users") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}

func TestMigrateRefusesWhileTheServiceHoldsTheDataDirectory(t *testing.T) {
	dir := migrateEnv(t)
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	csv := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(csv, []byte("alice,ocis-alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runMain(t, "admin", "migrate", "classic", "--db-type", "mysql", "--db-dsn", "x", "--users", csv)
	if code != 1 || !strings.Contains(stderr, "stop the ocis-groupware service") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}

func TestMigrateFixtureThroughTheCommand(t *testing.T) {
	classictest.Each(t, "classic_cmd", func(t *testing.T, db classictest.DB) {
		dir := migrateEnv(t)
		t.Setenv("GROUPWARE_MIGRATE_DB_DSN", db.DSN)
		t.Setenv("GROUPWARE_LEGACY_TOKEN_SECRET", db.Fixture.Secret)
		work := t.TempDir()
		users := filepath.Join(work, "users.csv")
		groups := filepath.Join(work, "groups.csv")
		if err := os.WriteFile(users, []byte("classic_id,ocis_id\nalice,u-alice\nbob,u-bob\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(groups, []byte("team,g-team\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(work, "report.json")
		args := []string{"admin", "migrate", "classic", "--db-type", db.Type, "--users", users, "--groups", groups,
			"--user", "bob", "--report-json", report}

		code, stdout, stderr := runMain(t, append(args, "--dry-run")...)
		if code != 0 || !strings.Contains(stdout, "Dry run") {
			t.Fatalf("dry run: code %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		if entries, _ := os.ReadDir(filepath.Join(dir, "owners")); len(entries) != 0 {
			t.Fatalf("the dry run wrote %d owners", len(entries))
		}

		code, stdout, stderr = runMain(t, args...)
		if code != 0 || !strings.Contains(stdout, "User bob (owner migrated-u-bob): created") {
			t.Fatalf("run: code %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		var rep struct {
			Totals struct {
				Users        int `json:"users"`
				ObjectsAdded int `json:"objects_added"`
			} `json:"totals"`
		}
		if err := json.Unmarshal(raw, &rep); err != nil || rep.Totals.Users != 1 || rep.Totals.ObjectsAdded != 13 {
			t.Fatalf("report = %s, %v", raw, err)
		}

		// A second run finds everything there.
		code, stdout, _ = runMain(t, args...)
		if code != 0 || !strings.Contains(stdout, "already there") {
			t.Fatalf("rerun: code %d, stdout %q", code, stdout)
		}
	})
}
