package mailcloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTestIdentityProviderKeycloak(t *testing.T) {
	cfg, closeServer := testKeycloakConfig(t)
	defer closeServer()

	result, err := TestIdentityProvider(context.Background(), &cfg)
	if err != nil {
		t.Fatalf("test idp: %v", err)
	}
	if !result.OK || result.Provider != "keycloak" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestTestIdentityProviderAuthentik(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/core/users/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("page_size") != "1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
	}))
	defer srv.Close()

	cfg := Config{}
	cfg.IDP.Provider = "authentik"
	cfg.IDP.Authentik.BaseURL = srv.URL
	cfg.IDP.Authentik.APIToken = "token"
	cfg.IDP.Authentik.CacheTTLSeconds = 1
	cfg.SQLite.Path = filepath.Join(t.TempDir(), "state.db")

	result, err := TestIdentityProvider(context.Background(), &cfg)
	if err != nil {
		t.Fatalf("test idp: %v", err)
	}
	if !result.OK || result.Provider != "authentik" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestApplySetupWritesConfigAndInitializesDB(t *testing.T) {
	cfg, closeServer := testKeycloakConfig(t)
	defer closeServer()

	dir := t.TempDir()
	cfg.SQLite.Path = filepath.Join(dir, "state.db")
	configPath := filepath.Join(dir, "mailcloak.yaml")

	result, err := ApplySetup(context.Background(), configPath, cfg.SQLite.Path, SetupApplyRequest{
		Config:    cfg,
		InitDB:    true,
		TestIDP:   true,
		Overwrite: false,
	})
	if err != nil {
		t.Fatalf("apply setup: %v", err)
	}
	if result.ConfigPath != configPath || result.DBPath != cfg.SQLite.Path || result.IDPTest == nil || !result.IDPTest.OK {
		t.Fatalf("unexpected apply result: %#v", result)
	}

	status := GetSetupStatus(configPath, cfg.SQLite.Path)
	if !status.ConfigExists || !status.ConfigValid || !status.ConfigWritable || !status.DBExists || !status.DBInitialized || !status.DBWritable {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestGetSetupStatusReportsUnwritableConfigAndDBPaths(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	status := GetSetupStatus(filepath.Join(blocker, "config.yaml"), filepath.Join(blocker, "state.db"))
	if status.ConfigWritable || status.ConfigWritableError == "" {
		t.Fatalf("expected config writable error, got %#v", status)
	}
	if status.DBWritable || status.DBWritableError == "" {
		t.Fatalf("expected db writable error, got %#v", status)
	}
}

func testKeycloakConfig(t *testing.T) (Config, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/realm/protocol/openid-connect/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" ||
				r.Form.Get("client_id") != "client" ||
				r.Form.Get("client_secret") != "secret" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "token",
				"expires_in":   300,
			})
		case "/admin/realms/realm/users":
			if got := r.Header.Get("Authorization"); got != "Bearer token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	cfg := Config{}
	cfg.IDP.Provider = "keycloak"
	cfg.IDP.Keycloak.BaseURL = srv.URL
	cfg.IDP.Keycloak.Realm = "realm"
	cfg.IDP.Keycloak.ClientID = "client"
	cfg.IDP.Keycloak.ClientSecret = "secret"
	cfg.IDP.Keycloak.CacheTTLSeconds = 1
	cfg.SQLite.Path = filepath.Join(t.TempDir(), "state.db")
	cfg.Policy.IDPFailureMode = "tempfail"
	cfg.Sockets.PolicySocket = "/var/spool/postfix/private/mailcloak-policy"
	cfg.Sockets.SocketmapSocket = "/var/spool/postfix/private/mailcloak-socketmap"
	cfg.Sockets.SocketOwnerUser = "postfix"
	cfg.Sockets.SocketOwnerGroup = "postfix"
	cfg.Sockets.SocketMode = "0660"
	cfg.Daemon.User = "mailcloak"
	return cfg, srv.Close
}
