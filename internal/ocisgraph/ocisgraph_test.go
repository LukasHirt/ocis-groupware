package ocisgraph

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const meBody = `{
  "id": "u-1",
  "displayName": "Albert Einstein",
  "mail": "einstein@example.org",
  "onPremisesSamAccountName": "einstein",
  "memberOf": [
    {"id": "g-1", "displayName": "users", "groupTypes": []},
    {"id": "g-2", "displayName": "physics-lovers"}
  ]
}`

func newClient(t *testing.T, base string, opts ...func(*Config)) *Client {
	t.Helper()
	cfg := Config{BaseURL: base}
	for _, o := range opts {
		o(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMe(t *testing.T) {
	var gotAuth, gotPath, gotExpand string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotExpand = r.Header.Get("Authorization"), r.URL.Path, r.URL.Query().Get("$expand")
		_, _ = w.Write([]byte(meBody))
	}))
	defer srv.Close()

	u, err := newClient(t, srv.URL+"/").Me(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotPath != "/graph/v1.0/me" || gotExpand != "memberOf" {
		t.Errorf("request: %q %q %q", gotAuth, gotPath, gotExpand)
	}
	want := User{
		ID: "u-1", Username: "einstein", DisplayName: "Albert Einstein", Mail: "einstein@example.org",
		Groups: []Group{{ID: "g-1", Name: "users"}, {ID: "g-2", Name: "physics-lovers"}},
	}
	if !reflect.DeepEqual(u, want) {
		t.Errorf("user = %+v, want %+v", u, want)
	}
}

func TestMeWithoutGroups(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u-1"}`))
	}))
	defer srv.Close()
	u, err := newClient(t, srv.URL).Me(context.Background(), "tok")
	if err != nil || u.ID != "u-1" || len(u.Groups) != 0 {
		t.Fatalf("user = %+v, %v", u, err)
	}
}

func TestMeStatuses(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusUnauthorized:        ErrUnauthorized,
		http.StatusForbidden:           ErrUnauthorized,
		http.StatusInternalServerError: nil,
		http.StatusServiceUnavailable:  nil,
		http.StatusNotFound:            nil,
	} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "nope", status)
			}))
			defer srv.Close()
			_, err := newClient(t, srv.URL).Me(context.Background(), "s3cret-value")
			if err == nil {
				t.Fatal("no error")
			}
			if want != nil && !errors.Is(err, want) {
				t.Errorf("err = %v, want %v", err, want)
			}
			if want == nil && errors.Is(err, ErrUnauthorized) {
				t.Errorf("err = %v must not be ErrUnauthorized", err)
			}
			if strings.Contains(err.Error(), "s3cret-value") {
				t.Errorf("error %q mentions the token", err)
			}
		})
	}
}

func TestMeMalformed(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     `<html>`,
		"no id":        `{"displayName":"x"}`,
		"truncated":    `{"id":"u-1","memberOf":[`,
		"wrong type":   `{"id":7}`,
		"groups wrong": `{"id":"u-1","memberOf":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			if _, err := newClient(t, srv.URL).Me(context.Background(), "tok"); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestMeSkipsGroupsWithoutID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u-1","memberOf":[{"displayName":"x"},{"id":"g-1","displayName":"y"}]}`))
	}))
	defer srv.Close()
	u, err := newClient(t, srv.URL).Me(context.Background(), "tok")
	if err != nil || len(u.Groups) != 1 || u.Groups[0].ID != "g-1" {
		t.Fatalf("user = %+v, %v", u, err)
	}
}

func TestMeTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	c := newClient(t, srv.URL, func(c *Config) { c.Timeout = 50 * time.Millisecond })
	start := time.Now()
	_, err := c.Me(context.Background(), "tok")
	if err == nil {
		t.Fatal("no error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("did not time out")
	}
}

func TestMeRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(meBody))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newClient(t, srv.URL).Me(ctx, "tok"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestMeFollowsMemberOfPaging(t *testing.T) {
	var srv *httptest.Server
	var hits atomic.Int32
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("page %s without the token", r.URL)
		}
		switch r.URL.Query().Get("page") {
		case "":
			_, _ = fmt.Fprintf(w, `{"id":"u-1","memberOf":[{"id":"g-1","displayName":"a"}],"memberOf@odata.nextLink":%q}`, srv.URL+"/graph/v1.0/me/memberOf?page=2")
		case "2":
			_, _ = fmt.Fprintf(w, `{"value":[{"id":"g-2","displayName":"b"}],"@odata.nextLink":%q}`, srv.URL+"/graph/v1.0/me/memberOf?page=3")
		default:
			_, _ = w.Write([]byte(`{"value":[{"id":"g-3","displayName":"c"}]}`))
		}
	}))
	defer srv.Close()
	u, err := newClient(t, srv.URL).Me(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Groups) != 3 || u.Groups[2].ID != "g-3" || hits.Load() != 3 {
		t.Fatalf("groups = %+v after %d requests", u.Groups, hits.Load())
	}
}

