package admin

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// socketClient is an HTTP client that dials the unix socket at path.
func socketClient(path string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", path)
			},
		},
		Timeout: 5 * time.Second,
	}
}

func TestServeAnswersOnTheSocket(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(shortDir(t), "admin.sock")
	l := mustListen(t, path)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, l, f.reg, nil) }()

	client := socketClient(path)
	resp, err := client.Get("http://admin/v1/principals")
	if err != nil {
		t.Fatalf("GET over the socket: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"username":"alice"`) {
		t.Fatalf("GET = %d %s", resp.StatusCode, body)
	}

	resp, err = client.Post("http://admin/v1/principals/disable", "application/json", strings.NewReader(`{"sub":"sub-1"}`))
	if err != nil {
		t.Fatalf("POST over the socket: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST = %d", resp.StatusCode)
	}
	if p, _ := f.reg.Get("sub-1"); !p.Disabled {
		t.Fatal("alice is not disabled")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the context was cancelled")
	}
	if _, err := client.Get("http://admin/v1/principals"); err == nil {
		t.Fatal("the socket still answers after Serve returned")
	}
}

// TestServeRefusesNonUnixListener pins that Serve takes a *net.UnixListener:
// any other listener is refused by the compiler, and a nil one by Serve.
func TestServeRefusesNonUnixListener(t *testing.T) {
	f := newFixture(t)
	err := Serve(context.Background(), nil, f.reg, nil)
	if err == nil || !strings.Contains(err.Error(), "listener") {
		t.Fatalf("Serve(nil) = %v, want an error about the listener", err)
	}
}
