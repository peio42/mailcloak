package mailcloak

import (
	"context"
	"fmt"
	"net/url"
)

type IDPTestResult struct {
	Provider string `json:"provider"`
	OK       bool   `json:"ok"`
}

func TestIdentityProvider(ctx context.Context, cfg *Config) (*IDPTestResult, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	switch cfg.IDP.Provider {
	case "keycloak":
		k := NewKeycloak(cfg)
		token, err := k.token(ctx)
		if err != nil {
			return nil, fmt.Errorf("keycloak token request: %w", err)
		}
		q := url.Values{}
		q.Set("max", "1")
		if _, err := k.adminGet(ctx, token, "/users", q); err != nil {
			return nil, fmt.Errorf("keycloak admin users request: %w", err)
		}
	case "authentik":
		a, err := NewAuthentik(cfg.IDP.Authentik)
		if err != nil {
			return nil, err
		}
		q := url.Values{}
		q.Set("page_size", "1")
		if _, err := a.users(ctx, q); err != nil {
			return nil, fmt.Errorf("authentik users request: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported idp.provider %q", cfg.IDP.Provider)
	}

	return &IDPTestResult{Provider: cfg.IDP.Provider, OK: true}, nil
}
