//go:build integration
// +build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mailcloak/internal/mailcloak"
)

type fakeAKMode int

const (
	akUserAbsent fakeAKMode = iota
	akUserAlice
	akDown
)

func newFakeAuthentik(t *testing.T, mode fakeAKMode) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/core/users/", func(w http.ResponseWriter, r *http.Request) {
		if mode == akDown {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("is_active") != "true" {
			http.Error(w, "missing is_active", http.StatusBadRequest)
			return
		}

		results := []map[string]any{}
		username := r.URL.Query().Get("username")
		email := r.URL.Query().Get("email")

		if mode == akUserAlice {
			switch {
			case strings.EqualFold(username, "alice"):
				results = []map[string]any{{
					"username":  "alice",
					"email":     "alice@d1.test",
					"is_active": true,
				}}
			case strings.EqualFold(email, "alice@d1.test"):
				results = []map[string]any{{
					"username":  "alice",
					"email":     "alice@d1.test",
					"is_active": true,
				}}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	})

	return httptest.NewServer(mux)
}

func TestPolicyWithAuthentik(t *testing.T) {
	tests := []struct {
		name             string
		akMode           fakeAKMode
		saslMethod       string
		saslUser         string
		sender           string
		recipient        string
		failureMode      string
		wantActionSubstr string
	}{
		{
			name:             "RCPT allowed for authentik primary email",
			akMode:           akUserAlice,
			sender:           "sender@example.net",
			recipient:        "alice@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "local sender allowed for authentik user primary email",
			akMode:           akUserAlice,
			saslMethod:       "xoauth2",
			saslUser:         "alice",
			sender:           "alice@d1.test",
			recipient:        "recipient@example.com",
			failureMode:      "tempfail",
			wantActionSubstr: "action=DUNNO",
		},
		{
			name:             "authentik down + failure_mode=tempfail => 451",
			akMode:           akDown,
			saslMethod:       "xoauth2",
			saslUser:         "alice",
			sender:           "alice@d1.test",
			recipient:        "alias1@d1.test",
			failureMode:      "tempfail",
			wantActionSubstr: "action=451 4.3.0 Temporary authentication/lookup failure",
		},
		{
			name:             "authentik down + failure_mode=dunno => DUNNO",
			akMode:           akDown,
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

			ak := newFakeAuthentik(t, tc.akMode)
			t.Cleanup(ak.Close)

			cfg := &mailcloak.Config{}
			cfg.Daemon.User = ""
			cfg.SQLite.Path = dbPath
			cfg.Sockets.PolicySocket = policySock
			cfg.Sockets.SocketmapSocket = socketmapSock
			cfg.IDP.Provider = "authentik"
			cfg.IDP.Authentik.BaseURL = ak.URL
			cfg.IDP.Authentik.APIToken = "test-token"
			cfg.IDP.Authentik.CacheTTLSeconds = 1
			cfg.Policy.IDPFailureMode = tc.failureMode

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

			respTrim := strings.TrimSpace(resp)
			if !strings.Contains(respTrim, tc.wantActionSubstr) {
				t.Fatalf("expected response to contain %q, got:\n%s", tc.wantActionSubstr, respTrim)
			}
			if err := svc.Err(); err != nil {
				t.Fatalf("service error: %v", err)
			}
		})
	}
}
