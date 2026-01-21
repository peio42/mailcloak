package mailcloak

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type AliasDB struct{ DB *sql.DB }

func OpenAliasDB(path string) (*AliasDB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
PRAGMA foreign_keys=ON;
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
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &AliasDB{DB: db}, nil
}

func (a *AliasDB) Close() error { return a.DB.Close() }

// Returns username owning alias, ok
func (a *AliasDB) AliasOwner(aliasEmail string) (string, bool, error) {
	var username string
	var enabled int
	err := a.DB.QueryRow(`SELECT username, enabled FROM aliases WHERE alias_email=?`, aliasEmail).Scan(&username, &enabled)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if enabled != 1 {
		return "", false, nil
	}
	return username, true, nil
}

// Returns true if alias belongs to username
func (a *AliasDB) AliasBelongsTo(aliasEmail, username string) (bool, error) {
	var enabled int
	err := a.DB.QueryRow(`SELECT enabled FROM aliases WHERE alias_email=? AND username=?`, aliasEmail, username).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return enabled == 1, nil
}
