package graphapi

import (
	"net/http"
	"sync"
)

const (
	// maxConcurrentImports is how many imports run at once in the service,
	// whoever sends them. An import holds the whole file and its parsed
	// objects in memory, so this bounds that to a few times maxImportBytes.
	maxConcurrentImports = 4
	// importRetryAfter is the Retry-After of a refused import, in seconds.
	importRetryAfter = "5"
)

// importSlots limits imports to one per owner and maxConcurrentImports in
// total.
type importSlots struct {
	mu     sync.Mutex
	owners map[string]struct{}
}

func newImportSlots() *importSlots {
	return &importSlots{owners: map[string]struct{}{}}
}

// acquire takes a slot for the owner. It returns false when the owner has an
// import running or the service is at its limit. The release function frees
// the slot, and calling it again does nothing.
func (s *importSlots) acquire(owner string) (release func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.owners[owner]; busy || len(s.owners) >= maxConcurrentImports {
		return nil, false
	}
	s.owners[owner] = struct{}{}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.owners, owner)
			s.mu.Unlock()
		})
	}, true
}

// inUse reports whether the owner has an import running.
func (s *importSlots) inUse(owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.owners[owner]
	return ok
}

func tooManyImports() *apiError {
	e := newError(http.StatusTooManyRequests, codeTooManyRequests, "too many imports are running, try again shortly")
	e.headers = map[string]string{"Retry-After": importRetryAfter}
	return e
}