func TestMePagingStaysOnTheServer(t *testing.T) {
	var other atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { other.Add(1) }))
	defer elsewhere.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"id":"u-1","memberOf":[],"memberOf@odata.nextLink":%q}`, elsewhere.URL+"/x")
	}))
	defer srv.Close()
	if _, err := newClient(t, srv.URL).Me(context.Background(), "tok"); err == nil {
		t.Fatal("a next link to another host was followed or ignored silently")
	}
	if other.Load() != 0 {
		t.Fatal("the token was sent to another host")
	}
}

func TestMePagingEndsAtALimit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"id":"u-1","value":[],"memberOf":[],"memberOf@odata.nextLink":%q,"@odata.nextLink":%q}`, srv.URL+"/again", srv.URL+"/again")
	}))
	defer srv.Close()
	if _, err := newClient(t, srv.URL).Me(context.Background(), "tok"); err == nil {
		t.Fatal("an endless chain of pages succeeded")
	}
}

func TestMeLimitsTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u-1","x":"`))
		chunk := []byte(strings.Repeat("a", 1<<20))
		for range 6 {
			_, _ = w.Write(chunk)
		}
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()
	if _, err := newClient(t, srv.URL).Me(context.Background(), "tok"); err == nil {
		t.Fatal("an oversized body was accepted")
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("empty base URL accepted")
	}
	if _, err := New(Config{BaseURL: "not a url"}); err == nil {
		t.Error("bad base URL accepted")
	}
	if _, err := New(Config{BaseURL: "ftp://x"}); err == nil {
		t.Error("non-http base URL accepted")
	}
	if _, err := New(Config{BaseURL: "https://x", CAFile: filepath.Join(t.TempDir(), "nope.pem")}); err == nil {
		t.Error("missing CA file accepted")
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{BaseURL: "https://x", CAFile: empty}); err == nil {
		t.Error("CA file without certificates accepted")
	}
}

func TestMeTrustsTheCAFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(meBody))
	}))
	defer srv.Close()
	if _, err := newClient(t, srv.URL).Me(context.Background(), "tok"); err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	pem := "-----BEGIN CERTIFICATE-----\n" + b64(srv.Certificate().Raw) + "-----END CERTIFICATE-----\n"
	if err := os.WriteFile(ca, []byte(pem), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, srv.URL, func(c *Config) { c.CAFile = ca })
	if _, err := c.Me(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
}

func b64(der []byte) string {
	s := base64.StdEncoding.EncodeToString(der)
	var b strings.Builder
	for len(s) > 64 {
		b.WriteString(s[:64] + "\n")
		s = s[64:]
	}
	b.WriteString(s + "\n")
	return b.String()
}

func TestLookupUserAndGroup(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.EscapedPath()
		switch {
		case strings.HasSuffix(gotPath, "/gone"):
			http.NotFound(w, r)
		case strings.HasSuffix(gotPath, "/forbidden"):
			w.WriteHeader(http.StatusForbidden)
		default:
			_, _ = w.Write([]byte(`{"id":"x-1","displayName":"Marie Curie","onPremisesSamAccountName":""}`))
		}
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	ctx := context.Background()

	e, err := c.User(ctx, "tok", "x-1")
	if err != nil {
		t.Fatal(err)
	}
	if e != (Entity{ID: "x-1", DisplayName: "Marie Curie"}) || gotAuth != "Bearer tok" || gotPath != "/graph/v1.0/users/x-1" {
		t.Errorf("user = %+v, auth %q, path %q", e, gotAuth, gotPath)
	}
	if e, err = c.Group(ctx, "tok", "g 1"); err != nil || e.ID != "x-1" || gotPath != "/graph/v1.0/groups/g%201" {
		t.Errorf("group = %+v, %v, path %q", e, err, gotPath)
	}
	if _, err := c.User(ctx, "tok", "gone"); !errors.Is(err, ErrNotFound) {
		t.Errorf("404 gave %v, want ErrNotFound", err)
	}
	if _, err := c.Group(ctx, "tok", "forbidden"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("403 gave %v, want ErrUnauthorized", err)
	}
	for _, id := range []string{"", ".", "..", "a/b"} {
		gotPath = ""
		if _, err := c.User(ctx, "tok", id); !errors.Is(err, ErrNotFound) {
			t.Errorf("id %q gave %v, want ErrNotFound", id, err)
		}
		if gotPath != "" {
			t.Errorf("id %q was sent to the server as %q", id, gotPath)
		}
	}
}
