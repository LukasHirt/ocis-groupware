//go:build stack

package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"

	"github.com/owncloud/ocis-groupware/internal/migrate"
	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
)

// The classic users get accounts in oCIS with the same login, as they would
// when both servers read one directory. Their passwords are only for the
// sign-in at the end; the devices use app tokens.
const (
	classicPassword = "Classic-pass-1234"
	classicTeam     = "team"
)

// classicFixture is the part of test/classic/dumps/mariadb.json this test
// needs: the instance secret and the app tokens in plain text.
type classicFixture struct {
	Secret    string `json:"secret"`
	AppTokens []struct {
		User  string `json:"user"`
		Name  string `json:"name"`
		Token string `json:"token"`
	} `json:"app_tokens"`
}

func (f classicFixture) token(t *testing.T, user, name string) string {
	t.Helper()
	for _, tok := range f.AppTokens {
		if tok.User == user && tok.Name == name {
			return tok.Token
		}
	}
	t.Fatalf("the fixture has no token %q for %s", name, user)
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test source")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// composeFiles are the files of the running stack: COMPOSE_FILE, as CI sets
// it, or the default compose file, followed by the classic database.
func composeFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if v := os.Getenv("COMPOSE_FILE"); v != "" {
		sep := os.Getenv("COMPOSE_PATH_SEPARATOR")
		if sep == "" {
			sep = string(os.PathListSeparator)
		}
		files = strings.Split(v, sep)
	} else {
		files = []string{"docker-compose.yml"}
	}
	return append(files, filepath.Join(root, "test", "stack", "classic-db.yml"))
}

// compose runs `docker compose` against the stack with the classic database
// added and returns what it printed. A failure is returned, not fatal, so that
// the refusal of a running service can be checked.
func compose(ctx context.Context, t *testing.T, root string, files []string, args ...string) (string, error) {
	t.Helper()
	full := []string{"compose"}
	for _, f := range files {
		full = append(full, "-f", f)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"CLASSIC_DUMP="+filepath.Join(root, "test", "classic", "dumps", "mariadb.sql"),
		"CLASSIC_SECRET="+classicSecret(t, root),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// The arguments can carry a token, so only the subcommand is named.
		err = fmt.Errorf("docker compose %s: %w\n%s", args[0], err, out)
	}
	return string(out), err
}

func classicSecret(t *testing.T, root string) string {
	t.Helper()
	return loadClassicFixture(t, root).Secret
}

