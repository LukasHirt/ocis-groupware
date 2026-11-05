package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store/filestore"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDataDirWritable(t *testing.T) {
	dir := t.TempDir()
	if err := dataDirWritable(dir)(context.Background()); err != nil {
		t.Fatalf("writable dir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("temp file left behind: %v", entries)
	}

	if err := dataDirWritable(filepath.Join(dir, "missing"))(context.Background()); err == nil {
		t.Error("missing dir: expected error")
	}

	if os.Geteuid() != 0 {
		ro := t.TempDir()
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
		if err := dataDirWritable(ro)(context.Background()); err == nil {
			t.Error("read-only dir: expected error")
		}
		entries, _ := os.ReadDir(ro)
		if len(entries) != 0 {
			t.Errorf("temp file left behind in read-only dir: %v", entries)
		}
	}
}

func TestOpenStoreSecondOpenIsLocked(t *testing.T) {
	dir := t.TempDir()
	first, err := openStore(dir, discardLogger())
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	defer func() { _ = first.Close() }()

	_, err = openStore(dir, discardLogger())
	if !errors.Is(err, filestore.ErrLocked) {
		t.Fatalf("second open error = %v, want ErrLocked", err)
	}
	if !strings.Contains(err.Error(), "in use by another process") {
		t.Errorf("error %q is not clear", err)
	}
	if n := strings.Count(err.Error(), dir); n != 1 {
		t.Errorf("path appears %d times in %q, want once", n, err)
	}
}

// A directory whose format-version this build does not read, as after a
// downgrade, stops the service before it touches anything: it exits with an
// error that names the version, leaves the directory as it found it and does
// not keep the lock.
func TestStartRefusesAFormatVersionItDoesNotRead(t *testing.T) {
	for _, version := range []string{"2", "0", "1.1"} {
		t.Run(version, func(t *testing.T) {
			dir := shortDir(t)
			versionFile := filepath.Join(dir, "format-version")
			if err := os.WriteFile(versionFile, []byte(version+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GROUPWARE_OIDC_ISSUER", "https://ocis.invalid")
			t.Setenv("GROUPWARE_DATA_DIR", dir)
			t.Setenv("GROUPWARE_HTTP_ADDR", "127.0.0.1:0")
			t.Setenv("GROUPWARE_DEBUG_ADDR", "")

			var stdout, stderr strings.Builder
			if code := realMain(nil, &stdout, &stderr); code != 1 {
				t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr.String())
			}
			msg := stderr.String()
			if !strings.Contains(msg, "unsupported format version") || !strings.Contains(msg, version) {
				t.Errorf("error %q does not name the unsupported version %q", msg, version)
			}

			if got, err := os.ReadFile(versionFile); err != nil || string(got) != version+"\n" {
				t.Errorf("format-version = %q, %v; it must stay as it was", got, err)
			}
			for _, name := range []string{"owners", "admin.sock"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s was created in a directory the service refused: %v", name, err)
				}
			}

			// The refused start gave the lock back.
			if err := os.WriteFile(versionFile, []byte("1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			st, err := openStore(dir, discardLogger())
			if err != nil {
				t.Fatalf("open after the refused start: %v", err)
			}
			_ = st.Close()
		})
	}
}

// shortDir returns a directory whose paths fit a unix socket address;
// t.TempDir() on macOS is often too long for one.
func shortDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if len(dir) < 60 {
		return dir
	}
	dir, err := os.MkdirTemp("/tmp", "gw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestAdminSocketIsRemovedOnShutdown(t *testing.T) {
	dir := shortDir(t)
	st, err := openStore(dir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "admin.sock")
	l, err := openAdminSocket(path)
	if err != nil {
		t.Fatalf("openAdminSocket: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveAdmin(ctx, l, path, reg, discardLogger()) }()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
	resp, err := client.Get("http://admin/v1/principals")
	if err != nil {
		t.Fatalf("GET over the socket: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveAdmin: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveAdmin did not return")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket file still exists after shutdown (stat err %v)", err)
	}
}

func TestAdminSocketFailureNamesPathAndSetting(t *testing.T) {
	path := filepath.Join(shortDir(t), "missing", "admin.sock")
	_, err := openAdminSocket(path)
	if err == nil {
		t.Fatal("expected an error for a socket in a missing directory")
	}
	for _, want := range []string{path, "admin_socket", "GROUPWARE_ADMIN_SOCKET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
