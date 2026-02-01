//go:build integration
// +build integration

package integration_test

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func waitUnixReady(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", path)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket not ready: %s", path)
}

func socketmapQuery(t *testing.T, sock string, payload string) string {
	t.Helper()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := writeSocketmapFrame(conn, payload); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	r := bufio.NewReader(conn)
	resp, err := readSocketmapFrame(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return resp
}

func writeSocketmapFrame(w io.Writer, payload string) error {
	// Postfix socketmap framing: "<len>:<payload>,"
	// len = number of bytes in payload (UTF-8 safe if payload is ASCII, which it is here)
	_, err := fmt.Fprintf(w, "%d:%s,", len(payload), payload)
	return err
}

func readSocketmapFrame(r *bufio.Reader) (string, error) {
	// Read length prefix until ':'
	var n int
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == ':' {
			break
		}
		if b < '0' || b > '9' {
			return "", fmt.Errorf("invalid frame length char: %q", b)
		}
		n = n*10 + int(b-'0')
	}

	// Read payload
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}

	// Read trailing comma
	b, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	if b != ',' {
		return "", fmt.Errorf("invalid frame trailer: %q", b)
	}

	return string(buf), nil
}

func seedTestDB(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql open: %v", err)
	}
	defer db.Close()

	schema := `
CREATE TABLE IF NOT EXISTS domains (
    domain_name TEXT PRIMARY KEY,
    enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE TABLE IF NOT EXISTS aliases (
    alias_email       TEXT PRIMARY KEY,
    target_user       TEXT NOT NULL,
    alias_domain_name TEXT NOT NULL,
    enabled           INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    updated_at        INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    FOREIGN KEY (alias_domain_name) REFERENCES domains(domain_name) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_aliases_target_user ON aliases(target_user);
CREATE INDEX IF NOT EXISTS idx_aliases_alias_domain ON aliases(alias_domain_name);

CREATE TABLE IF NOT EXISTS apps (
    app_id      TEXT PRIMARY KEY,
    secret_hash TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE TABLE IF NOT EXISTS app_from (
    app_id      TEXT NOT NULL,
    from_addr   TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    PRIMARY KEY (app_id, from_addr),
    FOREIGN KEY (app_id) REFERENCES apps(app_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_app_from_from_addr ON app_from(from_addr);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema exec: %v", err)
	}

	// Local domains
	mustExec(t, db, `INSERT INTO domains(domain_name, enabled) VALUES (?,1)`, "d1.test")
	mustExec(t, db, `INSERT INTO domains(domain_name, enabled) VALUES (?,1)`, "d2.test")

	// Alias on each domain
	mustExec(t, db, `INSERT INTO aliases(alias_email, target_user, alias_domain_name, enabled) VALUES (?,?,?,1)`,
		"alias1@d1.test", "alice", "d1.test")
	mustExec(t, db, `INSERT INTO aliases(alias_email, target_user, alias_domain_name, enabled) VALUES (?,?,?,1)`,
		"alias2@d2.test", "bob", "d2.test")

	// Apps
	mustExec(t, db, `INSERT INTO apps(app_id, secret_hash, enabled) VALUES (?,?,1)`,
		"app1", "{ARGON2ID}$argon2id$v=19$m=16,t=2,p=1$MTIzNDU2Nzg$Dhk8fwnes+f9vzOwgdALlA") // pass: password
	mustExec(t, db, `INSERT INTO app_from(app_id, from_addr, enabled) VALUES (?,?,1)`,
		"app1", "app1@d1.test")
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q args=%v: %v", q, args, err)
	}
}

// helper for common paths
func dbAndSockets(t *testing.T, dir string) (dbPath, policySock, socketmapSock string) {
	t.Helper()
	return filepath.Join(dir, "mailcloak.db"),
		filepath.Join(dir, "policy.sock"),
		filepath.Join(dir, "socketmap.sock")
}

func policyQuery(t *testing.T, sockPath string, kv map[string]string) string {
	t.Helper()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial unix: %v", err)
	}
	defer conn.Close()

	// Build request: key=value\n ... \n\n
	var b strings.Builder
	for k, v := range kv {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if _, err := conn.Write([]byte(b.String())); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(resp)
}

type fakeKCMode int

const (
	kcUserAbsent  fakeKCMode = iota // token OK, /users returns []
	kcUserPresent                   // token OK, /users returns alice with email alice@...
	kcDown                          // endpoints return 500
)

// newFakeKeycloak exposes (realm: "test"):
// - POST /realms/test/protocol/openid-connect/token
// - GET  /admin/realms/test/users?... (returns [] or user list or 500)
func newFakeKeycloak(t *testing.T, mode fakeKCMode) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	tokenPath := "/realms/test/protocol/openid-connect/token"
	usersPath := "/admin/realms/test/users"

	mux.HandleFunc(tokenPath, func(w http.ResponseWriter, r *http.Request) {
		if mode == kcDown {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"testtoken","expires_in":300}`))
	})

	mux.HandleFunc(usersPath, func(w http.ResponseWriter, r *http.Request) {
		if mode == kcDown {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if mode == kcUserPresent {
			_, _ = w.Write([]byte(`[{"id":"1","username":"alice","email":"alice@d1.test","enabled":true}]`))
			return
		}
		// kcUserAbsent
		_, _ = w.Write([]byte(`[]`))
	})

	return httptest.NewServer(mux)
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mcit-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
