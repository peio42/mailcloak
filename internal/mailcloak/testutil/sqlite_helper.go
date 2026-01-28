package testutil

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS aliases (
  alias_email TEXT PRIMARY KEY,
  username    TEXT NOT NULL,
  enabled     INTEGER NOT NULL DEFAULT 1,
  updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE INDEX IF NOT EXISTS idx_aliases_username ON aliases(username);

CREATE TABLE IF NOT EXISTS apps (
  app_id      TEXT PRIMARY KEY,
  secret_hash TEXT NOT NULL,
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS app_from (
  app_id    TEXT NOT NULL,
  from_addr TEXT NOT NULL,
  enabled   INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (app_id, from_addr),
  FOREIGN KEY (app_id) REFERENCES apps(app_id) ON DELETE CASCADE
);
`

func NewSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("write db file: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
PRAGMA foreign_keys=ON;
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
`); err != nil {
		_ = db.Close()
		t.Fatalf("init pragmas: %v", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		t.Fatalf("init schema: %v", err)
	}
	return db
}

func InsertAlias(t *testing.T, db *sql.DB, aliasEmail, username string, enabled bool) {
	t.Helper()
	en := 0
	if enabled {
		en = 1
	}
	_, err := db.Exec(`INSERT INTO aliases(alias_email, username, enabled, updated_at) VALUES(?,?,?,strftime('%s','now'))`, aliasEmail, username, en)
	if err != nil {
		t.Fatalf("insert alias: %v", err)
	}
}

func InsertApp(t *testing.T, db *sql.DB, appID string, enabled bool) {
	t.Helper()
	en := 0
	if enabled {
		en = 1
	}
	_, err := db.Exec(`INSERT INTO apps(app_id, secret_hash, enabled, created_at) VALUES(?,?,?,strftime('%s','now'))`, appID, "{ARGON2ID}dummy", en)
	if err != nil {
		t.Fatalf("insert app: %v", err)
	}
}

func InsertAppFrom(t *testing.T, db *sql.DB, appID, fromAddr string, enabled bool) {
	t.Helper()
	en := 0
	if enabled {
		en = 1
	}
	_, err := db.Exec(`INSERT INTO app_from(app_id, from_addr, enabled) VALUES(?,?,?)`, appID, fromAddr, en)
	if err != nil {
		t.Fatalf("insert app_from: %v", err)
	}
}
