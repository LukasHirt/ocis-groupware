// Package config loads ocis-groupware's configuration from defaults, an optional YAML
// file, and environment variables, in that precedence order (env wins).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/owncloud/ocis-groupware/internal/logging"
)

// Config holds ocis-groupware's runtime configuration.
type Config struct {
	HTTPAddr   string `yaml:"http_addr"`
	LogLevel   string `yaml:"log_level"`
	OIDCIssuer string `yaml:"oidc_issuer"`
	// OIDCAudience, when set, must appear in the aud claim of access tokens.
	// oCIS puts the requesting client's ID there (e.g. "web").
	OIDCAudience string `yaml:"oidc_audience"`
	// OIDCCAFile is an optional PEM bundle of extra CAs trusted when
	// contacting the issuer.
	OIDCCAFile string `yaml:"oidc_ca_file"`
	// OCISURL is the URL the service reaches the oCIS Graph API at, with the
	// caller's token, to learn the caller's oCIS id and groups at sign-in. It
	// defaults to OIDCIssuer. It uses OIDCCAFile for TLS.
	OCISURL string `yaml:"ocis_url"`
	// DataDir is the directory that holds all service state. It must be writable.
	DataDir string `yaml:"data_dir"`
	// AdminSocket is the path of the unix socket that serves the admin API.
	// It defaults to admin.sock in DataDir.
	AdminSocket string `yaml:"admin_socket"`
	// LegacyTokenSecret is the instance secret of a classic installation. It
	// is needed to sign in with an app token imported from there: the
	// classic server keeps a token as sha512(token . secret). Without it,
	// imported tokens do not work.
	LegacyTokenSecret string `yaml:"legacy_token_secret"`
	// LegacyTokenTTL is how long an imported token stays valid without being
	// used. A Go duration such as 4320h.
	LegacyTokenTTL time.Duration `yaml:"legacy_token_ttl"`
}

// Default returns the built-in configuration defaults.
func Default() Config {
	return Config{
		HTTPAddr: ":8080",
		LogLevel: "info",

		LegacyTokenTTL: 180 * 24 * time.Hour,
	}
}

// Load builds a Config starting from Default(), overlaying the YAML file at
// path (if path is non-empty), then overlaying any set GROUPWARE_* environment
// variables. Environment variables always win over the file, which always
// wins over defaults.
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("config: reading %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("config: parsing %s: %w", path, err)
		}
	}

	if v, ok := os.LookupEnv("GROUPWARE_HTTP_ADDR"); ok {
		cfg.HTTPAddr = v
	}
	if v, ok := os.LookupEnv("GROUPWARE_LOG_LEVEL"); ok {
		cfg.LogLevel = v
	}
	if v, ok := os.LookupEnv("GROUPWARE_OIDC_ISSUER"); ok {
		cfg.OIDCIssuer = v
	}
	if v, ok := os.LookupEnv("GROUPWARE_OIDC_AUDIENCE"); ok {
		cfg.OIDCAudience = v
	}
	if v, ok := os.LookupEnv("GROUPWARE_OIDC_CA_FILE"); ok {
		cfg.OIDCCAFile = v
	}
	if v, ok := os.LookupEnv("GROUPWARE_OCIS_URL"); ok {
		cfg.OCISURL = v
	}
	if cfg.OCISURL == "" {
		cfg.OCISURL = cfg.OIDCIssuer
	}
	if v, ok := os.LookupEnv("GROUPWARE_DATA_DIR"); ok {
		cfg.DataDir = v
	}
	if v, ok := os.LookupEnv("GROUPWARE_ADMIN_SOCKET"); ok {
		cfg.AdminSocket = v
	}
	if v, ok := os.LookupEnv("GROUPWARE_LEGACY_TOKEN_SECRET"); ok {
		cfg.LegacyTokenSecret = v
	}
	if v, ok := os.LookupEnv("GROUPWARE_LEGACY_TOKEN_TTL"); ok {
		ttl, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("config: GROUPWARE_LEGACY_TOKEN_TTL: %w", err)
		}
		cfg.LegacyTokenTTL = ttl
	}
	if cfg.AdminSocket == "" && cfg.DataDir != "" {
		cfg.AdminSocket = filepath.Join(cfg.DataDir, "admin.sock")
	}

	return cfg, nil
}

// Validate checks that cfg is well-formed.
func (cfg Config) Validate() error {
	if cfg.HTTPAddr == "" {
		return fmt.Errorf("config: http_addr must not be empty")
	}
	if _, err := logging.ParseLevel(cfg.LogLevel); err != nil {
		return fmt.Errorf("config: log_level: %w", err)
	}
	if cfg.OIDCIssuer == "" {
		return fmt.Errorf("config: oidc_issuer must be set (GROUPWARE_OIDC_ISSUER)")
	}
	if cfg.DataDir == "" {
		return fmt.Errorf("config: data_dir must be set (GROUPWARE_DATA_DIR)")
	}
	if cfg.LegacyTokenTTL < 0 {
		return fmt.Errorf("config: legacy_token_ttl must not be negative")
	}
	return nil
}
