package mailcloak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func TestLoadConfigLegacyKeycloakNormalization(t *testing.T) {
	p := writeTestConfig(t, `
keycloak:
  base_url: http://keycloak.local
  realm: myrealm
  client_id: my-client
  client_secret: my-secret
  cache_ttl_seconds: 33
sqlite:
  path: /tmp/mailcloak.db
policy:
  keycloak_failure_mode: dunno
`)

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.IDP.Provider != "keycloak" {
		t.Fatalf("expected provider keycloak, got %q", cfg.IDP.Provider)
	}
	if cfg.IDP.Keycloak.BaseURL != "http://keycloak.local" || cfg.IDP.Keycloak.Realm != "myrealm" {
		t.Fatalf("legacy keycloak values not normalized: %+v", cfg.IDP.Keycloak)
	}
	if cfg.IDP.Keycloak.CacheTTLSeconds != 33 {
		t.Fatalf("expected cache ttl 33, got %d", cfg.IDP.Keycloak.CacheTTLSeconds)
	}
	if cfg.Policy.IDPFailureMode != "dunno" {
		t.Fatalf("expected failure mode dunno from legacy key, got %q", cfg.Policy.IDPFailureMode)
	}
	if cfg.Daemon.User != "mailcloak" {
		t.Fatalf("expected default daemon user mailcloak, got %q", cfg.Daemon.User)
	}
}

func TestLoadConfigAuthentikNormalizationAndDefaults(t *testing.T) {
	p := writeTestConfig(t, `
idp:
  provider: "  AuThEnTiK "
  authentik:
    base_url: http://authentik.local
    api_token: token
sqlite:
  path: /tmp/mailcloak.db
policy:
  idp_failure_mode: tempfail
daemon:
  user: daemon-user
`)

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.IDP.Provider != "authentik" {
		t.Fatalf("expected normalized provider authentik, got %q", cfg.IDP.Provider)
	}
	if cfg.IDP.Authentik.CacheTTLSeconds != 120 {
		t.Fatalf("expected default authentik ttl 120, got %d", cfg.IDP.Authentik.CacheTTLSeconds)
	}
	if cfg.Policy.IDPFailureMode != "tempfail" {
		t.Fatalf("expected explicit idp_failure_mode to be preserved, got %q", cfg.Policy.IDPFailureMode)
	}
	if cfg.Daemon.User != "daemon-user" {
		t.Fatalf("expected daemon user daemon-user, got %q", cfg.Daemon.User)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "missing provider",
			body: `
sqlite:
  path: /tmp/mailcloak.db
`,
			wantErr: "missing idp.provider",
		},
		{
			name: "missing sqlite path",
			body: `
idp:
  provider: keycloak
  keycloak:
    base_url: http://keycloak.local
    realm: realm
`,
			wantErr: "missing sqlite.path",
		},
		{
			name: "unsupported provider",
			body: `
idp:
  provider: ldap
sqlite:
  path: /tmp/mailcloak.db
`,
			wantErr: "unsupported idp.provider",
		},
		{
			name: "keycloak missing required fields",
			body: `
idp:
  provider: keycloak
sqlite:
  path: /tmp/mailcloak.db
`,
			wantErr: "missing idp.keycloak.base_url or idp.keycloak.realm",
		},
		{
			name: "authentik missing required fields",
			body: `
idp:
  provider: authentik
  authentik:
    base_url: http://authentik.local
sqlite:
  path: /tmp/mailcloak.db
`,
			wantErr: "missing idp.authentik.base_url or idp.authentik.api_token",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeTestConfig(t, tc.body)
			_, err := LoadConfig(p)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
