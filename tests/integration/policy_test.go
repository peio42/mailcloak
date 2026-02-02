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

func baseConfig(dbPath, policySock, socketmapSock, kcURL, failureMode string) *mailcloak.Config {
	cfg := &mailcloak.Config{}
	cfg.Daemon.User = ""
	cfg.SQLite.Path = dbPath

	cfg.Sockets.PolicySocket = policySock
	cfg.Sockets.SocketmapSocket = socketmapSock

	cfg.Keycloak.BaseURL = kcURL
	cfg.Keycloak.Realm = "test"
	cfg.Keycloak.ClientID = "test"
	cfg.Keycloak.ClientSecret = "test"

	cfg.Policy.CacheTTLSeconds = 1
	cfg.Policy.KeycloakFailureMode = failureMode
	return cfg
}

func TestPolicy_RCPT(t *testing.T) {
	tests := []struct {
		name             string
		kcMode           fakeKCMode
		saslUser         string
		sender           string
		recipient        string
		failureMode      string
		wantActionSubstr string
	}{
		{
			name:             "RCPT allowed via alias when keycloak returns empty",
			kcMode:           kcUserAbsent,
			sender:           "sender@example.net",
			recipient:        "alias1@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "RCPT rejected when keycloak empty and alias missing",
			kcMode:           kcUserAbsent,
			sender:           "sender@example.net",
			recipient:        "missing@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=550 5.1.1 No such user",
		},
		{
			name:             "Local sender allowed when keycloak returns primary email",
			kcMode:           kcUserPresent,
			saslUser:         "alice",
			sender:           "alice@d1.test",
			recipient:        "recipient@example.net",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "Local sender rejected when not matching keycloak primary email",
			kcMode:           kcUserPresent,
			saslUser:         "alice",
			sender:           "bob@d2.test",
			recipient:        "recipient@example.net",
			failureMode:      "tempfail",
			wantActionSubstr: "action=553 5.7.1 Sender not owned by authenticated user",
		},
		{
			name:             "app mail allowed when sender allowed for app",
			kcMode:           kcUserAbsent,
			saslUser:         "app1",
			sender:           "app1@d1.test",
			recipient:        "recipient@example.net",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "app mail rejected when sender not allowed for app",
			kcMode:           kcUserAbsent,
			saslUser:         "app1",
			sender:           "app2@d1.test",
			recipient:        "recipient@example.net",
			failureMode:      "tempfail",
			wantActionSubstr: "action=553 5.7.1 Sender not owned by authenticated user",
		},
		{
			name:             "Keycloak down + failure_mode=tempfail => 451",
			kcMode:           kcDown,
			sender:           "alice@d1.test",
			recipient:        "alias1@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=451 4.3.0 Temporary authentication/lookup failure",
		},
		{
			name:             "Keycloak down + failure_mode=dunno => DUNNO",
			kcMode:           kcDown,
			sender:           "alice@d1.test",
			recipient:        "alias1@d1.test",
			failureMode:      "dunno",
			wantActionSubstr: "action=DUNNO",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := shortTempDir(t)
			dbPath, policySock, socketmapSock := dbAndSockets(t, dir)
			seedTestDB(t, dbPath)

			kc := newFakeKeycloak(t, tc.kcMode)
			t.Cleanup(kc.Close)

			cfg := baseConfig(dbPath, policySock, socketmapSock, kc.URL, tc.failureMode)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			svc, err := mailcloak.Start(ctx, cfg)
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			t.Cleanup(func() { cancel(); <-svc.Done() })

			waitUnixReady(t, policySock, 2*time.Second)

			resp := policyQuery(t, policySock, map[string]string{
				"request":             "smtpd_access_policy",
				"protocol_state":      "RCPT",
				"protocol_name":       "ESMTP",
				"helo_name":           "example.net",
				"queue_id":            "TEST123",
				"sender":              tc.sender,
				"recipient":           tc.recipient,
				"sasl_username":       tc.saslUser,
				"client_address":      "127.0.0.1",
				"client_name":         "localhost",
				"reverse_client_name": "localhost",
			})

			t.Cleanup(kc.Close)

			// Normalize a bit for debug
			respTrim := strings.TrimSpace(resp)
			if !strings.Contains(respTrim, tc.wantActionSubstr) {
				t.Fatalf("expected response to contain %q, got:\n%s", tc.wantActionSubstr, respTrim)
			}

			// Fails if a server has crashed.
			if err := svc.Err(); err != nil {
				t.Fatalf("service error: %v", err)
			}
		})
	}
}
