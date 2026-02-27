package mailcloak

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigValidationAndDefaults(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte("keycloak: ["), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(p)
		if err == nil {
			t.Fatal("expected yaml parse error")
		}
	})

	t.Run("missing required keycloak", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte("sqlite:\n  path: /tmp/db.sqlite\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(p)
		if err == nil {
			t.Fatal("expected missing keycloak error")
		}
	})

	t.Run("missing sqlite path", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte("keycloak:\n  base_url: http://kc\n  realm: test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(p)
		if err == nil {
			t.Fatal("expected missing sqlite.path error")
		}
	})

	t.Run("defaults", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "config.yaml")
		cfgContent := "keycloak:\n  base_url: http://kc\n  realm: test\nsqlite:\n  path: /tmp/db.sqlite\n"
		if err := os.WriteFile(p, []byte(cfgContent), 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadConfig(p)
		if err != nil {
			t.Fatalf("LoadConfig error: %v", err)
		}
		if cfg.Keycloak.CacheTTLSeconds != 120 {
			t.Fatalf("expected default cache ttl 120, got %d", cfg.Keycloak.CacheTTLSeconds)
		}
		if cfg.Policy.KeycloakFailureMode != "tempfail" {
			t.Fatalf("expected default keycloak failure mode tempfail, got %q", cfg.Policy.KeycloakFailureMode)
		}
		if cfg.Daemon.User != "mailcloak" {
			t.Fatalf("expected default daemon user mailcloak, got %q", cfg.Daemon.User)
		}
	})
}
