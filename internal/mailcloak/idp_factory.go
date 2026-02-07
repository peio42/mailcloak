package mailcloak

import (
	"fmt"
)

func NewIdentityResolver(cfg *Config) (IdentityResolver, error) {
	switch cfg.IDP.Provider {
	case "", "keycloak":
		return NewKeycloak(cfg), nil
	case "authentik":
		return NewAuthentik(cfg.IDP.Authentik)
	default:
		return nil, fmt.Errorf("unsupported idp.provider %q", cfg.IDP.Provider)
	}
}
