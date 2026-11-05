package sharing

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// writeFileAtomic replaces dir/name with data, mode 0600: it writes a
// temporary file in dir, flushes it and renames it over the old file, so
// readers see the old or the new content and never a part of it. Once the
// rename is done the new content is what every reader sees, so a failure to
// flush the directory is logged and not returned.
func writeFileAtomic(dir, name string, data []byte, logger *slog.Logger) (err error) {
	tmp, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	if syncErr := syncDir(dir); syncErr != nil {
		logger.Warn("could not flush directory after writing a file", "dir", dir, "error", syncErr)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

// removeTempFiles deletes the temporary files a crashed write of the named
// files left in dir.
func removeTempFiles(dir string, names ...string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		for _, n := range names {
			if strings.HasPrefix(e.Name(), "."+n+".tmp-") {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
}
