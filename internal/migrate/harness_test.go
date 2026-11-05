package migrate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/migrate/classic"
	"github.com/owncloud/ocis-groupware/internal/migrate/idmap"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// env is a data directory opened the way the command opens it.
type env struct {
	dir    string
	st     *filestore.Store
	reg    *principals.Registry
	shares *sharing.Shares
}

func newEnv(t *testing.T, secret string) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{dir: dir}
	e.open(t, secret)
	return e
}

// open (re)opens the data directory, as a new process would.
func (e *env) open(t *testing.T, secret string) {
	t.Helper()
	if e.st != nil {
		if err := e.st.Close(); err != nil {
			t.Fatal(err)
		}
	}
	st, err := filestore.Open(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	reg, err := principals.NewRegistry(e.dir, st, st.OwnerDir, nil,
		principals.WithBcryptCost(bcrypt.MinCost), principals.WithLegacyTokens(secret, 0))
	if err != nil {
		t.Fatal(err)
	}
	shares, err := sharing.Open(sharing.Config{
		OwnerDir: st.OwnerDir,
		Owners: func() []string {
			var out []string
			for _, p := range reg.ListPrincipals(context.Background()) {
				out = append(out, p.Owner)
			}
			return out
		},
		OCISID: func(owner string) string {
			id, _ := reg.Identity(owner)
			return id.OCISID
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg.SetShares(shares)
	e.st, e.reg, e.shares = st, reg, shares
}

func (e *env) config(src Source, m Mapper) Config {
	return Config{Source: src, Mapper: m, Store: e.st, Registry: e.reg, Shares: e.shares, LegacyTokensEnabled: true}
}

// csvMapper maps every id to "ocis-<id>" except the ones in skip.
func csvMapper(t *testing.T, users, groups []string) *idmap.Mapper {
	t.Helper()
	m := idmap.New()
	var u, g strings.Builder
	for _, id := range users {
		fmt.Fprintf(&u, "%s,ocis-%s\n", id, id)
	}
	for _, id := range groups {
		fmt.Fprintf(&g, "%s,ocis-%s\n", id, id)
	}
	if err := m.LoadUsersCSV(strings.NewReader(u.String())); err != nil {
		t.Fatal(err)
	}
	if err := m.LoadGroupsCSV(strings.NewReader(g.String())); err != nil {
		t.Fatal(err)
	}
	return m
}

// fakeSource is an in-memory classic installation.
type fakeSource struct {
	users   []classic.User
	groups  []classic.Group
	cals    []classic.Calendar
	objects map[int64][]classic.Object
	books   []classic.AddressBook
	cards   map[int64][]classic.Card
	subs    []classic.Subscription
	shares  []classic.Share
	tokens  []classic.AppToken
}

func (f *fakeSource) Users(context.Context) ([]classic.User, error)         { return f.users, nil }
func (f *fakeSource) Groups(context.Context) ([]classic.Group, error)       { return f.groups, nil }
func (f *fakeSource) Calendars(context.Context) ([]classic.Calendar, error) { return f.cals, nil }
func (f *fakeSource) AddressBooks(context.Context) ([]classic.AddressBook, error) {
	return f.books, nil
}
func (f *fakeSource) Subscriptions(context.Context) ([]classic.Subscription, error) {
	return f.subs, nil
}
func (f *fakeSource) Shares(context.Context) ([]classic.Share, error)       { return f.shares, nil }
func (f *fakeSource) AppTokens(context.Context) ([]classic.AppToken, error) { return f.tokens, nil }
func (f *fakeSource) CalendarObjects(_ context.Context, id int64, fn func(classic.Object) error) error {
	for _, o := range f.objects[id] {
		if err := fn(o); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeSource) Cards(_ context.Context, id int64, fn func(classic.Card) error) error {
	for _, c := range f.cards[id] {
		if err := fn(c); err != nil {
			return err
		}
	}
	return nil
}

func ics(uid, summary string) []byte {
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\nUID:" + uid +
		"\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260102T100000Z\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
}

func vcf(uid, name string) []byte {
	return []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + name + "\r\nN:" + name + ";;;;\r\nEND:VCARD\r\n")
}

func user(id string) classic.User {
	return classic.User{ID: id, DisplayName: strings.ToUpper(id[:1]) + id[1:], Enabled: true}
}

func userP(name string) classic.Principal {
	return classic.Principal{Kind: classic.PrincipalUser, Name: name}
}

var errNotFound = principals.ErrNotFound

func principalsIdentity(id string) principals.Identity { return principals.Identity{OCISID: id} }

// snapshot lists every file below dir with its content hash, sorted.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Name() == "lock" || d.Name() == "format-version" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		lines = append(lines, fmt.Sprintf("%s %x", rel, sha256.Sum256(data)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