func loadClassicFixture(t *testing.T, root string) classicFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "test", "classic", "dumps", "mariadb.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f classicFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func mustCompose(ctx context.Context, t *testing.T, root string, files []string, args ...string) string {
	t.Helper()
	out, err := compose(ctx, t, root, files, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// appPasswordRecords reads app_passwords.json of the owner out of the data
// directory, through the service's container, which may be stopped. The
// directory layout is the service's own; the file is found by name.
func appPasswordRecords(ctx context.Context, t *testing.T, root string, files []string, login string) []map[string]any {
	t.Helper()
	dir := t.TempDir()
	mustCompose(ctx, t, root, files, "cp", "ocis-groupware:/data/.", dir)
	var found []map[string]any
	var names []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "app_passwords.json" {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var doc struct {
			AppPasswords []map[string]any `json:"app_passwords"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		// The principal next to it tells whose file this is.
		var pr struct {
			Username string `json:"username"`
		}
		if raw, err := os.ReadFile(filepath.Join(filepath.Dir(p), "principal.json")); err == nil {
			_ = json.Unmarshal(raw, &pr)
		}
		if pr.Username == login {
			found = append(found, doc.AppPasswords...)
		}
		names = append(names, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatalf("no app passwords of %s in the data directory (looked at %v)", login, names)
	}
	return found
}

func recordNamed(t *testing.T, records []map[string]any, name string) map[string]any {
	t.Helper()
	for _, r := range records {
		if r["name"] == name {
			return r
		}
	}
	t.Fatalf("no app password named %q in %v", name, records)
	return nil
}

// TestMigrateClassicInstance moves the classic fixture into the stack's data
// directory with the service stopped, starts the service again and checks what
// a user of the classic instance sees: a device with the classic URL and an app
// token keeps syncing after one full resync, the token turns into an ordinary
// app password on its first use, the migrated users bind to their oCIS account
// at the first sign-in, and the shares reach the people they were made for.
//
// It changes the stack, so it runs only with STACK_MIGRATE=true, as its own
// step after the other stack tests, and needs the classic paths (STACK_COMPAT)
// and Docker access to the stack's compose project.
func TestMigrateClassicInstance(t *testing.T) {
	if os.Getenv("STACK_MIGRATE") != "true" {
		t.Skip("STACK_MIGRATE is not true; the test changes the stack's data")
	}
	needsCompat(t)
	needsReport(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	root := repoRoot(t)
	files := composeFiles(t, root)
	fx := loadClassicFixture(t, root)
	aliceToken := fx.token(t, "alice", "Phone One")
	bobToken := fx.token(t, "bob", "Thunderbird")

	// The classic users and their group exist in oCIS, and marie, who has no
	// classic account, is in the group. The migration finds the ids itself with
	// an administrator's token.
	admin := signIn(t, "admin", "admin")
	idClient, err := ocisgraph.New(ocisgraph.Config{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Transport: transport, Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	marie, err := idClient.Me(ctx, signIn(t, pyUser, pyPassword))
	if err != nil {
		t.Fatal(err)
	}
	for _, login := range []string{"alice", "bob"} {
		createOCISUser(t, admin, login)
	}
	createOCISGroup(t, admin, classicTeam, marie.ID)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _ = compose(ctx, t, root, files, "rm", "--stop", "--force", "--volumes", "classic-db")
	})
	mustCompose(ctx, t, root, files, "up", "--detach", "--wait", "--no-deps", "classic-db")

	reports := t.TempDir()
	if err := os.Chmod(reports, 0o777); err != nil {
		t.Fatal(err)
	}
	migrateArgs := func(extra ...string) []string {
		args := []string{"run", "--rm", "--no-deps", "-T",
			"-v", reports + ":/migrate",
			"-e", "GROUPWARE_MIGRATE_DB_DSN=root:root@tcp(classic-db:3306)/classic",
			"-e", "GROUPWARE_MIGRATE_OCIS_TOKEN=" + admin,
			"ocis-groupware", "admin", "migrate", "classic", "--db-type", "mysql"}
		return append(args, extra...)
	}

	// The service owns the data directory, so the migration refuses to run.
	out, err := compose(ctx, t, root, files, migrateArgs()...)
	if err == nil || !strings.Contains(out, "stop the ocis-groupware service") {
		t.Fatalf("the migration ran against a running service: %v\n%s", err, out)
	}

	mustCompose(ctx, t, root, files, "stop", "ocis-groupware")
	out = mustCompose(ctx, t, root, files, migrateArgs("--dry-run")...)
	t.Logf("dry run:\n%s", out)
	out, err = compose(ctx, t, root, files, migrateArgs("--report-json", "/migrate/report.json")...)
	t.Logf("migration:\n%s", out)
	if err != nil {
		t.Fatal(err)
	}
	var rep migrate.Report
	raw, err := os.ReadFile(filepath.Join(reports, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Problems != 0 || rep.Totals.Users-rep.Totals.UsersSkipped < 2 || rep.Totals.Tokens != 3 {
		t.Errorf("report totals = %+v, want at least alice and bob, 3 tokens, no problems", rep.Totals)
	}

	// Before the service runs again the token is still the classic digest.
	before := recordNamed(t, appPasswordRecords(ctx, t, root, files, "alice"), "Phone One")
	if before["legacy_sha512"] == nil || before["legacy_sha512"] == "" {
		t.Fatalf("the imported token carries no classic digest: %v", before)
	}

	mustCompose(ctx, t, root, files, "up", "--detach", "--no-deps", "ocis-groupware")
	if err := waitForStack(5 * time.Minute); err != nil {
		t.Fatal(err)
	}
	waitForClassicPaths(t)

	// Alice's phone: classic URL, classic user name, classic app token.
	phone := account{user: "alice", appPassword: aliceToken}
	work := compatPrefix + "calendars/alice/work/"
	t.Run("a device keeps syncing", func(t *testing.T) {
		ctx := ctxTimeout(t)
		// The sync token the phone holds is the classic server's. The service
		// does not know it and asks for a new start.
		headers := basicHeader(phone.user, phone.appPassword)
		headers["Content-Type"] = "application/xml"
		stale := `<?xml version="1.0"?><D:sync-collection xmlns:D="DAV:"><D:sync-token>http://sabre.io/ns/sync/5001</D:sync-token>` +
			`<D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`
		status, _, body := do(t, ctx, "REPORT", work, headers, strings.NewReader(stale))
		if status != http.StatusForbidden && status != http.StatusConflict && status != http.StatusPreconditionFailed || !strings.Contains(body, "valid-sync-token") {
			t.Errorf("the classic sync token got %d: %s; want a valid-sync-token error", status, body)
		}

		// The full resync brings every event, at the URLs the phone has.
		res := calSync(t, ctx, phone, work, "")
		if len(res.updated) != 5000 {
			t.Fatalf("the full resync returned %d events, want 5000", len(res.updated))
		}
		for _, h := range res.updated {
			if !strings.HasPrefix(h, work) {
				t.Fatalf("event at %q, not below the classic URL %s", h, work)
			}
		}
		// Asking again with the new token is an empty answer.
		if again := calSync(t, ctx, phone, work, res.token); len(again.updated)+len(again.deleted) != 0 {
			t.Errorf("sync after the resync: %+v", again)
		}

		// The other calendars and the address books are there too.
		names := calendarNames(t, ctx, phone, compatPrefix+"calendars/alice/")
		for _, want := range []string{"Work", "Personal", "Archiv 2024 (ä)"} {
			if !slices.Contains(names, want) {
				t.Errorf("alice's calendars %v lack %q", names, want)
			}
		}
		books := addressBookCount(t, ctx, phone, compatPrefix+"addressbooks/users/alice/contacts/")
		if books != 120 {
			t.Errorf("alice's contacts hold %d cards, want 120", books)
		}
	})

	t.Run("the token becomes an app password on first use", func(t *testing.T) {
		after := recordNamed(t, appPasswordRecords(ctx, t, root, files, "alice"), "Phone One")
		if after["legacy_sha512"] != nil && after["legacy_sha512"] != "" {
			t.Errorf("the classic digest is still stored after the first use: %v", after)
		}
		if hash, _ := after["hash"].(string); !strings.HasPrefix(hash, "$2") {
			t.Errorf("the stored hash is %q, want a bcrypt hash", hash)
		}
		// It keeps working through the new hash, and a wrong token does not.
		if status := davStatus(t, phone, "PROPFIND", work, ""); status != http.StatusMultiStatus && status != http.StatusOK {
			t.Errorf("second use of the token: %d", status)
		}
		wrong := account{user: "alice", appPassword: aliceToken + "x"}
		if status := davStatus(t, wrong, "PROPFIND", work, ""); status != http.StatusUnauthorized {
			t.Errorf("a wrong token: %d, want 401", status)
		}
		// The token that was never used still carries its digest.
		other := recordNamed(t, appPasswordRecords(ctx, t, root, files, "alice"), "DAVx5 on Pixel")
		if other["legacy_sha512"] == nil || other["legacy_sha512"] == "" {
			t.Errorf("an unused token lost its digest: %v", other)
		}
	})

	t.Run("migrated users bind at the first sign-in", func(t *testing.T) {
		for _, c := range []struct {
			user      string
			calendars []string
		}{
			{"alice", []string{"Work", "Personal", "Archiv 2024 (ä)", "Projects"}},
			{"bob", []string{"Projects", "Personal", "Work"}},
		} {
			a := account{user: c.user, token: signIn(t, c.user, classicPassword)}
			var list struct {
				Value []struct{ Name string } `json:"value"`
			}
			if status := graphJSON(t, a, http.MethodGet, "/graph/v1.0/me/calendars", "", &list); status != http.StatusOK {
				t.Fatalf("%s: calendars: %d", c.user, status)
			}
			var names []string
			for _, v := range list.Value {
				names = append(names, v.Name)
			}
			for _, want := range c.calendars {
				if !slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, want) }) {
					t.Errorf("%s signs in with oCIS and sees %v, lacking %q", c.user, names, want)
				}
			}
		}
		// Alice's migrated tokens are listed under her oCIS account.
		a := account{token: signIn(t, "alice", classicPassword)}
		var tokens struct {
			AppPasswords []struct{ Name string } `json:"app_passwords"`
		}
		status := graphJSON(t, a, http.MethodGet, "/groupware/v1/app-passwords", "", &tokens)
		if status != http.StatusOK {
			t.Fatalf("app passwords: %d", status)
		}
		var tokenNames []string
		for _, ap := range tokens.AppPasswords {
			tokenNames = append(tokenNames, ap.Name)
		}
		slices.Sort(tokenNames)
		if want := []string{"DAVx5 on Pixel", "Phone One"}; !slices.Equal(tokenNames, want) {
			t.Errorf("alice's app passwords after the sign-in are %v, want %v", tokenNames, want)
		}
		// Her phone's token still works after the binding.
		if status := davStatus(t, phone, "PROPFIND", work, ""); status != http.StatusMultiStatus {
			t.Errorf("the phone after the sign-in: %d", status)
		}
	})

	t.Run("shares are visible", func(t *testing.T) {
		ctx := ctxTimeout(t)
		// Marie was not migrated. The team group is physics-lovers for her, so
		// alice's Work calendar, shared with the team read-only, shows up.
		marie := signInWithAppPassword(t, pyUser, pyPassword)
		names := calendarPaths(t, ctx, marie, compatPrefix+"calendars/"+pyUser+"/")
		var shared string
		for p, n := range names {
			if strings.HasPrefix(n, "Work") {
				shared = p
			}
		}
		if shared == "" {
			t.Fatalf("marie does not see alice's Work calendar among %v", names)
		}
		res := calSync(t, ctx, marie, shared, "")
		if len(res.updated) != 5000 {
			t.Errorf("marie reads %d events of the shared calendar, want 5000", len(res.updated))
		}
		event := utcEvent("migrate-1", "20261201T100000Z", "Not allowed")
		if status := davStatus(t, marie, http.MethodPut, shared+"not-allowed.ics", event); status != http.StatusForbidden {
			t.Errorf("marie writes to a read-only share: %d, want 403", status)
		}

		// Bob's Thunderbird: his own calendar, and alice's Work with write access.
		tb := account{user: "bob", appPassword: bobToken}
		bobs := calendarPaths(t, ctx, tb, compatPrefix+"calendars/bob/")
		var bobWork string
		for p, n := range bobs {
			if strings.HasPrefix(n, "Work") {
				bobWork = p
			}
		}
		if bobWork == "" || !slices.Contains(mapValues(bobs), "Projects") {
			t.Fatalf("bob sees %v, want Projects and alice's Work", bobs)
		}
		if status := davStatus(t, tb, http.MethodPut, bobWork+"from-bob.ics", utcEvent("migrate-2", "20261201T110000Z", "From Bob")); status != http.StatusCreated {
			t.Errorf("bob writes to the read-write share: %d, want 201", status)
		}
		if status := davStatus(t, tb, http.MethodDelete, bobWork+"from-bob.ics", ""); status != http.StatusNoContent {
			t.Errorf("bob removes his event: %d, want 204", status)
		}
	})
}

