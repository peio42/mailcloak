//go:build integration
// +build integration

package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"mailcloak/internal/mailcloak"
)

func TestSocketmapAlias(t *testing.T) {
	dir := t.TempDir()
	dbPath, policySock, socketmapSock := dbAndSockets(t, dir)
	seedTestDB(t, dbPath)

	cfg := &mailcloak.Config{}

	cfg.Daemon.User = "" // IMPORTANT: no privileges drop in test

	cfg.SQLite.Path = dbPath

	cfg.Sockets.PolicySocket = policySock
	cfg.Sockets.SocketmapSocket = socketmapSock

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := mailcloak.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); <-svc.Done() })

	waitUnixReady(t, socketmapSock, 2*time.Second)

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"alias exists", "alias alias1@d1.test", "OK alice@d1.test"},
		{"alias missing", "alias missing@d1.test", "NOTFOUND"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := socketmapQuery(t, socketmapSock, tc.key)
			if tc.want != strings.TrimSpace(resp) {
				t.Fatalf("expected %q, got %q", tc.want, resp)
			}
		})
	}
}
