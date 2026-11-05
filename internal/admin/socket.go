// Package admin serves the administration API of ocis-groupware on a unix
// socket. Whoever can connect to the socket is an administrator: there is no
// token. The socket file is created with mode 0600 inside the data
// directory, so only the service's user and root reach it.
package admin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// Listen creates the admin socket at path with mode 0600 and returns the
// listener. A socket file that is already there is stale, because the data
// directory's lock is held by the calling process, and is replaced. The
// socket is created under a temporary name, given its mode and then renamed,
// so it never exists at path with a wider mode, whatever the umask.
//
// The listener does not unlink the file when closed: the caller removes
// path on shutdown.
func Listen(path string) (*net.UnixListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("admin: removing stale socket %s: %w", path, err)
	}

	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("admin: listen on %s: %w", path, err)
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-"+hex.EncodeToString(random[:]))

	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: tmp, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("admin: listen on %s: %w", path, err)
	}
	l.SetUnlinkOnClose(false)
	if err := os.Chmod(tmp, 0o600); err != nil {
		return nil, discard(l, tmp, fmt.Errorf("admin: setting the mode of socket %s: %w", path, err))
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, discard(l, tmp, fmt.Errorf("admin: creating socket %s: %w", path, err))
	}
	return l, nil
}

// discard closes a listener that is not going to be used and removes its
// file, keeping err as the error to report.
func discard(l *net.UnixListener, tmp string, err error) error {
	_ = l.Close()
	_ = os.Remove(tmp)
	return err
}
