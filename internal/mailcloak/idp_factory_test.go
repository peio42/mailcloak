package mailcloak

import "testing"

func TestNewIdentityResolver(t *testing.T) {
	t.Run("default provider is keycloak", func(t *testing.T) {
		cfg := &Config{}
		idp, err := NewIdentityResolver(cfg)
		if err != nil {
			t.Fatalf("NewIdentityResolver error: %v", err)
		}
		if _, ok := idp.(*Keycloak); !ok {
			t.Fatalf("expected *Keycloak, got %T", idp)
		}
	})

	t.Run("authentik provider", func(t *testing.T) {
		cfg := &Config{}
		cfg.IDP.Provider = "authentik"
		cfg.IDP.Authentik.BaseURL = "http://authentik.local"
		cfg.IDP.Authentik.APIToken = "token"
		cfg.IDP.Authentik.CacheTTLSeconds = 1

		idp, err := NewIdentityResolver(cfg)
		if err != nil {
			t.Fatalf("NewIdentityResolver error: %v", err)
		}
		if _, ok := idp.(*Authentik); !ok {
			t.Fatalf("expected *Authentik, got %T", idp)
		}
	})

	t.Run("authentik missing token", func(t *testing.T) {
		cfg := &Config{}
		cfg.IDP.Provider = "authentik"
		cfg.IDP.Authentik.BaseURL = "http://authentik.local"
		cfg.IDP.Authentik.APIToken = ""
		cfg.IDP.Authentik.CacheTTLSeconds = 1

		if _, err := NewIdentityResolver(cfg); err == nil {
			t.Fatal("expected error for missing authentik token")
		}
	})

	t.Run("unsupported provider", func(t *testing.T) {
		cfg := &Config{}
		cfg.IDP.Provider = "unsupported"
		if _, err := NewIdentityResolver(cfg); err == nil {
			t.Fatal("expected unsupported provider error")
		}
	})
}
