package classic

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fixture mirrors the description seed.sh writes next to each dump.
type fixture struct {
	Version string `json:"version"`
	// Secret is the instance secret the token hashes were made with.
	Secret string `json:"secret"`
	Users  []struct {
		Name     string   `json:"name"`
		Password string   `json:"password"`
		Groups   []string `json:"groups"`
	} `json:"users"`
	AppTokens []struct {
		User   string `json:"user"`
		Name   string `json:"name"`
		Token  string `json:"token"`
		Digest string `json:"sha512"`
	} `json:"app_tokens"`
	// Rows maps a table to the number of rows the dump must hold.
	Rows map[string]int `json:"rows"`
	// Calendars maps "owner/uri" to the number of events it holds.
	Calendars map[string]int `json:"calendars"`
}

// dumps maps a dump's base name to the pattern of its INSERT statements.
var dumps = map[string]*regexp.Regexp{
	"mariadb":  regexp.MustCompile("(?m)^INSERT INTO `?(oc_[a-z_]+)`? "),
	"postgres": regexp.MustCompile(`(?m)^INSERT INTO (?:public\.)?"?(oc_[a-z_]+)"? `),
}

func loadFixture(t *testing.T, name string) fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("dumps", name+".json"))
	if err != nil {
		t.Fatalf("%v (run seed.sh)", err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("dumps/%s.json: %v", name, err)
	}
	return f
}

func TestFixtureDescribesTheSeededData(t *testing.T) {
	for name := range dumps {
		t.Run(name, func(t *testing.T) { describes(t, loadFixture(t, name)) })
	}
}

func describes(t *testing.T, f fixture) {
	t.Helper()
	if f.Secret == "" || len(f.Users) < 3 || len(f.AppTokens) < 3 {
		t.Fatalf("fixture.json is incomplete: %d users, %d tokens", len(f.Users), len(f.AppTokens))
	}
	if n := f.Calendars["alice/work"]; n != 5000 {
		t.Errorf("alice/work holds %d events, want 5000", n)
	}
	sum := 0
	for _, n := range f.Calendars {
		sum += n
	}
	if sum != f.Rows["oc_calendarobjects"] {
		t.Errorf("calendars add up to %d events, the table is expected to hold %d", sum, f.Rows["oc_calendarobjects"])
	}
	for _, table := range []string{"oc_dav_shares", "oc_calendarsubscriptions", "oc_cards", "oc_addressbooks"} {
		if f.Rows[table] == 0 {
			t.Errorf("%s is expected to be empty", table)
		}
	}
}

func TestDumpsMatchTheirDescription(t *testing.T) {
	for name, insert := range dumps {
		t.Run(name, func(t *testing.T) {
			f := loadFixture(t, name)
			raw, err := os.ReadFile(filepath.Join("dumps", name+".sql"))
			if err != nil {
				t.Fatalf("%v (run seed.sh)", err)
			}
			dump := string(raw)

			got := map[string]int{}
			for _, m := range insert.FindAllStringSubmatch(dump, -1) {
				got[m[1]]++
			}
			for table, want := range f.Rows {
				if got[table] != want {
					t.Errorf("%s: %d rows, want %d", table, got[table], want)
				}
			}
			for table := range f.Rows {
				if !strings.Contains(dump, "CREATE TABLE") || !strings.Contains(dump, table) {
					t.Errorf("%s: no schema in the dump", table)
				}
			}
			for _, tok := range f.AppTokens {
				sum := sha512.Sum512([]byte(tok.Token + f.Secret))
				digest := hex.EncodeToString(sum[:])
				if digest != tok.Digest {
					t.Errorf("token %s/%s: fixture digest does not match the plaintext and secret", tok.User, tok.Name)
				}
				if !strings.Contains(dump, digest) {
					t.Errorf("token %s/%s: its hash is not in the dump", tok.User, tok.Name)
				}
				if strings.Contains(dump, tok.Token) {
					t.Errorf("token %s/%s: the plaintext is in the dump", tok.User, tok.Name)
				}
			}
		})
	}
}