// waitForClassicPaths waits until the ingress sends the classic paths to the
// new container. The container was replaced, and the ingress learns about it a
// moment after /healthz answers; until then oCIS answers the classic paths.
func waitForClassicPaths(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		_, headers, _ := do(t, ctxTimeout(t), "PROPFIND", compatPrefix+"calendars/alice/", map[string]string{"Depth": "0"}, nil)
		if answeredByService(headers) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the classic paths still reach oCIS a minute after the restart: %q", headers.Get("WWW-Authenticate"))
		}
		time.Sleep(time.Second)
	}
}

func mapValues(m map[string]string) []string {
	var v []string
	for _, x := range m {
		v = append(v, x)
	}
	return v
}

// calendarPaths lists the calendars below a classic home with Basic auth and
// returns path to display name.
func calendarPaths(t *testing.T, ctx context.Context, a account, home string) map[string]string {
	t.Helper()
	cals, err := calClientAt(t, a).FindCalendars(ctx, home)
	if err != nil {
		t.Fatalf("FindCalendars %s: %v", home, err)
	}
	out := map[string]string{}
	for _, c := range cals {
		out[c.Path] = c.Name
	}
	return out
}

func calClientAt(t *testing.T, a account) *caldav.Client {
	t.Helper()
	c, err := caldav.NewClient(a.basic(), baseURL+compatPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func calendarNames(t *testing.T, ctx context.Context, a account, home string) []string {
	t.Helper()
	return mapValues(calendarPaths(t, ctx, a, home))
}

func addressBookCount(t *testing.T, ctx context.Context, a account, book string) int {
	t.Helper()
	c, err := carddav.NewClient(a.basic(), baseURL+compatPrefix)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := c.QueryAddressBook(ctx, book, &carddav.AddressBookQuery{
		DataRequest: carddav.AddressDataRequest{Props: []string{"UID"}},
	})
	if err != nil {
		t.Fatalf("QueryAddressBook %s: %v", book, err)
	}
	return len(objs)
}

// createOCISUser makes a user in oCIS with the given login, which is what the
// migration looks up. The login is also the name the user signs in with.
func createOCISUser(t *testing.T, admin, login string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"onPremisesSamAccountName": login,
		"displayName":              strings.ToUpper(login[:1]) + login[1:],
		"mail":                     login + "@example.org",
		"passwordProfile":          map[string]string{"password": classicPassword},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, _, text := do(t, ctxTimeout(t), http.MethodPost, "/graph/v1.0/users", bearerJSON(admin), strings.NewReader(string(body)))
	if status != http.StatusCreated {
		t.Fatalf("creating oCIS user %s: %d: %s", login, status, text)
	}
	removeAfter(t, admin, "/graph/v1.0/users/", text)
}

// removeAfter deletes the user or group a creation answered with, when the
// test ends, so that a second run against the same stack starts clean.
func removeAfter(t *testing.T, admin, path, created string) {
	t.Helper()
	var o struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(created), &o); err != nil || o.ID == "" {
		t.Fatalf("no id in %s", created)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		status, _, text := do(t, ctx, http.MethodDelete, path+o.ID, bearerJSON(admin), nil)
		if status != http.StatusNoContent {
			t.Logf("removing %s%s: %d: %s", path, o.ID, status, text)
		}
	})
}

// createOCISGroup makes a group in oCIS and adds the user with the given id.
func createOCISGroup(t *testing.T, admin, name, memberID string) {
	t.Helper()
	status, _, text := do(t, ctxTimeout(t), http.MethodPost, "/graph/v1.0/groups", bearerJSON(admin), strings.NewReader(`{"displayName":"`+name+`"}`))
	if status != http.StatusCreated {
		t.Fatalf("creating oCIS group %s: %d: %s", name, status, text)
	}
	removeAfter(t, admin, "/graph/v1.0/groups/", text)
	var g struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text), &g); err != nil {
		t.Fatal(err)
	}
	ref := `{"@odata.id":"` + baseURL + `/graph/v1.0/users/` + memberID + `"}`
	status, _, text = do(t, ctxTimeout(t), http.MethodPost, "/graph/v1.0/groups/"+g.ID+"/members/$ref", bearerJSON(admin), strings.NewReader(ref))
	if status != http.StatusNoContent {
		t.Fatalf("adding a member to %s: %d: %s", name, status, text)
	}
}

func bearerJSON(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
}
