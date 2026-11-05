package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if !reflect.DeepEqual(cfg.OIDCAudience, StringList{"groupware"}) {
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
	if cfg.OIDCIssuer != "https://ocis.example" || !reflect.DeepEqual(cfg.OIDCAudience, StringList{"groupware"}) || cfg.OIDCCAFile != "/etc/groupware/ca.pem" {
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
		{"valid", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour}, false},
		{"empty addr", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: "", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour}, true},
		{"bad level", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "bogus", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour}, true},
		{"missing issuer", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour}, true},
		{"negative legacy lifetime", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour, LegacyTokenTTL: -time.Hour}, true},
		{"no failure limit", Config{AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour}, true},
		{"no failure window", Config{AuthMaxFailures: 10, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour}, true},
		{"bad proxy", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, TrustedProxies: []string{"nope"}, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour}, true},
		{"good proxies", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, TrustedProxies: []string{"10.0.0.0/8", "::1"}, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: 24 * time.Hour}, false},
		{"missing data dir", Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}}, true},
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

func TestOIDCAudienceDefaultsToWeb(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.OIDCAudience, StringList{"web"}) {
		t.Fatalf("OIDCAudience = %q, want [web]", cfg.OIDCAudience)
	}
}

func TestOIDCAudienceForms(t *testing.T) {
	for name, tc := range map[string]struct {
		yaml, env string
		want      StringList
	}{
		"yaml scalar":     {yaml: "oidc_audience: desktop\n", want: StringList{"desktop"}},
		"yaml list":       {yaml: "oidc_audience: [web, desktop]\n", want: StringList{"web", "desktop"}},
		"yaml comma":      {yaml: "oidc_audience: web, desktop\n", want: StringList{"web", "desktop"}},
		"env comma list":  {env: "web, desktop ,", want: StringList{"web", "desktop"}},
		"env beats yaml":  {yaml: "oidc_audience: web\n", env: "desktop", want: StringList{"desktop"}},
		"yaml empty":      {yaml: "oidc_audience: \"\"\n", want: StringList{}},
		"yaml empty list": {yaml: "oidc_audience: []\n", want: StringList{}},
	} {
		t.Run(name, func(t *testing.T) {
			path := ""
			if tc.yaml != "" {
				path = filepath.Join(t.TempDir(), "groupware.yaml")
				if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.env != "" {
				t.Setenv("GROUPWARE_OIDC_AUDIENCE", tc.env)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.OIDCAudience, tc.want) {
				t.Fatalf("OIDCAudience = %#v, want %#v", cfg.OIDCAudience, tc.want)
			}
		})
	}
}

func TestOIDCAudienceExplicitlyEmptyFailsValidation(t *testing.T) {
	t.Setenv("GROUPWARE_OIDC_ISSUER", "https://ocis.example")
	t.Setenv("GROUPWARE_DATA_DIR", "/data")
	t.Setenv("GROUPWARE_OIDC_AUDIENCE", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `"*"`) {
		t.Fatalf("Validate = %v, want an error that points to \"*\"", err)
	}
}

func TestOIDCAudienceWildcard(t *testing.T) {
	if !(StringList{"*"}).AllowsAnyClient() {
		t.Error("[*] should allow any client")
	}
	if (StringList{"web", "*"}).AllowsAnyClient() || (StringList{"web"}).AllowsAnyClient() {
		t.Error("only the sole entry * is the wildcard")
	}
}

func TestIdentityMaxAge(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IdentityMaxAge != 720*time.Hour {
		t.Fatalf("default = %v, want 720h", cfg.IdentityMaxAge)
	}

	path := filepath.Join(t.TempDir(), "groupware.yaml")
	if err := os.WriteFile(path, []byte("identity_max_age: 48h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(path); err != nil || cfg.IdentityMaxAge != 48*time.Hour {
		t.Fatalf("file: %v, %v", cfg.IdentityMaxAge, err)
	}
	t.Setenv("GROUPWARE_IDENTITY_MAX_AGE", "24h")
	if cfg, err = Load(path); err != nil || cfg.IdentityMaxAge != 24*time.Hour {
		t.Fatalf("env: %v, %v", cfg.IdentityMaxAge, err)
	}
	t.Setenv("GROUPWARE_IDENTITY_MAX_AGE", "soon")
	if _, err = Load(path); err == nil {
		t.Fatal("an invalid duration was accepted")
	}
}

func TestIdentityMaxAgeValidation(t *testing.T) {
	valid := func(d time.Duration) Config {
		return Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", IdentityMaxAge: d, AppPasswordMaxLifetime: 24 * time.Hour}
	}
	if err := valid(-time.Hour).Validate(); err == nil {
		t.Error("a negative identity_max_age was accepted")
	}
	if err := valid(0).Validate(); err != nil {
		t.Errorf("0 turns the lease off and must be accepted: %v", err)
	}
}

func TestAppPasswordMaxLifetime(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppPasswordMaxLifetime != 4320*time.Hour {
		t.Fatalf("default = %v, want 4320h", cfg.AppPasswordMaxLifetime)
	}

	path := filepath.Join(t.TempDir(), "groupware.yaml")
	if err := os.WriteFile(path, []byte("app_password_max_lifetime: 720h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(path); err != nil || cfg.AppPasswordMaxLifetime != 720*time.Hour {
		t.Fatalf("file: %v, %v", cfg.AppPasswordMaxLifetime, err)
	}
	t.Setenv("GROUPWARE_APP_PASSWORD_MAX_LIFETIME", "48h")
	if cfg, err = Load(path); err != nil || cfg.AppPasswordMaxLifetime != 48*time.Hour {
		t.Fatalf("env: %v, %v", cfg.AppPasswordMaxLifetime, err)
	}
	t.Setenv("GROUPWARE_APP_PASSWORD_MAX_LIFETIME", "long")
	if _, err = Load(path); err == nil {
		t.Fatal("an invalid duration was accepted")
	}
}

func TestAppPasswordMaxLifetimeValidation(t *testing.T) {
	with := func(d time.Duration) Config {
		return Config{AuthMaxFailures: 10, AuthFailureWindow: time.Minute, HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", OIDCAudience: StringList{"web"}, DataDir: "/data", AppPasswordMaxLifetime: d}
	}
	for d, ok := range map[time.Duration]bool{0: false, -time.Hour: false, 23 * time.Hour: false, 24 * time.Hour: true, 4320 * time.Hour: true} {
		if err := with(d).Validate(); (err == nil) != ok {
			t.Errorf("app_password_max_lifetime %v: Validate = %v, want ok = %v", d, err, ok)
		}
	}
}

func TestDebugAddr(t *testing.T) {
	t.Setenv("GROUPWARE_DEBUG_ADDR", "")
	_ = os.Unsetenv("GROUPWARE_DEBUG_ADDR")
	if got := Default().DebugAddr; got != "127.0.0.1:9180" {
		t.Errorf("default = %q, want 127.0.0.1:9180", got)
	}
	t.Setenv("GROUPWARE_DEBUG_ADDR", ":9999")
	cfg, err := Load("")
	if err != nil || cfg.DebugAddr != ":9999" {
		t.Errorf("env: %q, %v", cfg.DebugAddr, err)
	}
	// An empty value turns the listener off.
	t.Setenv("GROUPWARE_DEBUG_ADDR", "")
	cfg, err = Load("")
	if err != nil || cfg.DebugAddr != "" {
		t.Errorf("empty env: %q, %v", cfg.DebugAddr, err)
	}
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("debug_addr: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Unsetenv("GROUPWARE_DEBUG_ADDR")
	cfg, err = Load(path)
	if err != nil || cfg.DebugAddr != "" {
		t.Errorf("empty yaml: %q, %v", cfg.DebugAddr, err)
	}
}
