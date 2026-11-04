package provision

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

func fixture(t *testing.T) (*principals.Registry, *filestore.Store, *bytes.Buffer, *slog.Logger) {
	t.Helper()
	dir := t.TempDir()
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, logger, principals.WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	return reg, st, logs, logger
}

func TestPrincipalUsesUsernameAsDisplayName(t *testing.T) {
	reg, _, logs, logger := fixture(t)
	p, status := Principal(context.Background(), reg, auth.Claims{Subject: "sub-1", PreferredUsername: "alice"}, logger)
	if status != 0 {
		t.Fatalf("status %d", status)
	}
	if p.Sub != "sub-1" || p.Username != "alice" || p.DisplayName != "alice" {
		t.Errorf("principal %+v", p)
	}
	got, err := reg.Get("sub-1")
	if err != nil || got.DisplayName != "alice" {
		t.Errorf("stored principal %+v, %v", got, err)
	}
	if logs.Len() != 0 {
		t.Errorf("a success logged %q", logs.String())
	}
	// A second call is idempotent.
	if p2, status := Principal(context.Background(), reg, auth.Claims{Subject: "sub-1", PreferredUsername: "alice"}, logger); status != 0 || p2 != p {
		t.Errorf("second call: %+v, %d", p2, status)
	}
}

func TestPrincipalUsesTheClaimsDisplayName(t *testing.T) {
	reg, _, _, logger := fixture(t)
	p, status := Principal(context.Background(), reg, auth.Claims{Subject: "sub-1", PreferredUsername: "alice", DisplayName: "Alice Liddell"}, logger)
	if status != 0 {
		t.Fatalf("status %d", status)
	}
	if p.Username != "alice" || p.DisplayName != "Alice Liddell" {
		t.Errorf("principal %+v", p)
	}
}

func TestPrincipalInvalidNameIsForbidden(t *testing.T) {
	reg, _, logs, logger := fixture(t)
	_, status := Principal(context.Background(), reg, auth.Claims{Subject: "sub-1", PreferredUsername: "a/b"}, logger)
	if status != http.StatusForbidden {
		t.Fatalf("status %d, want 403", status)
	}
	assertLogged(t, logs.String(), "level=WARN")
}

func TestPrincipalOtherErrorsAreInternal(t *testing.T) {
	reg, st, logs, logger := fixture(t)
	// A file where the owner directory must go makes provisioning fail.
	if err := os.MkdirAll(filepath.Dir(st.OwnerDir("sub-1")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.OwnerDir("sub-1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, status := Principal(context.Background(), reg, auth.Claims{Subject: "sub-1", PreferredUsername: "alice"}, logger)
	if status != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", status)
	}
	assertLogged(t, logs.String(), "level=ERROR")
}

// assertLogged checks that the log has one record at the level, that it names
// the sub and an error, and that it holds nothing else of the caller.
func assertLogged(t *testing.T, logs, level string) {
	t.Helper()
	if n := strings.Count(logs, "\n"); n != 1 {
		t.Fatalf("%d log lines: %q", n, logs)
	}
	if !strings.Contains(logs, level) || !strings.Contains(logs, "sub=sub-1") || !strings.Contains(logs, "error=") {
		t.Errorf("log %q lacks the level, the sub or the error", logs)
	}
	for _, attr := range strings.Fields(logs) {
		key, _, ok := strings.Cut(attr, "=")
		if ok && key != "time" && key != "level" && key != "msg" && key != "sub" && key != "error" {
			t.Errorf("unexpected log attribute %q", key)
		}
	}
}
