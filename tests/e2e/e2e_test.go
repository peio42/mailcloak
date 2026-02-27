//go:build e2e
// +build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	composeBaseFile      = "docker-compose.base.yml"
	composeKeycloakFile  = "docker-compose.keycloak.yml"
	composeAuthentikFile = "docker-compose.authentik.yml"
)

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

func TestE2E(t *testing.T) {
	requireDocker(t)

	for _, provider := range selectedProviders(t) {
		provider := provider
		t.Run(provider.Name(), func(t *testing.T) {
			runner := newE2ERunner(provider)
			runner.composeUp(t)
			defer runner.composeDown(t)

			provider.WaitReady(t, runner)
			waitForCommonServices(t, runner)

			aliceToken := provider.FetchToken(t, runner, "alice", "password")

			cases := []struct {
				name       string
				from       string
				to         string
				wantSubstr string
				authMode   string // "oauth", "plain", or empty
				authUser   string
				authSecret string
				expectFail bool
			}{
				{
					name:       "RCPT allowed for primary email",
					from:       "sender@example.net",
					to:         "alice@d1.test",
					wantSubstr: "250 2.1.5",
				},
				{
					name:       "RCPT allowed for alias email",
					from:       "sender@example.net",
					to:         "alias1@d1.test",
					wantSubstr: "250 2.1.5",
				},
				{
					name:       "RCPT rejected when alias missing",
					from:       "sender@example.net",
					to:         "missing@d1.test",
					wantSubstr: "No such user",
					expectFail: true,
				},
				{
					name:       "RCPT rejected when sender from local without auth",
					from:       "alice@d1.test",
					to:         "alias1@d1.test",
					wantSubstr: "Sending from local domains requires authentication",
					expectFail: true,
				},
				{
					name:       "Local sender allowed for user primary email",
					from:       "alice@d1.test",
					to:         "recipient@example.com",
					wantSubstr: "250 2.1.5",
					authMode:   "oauth",
					authUser:   "alice",
					authSecret: aliceToken,
				},
				{
					name:       "Local sender allowed for user alias email",
					from:       "alias1@d1.test",
					to:         "recipient@example.com",
					wantSubstr: "250 2.1.5",
					authMode:   "oauth",
					authUser:   "alice",
					authSecret: aliceToken,
				},
				{
					name:       "Local sender rejected when not matching IdP primary email",
					from:       "bob@d2.test",
					to:         "recipient@example.com",
					wantSubstr: "Sender not owned by authenticated user",
					authMode:   "oauth",
					authUser:   "alice",
					authSecret: aliceToken,
					expectFail: true,
				},
				{
					name:       "App mail allowed when sender allowed for app",
					from:       "app1@d1.test",
					to:         "recipient@example.com",
					wantSubstr: "250 2.1.5",
					authMode:   "plain",
					authUser:   "app1",
					authSecret: "password",
				},
				{
					name:       "App mail rejected when sender not allowed for app",
					from:       "app2@d2.test",
					to:         "recipient@example.com",
					wantSubstr: "Sender not owned by authenticated user",
					authMode:   "plain",
					authUser:   "app1",
					authSecret: "password",
					expectFail: true,
				},
				{
					name:       "App mail can't receive mail",
					from:       "sender@example.net",
					to:         "app1@d1.test",
					wantSubstr: "No such user",
					expectFail: true,
				},
			}

			for _, tc := range cases {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					out := smtpCurl(t, runner, tc.from, tc.to, tc.authMode, tc.authUser, tc.authSecret, tc.expectFail)
					if !strings.Contains(out, tc.wantSubstr) {
						t.Fatalf("expected output to contain %q, got:\n%s", tc.wantSubstr, out)
					}
				})
			}
		})
	}
}

func requireDocker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		t.Skipf("docker compose not available: %v", err)
	}
}

type providerHarness interface {
	Name() string
	ComposeFiles() []string
	WaitReady(t *testing.T, runner *e2eRunner)
	FetchToken(t *testing.T, runner *e2eRunner, username, password string) string
}

type e2eRunner struct {
	composeFiles []string
	projectName  string
}

func newE2ERunner(provider providerHarness) *e2eRunner {
	return &e2eRunner{
		composeFiles: provider.ComposeFiles(),
		projectName:  "e2e-" + provider.Name(),
	}
}

func (r *e2eRunner) composeUp(t *testing.T) {
	t.Helper()
	r.compose(t, "up", "-d", "--build")
}

func (r *e2eRunner) composeDown(t *testing.T) {
	t.Helper()
	_ = r.compose(t, "down", "-v")
}

func waitForCommonServices(t *testing.T, runner *e2eRunner) {
	t.Helper()
	runner.composeExec(t, "mailcloak", "/scripts/wait_for.sh", "/var/spool/postfix/private/mailcloak-policy", "60")
	runner.composeExec(t, "postfix", "/scripts/wait_for.sh", "tcp://127.0.0.1:25", "60")
	runner.composeExec(t, "postfix-external", "/scripts/wait_for.sh", "tcp://127.0.0.1:25", "60")
}

type keycloakProvider struct{}

func (keycloakProvider) Name() string { return "keycloak" }

func (keycloakProvider) ComposeFiles() []string {
	return []string{composeBaseFile, composeKeycloakFile}
}

func (keycloakProvider) WaitReady(t *testing.T, runner *e2eRunner) {
	t.Helper()
	runner.composeExec(t, "mailcloak", "/scripts/wait_for.sh", "http://keycloak:8080/realms/test", "180")
}

