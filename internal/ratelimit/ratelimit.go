// Package ratelimit slows down guessing of app passwords. It counts failed
// HTTP Basic sign-ins and refuses further attempts for a while once a user, an
// address or a pair of them has failed too often.
package ratelimit

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

const (
	// DefaultMaxFailures is the number of failed attempts of one user from one
	// address that are allowed within a window.
	DefaultMaxFailures = 10
	// DefaultWindow is how long failures count, and so how long a block lasts.
	DefaultWindow = 15 * time.Minute
	// DefaultMaxEntries bounds the number of counters kept in memory.
	DefaultMaxEntries = 10000

	// wideFactor scales the limit of a whole user and of a whole address
	// against the limit of one user and address pair. It stops a guesser who
	// spreads attempts over many addresses or many users, without letting a
	// few mistyped passwords of one person lock out everybody else.
	wideFactor = 10
)

// Config configures a Limiter.
type Config struct {
	// MaxFailures is the number of failures of one user and address pair that
	// a window allows. A whole user, and a whole address, may fail 10 times as
	// often. It must be at least 1.
	MaxFailures int
	// Window is how long a counter lives, counted from its first failure. It
	// must be positive.
	Window time.Duration
	// MaxEntries bounds the memory. When it is reached, the counter that was
	// used least recently is dropped. 0 means DefaultMaxEntries.
	MaxEntries int
	// TrustedProxies are the networks whose X-Forwarded-For header is
	// believed. From anyone else the header is ignored.
	TrustedProxies []netip.Prefix
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
	// Registerer receives the metrics. Nil registers none.
	Registerer prometheus.Registerer
}

// LimitedError is the refusal of a Limiter. It matches principals.ErrRateLimited.
type LimitedError struct {
	// RetryAfter is how long until the block ends.
	RetryAfter time.Duration
}

func (e *LimitedError) Error() string {
	return fmt.Sprintf("%v (retry in %v)", principals.ErrRateLimited, e.RetryAfter.Round(time.Second))
}

func (e *LimitedError) Unwrap() error { return principals.ErrRateLimited }

type entry struct {
	key   string
	count int
	start time.Time
}

// Limiter counts failed sign-ins. It is safe for concurrent use.
type Limiter struct {
	max     int
	window  time.Duration
	cap     int
	proxies []netip.Prefix
	now     func() time.Time

	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List // front is the most recently used

	failures, limited prometheus.Counter
}

// New returns a Limiter.
func New(cfg Config) (*Limiter, error) {
	if cfg.MaxFailures < 1 {
		return nil, errors.New("ratelimit: MaxFailures must be at least 1")
	}
	if cfg.Window <= 0 {
		return nil, errors.New("ratelimit: Window must be positive")
	}
	if cfg.MaxEntries == 0 {
		cfg.MaxEntries = DefaultMaxEntries
	}
	if cfg.MaxEntries < 3 {
		return nil, errors.New("ratelimit: MaxEntries must be at least 3")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	l := &Limiter{
		max: cfg.MaxFailures, window: cfg.Window, cap: cfg.MaxEntries,
		proxies: cfg.TrustedProxies, now: cfg.Now,
		entries: map[string]*list.Element{}, order: list.New(),
		failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "groupware_auth_failures_total",
			Help: "Failed HTTP Basic sign-ins counted by the rate limiter.",
		}),
		limited: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "groupware_auth_rate_limited_total",
			Help: "HTTP Basic sign-in attempts refused by the rate limiter.",
		}),
	}
	if cfg.Registerer != nil {
		entries := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "groupware_auth_limiter_entries",
			Help: "Counters currently held by the rate limiter.",
		}, func() float64 { return float64(l.Len()) })
		for _, c := range []prometheus.Collector{l.failures, l.limited, entries} {
			if err := cfg.Registerer.Register(c); err != nil {
				return nil, fmt.Errorf("ratelimit: register metrics: %w", err)
			}
		}
	}
	return l, nil
}

// Len returns the number of counters held.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}

func pairKey(user, addr string) string { return "p\x00" + user + "\x00" + addr }
func userKey(user string) string       { return "u\x00" + user }
func addrKey(addr string) string       { return "a\x00" + addr }

