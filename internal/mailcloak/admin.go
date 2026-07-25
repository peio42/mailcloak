package mailcloak

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultDBPath = "/var/lib/mailcloak/state.db"

var ErrAlreadyExists = errors.New("already exists")

type Domain struct {
	DomainName string `json:"domain_name"`
	Enabled    bool   `json:"enabled"`
	UpdatedAt  int64  `json:"updated_at"`
}

type Alias struct {
	AliasEmail      string `json:"alias_email"`
	TargetUser      string `json:"target_user"`
	AliasDomainName string `json:"alias_domain_name"`
	Enabled         bool   `json:"enabled"`
	UpdatedAt       int64  `json:"updated_at"`
}

type App struct {
	AppID     string      `json:"app_id"`
	Enabled   bool        `json:"enabled"`
	UpdatedAt int64       `json:"updated_at"`
	Senders   []AppSender `json:"senders"`
}

type AppSender struct {
	AppID     string `json:"app_id"`
	FromAddr  string `json:"from_addr"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt int64  `json:"updated_at"`
}

func InitMailcloakDB(path string) (*MailcloakDB, error) {
	if err := ensureDBParent(path); err != nil {
		return nil, err
	}
	db, err := openMailcloakSQLite(path)
	if err != nil {
		return nil, err
	}
	if err := applyMailcloakSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &MailcloakDB{DB: db}, nil
}

func ensureDBParent(path string) error {
	if path == "" {
		return fmt.Errorf("sqlite path is empty")
	}
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite db directory %s: %w", dir, err)
	}
	return nil
}

func applyMailcloakSchema(db *sql.DB) error {
	if _, err := db.Exec(mailcloakSchemaSQL); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

const mailcloakSchemaSQL = `
CREATE TABLE IF NOT EXISTS domains (
    domain_name TEXT PRIMARY KEY,
    enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TRIGGER IF NOT EXISTS trg_domains_set_updated_at
AFTER UPDATE ON domains
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE domains
    SET updated_at = strftime('%s','now')
    WHERE domain_name = NEW.domain_name;
END;

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

CREATE TRIGGER IF NOT EXISTS trg_aliases_domain_check_ins
BEFORE INSERT ON aliases
FOR EACH ROW
BEGIN
    SELECT CASE
        WHEN instr(NEW.alias_email, '@') = 0 THEN
            RAISE(ABORT, 'aliases.alias_email must contain @')
    END;
    SELECT CASE
        WHEN NEW.alias_domain_name <> substr(NEW.alias_email, instr(NEW.alias_email,'@')+1) THEN
            RAISE(ABORT, 'aliases.alias_domain_name mismatch with alias_email domain')
    END;
END;

CREATE TRIGGER IF NOT EXISTS trg_aliases_domain_check_upd
BEFORE UPDATE ON aliases
FOR EACH ROW
BEGIN
    SELECT CASE
        WHEN instr(NEW.alias_email, '@') = 0 THEN
            RAISE(ABORT, 'aliases.alias_email must contain @')
    END;
    SELECT CASE
        WHEN NEW.alias_domain_name <> substr(NEW.alias_email, instr(NEW.alias_email,'@')+1) THEN
            RAISE(ABORT, 'aliases.alias_domain_name mismatch with alias_email domain')
    END;
END;

CREATE TRIGGER IF NOT EXISTS trg_aliases_set_updated_at
AFTER UPDATE ON aliases
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE aliases
    SET updated_at = strftime('%s','now')
    WHERE alias_email = NEW.alias_email;
END;

CREATE TABLE IF NOT EXISTS apps (
    app_id      TEXT PRIMARY KEY,
    secret_hash TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TRIGGER IF NOT EXISTS trg_apps_set_updated_at
AFTER UPDATE ON apps
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE apps
    SET updated_at = strftime('%s','now')
    WHERE app_id = NEW.app_id;
END;

CREATE TABLE IF NOT EXISTS app_from (
    app_id      TEXT NOT NULL,
    from_addr   TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    PRIMARY KEY (app_id, from_addr),
    FOREIGN KEY (app_id) REFERENCES apps(app_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_app_from_from_addr ON app_from(from_addr);

CREATE TRIGGER IF NOT EXISTS trg_app_from_set_updated_at
AFTER UPDATE ON app_from
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
    UPDATE app_from
    SET updated_at = strftime('%s','now')
    WHERE app_id = NEW.app_id AND from_addr = NEW.from_addr;
END;

PRAGMA user_version = 1;
`

func (a *MailcloakDB) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT domain_name, enabled, updated_at FROM domains ORDER BY domain_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []Domain
	for rows.Next() {
		var domain Domain
		var enabled int
		if err := rows.Scan(&domain.DomainName, &enabled, &domain.UpdatedAt); err != nil {
			return nil, err
		}
		domain.Enabled = enabled == 1
		domains = append(domains, domain)
	}
	return domains, rows.Err()
}

func (a *MailcloakDB) UpsertDomain(ctx context.Context, domainName string) error {
	domainName = normalizeDomain(domainName)
	if domainName == "" {
		return fmt.Errorf("domain name is empty")
	}
	var existing string
	err := a.DB.QueryRowContext(ctx, `SELECT domain_name FROM domains WHERE domain_name=?`, domainName).Scan(&existing)
	if err == nil {
		return fmt.Errorf("domain already exists: %s: %w", domainName, ErrAlreadyExists)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().Unix()
	_, err = a.DB.ExecContext(ctx, `
INSERT INTO domains(domain_name, enabled, updated_at) VALUES(?,1,?)
`, domainName, now)
	if isUniqueConstraintError(err) {
		return fmt.Errorf("domain already exists: %s: %w", domainName, ErrAlreadyExists)
	}
	return err
}

func (a *MailcloakDB) DeleteDomain(ctx context.Context, domainName string) error {
	_, err := a.DB.ExecContext(ctx, `DELETE FROM domains WHERE domain_name=?`, normalizeDomain(domainName))
	return err
}

func (a *MailcloakDB) SetDomainEnabled(ctx context.Context, domainName string, enabled bool) error {
	_, err := a.DB.ExecContext(ctx, `UPDATE domains SET enabled=?, updated_at=? WHERE domain_name=?`, boolInt(enabled), time.Now().Unix(), normalizeDomain(domainName))
	return err
}

func (a *MailcloakDB) ListAliases(ctx context.Context, targetUser string) ([]Alias, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if strings.TrimSpace(targetUser) == "" {
		rows, err = a.DB.QueryContext(ctx, `SELECT alias_email, target_user, alias_domain_name, enabled, updated_at FROM aliases ORDER BY target_user, alias_email`)
	} else {
		rows, err = a.DB.QueryContext(ctx, `SELECT alias_email, target_user, alias_domain_name, enabled, updated_at FROM aliases WHERE target_user=? ORDER BY alias_email`, strings.TrimSpace(targetUser))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []Alias
	for rows.Next() {
		var alias Alias
		var enabled int
		if err := rows.Scan(&alias.AliasEmail, &alias.TargetUser, &alias.AliasDomainName, &enabled, &alias.UpdatedAt); err != nil {
			return nil, err
		}
		alias.Enabled = enabled == 1
		aliases = append(aliases, alias)
	}
	return aliases, rows.Err()
}

func (a *MailcloakDB) UpsertAlias(ctx context.Context, aliasEmail, targetUser string) error {
	aliasEmail = normalizeEmail(aliasEmail)
	targetUser = strings.TrimSpace(targetUser)
	if targetUser == "" {
		return fmt.Errorf("target user is empty")
	}
	aliasDomain, ok := domainFromEmail(aliasEmail)
	if !ok || aliasDomain == "" {
		return fmt.Errorf("invalid alias email address")
	}
	var existingDomain string
	err := a.DB.QueryRowContext(ctx, `SELECT domain_name FROM domains WHERE domain_name=?`, aliasDomain).Scan(&existingDomain)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("domain not found: %s", aliasDomain)
	}
	if err != nil {
		return err
	}
	var existingAlias string
	err = a.DB.QueryRowContext(ctx, `SELECT alias_email FROM aliases WHERE alias_email=?`, aliasEmail).Scan(&existingAlias)
	if err == nil {
		return fmt.Errorf("alias already exists: %s: %w", aliasEmail, ErrAlreadyExists)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().Unix()
	_, err = a.DB.ExecContext(ctx, `
INSERT INTO aliases(alias_email, target_user, alias_domain_name, enabled, updated_at)
VALUES(?,?,?,1,?)
`, aliasEmail, targetUser, existingDomain, now)
	if isUniqueConstraintError(err) {
		return fmt.Errorf("alias already exists: %s: %w", aliasEmail, ErrAlreadyExists)
	}
	return err
}

func (a *MailcloakDB) DeleteAlias(ctx context.Context, aliasEmail string) error {
	_, err := a.DB.ExecContext(ctx, `DELETE FROM aliases WHERE alias_email=?`, normalizeEmail(aliasEmail))
	return err
}

func (a *MailcloakDB) SetAliasEnabled(ctx context.Context, aliasEmail string, enabled bool) error {
	_, err := a.DB.ExecContext(ctx, `UPDATE aliases SET enabled=?, updated_at=? WHERE alias_email=?`, boolInt(enabled), time.Now().Unix(), normalizeEmail(aliasEmail))
	return err
}

func (a *MailcloakDB) ListApps(ctx context.Context) ([]App, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT app_id, enabled, updated_at FROM apps ORDER BY app_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []App
	for rows.Next() {
		var app App
		var enabled int
		if err := rows.Scan(&app.AppID, &enabled, &app.UpdatedAt); err != nil {
			return nil, err
		}
		app.Enabled = enabled == 1
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range apps {
		senders, err := a.ListAppSenders(ctx, apps[i].AppID)
		if err != nil {
			return nil, err
		}
		apps[i].Senders = senders
	}
	return apps, nil
}

func (a *MailcloakDB) ListAppSenders(ctx context.Context, appID string) ([]AppSender, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT app_id, from_addr, enabled, updated_at FROM app_from WHERE app_id=? ORDER BY from_addr`, normalizeID(appID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var senders []AppSender
	for rows.Next() {
		var sender AppSender
		var enabled int
		if err := rows.Scan(&sender.AppID, &sender.FromAddr, &enabled, &sender.UpdatedAt); err != nil {
			return nil, err
		}
		sender.Enabled = enabled == 1
		senders = append(senders, sender)
	}
	return senders, rows.Err()
}

func (a *MailcloakDB) UpsertAppPassword(ctx context.Context, appID, password string) error {
	secretHash, err := HashAppPassword(password)
	if err != nil {
		return err
	}
	return a.UpsertAppSecretHash(ctx, appID, secretHash)
}

func (a *MailcloakDB) UpsertAppSecretHash(ctx context.Context, appID, secretHash string) error {
	appID = normalizeID(appID)
	if appID == "" {
		return fmt.Errorf("app id is empty")
	}
	if strings.TrimSpace(secretHash) == "" {
		return fmt.Errorf("secret hash is empty")
	}
	now := time.Now().Unix()
	_, err := a.DB.ExecContext(ctx, `
INSERT INTO apps(app_id, secret_hash, enabled, updated_at) VALUES(?,?,1,?)
ON CONFLICT(app_id) DO UPDATE SET secret_hash=excluded.secret_hash, enabled=1, updated_at=excluded.updated_at
`, appID, secretHash, now)
	return err
}

func (a *MailcloakDB) DeleteApp(ctx context.Context, appID string) error {
	_, err := a.DB.ExecContext(ctx, `DELETE FROM apps WHERE app_id=?`, normalizeID(appID))
	return err
}

func (a *MailcloakDB) SetAppEnabled(ctx context.Context, appID string, enabled bool) error {
	_, err := a.DB.ExecContext(ctx, `UPDATE apps SET enabled=?, updated_at=? WHERE app_id=?`, boolInt(enabled), time.Now().Unix(), normalizeID(appID))
	return err
}

func (a *MailcloakDB) AllowAppSender(ctx context.Context, appID, fromAddr string) error {
	appID = normalizeID(appID)
	fromAddr = normalizeEmail(fromAddr)
	if appID == "" {
		return fmt.Errorf("app id is empty")
	}
	if _, ok := domainFromEmail(fromAddr); !ok {
		return fmt.Errorf("invalid sender email address")
	}
	now := time.Now().Unix()
	_, err := a.DB.ExecContext(ctx, `
INSERT INTO app_from(app_id, from_addr, enabled, updated_at) VALUES(?,?,1,?)
ON CONFLICT(app_id, from_addr) DO UPDATE SET enabled=1, updated_at=excluded.updated_at
`, appID, fromAddr, now)
	return err
}

func (a *MailcloakDB) DeleteAppSender(ctx context.Context, appID, fromAddr string) error {
	_, err := a.DB.ExecContext(ctx, `DELETE FROM app_from WHERE app_id=? AND from_addr=?`, normalizeID(appID), normalizeEmail(fromAddr))
	return err
}

func normalizeDomain(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeID(value string) string {
	return strings.TrimSpace(value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
