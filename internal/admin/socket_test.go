package admin

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// shortDir returns a directory whose paths fit a unix socket address.
// t.TempDir() on macOS is often longer than the 104 bytes sun_path allows,
// so a directory under /tmp is made instead when needed.
func shortDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if len(dir) < 60 {
		return dir
	}
	dir, err := os.MkdirTemp("/tmp", "admin-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func mustListen(t *testing.T, path string) *net.UnixListener {
	t.Helper()
	l, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// assertConnects dials path and accepts the connection on l.
func assertConnects(t *testing.T, l *net.UnixListener, path string) {
	t.Helper()
	accepted := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	_ = c.Close()
	if err := <-accepted; err != nil {
		t.Fatalf("accept: %v", err)
	}
}

func socketMode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q): %v", path, err)
	}
	if info.Mode().Type() != fs.ModeSocket {
		t.Fatalf("%s is not a socket: %v", path, info.Mode())
	}
	return info.Mode().Perm()
}

func TestSocketIsMode0600AndInsideDataDir(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "admin.sock")
	l := mustListen(t, path)

	if mode := socketMode(t, path); mode != 0o600 {
		t.Fatalf("socket mode = %o, want 0600", mode)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "admin.sock" {
		t.Fatalf("directory holds %v, want only admin.sock (no temp name left)", entries)
	}
	assertConnects(t, l, path)
}

// TestSocketNeverVisibleWithWiderMode creates the socket with a permissive
// umask and watches the final path the whole time: it must only ever be seen
// with mode 0600.
func TestSocketNeverVisibleWithWiderMode(t *testing.T) {
	old := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(old) })

	dir := shortDir(t)
	path := filepath.Join(dir, "admin.sock")

	var (
		wg   sync.WaitGroup
		stop = make(chan struct{})
		seen []fs.FileMode
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if info, err := os.Lstat(path); err == nil && info.Mode().Perm() != 0o600 {
				seen = append(seen, info.Mode().Perm())
			}
		}
	}()
	mustListen(t, path)
	close(stop)
	wg.Wait()

	if len(seen) != 0 {
		t.Fatalf("socket was visible with modes %o", seen)
	}
	if mode := socketMode(t, path); mode != 0o600 {
		t.Fatalf("socket mode = %o under umask 0, want 0600", mode)
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "admin.sock")

	// A socket file of a process that is gone: created, then closed without
	// unlinking it.
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket file is missing: %v", err)
	}

	l := mustListen(t, path)
	if mode := socketMode(t, path); mode != 0o600 {
		t.Fatalf("socket mode = %o, want 0600", mode)
	}
	assertConnects(t, l, path)
}

func TestListenErrorNamesThePath(t *testing.T) {
	path := filepath.Join(shortDir(t), "missing", "admin.sock")
	_, err := Listen(path)
	if err == nil {
		t.Fatal("Listen in a missing directory: expected error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error %q does not name the path", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %q does not wrap the cause", err)
	}
}
