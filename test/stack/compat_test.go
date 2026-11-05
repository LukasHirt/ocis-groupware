//go:build stack

package stack

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"
)

// The classic layout, served below /remote.php/dav when the stack runs with
// deploy/compose/compat-paths.yml (STACK_COMPAT=true). The URLs are the ones a
// client set up against a classic server has stored.
const (
	compatPrefix    = "/remote.php/dav/"
	compatPrincipal = compatPrefix + "principals/users/" + goUser + "/"
	compatBooks     = compatPrefix + "addressbooks/users/" + goUser + "/"
	compatCalendars = compatPrefix + "calendars/" + goUser + "/"
)

func needsCompat(t *testing.T) {
	t.Helper()
	if os.Getenv("STACK_COMPAT") != "true" {
		t.Skip("STACK_COMPAT is not true; the stack has no classic paths")
	}
	needsReport(t)
}

// TestCompatUnauthenticated checks that the classic paths are guarded like
// /dav: the service itself answers 401 and names itself in the challenge.
func TestCompatUnauthenticated(t *testing.T) {
	needsCompat(t)
	ctx := ctxTimeout(t)
	for _, p := range []string{compatPrincipal, compatBooks, compatCalendars, compatBooks + "contacts/"} {
		status, headers, _ := do(t, ctx, "PROPFIND", p, map[string]string{"Depth": "0"}, nil)
		if status != http.StatusUnauthorized || !answeredByService(headers) {
			t.Errorf("PROPFIND %s = %d, challenge %q; want 401 from the service", p, status, headers.Get("WWW-Authenticate"))
		}
	}
}

// TestCompatLeavesOCISPaths checks that the ingress takes only the classic
// DAV paths: the rest of /remote.php/dav stays with oCIS.
func TestCompatLeavesOCISPaths(t *testing.T) {
	needsCompat(t)
	ctx := ctxTimeout(t)
	for _, p := range []string{compatPrefix + "files/" + goUser + "/", compatPrefix + "spaces/"} {
		status, headers, _ := do(t, ctx, "PROPFIND", p, map[string]string{"Depth": "0"}, nil)
		if status != http.StatusUnauthorized || answeredByService(headers) {
			t.Errorf("PROPFIND %s = %d, challenge %q; want 401 from oCIS", p, status, headers.Get("WWW-Authenticate"))
		}
	}
}

// TestCompatClassicHrefs signs in at the stored classic principal URL, as a
// device would after a migration, and follows the hrefs the service sends.
func TestCompatClassicHrefs(t *testing.T) {
	needsCompat(t)
	ctx := ctxTimeout(t)
	a := signInWithAppPassword(t, goUser, goPassword)

	cc, err := carddav.NewClient(a.basic(), baseURL+compatPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := cc.FindCurrentUserPrincipal(ctx)
	if err != nil {
		t.Fatalf("FindCurrentUserPrincipal: %v", err)
	}
	if principal != compatPrincipal {
		t.Fatalf("principal = %q, want %q", principal, compatPrincipal)
	}
	home, err := cc.FindAddressBookHomeSet(ctx, principal)
	if err != nil {
		t.Fatalf("FindAddressBookHomeSet: %v", err)
	}
	if home != compatBooks {
		t.Fatalf("address book home = %q, want %q", home, compatBooks)
	}
	books, err := cc.FindAddressBooks(ctx, home)
	if err != nil {
		t.Fatalf("FindAddressBooks: %v", err)
	}
	if !slices.ContainsFunc(books, func(b carddav.AddressBook) bool { return b.Path == compatBooks+"contacts/" }) {
		t.Fatalf("the default address book is not at its classic path: %+v", books)
	}

	// A card written at the classic path is the same card at the /dav path.
	card := compatBooks + "contacts/compat-" + randomString(t)[:8] + ".vcf"
	t.Cleanup(func() { deleteCollection(t, a, card) })
	if _, err := cc.PutAddressObject(ctx, card, testCard("uid-compat", "Compat Check")); err != nil {
		t.Fatalf("PutAddressObject: %v", err)
	}
	flat := cardClient(t, a.basic())
	ao, err := flat.GetAddressObject(ctx, "/dav/addressbooks/"+goUser+"/contacts/"+card[strings.LastIndex(card, "/")+1:])
	if err != nil {
		t.Fatalf("GetAddressObject through /dav: %v", err)
	}
	if ao.Card.PreferredValue(vcard.FieldFormattedName) != "Compat Check" {
		t.Errorf("card through /dav = %q", ao.Card.PreferredValue(vcard.FieldFormattedName))
	}

	// Calendars keep the flat layout below the classic prefix.
	cal, err := caldav.NewClient(a.basic(), baseURL+compatPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	chome, err := cal.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		t.Fatalf("FindCalendarHomeSet: %v", err)
	}
	if chome != compatCalendars {
		t.Fatalf("calendar home = %q, want %q", chome, compatCalendars)
	}
	cals, err := cal.FindCalendars(ctx, chome)
	if err != nil {
		t.Fatalf("FindCalendars: %v", err)
	}
	if !slices.ContainsFunc(cals, func(c caldav.Calendar) bool { return strings.HasPrefix(c.Path, compatCalendars) && c.Name == "Personal" }) {
		t.Errorf("the default calendar is not below its classic path: %+v", cals)
	}

	// Another user's classic paths stay closed.
	status, _, _ := do(t, ctx, "PROPFIND", compatPrefix+"addressbooks/users/"+pyUser+"/", basicHeader(a.user, a.appPassword), nil)
	if status != http.StatusForbidden {
		t.Errorf("another user's address books = %d, want 403", status)
	}
}

// TestCompatPythonClient points the Python caldav client at a classic
// principal URL, which is what a device keeps after the move.
func TestCompatPythonClient(t *testing.T) {
	needsCompat(t)
	python := os.Getenv("STACK_PYTHON")
	caFile := os.Getenv("STACK_CA_FILE")
	if python == "" || caFile == "" {
		t.Skip("STACK_PYTHON and STACK_CA_FILE are needed for the Python client")
	}
	ctx := ctxTimeout(t)
	script := groupkitScript(ctx, t, "accept.py")

	a := signInWithAppPassword(t, pyUser, pyPassword)
	runPython(ctx, t, python, script, caFile, baseURL+compatPrefix+"principals/users/"+a.user+"/", a)
}

func groupkitScript(ctx context.Context, t *testing.T, name string) string {
	t.Helper()
	dir, err := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}}", "github.com/LukasHirt/groupkit").Output()
	if err != nil {
		t.Fatalf("locating the groupkit module: %v", err)
	}
	script := filepath.Join(strings.TrimSpace(string(dir)), "test", "pyclient", name)
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}
	return script
}

func runPython(ctx context.Context, t *testing.T, python, script, caFile, url string, a account) {
	t.Helper()
	cmd := exec.CommandContext(ctx, python, "-I", script)
	cmd.Env = append(os.Environ(),
		"GROUPKIT_URL="+url,
		"GROUPKIT_USER="+a.user,
		"GROUPKIT_PASSWORD="+a.appPassword,
		"REQUESTS_CA_BUNDLE="+caFile,
	)
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("the Python client check failed: %v", err)
	}
}
