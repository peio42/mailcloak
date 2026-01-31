package mailcloak

import (
	"errors"
	"testing"

	"mailcloak/internal/mailcloak/testutil"
)

func testPolicyConfig(failureMode string) *Config {
	cfg := &Config{}
	cfg.Policy.KeycloakFailureMode = failureMode
	return cfg
}

func TestPolicyRCPT(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertDomain(t, sqlDB, "example.com", true)
	testutil.InsertDomain(t, sqlDB, "other-local.com", true)
	testutil.InsertDomain(t, sqlDB, "disabled.com", false)
	testutil.InsertAlias(t, sqlDB, "alias@example.com", "alice", true)
	testutil.InsertAlias(t, sqlDB, "bob@example.com", "bob", true)
	testutil.InsertAlias(t, sqlDB, "alias@other-local.com", "alice", true)

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
		{name: "disabled domain", rcpt: "user@disabled.com", expect: "DUNNO"},
		{name: "keycloak primary exists", rcpt: "alice@example.com", idpExists: true, expect: "DUNNO"},
		{name: "alias exists", rcpt: "alias@example.com", expect: "DUNNO"},
		{name: "alias exists other local domain", rcpt: "alias@other-local.com", expect: "DUNNO"},
		{name: "same local user other domain missing", rcpt: "bob@other-local.com", expect: "550 5.1.1 No such user"},
		{name: "missing user", rcpt: "missing@example.com", expect: "550 5.1.1 No such user"},
		{name: "keycloak error tempfail", rcpt: "err@example.com", idpErr: errors.New("idp"), failureMode: "tempfail", expect: "451 4.3.0 Temporary authentication/lookup failure"},
		{name: "keycloak error dunno", rcpt: "err2@example.com", idpErr: errors.New("idp"), failureMode: "dunno", expect: "DUNNO"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testPolicyConfig(tc.failureMode)
			fakeIDP := &testutil.FakeIdentityResolver{
				EmailExistsSet: map[string]bool{tc.rcpt: tc.idpExists},
				EmailExistsErr: tc.idpErr,
			}

			got := policyRCPT(cfg, db, fakeIDP, tc.rcpt)
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
	testutil.InsertDomain(t, sqlDB, "example.com", true)
	testutil.InsertDomain(t, sqlDB, "other-local.com", true)
	testutil.InsertDomain(t, sqlDB, "disabled.com", false)
	testutil.InsertAlias(t, sqlDB, "alias@example.com", "alice", true)
	testutil.InsertAlias(t, sqlDB, "alias@other-local.com", "alice", true)
	testutil.InsertApp(t, sqlDB, "app:myapp", true)
	testutil.InsertAppFrom(t, sqlDB, "app:myapp", "myapp@example.com", true)

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
		{name: "disabled domain", saslUser: "alice", sender: "user@disabled.com", expect: "DUNNO"},
		{name: "primary email", saslUser: "alice", sender: "alice@example.com", idpEmail: "alice@example.com", expect: "DUNNO"},
		{name: "alias belongs", saslUser: "alice", sender: "alias@example.com", expect: "DUNNO"},
		{name: "alias belongs other local domain", saslUser: "alice", sender: "alias@other-local.com", expect: "DUNNO"},
		{name: "alias missing other local domain", saslUser: "alice", sender: "missing@other-local.com", expect: "553 5.7.1 Sender not owned by authenticated user"},
		{name: "app allowed", saslUser: "app:myapp", sender: "myapp@example.com", expect: "DUNNO"},
		{name: "keycloak error tempfail", saslUser: "alice", sender: "bad@example.com", idpErr: errors.New("idp"), failureMode: "tempfail", expect: "451 4.3.0 Temporary authentication/lookup failure"},
		{name: "sender not owned", saslUser: "alice", sender: "nope@example.com", expect: "553 5.7.1 Sender not owned by authenticated user"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testPolicyConfig(tc.failureMode)
			fakeIDP := &testutil.FakeIdentityResolver{
				EmailByUser:         map[string]string{tc.saslUser: tc.idpEmail},
				ResolveUserEmailErr: tc.idpErr,
			}

			got := policyMAIL(cfg, db, fakeIDP, tc.saslUser, tc.sender)
			if got != tc.expect {
				t.Fatalf("expected %q, got %q", tc.expect, got)
			}
		})
	}
}
