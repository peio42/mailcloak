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
	composeFile = "docker-compose.yml"
	projectName = "e2e"
)

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

func TestE2E(t *testing.T) {
	requireDocker(t)

	composeUp(t)
	defer composeDown(t)

	waitForServices(t)

	aliceToken := fetchToken(t, "alice", "password")

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
			name:       "Local sender rejected when not matching keycloak primary email",
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
			out := smtpCurl(t, tc.from, tc.to, tc.authMode, tc.authUser, tc.authSecret, tc.expectFail)
			if !strings.Contains(out, tc.wantSubstr) {
				t.Fatalf("expected output to contain %q, got:\n%s", tc.wantSubstr, out)
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

func composeUp(t *testing.T) {
	t.Helper()
	dockerCompose(t, "up", "-d", "--build")
}

func composeDown(t *testing.T) {
	t.Helper()
	_ = dockerCompose(t, "down", "-v")
}

func waitForServices(t *testing.T) {
	t.Helper()
	dockerComposeExec(t, "mailcloak", "/scripts/wait_for.sh", "http://keycloak:8080/realms/test", "120")
	dockerComposeExec(t, "mailcloak", "/scripts/wait_for.sh", "/var/spool/postfix/private/mailcloak-policy", "60")
	dockerComposeExec(t, "postfix", "/scripts/wait_for.sh", "tcp://127.0.0.1:25", "60")
	dockerComposeExec(t, "postfix-external", "/scripts/wait_for.sh", "tcp://127.0.0.1:25", "60")
}

func fetchToken(t *testing.T, username, password string) string {
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

	out := dockerComposeExec(t, "postfix", args...)
	var tr tokenResponse
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&tr); err != nil {
		t.Fatalf("token decode: %v\nraw: %s", err, out)
	}
	if tr.AccessToken == "" {
		t.Fatalf("token response missing access_token: %s", out)
	}
	return tr.AccessToken
}

func smtpCurl(t *testing.T, from, to, authMode, authUser, authSecret string, expectFail bool) string {
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
		return dockerComposeExecAllowFail(t, "postfix", args...)
	}
	return dockerComposeExec(t, "postfix", args...)
}

func dockerComposeExecAllowFail(t *testing.T, service string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"exec", "-T", service}, args...)
	return dockerComposeAllowFail(t, cmdArgs...)
}

func dockerComposeAllowFail(t *testing.T, args ...string) string {
	t.Helper()
	absCompose := filepath.FromSlash(composeFile)
	cmdArgs := append([]string{"compose", "-f", absCompose}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_NAME=%s", projectName))

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	_ = cmd.Run()
	return buf.String()
}

func dockerComposeExec(t *testing.T, service string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"exec", "-T", service}, args...)
	return dockerCompose(t, cmdArgs...)
}

func dockerCompose(t *testing.T, args ...string) string {
	t.Helper()
	absCompose := filepath.FromSlash(composeFile)
	cmdArgs := append([]string{"compose", "-f", absCompose}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_NAME=%s", projectName))

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		t.Fatalf("docker compose %s failed: %v\n%s", strings.Join(args, " "), err, buf.String())
	}

	return buf.String()
}
