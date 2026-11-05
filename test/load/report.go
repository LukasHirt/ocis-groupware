package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// userIndex returns the index of a seeded principal from its name.
func userIndex(name string) int {
	var i int
	_, _ = fmt.Sscanf(name, "user%d", &i)
	return i
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond)) }

func mb(kb int64) string { return fmt.Sprintf("%.0f MB", float64(kb)/1024) }

// writeReport prints the results as text.
func writeReport(w io.Writer, r *results) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	c := r.Config
	p("\nLoad run, %s\n", r.Started.Format(time.RFC3339))
	p("Machine: %s/%s, %d CPUs, %s\n", r.OS, r.Arch, r.CPUs, r.GoVersion)
	p("Data: %d principals, %d events in all, %.0f MB on disk, seeded in %s\n",
		c.Principals, r.Objects, float64(r.DataBytes)/1e6, r.SeedTime.Round(time.Second))
	p("Storm: %d clients every %s for %s, %.1f writes per second, %d recipients per shared calendar\n\n",
		c.Clients, c.Interval, c.Duration, c.WriteRate, c.RecipientsPer)

	p("%s\n", "Cold start")
	for _, cs := range r.ColdStarts {
		p("  %s: healthy after %s, memory %s\n", cs.Label, ms(cs.Ready), mb(cs.RSSKB))
		for _, size := range c.LargeSizes {
			p("    first range query on the calendar of %d events: %s\n", size, ms(cs.FirstTouch[size]))
		}
	}

	p("%s\n", "\nMemory (resident set of the service)")
	for _, m := range r.Memory {
		p("  %-36s %8s (peak so far %s)\n", m.Phase, mb(m.RSSKB), mb(m.PeakKB))
	}

	p("%s\n", "\nLatency")
	p("  %-48s %7s %6s %10s %10s %10s %10s\n", "operation", "count", "errors", "p50", "p95", "p99", "max")
	for _, s := range r.Ops {
		p("  %-48s %7d %6d %10s %10s %10s %10s\n", s.Name, s.Count, s.Errors, ms(s.P50), ms(s.P95), ms(s.P99), ms(s.Max))
	}

	p("%s\n", "\nTarget")
	p("%s\n", "  "+targetLine(r))
}

// targetLine states whether range queries on 10,000 events stayed within the
// limit. It only reports; nothing fails on it.
func targetLine(r *results) string {
	for _, s := range r.Ops {
		if s.Name == "calendar-query 10000" {
			verdict := "within"
			if s.P95 > targetP95 {
				verdict = "OVER"
			}
			return fmt.Sprintf("range query on 10000 events: p95 %s, %s the %s limit", ms(s.P95), verdict, targetP95)
		}
	}
	return "no range query on a calendar of 10000 events in this run"
}

type jsonResults struct {
	*results
	Target string
}

func writeJSON(path string, r *results) error {
	b, err := json.MarshalIndent(jsonResults{results: r, Target: strings.TrimSpace(targetLine(r))}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
