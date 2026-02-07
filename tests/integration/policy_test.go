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

	cfg.IDP.Provider = "keycloak"
	cfg.IDP.Keycloak.BaseURL = kcURL
	cfg.IDP.Keycloak.Realm = "test"
	cfg.IDP.Keycloak.ClientID = "test"
	cfg.IDP.Keycloak.ClientSecret = "test"
	cfg.IDP.Keycloak.CacheTTLSeconds = 1
	cfg.Policy.IDPFailureMode = failureMode
	return cfg
}

func TestPolicy(t *testing.T) {
	tests := []struct {
		name             string
		kcMode           fakeKCMode
		saslMethod       string
		saslUser         string
		sender           string
		recipient        string
		failureMode      string
		wantActionSubstr string
	}{
		{
			name:             "RCPT allowed for primary email",
			kcMode:           kcUserAlice,
			sender:           "sender@example.net",
			recipient:        "alice@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "RCPT allowed for alias email",
			kcMode:           kcUserAbsent,
			sender:           "sender@example.net",
			recipient:        "alias1@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "RCPT rejected when alias missing",
			kcMode:           kcUserAbsent,
			sender:           "sender@example.net",
			recipient:        "missing@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=550 5.1.1 No such user",
		},
		{
			name:             "RCPT rejected when sender from local without auth",
			kcMode:           kcUserAbsent,
			sender:           "alice@d1.test",
			recipient:        "alias1@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=553 5.7.1 Sending from local domains requires authentication",
		},
		{
			name:             "RCPT rejected for mail relay",
			kcMode:           kcUserAbsent,
			sender:           "sender@example.net",
			recipient:        "recipient@example.com",
			failureMode:      "tempfail",
			wantActionSubstr: "action=550 5.7.1 Recipient domain not local",
		},
		{
			name:             "Local sender allowed for user primary email",
			kcMode:           kcUserAlice,
			saslMethod:       "xoauth2",
			saslUser:         "alice",
			sender:           "alice@d1.test",
			recipient:        "recipient@example.com",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "Local sender allowed for user alias email",
			kcMode:           kcUserAlice,
			saslMethod:       "xoauth2",
			saslUser:         "alice",
			sender:           "alias1@d1.test",
			recipient:        "recipient@example.com",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "Local sender rejected when not matching keycloak primary email",
			kcMode:           kcUserAlice,
			saslMethod:       "xoauth2",
			saslUser:         "alice",
			sender:           "bob@d2.test",
			recipient:        "recipient@example.com",
			failureMode:      "tempfail",
			wantActionSubstr: "action=553 5.7.1 Sender not owned by authenticated user",
		},
		{
			name:             "app mail allowed when sender allowed for app",
			kcMode:           kcUserAbsent,
			saslMethod:       "plain",
			saslUser:         "app1",
			sender:           "app1@d1.test",
			recipient:        "recipient@example.com",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "app mail rejected when sender not allowed for app",
			kcMode:           kcUserAbsent,
			saslMethod:       "plain",
			saslUser:         "app1",
			sender:           "app2@d1.test",
			recipient:        "recipient@example.com",
			failureMode:      "tempfail",
			wantActionSubstr: "action=553 5.7.1 Sender not owned by authenticated user",
		},
		{
			name:             "Keycloak down + failure_mode=tempfail => 451",
			kcMode:           kcDown,
			saslMethod:       "xoauth2",
			sender:           "alice@d1.test",
			recipient:        "alias1@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=451 4.3.0 Temporary authentication/lookup failure",
		},
		{
			name:             "Keycloak down + failure_mode=dunno => DUNNO",
			kcMode:           kcDown,
			saslMethod:       "xoauth2",
			saslUser:         "alice",
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
				"sasl_method":         tc.saslMethod,
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
