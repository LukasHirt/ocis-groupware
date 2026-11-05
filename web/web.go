// Package web serves the built web application under /web/.
//
// The build output lives in dist and is embedded in the binary, so the
// service needs nothing from the filesystem at run time. A placeholder keeps
// the directory in git; without a build the handler answers 404 for every
// path, and the Go tests still run.
package web

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var distFS embed.FS

// Prefix is the URL path the application is served under.
const Prefix = "/web/"

// entry is the file oCIS Web loads to start the application. Its name must
// stay fixed, so it is served with no-cache; every other file in the build
// carries a content hash in its name and can be cached for good.
const entry = "groupware.js"

// Handler serves the embedded build output under Prefix.
func Handler() http.Handler {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return handler(dist)
}

// contentTypes names the types of the files a build produces. Go's own
// table knows the common ones but not fonts or source maps, and it must not
// depend on the host's mime database.
var contentTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".map":   "application/json",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".woff":  "font/woff",
	".woff2": "font/woff2",
}

func handler(dist fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name, ok := fileName(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		f, err := dist.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		content, ok := f.(io.ReadSeeker)
		if !ok {
			http.NotFound(w, r)
			return
		}

		h := w.Header()
		h.Set("Content-Type", contentType(name))
		h.Set("X-Content-Type-Options", "nosniff")
		if name == entry {
			h.Set("Cache-Control", "no-cache")
		} else {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		// No modification time: embedded files have none, and the cache
		// headers above decide revalidation.
		http.ServeContent(w, r, "", time.Time{}, content)
	})
}

// fileName maps a request path to a file name in the build, or reports
// false for anything that is not a plain path to a file below Prefix: the
// router cleans dot segments and double slashes before the handler runs,
// but the handler does not rely on that. Names with a hidden segment are
// refused, which keeps the placeholder out of the response.
func fileName(p string) (string, bool) {
	name, ok := strings.CutPrefix(p, Prefix)
	if !ok || name == "" || strings.HasSuffix(name, "/") || !fs.ValidPath(name) {
		return "", false
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return "", false
		}
	}
	return name, true
}

func contentType(name string) string {
	ext := path.Ext(name)
	if ct, ok := contentTypes[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
