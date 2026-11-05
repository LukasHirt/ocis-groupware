package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// buildOutput mimics what the Vite build leaves in dist: the entry with a
// fixed name, and chunks and assets with a content hash in their names.
var buildOutput = fstest.MapFS{
	"groupware.js":                     {Data: []byte("define([],function(){});")},
	"chunks/calendar-Ab12Cd34.mjs":     {Data: []byte("define([],function(){return 1});")},
	"assets/groupware-Zy98Xw76.css":    {Data: []byte("body{}")},
	"assets/icon-Qw12Er34.svg":         {Data: []byte("<svg/>")},
	"assets/inter-Ty56Ui78.woff2":      {Data: []byte("wOF2")},
	"chunks/calendar-Ab12Cd34.mjs.map": {Data: []byte("{}")},
	".hidden":                          {Data: []byte("secret")},
	"assets/.keep":                     {Data: []byte("")},
}

const (
	immutable = "public, max-age=31536000, immutable"
	noCache   = "no-cache"
)

func get(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestServesBuildOutputWithTypesAndCaching(t *testing.T) {
	h := handler(buildOutput)
	tests := []struct {
		path, contentType, cacheControl, body string
	}{
		{"/web/groupware.js", "text/javascript; charset=utf-8", noCache, "define([],function(){});"},
		{"/web/chunks/calendar-Ab12Cd34.mjs", "text/javascript; charset=utf-8", immutable, "define([],function(){return 1});"},
		{"/web/assets/groupware-Zy98Xw76.css", "text/css; charset=utf-8", immutable, "body{}"},
		{"/web/assets/icon-Qw12Er34.svg", "image/svg+xml", immutable, "<svg/>"},
		{"/web/assets/inter-Ty56Ui78.woff2", "font/woff2", immutable, "wOF2"},
		{"/web/chunks/calendar-Ab12Cd34.mjs.map", "application/json", immutable, "{}"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			rec := get(t, h, http.MethodGet, tc.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %q", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tc.contentType)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.cacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, tc.cacheControl)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := rec.Body.String(); got != tc.body {
				t.Errorf("body = %q, want %q", got, tc.body)
			}
		})
	}
}

func TestHeadAnswersWithHeadersOnly(t *testing.T) {
	rec := get(t, handler(buildOutput), http.MethodHead, "/web/groupware.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "24" {
		t.Errorf("Content-Length = %q, want 24", got)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", rec.Body.String())
	}
}

func TestNotFound(t *testing.T) {
	h := handler(buildOutput)
	for _, p := range []string{
		"/web/",
		"/web",
		"/web/index.html",
		"/web/nope.js",
		"/web/chunks",
		"/web/chunks/",
		"/web/assets/",
		"/web/.hidden",
		"/web/assets/.keep",
		"/web/../groupware.js",
		"/web/chunks/../groupware.js",
		"/web/./groupware.js",
		"/web//groupware.js",
		"/web/groupware.js/",
		"/web/GROUPWARE.JS",
		"/other/groupware.js",
	} {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			// Set the path directly: the handler must refuse what the
			// router would normally clean or redirect, not rely on it.
			req.URL.Path = p
			req.RequestURI = p
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body %q", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "define") || strings.Contains(rec.Body.String(), "secret") {
				t.Fatalf("body leaked a file: %q", rec.Body.String())
			}
		})
	}
}

func TestOtherMethodsAreNotAllowed(t *testing.T) {
	h := handler(buildOutput)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions, "PROPFIND"} {
		rec := get(t, h, m, "/web/groupware.js")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", m, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s: Allow = %q, want %q", m, got, "GET, HEAD")
		}
	}
}

func TestRangeRequests(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/web/groupware.js", nil)
	req.Header.Set("Range", "bytes=0-5")
	rec := httptest.NewRecorder()
	handler(buildOutput).ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got, _ := io.ReadAll(rec.Body); string(got) != "define" {
		t.Errorf("body = %q, want %q", got, "define")
	}
}

// TestEmbeddedPlaceholder pins that the committed placeholder in dist is not
// served, whether or not a build has been run.
func TestEmbeddedPlaceholder(t *testing.T) {
	rec := get(t, Handler(), http.MethodGet, "/web/.gitkeep")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
