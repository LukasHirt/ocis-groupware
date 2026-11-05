//go:build stack

package stack

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestWebAppIsAnnouncedToWeb checks that the Web configuration oCIS serves
// lists the application, so that oCIS Web loads it at start.
func TestWebAppIsAnnouncedToWeb(t *testing.T) {
	status, _, body := do(t, ctxTimeout(t), http.MethodGet, "/config.json", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /config.json: status %d: %s", status, body)
	}
	var cfg struct {
		ExternalApps []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"external_apps"`
	}
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("GET /config.json: %v: %s", err, body)
	}
	for _, app := range cfg.ExternalApps {
		if app.ID == "groupware" {
			if app.Path != "/web/groupware.js" {
				t.Fatalf("external app groupware has path %q, want /web/groupware.js", app.Path)
			}
			return
		}
	}
	t.Fatalf("external_apps lacks groupware: %s", body)
}

// TestWebAppBundleIsServed checks that the entry oCIS Web loads is reachable
// through the stack's route, with the headers the server promises.
func TestWebAppBundleIsServed(t *testing.T) {
	status, headers, body := do(t, ctxTimeout(t), http.MethodGet, "/web/groupware.js", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /web/groupware.js: status %d: %s", status, body)
	}
	if ct := headers.Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := headers.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if !strings.HasPrefix(body, "define(") {
		t.Errorf("body is not an AMD module: %.80q", body)
	}
}