// Allow reports whether a sign-in attempt of user from addr may go on. When it
// may not, it returns how long to wait.
func (l *Limiter) Allow(user, addr string) (time.Duration, bool) {
	now := l.now()
	l.mu.Lock()
	var wait time.Duration
	for _, k := range []struct {
		key   string
		limit int
	}{
		{pairKey(user, addr), l.max},
		{userKey(user), l.max * wideFactor},
		{addrKey(addr), l.max * wideFactor},
	} {
		el, ok := l.entries[k.key]
		if !ok {
			continue
		}
		e := el.Value.(*entry)
		if !now.Before(e.start.Add(l.window)) {
			l.remove(el)
			continue
		}
		if e.count >= k.limit {
			wait = max(wait, e.start.Add(l.window).Sub(now))
		}
	}
	l.mu.Unlock()
	if wait > 0 {
		l.limited.Inc()
		return wait, false
	}
	return 0, true
}

// Fail records a failed attempt of user from addr.
func (l *Limiter) Fail(user, addr string) {
	now := l.now()
	l.mu.Lock()
	for _, key := range []string{pairKey(user, addr), userKey(user), addrKey(addr)} {
		if el, ok := l.entries[key]; ok {
			e := el.Value.(*entry)
			if now.Before(e.start.Add(l.window)) {
				e.count++
				l.order.MoveToFront(el)
				continue
			}
			l.remove(el)
		}
		l.entries[key] = l.order.PushFront(&entry{key: key, count: 1, start: now})
	}
	for l.order.Len() > l.cap {
		l.remove(l.order.Back())
	}
	l.mu.Unlock()
	l.failures.Inc()
}

// Succeed clears the failures of user from addr after a good sign-in. The
// counts of the whole user and the whole address stay, so a guesser cannot
// reset them by signing in with a password of its own.
func (l *Limiter) Succeed(user, addr string) {
	l.mu.Lock()
	if el, ok := l.entries[pairKey(user, addr)]; ok {
		l.remove(el)
	}
	l.mu.Unlock()
}

func (l *Limiter) remove(el *list.Element) {
	delete(l.entries, el.Value.(*entry).key)
	l.order.Remove(el)
}

type addrKeyType struct{}

// WithAddr returns a context that carries the client address Gate looks at.
func WithAddr(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, addrKeyType{}, addr)
}

// Gate is a check for principals.WithLegacyGate. It refuses with a
// *LimitedError while the user and the address in ctx (see WithAddr) are
// blocked. It does not count anything.
func (l *Limiter) Gate(ctx context.Context, username string) error {
	addr, _ := ctx.Value(addrKeyType{}).(string)
	if wait, ok := l.Allow(username, addr); !ok {
		return &LimitedError{RetryAfter: wait}
	}
	return nil
}

// ParseProxies parses a list of addresses and networks such as 10.0.0.0/8 or
// 192.168.1.5. Blank entries are skipped.
func ParseProxies(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("ratelimit: %q is not an address or network", s)
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

func (l *Limiter) trusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range l.proxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ClientAddr returns the address a request is counted under. It is the
// address of the peer, unless the peer is a trusted proxy. Then it is the
// rightmost X-Forwarded-For entry that is not itself a trusted proxy, because
// only the entries that trusted proxies appended can be believed. IPv6
// addresses are reduced to their /64.
func (l *Limiter) ClientAddr(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	peer, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return host
	}
	client := peer
	if l.trusted(peer) {
		var hops []string
		for _, v := range r.Header.Values("X-Forwarded-For") {
			for _, h := range strings.Split(v, ",") {
				hops = append(hops, strings.TrimSpace(h))
			}
		}
		for i := len(hops) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(hops[i])
			if err != nil {
				break // not written by a proxy we know: keep the peer
			}
			client = a
			if !l.trusted(a) {
				break
			}
		}
	}
	client = client.Unmap()
	if client.Is6() {
		return netip.PrefixFrom(client, 64).Masked().Addr().String()
	}
	return client.String()
}
