package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// targetP95 is the limit a range query on 10,000 events is held to.
const targetP95 = time.Second

// coldStart is one start of the service on a filled data directory.
type coldStart struct {
	Label string
	// Ready is the time from starting the process to a healthy /healthz.
	Ready time.Duration
	// FirstTouch is the first range query on each large calendar after the
	// start, which is when the store reads that calendar.
	FirstTouch map[int]time.Duration
	RSSKB      int64
}

type memoryPoint struct {
	Phase string
	RSSKB int64
	// PeakKB is the largest RSS seen up to that point.
	PeakKB int64
}

// results is everything one run reports.
type results struct {
	Started    time.Time
	GoVersion  string
	OS, Arch   string
	CPUs       int
	Config     config
	Objects    int
	DataBytes  int64
	SeedTime   time.Duration
	ColdStarts []coldStart
	Memory     []memoryPoint
	Ops        []summary
	Notes      []string
}

func run(ctx context.Context, cfg config, logf func(string, ...any)) (*results, error) {
	res := &results{
		Started: time.Now().UTC(), GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		CPUs: runtime.NumCPU(), Config: cfg,
	}

	work, err := os.MkdirTemp("", "load-")
	if err != nil {
		return nil, err
	}
	// The admin socket needs a short path.
	sock, err := os.MkdirTemp("/tmp", "ld")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(sock) }()
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(work, "data")
	}
	defer func() {
		if cfg.KeepData {
			logf("data directory kept at %s", dataDir)
			return
		}
		_ = os.RemoveAll(work)
		if cfg.DataDir != "" {
			_ = os.RemoveAll(cfg.DataDir)
		}
	}()

	bin := cfg.Binary
	if bin == "" {
		logf("building the service")
		if bin, err = buildBinary(work); err != nil {
			return nil, err
		}
	}

	began := time.Now()
	data, err := seed(ctx, dataDir, cfg, func(s string) { logf("%s", s) })
	if err != nil {
		return nil, fmt.Errorf("seeding: %w", err)
	}
	res.SeedTime = time.Since(began)
	res.Objects = data.Objects
	res.DataBytes = dirSize(dataDir)
	logf("seeded %d events (%.0f MB on disk) in %s", data.Objects, float64(res.DataBytes)/1e6, res.SeedTime.Round(time.Second))

	rec := newRecorder()
	cl := newClient("", rec)

	svc, err := startService(ctx, bin, dataDir, sock)
	if err != nil {
		return nil, err
	}
	defer func() { svc.stop() }()
	cl.base = svc.URL
	mem := func(phase string) {
		res.Memory = append(res.Memory, memoryPoint{Phase: phase, RSSKB: svc.RSS(), PeakKB: svc.PeakRSS()})
	}

	cold := func(label string) error {
		cs := coldStart{Label: label, Ready: svc.StartToReady, FirstTouch: map[int]time.Duration{}}
		logf("%s: healthy after %s", label, cs.Ready.Round(time.Millisecond))
		for _, lc := range data.Large {
			t := time.Now()
			if _, err := cl.rangeQuery(ctx, "first-touch", lc.User, lc.Path, queryWeek(0)); err != nil {
				return err
			}
			cs.FirstTouch[lc.Size] = time.Since(t)
		}
		cs.RSSKB = svc.RSS()
		res.ColdStarts = append(res.ColdStarts, cs)
		mem(label)
		return nil
	}
	if err := cold("start on the seeded directory"); err != nil {
		return nil, err
	}

	// Range queries and syncs on each large calendar.
	for _, lc := range data.Large {
		logf("large calendar of %d events", lc.Size)
		for i := 0; i < cfg.Queries; i++ {
			if _, err := cl.rangeQuery(ctx, fmt.Sprintf("calendar-query %d", lc.Size), lc.User, lc.Path, queryWeek(i)); err != nil {
				logf("  query: %v", err)
			}
		}
		for i := 0; i < 5; i++ {
			if _, err := cl.sync(ctx, fmt.Sprintf("sync-collection full %d", lc.Size), lc.User, lc.Path, ""); err != nil {
				logf("  sync: %v", err)
			}
		}
		token, err := cl.sync(ctx, fmt.Sprintf("sync-collection full %d", lc.Size), lc.User, lc.Path, "")
		if err != nil {
			logf("  sync: %v", err)
		}
		for i := 0; i < 20; i++ {
			name := fmt.Sprintf("probe-%d.ics", i)
			if err := cl.put(ctx, fmt.Sprintf("put %d", lc.Size), lc.User, lc.Path+name, buildEvent("probe", i)); err != nil {
				logf("  put: %v", err)
			}
			if token, err = cl.sync(ctx, fmt.Sprintf("sync-collection delta %d", lc.Size), lc.User, lc.Path, token); err != nil {
				logf("  sync: %v", err)
			}
		}
		mem(fmt.Sprintf("after the %d calendar", lc.Size))
	}

	// Recipients of the shared calendars.
	logf("shares")
	if len(data.Large) > 0 {
		recipients := sortedKeys(data.LargeShared)
		err := parallel(ctx, len(recipients), cfg.Concurrency, func(i int) error {
			u := data.Users[userIndex(recipients[i])]
			_ = cl.home(ctx, "propfind home with shares", u)
			_, _ = cl.sync(ctx, "sync-collection full shared large", u, data.LargeShared[u.Name], "")
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	mem("after the shares")

	// The storm.
	if err := storm(ctx, cfg, cl, data, logf); err != nil {
		return nil, err
	}
	mem("after the sync storm")

	// A second start, on the directory the load has written to.
	svc.stop()
	svc, err = startService(ctx, bin, dataDir, sock)
	if err != nil {
		return nil, err
	}
	cl.base = svc.URL
	if err := cold("restart after the load"); err != nil {
		return nil, err
	}

	res.Ops = rec.summaries()
	return res, nil
}

// queryWeek returns the start of the i-th query week.
func queryWeek(i int) time.Time {
	return eventsStart.Add(time.Duration(7*(i%50)) * 24 * time.Hour).Truncate(24 * time.Hour)
}

// storm runs the clients. Each polls its own calendar and the calendars
// shared with it with sync-collection from the token of its first sync, every
// interval, starting at a random point of the first interval. The recipients
// of the large calendar also ask it for a week once per poll. A writer adds
// events to the clients' calendars so that the polls find changes.
func storm(ctx context.Context, cfg config, cl *client, data *seeded, logf func(string, ...any)) error {
	logf("sync storm: %d clients every %s for %s", cfg.Clients, cfg.Interval, cfg.Duration)
	sctx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < cfg.Clients; i++ {
		u := data.Users[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(i)))
			own := "/dav/calendars/" + u.Name + "/personal/"
			paths := append([]string{own}, data.SharedPaths[u.Name]...)
			tokens := make([]string, len(paths))
			// The first sync of a client is a full one. It is not part of
			// the poll figures.
			select {
			case <-time.After(time.Duration(rng.Int63n(int64(cfg.Interval)))):
			case <-sctx.Done():
				return
			}
			for k, p := range paths {
				tokens[k], _ = cl.sync(sctx, "storm first sync", u, p, "")
			}
			large := data.LargeShared[u.Name]
			t := time.NewTicker(cfg.Interval)
			defer t.Stop()
			for {
				select {
				case <-t.C:
				case <-sctx.Done():
					return
				}
				for k, p := range paths {
					op := "storm poll own calendar"
					if k > 0 {
						op = "storm poll shared calendar"
					}
					tokens[k], _ = cl.sync(sctx, op, u, p, tokens[k])
				}
				if large != "" {
					_, _ = cl.rangeQuery(sctx, "storm range query on the large shared calendar", u, large, queryWeek(rng.Intn(50)))
				}
			}
		}()
	}
	if cfg.WriteRate > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(99))
			t := time.NewTicker(time.Duration(float64(time.Second) / cfg.WriteRate))
			defer t.Stop()
			for n := 0; ; n++ {
				select {
				case <-t.C:
				case <-sctx.Done():
					return
				}
				u := data.Users[rng.Intn(cfg.Clients)]
				_ = cl.put(sctx, "storm write", u, fmt.Sprintf("/dav/calendars/%s/personal/storm-%d.ics", u.Name, n), buildEvent("storm", n))
			}
		}()
	}
	wg.Wait()
	return nil
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}
