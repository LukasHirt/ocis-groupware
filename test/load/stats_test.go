package main

import (
	"errors"
	"testing"
	"time"
)

var errTest = errors.New("test error")

func TestSummarizePercentiles(t *testing.T) {
	r := newRecorder()
	for i := 1; i <= 100; i++ {
		r.add("op", time.Duration(i)*time.Millisecond, nil)
	}
	r.add("op", 0, errTest)
	got := r.summaries()
	if len(got) != 1 {
		t.Fatalf("got %d summaries, want 1", len(got))
	}
	s := got[0]
	if s.Count != 100 || s.Errors != 1 {
		t.Errorf("count/errors = %d/%d, want 100/1", s.Count, s.Errors)
	}
	if s.P50 != 50*time.Millisecond || s.P95 != 95*time.Millisecond || s.P99 != 99*time.Millisecond || s.Max != 100*time.Millisecond {
		t.Errorf("p50/p95/p99/max = %v/%v/%v/%v", s.P50, s.P95, s.P99, s.Max)
	}
}

func TestSummarizeEmptyAndOrder(t *testing.T) {
	r := newRecorder()
	r.add("b", time.Millisecond, nil)
	r.add("a", time.Millisecond, errTest)
	got := r.summaries()
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("summaries not sorted by name: %+v", got)
	}
	if got[0].Count != 0 || got[0].P95 != 0 {
		t.Errorf("an op with only errors has no latency: %+v", got[0])
	}
}
