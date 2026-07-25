package mailcloak

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type KeycloakConfig struct {
	BaseURL         string `yaml:"base_url" json:"base_url"`
	Realm           string `yaml:"realm" json:"realm"`
	ClientID        string `yaml:"client_id" json:"client_id"`
	ClientSecret    string `yaml:"client_secret" json:"client_secret"`
	CacheTTLSeconds int    `yaml:"cache_ttl_seconds" json:"cache_ttl_seconds"`
}

type AuthentikConfig struct {
	BaseURL         string `yaml:"base_url" json:"base_url"`
	APIToken        string `yaml:"api_token" json:"api_token"`
	CacheTTLSeconds int    `yaml:"cache_ttl_seconds" json:"cache_ttl_seconds"`
}

type IDPConfig struct {
	Provider  string          `yaml:"provider" json:"provider"`
	Keycloak  KeycloakConfig  `yaml:"keycloak" json:"keycloak"`
	Authentik AuthentikConfig `yaml:"authentik" json:"authentik"`
}

type Config struct {
	Daemon struct {
		User string `yaml:"user" json:"user"`
	} `yaml:"daemon" json:"daemon"`

	IDP      IDPConfig      `yaml:"idp" json:"idp"`
	Keycloak KeycloakConfig `yaml:"keycloak,omitempty" json:"keycloak,omitempty"`

	SQLite struct {
		Path string `yaml:"path" json:"path"`
	} `yaml:"sqlite" json:"sqlite"`

	Policy struct {
		IDPFailureMode      string `yaml:"idp_failure_mode" json:"idp_failure_mode"` // "tempfail" or "dunno"
		KeycloakFailureMode string `yaml:"keycloak_failure_mode,omitempty" json:"-"` // legacy
	} `yaml:"policy" json:"policy"`

	Sockets struct {
		PolicySocket     string `yaml:"policy_socket" json:"policy_socket"`
		SocketmapSocket  string `yaml:"socketmap_socket" json:"socketmap_socket"`
		SocketOwnerUser  string `yaml:"socket_owner_user" json:"socket_owner_user"`
		SocketOwnerGroup string `yaml:"socket_owner_group" json:"socket_owner_group"`
		SocketMode       string `yaml:"socket_mode" json:"socket_mode"`
	} `yaml:"sockets" json:"sockets"`
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if err := ValidateConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ValidateConfig(cfg *Config) error {
	normalizeLegacyKeycloak(cfg)
	normalizeIDPProvider(cfg)
	if cfg.IDP.Provider == "" {
		return fmt.Errorf("missing idp.provider")
	}
	if cfg.SQLite.Path == "" {
		return fmt.Errorf("missing sqlite.path")
	}
	if err := validateIDPConfig(cfg); err != nil {
		return err
	}
	if cfg.Policy.IDPFailureMode == "" {
		if cfg.Policy.KeycloakFailureMode != "" {
			cfg.Policy.IDPFailureMode = cfg.Policy.KeycloakFailureMode
		} else {
			cfg.Policy.IDPFailureMode = "tempfail"
			log.Printf("config: policy.idp_failure_mode not set, defaulting to %s", cfg.Policy.IDPFailureMode)
		}
	}
	if cfg.Daemon.User == "" {
		cfg.Daemon.User = "mailcloak"
		log.Printf("config: daemon.user not set, defaulting to %s", cfg.Daemon.User)
	}
	return nil
}

func normalizeLegacyKeycloak(cfg *Config) {
	if cfg.Keycloak.BaseURL == "" &&
		cfg.Keycloak.Realm == "" &&
		cfg.Keycloak.ClientID == "" &&
		cfg.Keycloak.ClientSecret == "" &&
		cfg.Keycloak.CacheTTLSeconds == 0 {
		return
	}
	if cfg.IDP.Provider == "" {
		cfg.IDP.Provider = "keycloak"
	}
	if cfg.IDP.Keycloak.BaseURL == "" {
		cfg.IDP.Keycloak.BaseURL = cfg.Keycloak.BaseURL
	}
	if cfg.IDP.Keycloak.Realm == "" {
		cfg.IDP.Keycloak.Realm = cfg.Keycloak.Realm
	}
	if cfg.IDP.Keycloak.ClientID == "" {
		cfg.IDP.Keycloak.ClientID = cfg.Keycloak.ClientID
	}
	if cfg.IDP.Keycloak.ClientSecret == "" {
		cfg.IDP.Keycloak.ClientSecret = cfg.Keycloak.ClientSecret
	}
	if cfg.IDP.Keycloak.CacheTTLSeconds == 0 {
		cfg.IDP.Keycloak.CacheTTLSeconds = cfg.Keycloak.CacheTTLSeconds
	}
}

func normalizeIDPProvider(cfg *Config) {
	cfg.IDP.Provider = strings.TrimSpace(strings.ToLower(cfg.IDP.Provider))
}

func validateIDPConfig(cfg *Config) error {
	const defaultCacheTTLSeconds = 120
	switch cfg.IDP.Provider {
	case "keycloak":
		if cfg.IDP.Keycloak.BaseURL == "" || cfg.IDP.Keycloak.Realm == "" {
			return fmt.Errorf("missing idp.keycloak.base_url or idp.keycloak.realm")
		}
		if cfg.IDP.Keycloak.CacheTTLSeconds <= 0 {
			cfg.IDP.Keycloak.CacheTTLSeconds = defaultCacheTTLSeconds
			log.Printf("config: idp.keycloak.cache_ttl_seconds not set, defaulting to %d", cfg.IDP.Keycloak.CacheTTLSeconds)
		}
	case "authentik":
		if cfg.IDP.Authentik.BaseURL == "" || cfg.IDP.Authentik.APIToken == "" {
			return fmt.Errorf("missing idp.authentik.base_url or idp.authentik.api_token")
		}
		if cfg.IDP.Authentik.CacheTTLSeconds <= 0 {
			cfg.IDP.Authentik.CacheTTLSeconds = defaultCacheTTLSeconds
			log.Printf("config: idp.authentik.cache_ttl_seconds not set, defaulting to %d", cfg.IDP.Authentik.CacheTTLSeconds)
		}
	default:
		return fmt.Errorf("unsupported idp.provider %q", cfg.IDP.Provider)
	}
	return nil
}
