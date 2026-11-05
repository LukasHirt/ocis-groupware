package principals

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"sync"
	"time"
)

// cacheTTL is how long a verified credential is trusted without checking
// its bcrypt hash again.
const cacheTTL = 5 * time.Minute

// cacheKey is the HMAC-SHA256 of an Authorization header.
type cacheKey [sha256.Size]byte

// cacheEntry is a credential that was verified recently. It names the
// principal and the app password, never the credential itself.
type cacheEntry struct {
	sub string // the owner
	id  string
	// gen is the principal's generation when the verification started.
	gen uint64
	// expires is the end of the TTL or of the app password's lifetime,
	// whichever comes first.
	expires time.Time
}

// credentialCache remembers verified credentials for a short time, so that a
// DAV client, which sends its credentials with every request, does not cost a
// bcrypt compare per request.
//
// Entries are keyed by an HMAC of the Authorization header under a key that
// is random per process. The header and the password are never kept, and a
// key cannot be matched against a guessed password without the HMAC key.
//
// Each principal has a generation number. invalidate increments it and drops
// the principal's entries. An entry made by a verification that started
// before the increment carries the old number and is refused by put and
// ignored by get, so a revoke cannot be undone by a verification that was
// already running.
type credentialCache struct {
	secret []byte
	ttl    time.Duration

	mu        sync.Mutex
	entries   map[cacheKey]cacheEntry
	gens      map[string]uint64
	lastSweep time.Time
}

func newCredentialCache(ttl time.Duration) *credentialCache {
	secret := make([]byte, 32)
	// crypto/rand.Read never returns an error; it crashes the program if
	// the system has no randomness.
	_, _ = rand.Read(secret)
	return &credentialCache{
		secret:  secret,
		ttl:     ttl,
		entries: make(map[cacheKey]cacheEntry),
		gens:    make(map[string]uint64),
	}
}

// key returns the cache key for an Authorization header.
func (c *credentialCache) key(header string) cacheKey {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(header))
	var k cacheKey
	mac.Sum(k[:0])
	return k
}

// generation returns the principal's current generation. A verification
// reads it before it reads the app passwords and passes it to put.
func (c *credentialCache) generation(sub string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gens[sub]
}

// get returns the entry for k if it has not expired and its principal has
// not been invalidated since.
func (c *credentialCache) get(k cacheKey, now time.Time) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		return cacheEntry{}, false
	}
	if !now.Before(e.expires) || e.gen != c.gens[e.sub] {
		delete(c.entries, k)
		return cacheEntry{}, false
	}
	return e, true
}

// put stores e under k for at most the TTL. It refuses an entry whose
// generation is no longer current and reports whether it stored it.
func (c *credentialCache) put(k cacheKey, e cacheEntry, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.gen != c.gens[e.sub] {
		return false
	}
	if limit := now.Add(c.ttl); limit.Before(e.expires) {
		e.expires = limit
	}
	if !now.Before(e.expires) {
		return false
	}
	if now.Sub(c.lastSweep) >= c.ttl {
		c.sweep(now)
	}
	c.entries[k] = e
	return true
}

// sweep drops expired entries. The caller holds c.mu.
func (c *credentialCache) sweep(now time.Time) {
	for k, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, k)
		}
	}
	c.lastSweep = now
}

// invalidate drops every entry of the principal and makes entries from
// verifications still running unusable.
func (c *credentialCache) invalidate(sub string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gens[sub]++
	for k, e := range c.entries {
		if e.sub == sub {
			delete(c.entries, k)
		}
	}
}

func (c *credentialCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