func (keycloakProvider) FetchToken(t *testing.T, runner *e2eRunner, username, password string) string {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", "mailcloak-admin")
	form.Set("client_secret", "mailcloak-admin-secret")
	form.Set("username", username)
	form.Set("password", password)

	args := []string{
		"curl",
		"-sS",
		"-X", "POST",
		"http://keycloak:8080/realms/test/protocol/openid-connect/token",
		"-H", "Content-Type: application/x-www-form-urlencoded",
		"-d", form.Encode(),
	}

	out := runner.composeExec(t, "postfix", args...)
	var tr tokenResponse
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&tr); err != nil {
		t.Fatalf("token decode: %v\nraw: %s", err, out)
	}
	if tr.AccessToken == "" {
		t.Fatalf("token response missing access_token: %s", out)
	}
	return tr.AccessToken
}

type authentikProvider struct{}

func (authentikProvider) Name() string { return "authentik" }

func (authentikProvider) ComposeFiles() []string {
	return []string{composeBaseFile, composeAuthentikFile}
}

func (authentikProvider) WaitReady(t *testing.T, runner *e2eRunner) {
	t.Helper()
	runner.composeExec(t, "mailcloak", "/scripts/wait_for.sh", "http://authentik-server:9000/-/health/live/", "240")
	runner.composeExec(t, "mailcloak", "/scripts/wait_for.sh", "http://authentik-server:9000/api/v3/", "240")
	runner.composeExec(t, "authentik-worker", "/providers/authentik/bootstrap.sh")
	runner.composeExec(t, "mailcloak",
		"curl", "-fsS",
		"-H", "Authorization: Bearer mailcloak-authentik-api-token",
		"http://authentik-server:9000/api/v3/core/users/?username=alice&is_active=true",
	)
	runner.composeExec(t, "mailcloak",
		"curl", "-fsS",
		"-H", "Authorization: Bearer mailcloak-authentik-api-token",
		"http://authentik-server:9000/api/v3/core/users/?email=app1@d1.test&is_active=true",
	)
	runner.composeExec(t, "postfix",
		"curl", "-fsS",
		"-u", "mailcloak-admin:mailcloak-admin-secret",
		"-X", "POST",
		"http://authentik-server:9000/application/o/introspect/",
		"-d", "token=alice-direct-access-token",
	)
}

func (authentikProvider) FetchToken(t *testing.T, runner *e2eRunner, username, password string) string {
	t.Helper()
	return fmt.Sprintf("%s-direct-access-token", username)
}

func selectedProviders(t *testing.T) []providerHarness {
	t.Helper()

	switch strings.TrimSpace(strings.ToLower(os.Getenv("E2E_PROVIDER"))) {
	case "", "keycloak":
		return []providerHarness{keycloakProvider{}}
	case "authentik":
		return []providerHarness{authentikProvider{}}
	case "all":
		return []providerHarness{keycloakProvider{}, authentikProvider{}}
	default:
		t.Fatalf("invalid E2E_PROVIDER %q (expected keycloak, authentik, or all)", os.Getenv("E2E_PROVIDER"))
		return nil
	}
}

func smtpCurl(t *testing.T, runner *e2eRunner, from, to, authMode, authUser, authSecret string, expectFail bool) string {
	t.Helper()
	args := []string{
		"curl",
		"--verbose",
		"--connect-timeout", "5",
		"--max-time", "20",
		"--url", "smtp://127.0.0.1",
		"--mail-from", from,
		"--mail-rcpt", to,
		"--upload-file", "/dev/null",
	}

	switch authMode {
	case "oauth":
		args = append(args,
			"--user", fmt.Sprintf("%s:", authUser),
			"--login-options", "AUTH=OAUTHBEARER",
			"--oauth2-bearer", authSecret,
		)
	case "plain":
		args = append(args,
			"--user", fmt.Sprintf("%s:%s", authUser, authSecret),
			"--login-options", "AUTH=PLAIN",
		)
	}

	if expectFail {
		return runner.composeExecAllowFail(t, "postfix", args...)
	}
	return runner.composeExec(t, "postfix", args...)
}

func (r *e2eRunner) composeExecAllowFail(t *testing.T, service string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"exec", "-T", service}, args...)
	return r.composeAllowFail(t, cmdArgs...)
}

func (r *e2eRunner) composeAllowFail(t *testing.T, args ...string) string {
	t.Helper()
	cmdArgs := []string{"compose"}
	for _, composeFile := range r.composeFiles {
		cmdArgs = append(cmdArgs, "-f", filepath.FromSlash(composeFile))
	}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_NAME=%s", r.projectName))

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	_ = cmd.Run()
	return buf.String()
}

func (r *e2eRunner) composeExec(t *testing.T, service string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"exec", "-T", service}, args...)
	return r.compose(t, cmdArgs...)
}

func (r *e2eRunner) compose(t *testing.T, args ...string) string {
	t.Helper()
	cmdArgs := []string{"compose"}
	for _, composeFile := range r.composeFiles {
		cmdArgs = append(cmdArgs, "-f", filepath.FromSlash(composeFile))
	}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_NAME=%s", r.projectName))

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		t.Fatalf("docker compose %s failed: %v\n%s", strings.Join(args, " "), err, buf.String())
	}

	return buf.String()
}
