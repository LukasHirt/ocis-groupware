package graphapi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestImportSlotsLimitPerOwnerAndInTotal(t *testing.T) {
	s := newImportSlots()
	var releases []func()
	var refused int
	for i := range 5 {
		release, ok := s.acquire(fmt.Sprintf("owner-%d", i))
		if !ok {
			refused++
			continue
		}
		releases = append(releases, release)
	}
	if len(releases) != maxConcurrentImports || refused != 1 {
		t.Fatalf("%d admitted, %d refused; want %d and 1", len(releases), refused, maxConcurrentImports)
	}

	// A second slot for an owner that has one is refused, even with room left.
	releases[0]()
	releases[0]() // releasing twice frees nothing more
	if _, ok := s.acquire("owner-1"); ok {
		t.Fatal("owner-1 got a second slot")
	}
	if _, ok := s.acquire("owner-9"); !ok {
		t.Fatal("a freed slot was not available")
	}
	if _, ok := s.acquire("owner-5"); ok {
		t.Fatal("the service-wide limit was exceeded")
	}
}

// blockedUpload starts an import whose body does not end until the returned
// writer is closed, so the request holds its slot.
func (f *fixture) blockedUpload(target, token string) (pw *io.PipeWriter, done <-chan *httptest.ResponseRecorder) {
	pr, pw := io.Pipe()
	req := httptest.NewRequest(http.MethodPost, target, pr)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/calendar")
	rec := httptest.NewRecorder()
	ch := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		f.handler.ServeHTTP(rec, req)
		ch <- rec
	}()
	return pw, ch
}

// waitUntil retries cond until it holds, since the blocked request takes its
// slot on another goroutine.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 500 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSecondConcurrentImportOfTheSameOwnerIsRefused(t *testing.T) {
	f := newFixture(t)
	_, collID := einsteinCalendar(t, f)
	target := calImport(CalendarID(collID))

	pw, done := f.blockedUpload(target, "einstein")
	waitUntil(t, "the first import to hold its slot", func() bool { return f.h.imports.inUse("sub-einstein") })

	rec := f.upload(target, "einstein", "text/calendar", mixedCalendar)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second import: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want 5", got)
	}

	// Someone else is not held up by it.
	marieID := listCalendarsOf(t, f, "marie")[0].ID
	if rec := f.upload(calImport(marieID), "marie", "text/calendar", mixedCalendar); rec.Code != http.StatusOK {
		t.Fatalf("another owner: %d %s", rec.Code, rec.Body.String())
	}

	_, _ = pw.Write([]byte(mixedCalendar))
	_ = pw.Close()
	if rec := <-done; rec.Code != http.StatusOK {
		t.Fatalf("first import: %d %s", rec.Code, rec.Body.String())
	}
	// The slot is free again after a success.
	if rec := f.upload(target, "einstein", "text/calendar", mixedCalendar); rec.Code != http.StatusOK {
		t.Fatalf("after success: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportSlotIsReleasedAfterErrorAndDisconnect(t *testing.T) {
	f := newFixture(t)
	_, collID := einsteinCalendar(t, f)
	target := calImport(CalendarID(collID))

	// An error: a file that cannot be split.
	if rec := f.upload(target, "einstein", "text/calendar", "not a calendar"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad file: %d", rec.Code)
	}
	if f.h.imports.inUse("sub-einstein") {
		t.Fatal("the slot was kept after an error")
	}

	// A client that goes away in the middle of the body.
	pw, done := f.blockedUpload(target, "einstein")
	waitUntil(t, "the import to hold its slot", func() bool { return f.h.imports.inUse("sub-einstein") })
	_ = pw.CloseWithError(io.ErrUnexpectedEOF)
	rec := <-done
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disconnect: %d %s", rec.Code, rec.Body.String())
	}
	if f.h.imports.inUse("sub-einstein") {
		t.Fatal("the slot was kept after a disconnect")
	}
	if rec := f.upload(target, "einstein", "text/calendar", mixedCalendar); rec.Code != http.StatusOK {
		t.Fatalf("after disconnect: %d", rec.Code)
	}
}

func TestImportsAcrossOwnersAreCappedForTheService(t *testing.T) {
	f := newFixture(t)
	// Fill the service-wide slots with other owners, then import.
	var releases []func()
	for i := range maxConcurrentImports {
		r, ok := f.h.imports.acquire(fmt.Sprintf("other-%d", i))
		if !ok {
			t.Fatal("could not fill a slot")
		}
		releases = append(releases, r)
	}
	_, collID := einsteinCalendar(t, f)
	rec := f.upload(calImport(CalendarID(collID)), "einstein", "text/calendar", mixedCalendar)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "5" {
		t.Fatalf("over the service limit: %d %v", rec.Code, rec.Header())
	}
	for _, r := range releases {
		r()
	}
	if rec := f.upload(calImport(CalendarID(collID)), "einstein", "text/calendar", mixedCalendar); rec.Code != http.StatusOK {
		t.Fatalf("after release: %d", rec.Code)
	}
}
