package mailcloak

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type MailcloakDB struct{ DB *sql.DB }

func OpenMailcloakDB(path string) (*MailcloakDB, error) {
	log.Printf("sqlite: opening db at %s", path)
	if err := ensureDBExists(path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
PRAGMA foreign_keys=ON;
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init pragmas: %w", err)
	}
	log.Printf("sqlite: db ready")

	return &MailcloakDB{DB: db}, nil
}

func (a *MailcloakDB) Close() error { return a.DB.Close() }

// Returns true if domain exists and is enabled
func (a *MailcloakDB) DomainEnabled(domain string) (bool, error) {
	if domain == "" {
		return false, nil
	}
	var enabled int
	err := a.DB.QueryRow(`SELECT enabled FROM domains WHERE domain_name=?`, strings.ToLower(domain)).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return enabled == 1, nil
}

// Returns email owning alias, ok
func (a *MailcloakDB) AliasOwner(aliasEmail string) (string, bool, error) {
	var user string
	var enabled int
	err := a.DB.QueryRow(`SELECT target_user, enabled FROM aliases WHERE alias_email=?`, aliasEmail).Scan(&user, &enabled)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if enabled != 1 {
		return "", false, nil
	}
	return user, true, nil
}

// Returns true if alias belongs to user
func (a *MailcloakDB) AliasBelongsTo(aliasEmail, user string) (bool, error) {
	var enabled int
	err := a.DB.QueryRow(`SELECT enabled FROM aliases WHERE alias_email=? AND target_user=?`, aliasEmail, user).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return enabled == 1, nil
}

// Returns true if app_id is enabled and sender is allowed for app
func (a *MailcloakDB) AppFromAllowed(appID, fromAddr string) (bool, error) {
	var appEnabled int
	var fromEnabled int
	err := a.DB.QueryRow(`
SELECT a.enabled, af.enabled
FROM app_from af
JOIN apps a ON a.app_id = af.app_id
WHERE af.app_id=? AND af.from_addr=?`, appID, fromAddr).Scan(&appEnabled, &fromEnabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return appEnabled == 1 && fromEnabled == 1, nil
}

func ensureDBExists(path string) error {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	if path == "" {
		return fmt.Errorf("sqlite path is empty")
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("sqlite db not found at %s; create it with 'mailcloakctl init'", path)
			}
			return err
		}
		return nil
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sqlite db directory not found at %s; create the db with 'mailcloakctl init'", dir)
		}
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sqlite db not found at %s; create it with 'mailcloakctl init'", path)
		}
		return err
	}
	return nil
}
