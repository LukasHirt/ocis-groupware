package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	want := Default()
	if cfg != want {
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
		{"valid", Config{HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, false},
		{"empty addr", Config{HTTPAddr: "", LogLevel: "info", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, true},
		{"bad level", Config{HTTPAddr: ":8080", LogLevel: "bogus", OIDCIssuer: "https://ocis.example", DataDir: "/data"}, true},
		{"missing issuer", Config{HTTPAddr: ":8080", LogLevel: "info", DataDir: "/data"}, true},
		{"missing data dir", Config{HTTPAddr: ":8080", LogLevel: "info", OIDCIssuer: "https://ocis.example"}, true},
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
