package mailcloak

import (
	"errors"
	"testing"
	"time"

	"mailcloak/internal/mailcloak/testutil"
)

func testPolicyConfig(domain, failureMode string) *Config {
	cfg := &Config{}
	cfg.Policy.Domain = domain
	cfg.Policy.KeycloakFailureMode = failureMode
	return cfg
}

func TestPolicyRCPT(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertAlias(t, sqlDB, "alias@example.com", "alice", true)

	cases := []struct {
		name        string
		rcpt        string
		idpExists   bool
		idpErr      error
		failureMode string
		expect      string
	}{
		{name: "empty rcpt", rcpt: "", expect: "DUNNO"},
		{name: "other domain", rcpt: "user@other.com", expect: "DUNNO"},
		{name: "keycloak primary exists", rcpt: "alice@example.com", idpExists: true, expect: "DUNNO"},
		{name: "alias exists", rcpt: "alias@example.com", expect: "DUNNO"},
		{name: "missing user", rcpt: "missing@example.com", expect: "550 5.1.1 No such user"},
		{name: "keycloak error tempfail", rcpt: "err@example.com", idpErr: errors.New("idp"), failureMode: "tempfail", expect: "451 4.3.0 Temporary authentication/lookup failure"},
		{name: "keycloak error dunno", rcpt: "err2@example.com", idpErr: errors.New("idp"), failureMode: "dunno", expect: "DUNNO"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testPolicyConfig("example.com", tc.failureMode)
			fakeIDP := &testutil.FakeIdentityResolver{
				EmailExistsSet: map[string]bool{tc.rcpt: tc.idpExists},
				EmailExistsErr: tc.idpErr,
			}
			cache := NewCache(1 * time.Minute)

			got := policyRCPT(cfg, db, fakeIDP, cache, tc.rcpt)
			if got != tc.expect {
				t.Fatalf("expected %q, got %q", tc.expect, got)
			}
		})
	}
}

func TestPolicyMAIL(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertAlias(t, sqlDB, "alias@example.com", "alice", true)
	testutil.InsertApp(t, sqlDB, "app1", true)
	testutil.InsertAppFrom(t, sqlDB, "app1", "from@example.com", true)

	cases := []struct {
		name        string
		saslUser    string
		sender      string
		idpEmail    string
		idpErr      error
		failureMode string
		expect      string
	}{
		{name: "empty sender", saslUser: "alice", sender: "", expect: "DUNNO"},
		{name: "other domain", saslUser: "alice", sender: "user@other.com", expect: "DUNNO"},
		{name: "primary email", saslUser: "alice", sender: "alice@example.com", idpEmail: "alice@example.com", expect: "DUNNO"},
		{name: "alias belongs", saslUser: "alice", sender: "alias@example.com", expect: "DUNNO"},
		{name: "app allowed", saslUser: "app1", sender: "from@example.com", expect: "DUNNO"},
		{name: "keycloak error tempfail", saslUser: "alice", sender: "bad@example.com", idpErr: errors.New("idp"), failureMode: "tempfail", expect: "451 4.3.0 Temporary authentication/lookup failure"},
		{name: "sender not owned", saslUser: "alice", sender: "nope@example.com", expect: "553 5.7.1 Sender not owned by authenticated user"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testPolicyConfig("example.com", tc.failureMode)
			fakeIDP := &testutil.FakeIdentityResolver{
				EmailByUser:        map[string]string{tc.saslUser: tc.idpEmail},
				EmailByUsernameErr: tc.idpErr,
			}
			cache := NewCache(1 * time.Minute)

			got := policyMAIL(cfg, db, fakeIDP, cache, tc.saslUser, tc.sender)
			if got != tc.expect {
				t.Fatalf("expected %q, got %q", tc.expect, got)
			}
		})
	}
}
