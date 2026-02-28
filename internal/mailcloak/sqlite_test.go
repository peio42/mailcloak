package mailcloak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mailcloak/internal/mailcloak/testutil"
)

func TestAliasOwnerAndBelongsTo(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertDomain(t, sqlDB, "example.com", true)
	testutil.InsertDomain(t, sqlDB, "other.com", true)
	testutil.InsertAlias(t, sqlDB, "enabled@example.com", "alice", true)
	testutil.InsertAlias(t, sqlDB, "disabled@example.com", "alice", false)
	testutil.InsertAlias(t, sqlDB, "enabled@other.com", "alice", true)

	owner, ok, err := db.AliasOwner("enabled@example.com")
	if err != nil {
		t.Fatalf("AliasOwner error: %v", err)
	}
	if !ok || owner != "alice" {
		t.Fatalf("expected owner alice, ok true, got owner=%q ok=%v", owner, ok)
	}

	owner, ok, err = db.AliasOwner("enabled@other.com")
	if err != nil {
		t.Fatalf("AliasOwner error: %v", err)
	}
	if !ok || owner != "alice" {
		t.Fatalf("expected owner alice for other domain, ok true, got owner=%q ok=%v", owner, ok)
	}

	_, ok, err = db.AliasOwner("disabled@example.com")
	if err != nil {
		t.Fatalf("AliasOwner error: %v", err)
	}
	if ok {
		t.Fatalf("expected disabled alias to be not ok")
	}

	_, ok, err = db.AliasOwner("missing@example.com")
	if err != nil {
		t.Fatalf("AliasOwner error: %v", err)
	}
	if ok {
		t.Fatalf("expected missing alias to be not ok")
	}

	belongs, err := db.AliasBelongsTo("enabled@example.com", "alice")
	if err != nil {
		t.Fatalf("AliasBelongsTo error: %v", err)
	}
	if !belongs {
		t.Fatalf("expected alias to belong to alice")
	}

	belongs, err = db.AliasBelongsTo("disabled@example.com", "alice")
	if err != nil {
		t.Fatalf("AliasBelongsTo error: %v", err)
	}
	if belongs {
		t.Fatalf("expected disabled alias to not belong")
	}

	belongs, err = db.AliasBelongsTo("missing@example.com", "alice")
	if err != nil {
		t.Fatalf("AliasBelongsTo error: %v", err)
	}
	if belongs {
		t.Fatalf("expected missing alias to not belong")
	}
}

func TestAppFromAllowed(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertApp(t, sqlDB, "app1", true)
	testutil.InsertAppFrom(t, sqlDB, "app1", "from@example.com", true)
	testutil.InsertApp(t, sqlDB, "app2", false)
	testutil.InsertAppFrom(t, sqlDB, "app2", "from@example.com", true)
	testutil.InsertApp(t, sqlDB, "app3", true)
	testutil.InsertAppFrom(t, sqlDB, "app3", "from@example.com", false)

	allowed, err := db.AppFromAllowed("app1", "from@example.com")
	if err != nil {
		t.Fatalf("AppFromAllowed error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected app1 to be allowed")
	}

	allowed, err = db.AppFromAllowed("app2", "from@example.com")
	if err != nil {
		t.Fatalf("AppFromAllowed error: %v", err)
	}
	if allowed {
		t.Fatalf("expected disabled app to not be allowed")
	}

	allowed, err = db.AppFromAllowed("app3", "from@example.com")
	if err != nil {
		t.Fatalf("AppFromAllowed error: %v", err)
	}
	if allowed {
		t.Fatalf("expected disabled from to not be allowed")
	}

	allowed, err = db.AppFromAllowed("missing", "from@example.com")
	if err != nil {
		t.Fatalf("AppFromAllowed error: %v", err)
	}
	if allowed {
		t.Fatalf("expected missing app to not be allowed")
	}
}

func TestDomainEnabled(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertDomain(t, sqlDB, "example.com", true)
	testutil.InsertDomain(t, sqlDB, "disabled.com", false)

	local, err := db.DomainEnabled("example.com")
	if err != nil {
		t.Fatalf("DomainEnabled error: %v", err)
	}
	if !local {
		t.Fatalf("expected example.com to be enabled")
	}

	local, err = db.DomainEnabled("disabled.com")
	if err != nil {
		t.Fatalf("DomainEnabled error: %v", err)
	}
	if local {
		t.Fatalf("expected disabled.com to be disabled")
	}

	local, err = db.DomainEnabled("missing.com")
	if err != nil {
		t.Fatalf("DomainEnabled error: %v", err)
	}
	if local {
		t.Fatalf("expected missing.com to be false")
	}
}

func TestOpenMailcloakDBAndClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mailcloak.db")
	if err := os.WriteFile(dbPath, []byte{}, 0o600); err != nil {
		t.Fatalf("write db file: %v", err)
	}

	db, err := OpenMailcloakDB(dbPath)
	if err != nil {
		t.Fatalf("OpenMailcloakDB error: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

func TestEnsureDBExists(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "mailcloak.db")
	if err := os.WriteFile(existing, []byte{}, 0o600); err != nil {
		t.Fatalf("write db file: %v", err)
	}

	cases := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "memory path", path: ":memory:"},
		{name: "sqlite uri", path: "file::memory:?cache=shared"},
		{name: "empty path", path: "", wantErr: "sqlite path is empty"},
		{name: "missing directory", path: filepath.Join(dir, "missing", "mailcloak.db"), wantErr: "sqlite db directory not found"},
		{name: "missing file in existing directory", path: filepath.Join(dir, "missing.db"), wantErr: "sqlite db not found"},
		{name: "existing file", path: existing},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ensureDBExists(tc.path)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
