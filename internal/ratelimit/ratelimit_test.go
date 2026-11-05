package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/owncloud/ocis-groupware/internal/principals"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }

func newLimiter(t *testing.T, mutate ...func(*Config)) (*Limiter, *clock, *prometheus.Registry) {
	t.Helper()
	clk := &clock{now: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	reg := prometheus.NewRegistry()
	cfg := Config{MaxFailures: 3, Window: 10 * time.Minute, MaxEntries: 100, Now: clk.Now, Registerer: reg}
	for _, m := range mutate {
		m(&cfg)
	}
	l, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l, clk, reg
}

func counter(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		var sum float64
		for _, m := range f.GetMetric() {
			sum += m.GetCounter().GetValue() + m.GetGauge().GetValue()
		}
		return sum
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func TestBlocksAfterMaxFailuresWithRetryAfter(t *testing.T) {
	l, clk, _ := newLimiter(t)
	for i := 0; i < 3; i++ {
		if _, ok := l.Allow("alice", "10.0.0.1"); !ok {
			t.Fatalf("attempt %d refused early", i)
		}
		l.Fail("alice", "10.0.0.1")
		clk.advance(time.Minute)
	}
	retry, ok := l.Allow("alice", "10.0.0.1")
	if ok {
		t.Fatal("attempt after the limit was allowed")
	}
	if retry != 7*time.Minute {
		t.Fatalf("retry after = %v, want 7m", retry)
	}
	// Another address and another user are not affected.
	if _, ok := l.Allow("alice", "10.0.0.2"); !ok {
		t.Error("another address was refused")
	}
	if _, ok := l.Allow("bob", "10.0.0.1"); !ok {
		t.Error("another user was refused")
	}
	clk.advance(7 * time.Minute)
	if _, ok := l.Allow("alice", "10.0.0.1"); !ok {
		t.Error("still refused after the window")
	}
}

func TestSuccessIsNotLimitedAndResetsTheCount(t *testing.T) {
	l, _, _ := newLimiter(t)
	for i := 0; i < 12; i++ {
		l.Fail("alice", "10.0.0.1")
		l.Fail("alice", "10.0.0.1")
		l.Succeed("alice", "10.0.0.1")
		if _, ok := l.Allow("alice", "10.0.0.1"); !ok {
			t.Fatalf("round %d: refused although every other attempt succeeded", i)
		}
	}
}

func TestOneAddressGuessingManyUsersIsStopped(t *testing.T) {
	l, _, _ := newLimiter(t)
	blocked := false
	for i := 0; i < 100 && !blocked; i++ {
		user := string(rune('a'+i%26)) + string(rune('a'+i/26))
		l.Fail(user, "10.0.0.9")
		_, ok := l.Allow("fresh", "10.0.0.9")
		blocked = !ok
	}
	if !blocked {
		t.Fatal("an address with many failed users was never refused")
	}
}

func TestOneUserGuessedFromManyAddressesIsStopped(t *testing.T) {
	l, _, _ := newLimiter(t)
	blocked := false
	for i := 0; i < 100 && !blocked; i++ {
		l.Fail("alice", netip.AddrFrom4([4]byte{10, 1, 0, byte(i)}).String())
		_, ok := l.Allow("alice", "192.0.2.1")
		blocked = !ok
	}
	if !blocked {
		t.Fatal("a user failed from many addresses was never refused")
	}
}

func TestMemoryIsBounded(t *testing.T) {
	l, _, reg := newLimiter(t, func(c *Config) { c.MaxEntries = 50 })
	for i := 0; i < 1000; i++ {
		l.Fail("user"+itoa(i), "10.0."+itoa(i/250)+"."+itoa(i%250))
	}
	if n := l.Len(); n > 50 {
		t.Fatalf("limiter holds %d entries, want at most 50", n)
	}
	if got := counter(t, reg, "groupware_auth_limiter_entries"); got > 50 || got == 0 {
		t.Fatalf("entries gauge = %v", got)
	}
}

func TestLeastRecentlyUsedEntryGoesFirst(t *testing.T) {
	l, _, _ := newLimiter(t, func(c *Config) { c.MaxEntries = 6 })
	// Two pair entries, one user entry and one address entry per attempt would
	// not fit; what matters is that the busy offender stays.
	for i := 0; i < 3; i++ {
		l.Fail("alice", "10.0.0.1")
	}
	for i := 0; i < 50; i++ {
		l.Fail("u"+itoa(i), "10.9."+itoa(i))
		l.Fail("alice", "10.0.0.1") // keeps alice recent
	}
	if _, ok := l.Allow("alice", "10.0.0.1"); ok {
		t.Fatal("the recently used offender was evicted")
	}
}

func TestMetrics(t *testing.T) {
	l, _, reg := newLimiter(t)
	for i := 0; i < 3; i++ {
		l.Fail("alice", "10.0.0.1")
	}
	l.Allow("alice", "10.0.0.1")
	l.Allow("alice", "10.0.0.1")
	if got := counter(t, reg, "groupware_auth_failures_total"); got != 3 {
		t.Errorf("failures = %v, want 3", got)
	}
	if got := counter(t, reg, "groupware_auth_rate_limited_total"); got != 2 {
		t.Errorf("rate limited = %v, want 2", got)
	}
}

func TestGateRefusesLikeAllow(t *testing.T) {
	l, _, _ := newLimiter(t)
	ctx := WithAddr(context.Background(), "10.0.0.1")
	if err := l.Gate(ctx, "alice"); err != nil {
		t.Fatalf("Gate before failures: %v", err)
	}
	for i := 0; i < 3; i++ {
		l.Fail("alice", "10.0.0.1")
	}
	err := l.Gate(ctx, "alice")
	if !errors.Is(err, principals.ErrRateLimited) {
		t.Fatalf("Gate = %v, want ErrRateLimited", err)
	}
	var limited *LimitedError
	if !errors.As(err, &limited) || limited.RetryAfter <= 0 {
		t.Fatalf("Gate error carries no retry time: %v", err)
	}
	if err := l.Gate(WithAddr(context.Background(), "10.0.0.2"), "alice"); err != nil {
		t.Errorf("Gate for another address: %v", err)
	}
}

func request(remote string, xff ...string) *http.Request {
	r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientAddr(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.5/32")}
	l, _, _ := newLimiter(t, func(c *Config) { c.TrustedProxies = proxies })
	tests := []struct {
		name string
		req  *http.Request
		want string
	}{
		{"no proxy, header ignored", request("203.0.113.7:5000", "1.2.3.4"), "203.0.113.7"},
		{"untrusted peer cannot spoof", request("198.51.100.2:80", "10.0.0.3, 1.2.3.4"), "198.51.100.2"},
		{"trusted proxy, client from header", request("10.0.0.2:80", "203.0.113.7"), "203.0.113.7"},
		{"chain of trusted proxies", request("10.0.0.2:80", "203.0.113.7, 10.0.0.9, 192.168.1.5"), "203.0.113.7"},
		{"client-supplied prefix is skipped", request("10.0.0.2:80", "6.6.6.6, 203.0.113.7"), "203.0.113.7"},
		{"several header lines", request("10.0.0.2:80", "6.6.6.6", "203.0.113.7"), "203.0.113.7"},
		{"garbage falls back to the peer", request("10.0.0.2:80", "not-an-address"), "10.0.0.2"},
		{"no header", request("10.0.0.2:80"), "10.0.0.2"},
		{"all trusted", request("10.0.0.2:80", "10.0.0.8"), "10.0.0.8"},
		{"ipv6 client counts by /64", request("10.0.0.2:80", "2001:db8:1:2:aaaa:bbbb:cccc:dddd"), "2001:db8:1:2::"},
		{"ipv4 in ipv6 form", request("[::ffff:203.0.113.7]:80"), "203.0.113.7"},
		{"peer without port", request("203.0.113.7"), "203.0.113.7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := l.ClientAddr(tc.req); got != tc.want {
				t.Errorf("ClientAddr = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	for _, c := range []Config{{MaxFailures: 0, Window: time.Minute}, {MaxFailures: 1}} {
		if _, err := New(c); err == nil {
			t.Errorf("New(%+v) succeeded", c)
		}
	}
}

func TestParseProxies(t *testing.T) {
	got, err := ParseProxies([]string{"10.0.0.0/8", " 192.168.1.5 ", "fd00::/8", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.5/32"), netip.MustParsePrefix("fd00::/8")}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if _, err := ParseProxies([]string{"nope"}); err == nil {
		t.Error("invalid entry accepted")
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
