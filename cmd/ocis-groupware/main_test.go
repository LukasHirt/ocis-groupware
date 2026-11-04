package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LukasHirt/groupkit/store/filestore"
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
