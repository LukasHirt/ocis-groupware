//go:build stack

// Package stack runs client flows against the compose stack: oCIS, the service
// and Traefik on one domain. The tests sign in through the oCIS identity
// provider as the demo users, create app passwords and then talk CalDAV and
// CardDAV the way real clients do. They are built only with the "stack" tag:
//
//	go test -tags stack -count=1 ./test/stack/
//
// The stack is found through the environment:
//
//	STACK_URL      the public URL of the stack, default https://ocis.localhost
//	STACK_CA_FILE  PEM file with the stack's certificate; without it, TLS
//	               verification is off, which suits the generated dev cert
//	STACK_PYTHON   a python interpreter with the caldav package installed,
//	               used for the Python client check; unset skips that test
//	STACK_WAIT     how long to wait for the stack to come up, default 3m
package stack

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Demo users oCIS creates with IDM_CREATE_DEMO_USERS=true. The Go flows use
// einstein; the Python client check uses marie, because it expects a user
// with nothing but the default calendar.
const (
	goUser     = "einstein"
	goPassword = "relativity"
	pyUser     = "marie"
	pyPassword = "radioactivity"
)

var (
	baseURL   string
	transport *http.Transport
)

func TestMain(m *testing.M) {
	baseURL = strings.TrimSuffix(env("STACK_URL", "https://ocis.localhost"), "/")
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if ca := os.Getenv("STACK_CA_FILE"); ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			fmt.Fprintln(os.Stderr, "STACK_CA_FILE:", err)
			os.Exit(1)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			fmt.Fprintln(os.Stderr, "STACK_CA_FILE: no certificate in", ca)
			os.Exit(1)
		}
		tlsConfig.RootCAs = pool
	} else {
		tlsConfig.InsecureSkipVerify = true
	}
	transport = &http.Transport{TLSClientConfig: tlsConfig, Proxy: http.ProxyFromEnvironment}

	wait := 3 * time.Minute
	if s := os.Getenv("STACK_WAIT"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "STACK_WAIT:", err)
			os.Exit(1)
		}
		wait = d
	}
	if err := waitForStack(wait); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// waitForStack polls the service's health endpoint and the identity
// provider's discovery and sign-in pages until all of them answer 200.
func waitForStack(wait time.Duration) error {
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	paths := []string{"/healthz", "/.well-known/openid-configuration", "/signin/v1/identifier"}
	deadline := time.Now().Add(wait)
	for {
		pending := ""
		for _, p := range paths {
			resp, err := client.Get(baseURL + p)
			if err != nil {
				pending = p + ": " + err.Error()
				break
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				pending = fmt.Sprintf("%s: status %d", p, resp.StatusCode)
				break
			}
		}
		if pending == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("stack at %s not ready after %s: %s", baseURL, wait, pending)
		}
		time.Sleep(2 * time.Second)
	}
}

func ctxTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// newHTTPClient returns a client for the stack that does not follow
// redirects, so that well-known answers can be inspected.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// bearerClient adds a bearer token to every request, as the web UI does.
type bearerClient struct {
	client *http.Client
	token  string
}

func (b bearerClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.client.Do(req)
}

// do sends one request with the given headers and returns the status, the
// response headers and the body.
func do(t *testing.T, ctx context.Context, method, path string, headers map[string]string, body io.Reader) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := newHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: reading body: %v", method, path, err)
	}
	return resp.StatusCode, resp.Header, string(data)
}

func basicHeader(user, password string) map[string]string {
	req := &http.Request{Header: http.Header{}}
	req.SetBasicAuth(user, password)
	return map[string]string{"Authorization": req.Header.Get("Authorization")}
}
