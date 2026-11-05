package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFlagsRejectsBadInput(t *testing.T) {
	for _, args := range [][]string{
		{"-sizes", "abc"},
		{"-clients", "50", "-principals", "10"},
		{"-shares", "10", "-principals", "10"},
		{"-interval", "0s"},
	} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v): expected an error", args)
		}
	}
	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Principals != 1000 || cfg.Clients != 200 || cfg.RecipientsPer != 50 || len(cfg.LargeSizes) != 1 || cfg.LargeSizes[0] != 10000 {
		t.Errorf("defaults are not the load target: %+v", cfg)
	}
}

// TestRunSmall runs the whole tool against a binary built from this module,
// at a size that takes a few seconds.
func TestRunSmall(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and starts the service")
	}
	cfg, err := parseFlags([]string{
		"-principals", "20", "-events-per-user", "3", "-sizes", "150,300", "-queries", "5",
		"-shared-collections", "2", "-shared-events", "10", "-shares", "5",
		"-clients", "8", "-interval", "1s", "-duration", "3s", "-write-rate", "5", "-bcrypt-cost", "4",
		"-concurrency", "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	res, err := run(ctx, cfg, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ColdStarts) != 2 {
		t.Fatalf("cold starts = %d, want 2", len(res.ColdStarts))
	}
	for _, cs := range res.ColdStarts {
		if cs.Ready <= 0 || cs.FirstTouch[150] <= 0 || cs.FirstTouch[300] <= 0 {
			t.Errorf("cold start not measured: %+v", cs)
		}
	}
	seen := map[string]summary{}
	for _, s := range res.Ops {
		seen[s.Name] = s
	}
	for _, name := range []string{"calendar-query 150", "calendar-query 300", "sync-collection full 150", "sync-collection delta 150",
		"put 150", "storm poll own calendar", "storm poll shared calendar", "storm write", "propfind home with shares"} {
		s, ok := seen[name]
		if !ok || s.Count == 0 {
			t.Errorf("no successful %q requests: %+v", name, s)
		}
		if s.Errors != 0 {
			t.Errorf("%q: %d errors", name, s.Errors)
		}
	}

	var out strings.Builder
	writeReport(&out, res)
	for _, want := range []string{"Cold start", "Memory", "Latency", "Target"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q", want)
		}
	}
	path := filepath.Join(t.TempDir(), "r.json")
	if err := writeJSON(path, res); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		t.Errorf("json not written: %v", err)
	}
}
