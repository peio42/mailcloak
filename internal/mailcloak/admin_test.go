package mailcloak

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitMailcloakDBCreatesSchemaAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "state.db")

	db, err := InitMailcloakDB(path)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	db, err = InitMailcloakDB(path)
	if err != nil {
		t.Fatalf("second init db: %v", err)
	}
	defer db.Close()

	domains, err := db.ListDomains(ctx)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("expected no domains, got %d", len(domains))
	}
}

func TestAdminDomainAliasAndAppFlow(t *testing.T) {
	ctx := context.Background()
	db := newAdminTestDB(t)
	defer db.Close()

	if err := db.UpsertDomain(ctx, "Example.COM"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	domains, err := db.ListDomains(ctx)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	if len(domains) != 1 || domains[0].DomainName != "example.com" || !domains[0].Enabled {
		t.Fatalf("unexpected domains: %#v", domains)
	}

	if local, err := db.DomainEnabled("example.com"); err != nil || !local {
		t.Fatalf("domain should be enabled, local=%v err=%v", local, err)
	}
	if err := db.SetDomainEnabled(ctx, "example.com", false); err != nil {
		t.Fatalf("disable domain: %v", err)
	}
	if local, err := db.DomainEnabled("example.com"); err != nil || local {
		t.Fatalf("domain should be disabled, local=%v err=%v", local, err)
	}
	if err := db.SetDomainEnabled(ctx, "example.com", true); err != nil {
		t.Fatalf("enable domain: %v", err)
	}

	if err := db.UpsertAlias(ctx, "Alias@Example.COM", "alice"); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	aliases, err := db.ListAliases(ctx, "alice")
	if err != nil {
		t.Fatalf("list aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].AliasEmail != "alias@example.com" || aliases[0].TargetUser != "alice" {
		t.Fatalf("unexpected aliases: %#v", aliases)
	}
	if owner, ok, err := db.AliasOwner("alias@example.com"); err != nil || !ok || owner != "alice" {
		t.Fatalf("alias owner mismatch: owner=%q ok=%v err=%v", owner, ok, err)
	}
	if err := db.SetAliasEnabled(ctx, "alias@example.com", false); err != nil {
		t.Fatalf("disable alias: %v", err)
	}
	if _, ok, err := db.AliasOwner("alias@example.com"); err != nil || ok {
		t.Fatalf("alias should be disabled, ok=%v err=%v", ok, err)
	}

	if err := db.UpsertAppSecretHash(ctx, "app1", "{ARGON2ID}dummy"); err != nil {
		t.Fatalf("add app: %v", err)
	}
	if err := db.AllowAppSender(ctx, "app1", "Sender@Example.COM"); err != nil {
		t.Fatalf("allow app sender: %v", err)
	}
	apps, err := db.ListApps(ctx)
	if err != nil {
		t.Fatalf("list apps: %v", err)
	}
	if len(apps) != 1 || apps[0].AppID != "app1" || len(apps[0].Senders) != 1 || apps[0].Senders[0].FromAddr != "sender@example.com" {
		t.Fatalf("unexpected apps: %#v", apps)
	}
	if allowed, err := db.AppFromAllowed("app1", "sender@example.com"); err != nil || !allowed {
		t.Fatalf("app sender should be allowed, allowed=%v err=%v", allowed, err)
	}
}

func TestUpsertAliasRequiresLocalDomain(t *testing.T) {
	db := newAdminTestDB(t)
	defer db.Close()

	err := db.UpsertAlias(context.Background(), "alias@example.com", "alice")
	if err == nil || !strings.Contains(err.Error(), "domain not found: example.com") {
		t.Fatalf("expected domain error, got %v", err)
	}
}

func TestHashAppPasswordFormat(t *testing.T) {
	hash, err := HashAppPassword("password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !strings.HasPrefix(hash, "{ARGON2ID}$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatalf("unexpected hash format: %q", hash)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Fatalf("expected 6 phc parts, got %d in %q", len(parts), hash)
	}
}

func newAdminTestDB(t *testing.T) *MailcloakDB {
	t.Helper()
	db, err := InitMailcloakDB(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	return db
}
