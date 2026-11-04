package principals

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testHeader = "Basic YWxpY2U6c2VjcmV0" // alice:secret

func TestCacheKeyIsHMACOfHeader(t *testing.T) {
	c := newCredentialCache(cacheTTL)
	got := c.key(testHeader)

	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(testHeader))
	if want := mac.Sum(nil); !bytes.Equal(got[:], want) {
		t.Fatalf("key = %x, want HMAC-SHA256 %x", got, want)
	}
	if c.key(testHeader+" ") == got {
		t.Fatal("different headers share a key")
	}
	if len(c.secret) < 32 {
		t.Fatalf("HMAC key has %d bytes, want at least 32", len(c.secret))
	}
}

func TestCacheKeyDiffersPerProcess(t *testing.T) {
	a, b := newCredentialCache(cacheTTL), newCredentialCache(cacheTTL)
	if bytes.Equal(a.secret, b.secret) || a.key(testHeader) == b.key(testHeader) {
		t.Fatal("two caches share an HMAC key")
	}
}

func TestCacheNeverHoldsTheHeader(t *testing.T) {
	c := newCredentialCache(cacheTTL)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	k := c.key(testHeader)
	c.put(k, cacheEntry{sub: "sub-1", id: "id-1", gen: c.generation("sub-1"), expires: now.Add(time.Hour)}, now)

	dump := fmt.Sprintf("%#v %v", c.entries, c.entries)
	for _, secret := range []string{testHeader, "YWxpY2U6c2VjcmV0", "secret"} {
		if strings.Contains(dump, secret) {
			t.Fatalf("cache holds %q: %s", secret, dump)
		}
	}
}

func TestCacheGetAndTTL(t *testing.T) {
	c := newCredentialCache(5 * time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	k := c.key(testHeader)
	if _, ok := c.get(k, now); ok {
		t.Fatal("empty cache returned an entry")
	}
	c.put(k, cacheEntry{sub: "sub-1", id: "id-1", gen: c.generation("sub-1"), expires: now.Add(time.Hour)}, now)

	e, ok := c.get(k, now.Add(5*time.Minute-time.Nanosecond))
	if !ok || e.sub != "sub-1" || e.id != "id-1" {
		t.Fatalf("get before TTL = %+v, %v", e, ok)
	}
	if _, ok := c.get(k, now.Add(5*time.Minute)); ok {
		t.Fatal("entry survived its 5 minute TTL")
	}
	if c.len() != 0 {
		t.Fatalf("expired entry kept, len = %d", c.len())
	}
}

// An entry never outlives the app password it was verified against.
func TestCacheEntryEndsWithThePassword(t *testing.T) {
	c := newCredentialCache(5 * time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	k := c.key(testHeader)
	c.put(k, cacheEntry{sub: "sub-1", id: "id-1", gen: c.generation("sub-1"), expires: now.Add(time.Minute)}, now)
	if _, ok := c.get(k, now.Add(time.Minute-time.Nanosecond)); !ok {
		t.Fatal("entry gone before the password expired")
	}
	if _, ok := c.get(k, now.Add(time.Minute)); ok {
		t.Fatal("entry outlived the password")
	}
}

func TestCacheInvalidate(t *testing.T) {
	c := newCredentialCache(cacheTTL)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	k1, k2, k3 := c.key("Basic a"), c.key("Basic b"), c.key("Basic c")
	c.put(k1, cacheEntry{sub: "sub-1", id: "a", gen: c.generation("sub-1"), expires: later}, now)
	c.put(k2, cacheEntry{sub: "sub-1", id: "b", gen: c.generation("sub-1"), expires: later}, now)
	c.put(k3, cacheEntry{sub: "sub-2", id: "c", gen: c.generation("sub-2"), expires: later}, now)

	c.invalidate("sub-1")
	for _, k := range []cacheKey{k1, k2} {
		if _, ok := c.get(k, now); ok {
			t.Fatal("entry of an invalidated principal survived")
		}
	}
	if _, ok := c.get(k3, now); !ok {
		t.Fatal("entry of another principal was dropped")
	}
	if c.len() != 1 {
		t.Fatalf("len = %d, want 1", c.len())
	}
}

// A verification that started before a revoke must not put its result into
// the cache after the revoke cleared it.
func TestCacheRefusesStaleGeneration(t *testing.T) {
	c := newCredentialCache(cacheTTL)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	gen := c.generation("sub-1")
	c.invalidate("sub-1")
	k := c.key(testHeader)
	if c.put(k, cacheEntry{sub: "sub-1", id: "a", gen: gen, expires: now.Add(time.Hour)}, now) {
		t.Fatal("put with a stale generation succeeded")
	}
	if _, ok := c.get(k, now); ok {
		t.Fatal("stale entry is in the cache")
	}
}

func TestCacheSweepsExpiredEntries(t *testing.T) {
	c := newCredentialCache(5 * time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 10 {
		k := c.key(fmt.Sprintf("Basic %d", i))
		c.put(k, cacheEntry{sub: "sub-1", gen: c.generation("sub-1"), expires: now.Add(time.Hour)}, now)
	}
	later := now.Add(6 * time.Minute)
	c.put(c.key("Basic new"), cacheEntry{sub: "sub-1", gen: c.generation("sub-1"), expires: later.Add(time.Hour)}, later)
	if c.len() != 1 {
		t.Fatalf("len = %d after a put past the TTL, want 1", c.len())
	}
}
