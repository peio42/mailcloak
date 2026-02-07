package mailcloak

import (
	"context"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"
)

func testSocketOwner(t *testing.T) (string, string) {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		t.Fatalf("lookup group: %v", err)
	}
	return u.Username, g.Name
}

func testConfig(t *testing.T, dir string) *Config {
	t.Helper()
	userName, groupName := testSocketOwner(t)
	return &Config{
		Daemon: struct {
			User string `yaml:"user"`
		}{
			User: "",
		},
		IDP: IDPConfig{
			Provider: "keycloak",
			Keycloak: KeycloakConfig{
				BaseURL:         "http://example.com",
				Realm:           "realm",
				ClientID:        "client",
				ClientSecret:    "secret",
				CacheTTLSeconds: 1,
			},
		},
		SQLite: struct {
			Path string `yaml:"path"`
		}{
			Path: filepath.Join(dir, "state.db"),
		},
		Policy: struct {
			IDPFailureMode      string `yaml:"idp_failure_mode"`
			KeycloakFailureMode string `yaml:"keycloak_failure_mode"`
		}{
			IDPFailureMode: "tempfail",
		},
		Sockets: struct {
			PolicySocket     string `yaml:"policy_socket"`
			SocketmapSocket  string `yaml:"socketmap_socket"`
			SocketOwnerUser  string `yaml:"socket_owner_user"`
			SocketOwnerGroup string `yaml:"socket_owner_group"`
			SocketMode       string `yaml:"socket_mode"`
		}{
			PolicySocket:     filepath.Join(dir, "policy.sock"),
			SocketmapSocket:  filepath.Join(dir, "socketmap.sock"),
			SocketOwnerUser:  userName,
			SocketOwnerGroup: groupName,
			SocketMode:       "0600",
		},
	}
}

func TestStartShutdown(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	if err := os.WriteFile(cfg.SQLite.Path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Close()

	cancel()
	select {
	case <-svc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for shutdown")
	}
	if err := svc.Err(); err != nil {
		t.Fatalf("unexpected service error: %v", err)
	}
}

func TestStartPolicyListenerError(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	cfg.Sockets.PolicySocket = filepath.Join(dir, "missing", "policy.sock")
	if err := os.WriteFile(cfg.SQLite.Path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := Start(ctx, cfg); err == nil {
		t.Fatal("expected error")
	}
}

func TestStartSocketmapListenerErrorClosesPolicy(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	cfg.Sockets.SocketmapSocket = filepath.Join(dir, "missing", "socketmap.sock")
	if err := os.WriteFile(cfg.SQLite.Path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := Start(ctx, cfg); err == nil {
		t.Fatal("expected error")
	}

	conn, err := net.DialTimeout("unix", cfg.Sockets.PolicySocket, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected policy listener to be closed")
	}
}
