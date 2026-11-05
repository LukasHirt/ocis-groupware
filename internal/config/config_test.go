package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	want := Default()
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Load(\"\") = %+v, want %+v", cfg, want)
	}
}

func TestLoadFileOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "groupware.yaml")
	yamlContent := "http_addr: \":9090\"\n"
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":9090")
	}
	// LogLevel was omitted from the file, so the default survives.
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q (default should survive)", cfg.LogLevel, "info")
	}
}

func TestLoadEnvOverridesFileAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "groupware.yaml")
	yamlContent := "http_addr: \":9090\"\nlog_level: \"warn\"\n"
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("GROUPWARE_HTTP_ADDR", ":7070")
	t.Setenv("GROUPWARE_LOG_LEVEL", "debug")
	t.Setenv("GROUPWARE_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("GROUPWARE_OIDC_AUDIENCE", "groupware")
	t.Setenv("GROUPWARE_OIDC_CA_FILE", "/etc/groupware/ca.pem")
	t.Setenv("GROUPWARE_DATA_DIR", "/var/lib/groupware")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}
	if cfg.HTTPAddr != ":7070" {
		t.Errorf("HTTPAddr = %q, want env value %q", cfg.HTTPAddr, ":7070")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want env value %q", cfg.LogLevel, "debug")
	}
	if cfg.OIDCIssuer != "https://idp.example.com" {
		t.Errorf("OIDCIssuer = %q, want env value", cfg.OIDCIssuer)
	}
	if cfg.OIDCAudience != "groupware" {
		t.Errorf("OIDCAudience = %q, want env value", cfg.OIDCAudience)
	}
	if cfg.OIDCCAFile != "/etc/groupware/ca.pem" {
		t.Errorf("OIDCCAFile = %q, want env value", cfg.OIDCCAFile)
	}
	if cfg.DataDir != "/var/lib/groupware" {
		t.Errorf("DataDir = %q, want env value", cfg.DataDir)
	}
}

func TestLoadFileReadsDataDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groupware.yaml")
	if err := os.WriteFile(path, []byte("data_dir: /srv/groupware\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}
	if cfg.DataDir != "/srv/groupware" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/srv/groupware")
	}
}

func TestLoadFileReadsOIDCSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groupware.yaml")
	yamlContent := "oidc_issuer: https://ocis.example\noidc_audience: groupware\noidc_ca_file: /etc/groupware/ca.pem\n"
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}
	if cfg.OIDCIssuer != "https://ocis.example" || cfg.OIDCAudience != "groupware" || cfg.OIDCCAFile != "/etc/groupware/ca.pem" {
		t.Errorf("OIDC settings = %q, %q, %q", cfg.OIDCIssuer, cfg.OIDCAudience, cfg.OIDCCAFile)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/groupware.yaml"); err == nil {
		t.Fatal("Load with nonexistent path: expected error, got nil")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"valid", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, false},
		{"empty addr", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: "", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, true},
		{"bad level", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "bogus", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, true},
		{"missing issuer", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", DataDir: "/data"}, true},
		{"negative legacy lifetime", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data", LegacyTokenTTL: -time.Hour}, true},
		{"no failure limit", Config{AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, true},
		{"no failure window", Config{AuthMaxFailures: 10, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, true},
		{"bad proxy", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, TrustedProxies: []string{"nope"}, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, true},
		{"good proxies", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, TrustedProxies: []string{"10.0.0.0/8", "::1"}, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, false},
		{"missing data dir", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example"}, true},
	}

	for _, tc := range cases {
		err := tc.cfg.Validate()
		if tc.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
	}
}

func TestAdminSocketDefaultsToDataDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groupware.yaml")
	if err := os.WriteFile(path, []byte("data_dir: /srv/groupware\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}
	if cfg.AdminSocket != "/srv/groupware/admin.sock" {
		t.Errorf("AdminSocket = %q, want %q", cfg.AdminSocket, "/srv/groupware/admin.sock")
	}
}

func TestAdminSocketFromFileAndEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groupware.yaml")
	yamlContent := "data_dir: /srv/groupware\nadmin_socket: /run/groupware.sock\n"
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}
	if cfg.AdminSocket != "/run/groupware.sock" {
		t.Errorf("AdminSocket = %q, want file value", cfg.AdminSocket)
	}

	t.Setenv("GROUPWARE_ADMIN_SOCKET", "/tmp/gw.sock")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}
	if cfg.AdminSocket != "/tmp/gw.sock" {
		t.Errorf("AdminSocket = %q, want env value", cfg.AdminSocket)
	}
}

func TestOCISURLDefaultsToTheIssuer(t *testing.T) {
	t.Setenv("GROUPWARE_OIDC_ISSUER", "https://ocis.example")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OCISURL != "https://ocis.example" {
		t.Errorf("OCISURL = %q, want the issuer", cfg.OCISURL)
	}
}

func TestOCISURLFromFileAndEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groupware.yaml")
	if err := os.WriteFile(path, []byte("oidc_issuer: https://ocis.example\nocis_url: http://ocis:9200\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OCISURL != "http://ocis:9200" {
		t.Errorf("OCISURL = %q, want the file value", cfg.OCISURL)
	}
	t.Setenv("GROUPWARE_OCIS_URL", "http://other:9200")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OCISURL != "http://other:9200" {
		t.Errorf("OCISURL = %q, want the env value", cfg.OCISURL)
	}
}

func TestLegacyTokenSettings(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LegacyTokenSecret != "" || cfg.LegacyTokenTTL != 180*24*time.Hour {
		t.Fatalf("defaults = %q, %v", cfg.LegacyTokenSecret, cfg.LegacyTokenTTL)
	}

	path := filepath.Join(t.TempDir(), "groupware.yaml")
	if err := os.WriteFile(path, []byte("legacy_token_secret: from-file\nlegacy_token_ttl: 720h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LegacyTokenSecret != "from-file" || cfg.LegacyTokenTTL != 720*time.Hour {
		t.Fatalf("file = %q, %v", cfg.LegacyTokenSecret, cfg.LegacyTokenTTL)
	}

	t.Setenv("GROUPWARE_LEGACY_TOKEN_SECRET", "from-env")
	t.Setenv("GROUPWARE_LEGACY_TOKEN_TTL", "48h")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LegacyTokenSecret != "from-env" || cfg.LegacyTokenTTL != 48*time.Hour {
		t.Fatalf("env = %q, %v", cfg.LegacyTokenSecret, cfg.LegacyTokenTTL)
	}

	t.Setenv("GROUPWARE_LEGACY_TOKEN_TTL", "soon")
	if _, err := Load(path); err == nil {
		t.Fatal("a lifetime that is not a duration was accepted")
	}
}

func TestCompatPaths(t *testing.T) {
	t.Setenv("GROUPWARE_COMPAT_PATHS", "")
	_ = os.Unsetenv("GROUPWARE_COMPAT_PATHS")
	cfg, err := Load("")
	if err != nil || cfg.CompatPaths {
		t.Fatalf("default = %v, %v; want off", cfg.CompatPaths, err)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("compat_paths: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil || !cfg.CompatPaths {
		t.Fatalf("file = %v, %v; want on", cfg.CompatPaths, err)
	}

	t.Setenv("GROUPWARE_COMPAT_PATHS", "false")
	cfg, err = Load(path)
	if err != nil || cfg.CompatPaths {
		t.Fatalf("env false over file true = %v, %v", cfg.CompatPaths, err)
	}
	t.Setenv("GROUPWARE_COMPAT_PATHS", "true")
	cfg, err = Load("")
	if err != nil || !cfg.CompatPaths {
		t.Fatalf("env = %v, %v; want on", cfg.CompatPaths, err)
	}

	t.Setenv("GROUPWARE_COMPAT_PATHS", "maybe")
	if _, err := Load(""); err == nil {
		t.Fatal("a value that is not a boolean was accepted")
	}
}

func TestSignInLimitSettings(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMaxFailures != 10 || cfg.AuthFailureWindow != 15*time.Minute || len(cfg.TrustedProxies) != 0 {
		t.Fatalf("defaults = %d, %v, %v", cfg.AuthMaxFailures, cfg.AuthFailureWindow, cfg.TrustedProxies)
	}

	path := filepath.Join(t.TempDir(), "groupware.yaml")
	yaml := "auth_max_failures: 5\nauth_failure_window: 30m\ntrusted_proxies:\n  - 10.0.0.0/8\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMaxFailures != 5 || cfg.AuthFailureWindow != 30*time.Minute || len(cfg.TrustedProxies) != 1 {
		t.Fatalf("file = %d, %v, %v", cfg.AuthMaxFailures, cfg.AuthFailureWindow, cfg.TrustedProxies)
	}

	t.Setenv("GROUPWARE_AUTH_MAX_FAILURES", "3")
	t.Setenv("GROUPWARE_AUTH_FAILURE_WINDOW", "1h")
	t.Setenv("GROUPWARE_TRUSTED_PROXIES", "192.168.0.0/16, 172.16.0.1 ,")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMaxFailures != 3 || cfg.AuthFailureWindow != time.Hour ||
		len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "192.168.0.0/16" || cfg.TrustedProxies[1] != "172.16.0.1" {
		t.Fatalf("env = %d, %v, %v", cfg.AuthMaxFailures, cfg.AuthFailureWindow, cfg.TrustedProxies)
	}

	t.Setenv("GROUPWARE_AUTH_MAX_FAILURES", "many")
	if _, err := Load(path); err == nil {
		t.Fatal("a failure limit that is not a number was accepted")
	}
	t.Setenv("GROUPWARE_AUTH_MAX_FAILURES", "3")
	t.Setenv("GROUPWARE_AUTH_FAILURE_WINDOW", "soon")
	if _, err := Load(path); err == nil {
		t.Fatal("a window that is not a duration was accepted")
	}
}
