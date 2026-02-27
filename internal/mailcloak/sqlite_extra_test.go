package mailcloak

import (
	"path/filepath"
	"testing"

	"mailcloak/internal/mailcloak/testutil"
)

func TestEnsureDBExists(t *testing.T) {
	t.Run("memory and file URI are accepted", func(t *testing.T) {
		if err := ensureDBExists(":memory:"); err != nil {
			t.Fatalf("unexpected error for in-memory db: %v", err)
		}
		if err := ensureDBExists("file::memory:?cache=shared"); err != nil {
			t.Fatalf("unexpected error for sqlite file URI: %v", err)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		if err := ensureDBExists(""); err == nil {
			t.Fatal("expected error for empty sqlite path")
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		missingPath := filepath.Join(t.TempDir(), "missing-dir", "mailcloak.db")
		if err := ensureDBExists(missingPath); err == nil {
			t.Fatal("expected error for missing sqlite directory")
		}
	})
}

func TestDomainFromEmailIsLocal(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertDomain(t, sqlDB, "example.com", true)
	testutil.InsertDomain(t, sqlDB, "disabled.com", false)

	local, err := db.DomainFromEmailIsLocal("alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !local {
		t.Fatal("expected enabled local domain to be true")
	}

	local, err = db.DomainFromEmailIsLocal("alice@disabled.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if local {
		t.Fatal("expected disabled domain to be false")
	}

	local, err = db.DomainFromEmailIsLocal("not-an-email")
	if err != nil {
		t.Fatalf("unexpected error for invalid email: %v", err)
	}
	if local {
		t.Fatal("expected invalid email to return false")
	}
}

func TestOpenMailcloakDBMemory(t *testing.T) {
	db, err := OpenMailcloakDB(":memory:")
	if err != nil {
		t.Fatalf("OpenMailcloakDB(:memory:) error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
}
