package mailcloak

import (
	"errors"
	"testing"

	"mailcloak/internal/mailcloak/testutil"
)

func testPolicyConfig(failureMode string) *Config {
	cfg := &Config{}
	cfg.Policy.IDPFailureMode = failureMode
	return cfg
}

func TestPolicy(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertDomain(t, sqlDB, "example.com", true)
	testutil.InsertDomain(t, sqlDB, "other-local.com", true)
	testutil.InsertDomain(t, sqlDB, "disabled.com", false)
	testutil.InsertAlias(t, sqlDB, "alias@example.com", "alice", true)
	testutil.InsertAlias(t, sqlDB, "alias@other-local.com", "alice", true)
	testutil.InsertAlias(t, sqlDB, "bob@example.com", "bob", true)
	testutil.InsertApp(t, sqlDB, "myapp", true)
	testutil.InsertAppFrom(t, sqlDB, "myapp", "myapp@example.com", true)

	cases := []struct {
		name        string
		sender      string
		rcpt        string
		saslMethod  string
		saslUser    string
		idpExists   bool
		idpEmail    string
		idpErr      error
		failureMode string
		expect      string
	}{
		{
			name:   "empty rcpt",
			sender: "user@other.com",
			rcpt:   "",
			expect: "DUNNO",
		},
		{
			name:   "unauthenticated non-local rcpt",
			sender: "user@other.com",
			rcpt:   "user@other.com",
			expect: "550 5.7.1 Recipient domain not local",
		},
		{
			name:      "local rcpt exists",
			sender:    "user@other.com",
			rcpt:      "alice@example.com",
			idpExists: true,
			expect:    "DUNNO",
		},
		{
			name:   "local rcpt missing",
			sender: "user@other.com",
			rcpt:   "missing@example.com",
			expect: "550 5.1.1 No such user",
		},
		{
			name:        "local rcpt idp error tempfail",
			sender:      "user@other.com",
			rcpt:        "err@example.com",
			idpErr:      errors.New("idp"),
			failureMode: "tempfail",
			expect:      "451 4.3.0 Temporary authentication/lookup failure",
		},
		{
			name:        "local rcpt idp error dunno",
			sender:      "user@other.com",
			rcpt:        "err2@example.com",
			idpErr:      errors.New("idp"),
			failureMode: "dunno",
			expect:      "DUNNO",
		},
		{
			name:      "unauthenticated sender local",
			sender:    "alice@example.com",
			rcpt:      "alice@example.com",
			idpExists: true,
			expect:    "553 5.7.1 Sending from local domains requires authentication",
		},
		{
			name:      "unauthenticated sender non-local",
			sender:    "user@other.com",
			rcpt:      "alice@example.com",
			idpExists: true,
			expect:    "DUNNO",
		},
		{
			name:       "oidc sender primary email",
			sender:     "alice@example.com",
			rcpt:       "alice@example.com",
			saslMethod: "xoauth2",
			saslUser:   "alice",
			idpExists:  true,
			idpEmail:   "alice@example.com",
			expect:     "DUNNO",
		},
		{
			name:       "oidc sender alias belongs",
			sender:     "alias@example.com",
			rcpt:       "alice@example.com",
			saslMethod: "oauthbearer",
			saslUser:   "alice",
			idpExists:  true,
			idpEmail:   "alice@example.com",
			expect:     "DUNNO",
		},
		{
			name:       "oidc sender not owned",
			sender:     "nope@example.com",
			rcpt:       "alice@example.com",
			saslMethod: "xoauth2",
			saslUser:   "alice",
			idpExists:  true,
			idpEmail:   "alice@example.com",
			expect:     "553 5.7.1 Sender not owned by authenticated user",
		},
		{
			name:        "oidc idp error tempfail",
			sender:      "bad@example.com",
			rcpt:        "alice@example.com",
			saslMethod:  "xoauth2",
			saslUser:    "alice",
			idpExists:   true,
			idpErr:      errors.New("idp"),
			failureMode: "tempfail",
			expect:      "451 4.3.0 Temporary authentication/lookup failure",
		},
		{
			name:       "app sender allowed",
			sender:     "myapp@example.com",
			rcpt:       "alice@example.com",
			saslMethod: "plain",
			saslUser:   "myapp",
			idpExists:  true,
			expect:     "DUNNO",
		},
		{
			name:       "app sender not allowed",
			sender:     "nope@example.com",
			rcpt:       "alice@example.com",
			saslMethod: "login",
			saslUser:   "myapp",
			idpExists:  true,
			expect:     "553 5.7.1 Sender not owned by authenticated user",
		},
		{
			name:       "unsupported auth method",
			sender:     "user@other.com",
			rcpt:       "alice@example.com",
			saslMethod: "cram-md5",
			saslUser:   "alice",
			idpExists:  true,
			expect:     "553 5.7.1 Unsupported authentication method",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testPolicyConfig(tc.failureMode)
			fakeIDP := &testutil.FakeIdentityResolver{
				EmailByUser:         map[string]string{tc.saslUser: tc.idpEmail},
				EmailExistsSet:      map[string]bool{tc.rcpt: tc.idpExists},
				ResolveUserEmailErr: tc.idpErr,
				EmailExistsErr:      tc.idpErr,
			}

			got := policy(cfg, db, fakeIDP, tc.sender, tc.rcpt, tc.saslMethod, tc.saslUser)
			if got != tc.expect {
				t.Fatalf("expected %q, got %q", tc.expect, got)
			}
		})
	}
}
