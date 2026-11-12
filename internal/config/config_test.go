package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validKey(seed byte) string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = seed
	}
	return base64.StdEncoding.EncodeToString(key)
}

func validConfig() Config {
	cfg := Default()
	cfg.DevKubeconfig = "/tmp/kubeconfig"
	return cfg
}

func TestDevConfigIsValid(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("dev config should be valid: %v", err)
	}
}

func TestValidateRequiresAuthOrDevMode(t *testing.T) {
	if err := Default().Validate(); err == nil || !strings.Contains(err.Error(), "oidc_issuer") {
		t.Fatalf("default config must require OIDC, got %v", err)
	}
	cfg := Default()
	cfg.OIDCIssuer = "https://idp.example.com"
	cfg.OIDCClientID = "kubedeck"
	cfg.OIDCRedirectURL = "https://kubedeck.example.com/auth/callback"
	cfg.MasterKey = validKey(1)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("oidc config should be valid: %v", err)
	}
}

func TestValidateRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{"empty addr", func(c *Config) { c.Addr = "" }, "addr"},
		{"bad log level", func(c *Config) { c.LogLevel = "verbose" }, "log_level"},
		{"wildcard cors", func(c *Config) { c.AllowedOrigins = []string{"*"} }, "allowed_origins"},
		{"dev cluster without name", func(c *Config) {
			c.DevKubeconfig = "/tmp/kubeconfig"
			c.DevClusterName = ""
		}, "dev_cluster_name"},
		{"bad master key base64", func(c *Config) { c.MasterKey = "not-base64!!" }, "master_key"},
		{"short master key", func(c *Config) {
			c.MasterKey = base64.StdEncoding.EncodeToString([]byte("short"))
		}, "32 bytes"},
		{"version without key", func(c *Config) { c.MasterKeyVersion = 2 }, "master_key_version"},
		{"previous key same version", func(c *Config) {
			c.MasterKey = validKey(1)
			c.PreviousMasterKey = validKey(2)
			c.PreviousMasterKeyVersion = 1
		}, "previous_master_key_version"},
		{"previous key missing version", func(c *Config) {
			c.PreviousMasterKey = validKey(2)
		}, "previous_master_key_version"},
		{"previous version without key", func(c *Config) {
			c.PreviousMasterKeyVersion = 2
		}, "previous_master_key"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q should mention %q", err, tc.wantSub)
			}
		})
	}
}

func TestValidateAcceptsKeyRing(t *testing.T) {
	cfg := validConfig()
	cfg.MasterKey = validKey(1)
	cfg.MasterKeyVersion = 2
	cfg.PreviousMasterKey = validKey(2)
	cfg.PreviousMasterKeyVersion = 1
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid key ring rejected: %v", err)
	}
}

func TestLoadRejectsUnknownYAMLKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("addr: \":9090\"\nlog_leve: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBEDECK_CONFIG", path)

	if _, err := Load(); err == nil {
		t.Fatal("expected unknown key error, got nil")
	} else if !strings.Contains(err.Error(), "log_leve") {
		t.Fatalf("error %q should mention the unknown key", err)
	}
}

func TestLoadAppliesEnvOverrides(t *testing.T) {
	t.Setenv("KUBEDECK_CONFIG", "")
	t.Setenv("KUBEDECK_DEV_KUBECONFIG", "/tmp/kubeconfig")
	t.Setenv("KUBEDECK_ADDR", "127.0.0.1:9090")
	t.Setenv("KUBEDECK_LOG_LEVEL", "DEBUG")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9090" || cfg.LogLevel != "debug" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadRejectsInvalidKeyVersionEnv(t *testing.T) {
	t.Setenv("KUBEDECK_CONFIG", "")
	t.Setenv("KUBEDECK_MASTER_KEY_VERSION", "zero")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for non-numeric key version")
	}
}
