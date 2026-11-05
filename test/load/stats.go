package main

import (
	"sort"
	"sync"
	"time"
)

// recorder collects request latencies by operation name. Failed requests are
// counted but their latency is left out of the percentiles.
type recorder struct {
	mu   sync.Mutex
	ops  map[string][]time.Duration
	errs map[string]int
}

func newRecorder() *recorder {
	return &recorder{ops: map[string][]time.Duration{}, errs: map[string]int{}}
}

func (r *recorder) add(op string, d time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ops[op]; !ok {
		r.ops[op] = nil
	}
	if err != nil {
		r.errs[op]++
		return
	}
	r.ops[op] = append(r.ops[op], d)
}

// summary is the latency distribution of one operation.
type summary struct {
	Name          string
	Count         int
	Errors        int
	P50, P95, P99 time.Duration
	Max           time.Duration
}

func (r *recorder) summaries() []summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.ops))
	for n := range r.ops {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]summary, 0, len(names))
	for _, n := range names {
		ds := append([]time.Duration(nil), r.ops[n]...)
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		s := summary{Name: n, Count: len(ds), Errors: r.errs[n]}
		if len(ds) > 0 {
			s.P50, s.P95, s.P99, s.Max = pct(ds, 50), pct(ds, 95), pct(ds, 99), ds[len(ds)-1]
		}
		out = append(out, s)
	}
	return out
}

// pct returns the nearest-rank percentile of sorted ds.
func pct(ds []time.Duration, p int) time.Duration {
	rank := (p*len(ds) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return ds[rank-1]
}
