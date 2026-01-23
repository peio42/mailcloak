package mailcloak

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Daemon struct {
		User string `yaml:"user"`
	} `yaml:"daemon"`

	Keycloak struct {
		BaseURL      string `yaml:"base_url"`
		Realm        string `yaml:"realm"`
		ClientID     string `yaml:"client_id"`
		ClientSecret string `yaml:"client_secret"`
	} `yaml:"keycloak"`

	SQLite struct {
		Path string `yaml:"path"`
	} `yaml:"sqlite"`

	Policy struct {
		Domain              string `yaml:"domain"`
		CacheTTLSeconds     int    `yaml:"cache_ttl_seconds"`
		KeycloakFailureMode string `yaml:"keycloak_failure_mode"` // "tempfail" or "dunno"
	} `yaml:"policy"`

	Sockets struct {
		PolicySocket     string `yaml:"policy_socket"`
		SocketmapSocket  string `yaml:"socketmap_socket"`
		SocketOwnerUser  string `yaml:"socket_owner_user"`
		SocketOwnerGroup string `yaml:"socket_owner_group"`
		SocketMode       string `yaml:"socket_mode"`
	} `yaml:"sockets"`
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
	if cfg.Keycloak.BaseURL == "" || cfg.Keycloak.Realm == "" {
		return nil, fmt.Errorf("missing keycloak.base_url or keycloak.realm")
	}
	if cfg.SQLite.Path == "" {
		return nil, fmt.Errorf("missing sqlite.path")
	}
	if cfg.Policy.Domain == "" {
		return nil, fmt.Errorf("missing policy.domain")
	}
	if cfg.Policy.CacheTTLSeconds <= 0 {
		cfg.Policy.CacheTTLSeconds = 120
		log.Printf("config: policy.cache_ttl_seconds not set, defaulting to %d", cfg.Policy.CacheTTLSeconds)
	}
	if cfg.Policy.KeycloakFailureMode == "" {
		cfg.Policy.KeycloakFailureMode = "tempfail"
		log.Printf("config: policy.keycloak_failure_mode not set, defaulting to %s", cfg.Policy.KeycloakFailureMode)
	}
	if cfg.Daemon.User == "" {
		cfg.Daemon.User = "mailcloak"
		log.Printf("config: daemon.user not set, defaulting to %s", cfg.Daemon.User)
	}
	return &cfg, nil
}
