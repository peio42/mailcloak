package mailcloak

import (
	"testing"

	"mailcloak/internal/mailcloak/testutil"
)

func TestAliasOwnerAndBelongsTo(t *testing.T) {
	sqlDB := testutil.NewSQLiteDB(t)
	defer sqlDB.Close()

	db := &MailcloakDB{DB: sqlDB}
	testutil.InsertAlias(t, sqlDB, "enabled@example.com", "alice", true)
	testutil.InsertAlias(t, sqlDB, "disabled@example.com", "alice", false)

	owner, ok, err := db.AliasOwner("enabled@example.com")
	if err != nil {
		t.Fatalf("AliasOwner error: %v", err)
	}
	if !ok || owner != "alice" {
		t.Fatalf("expected owner alice, ok true, got owner=%q ok=%v", owner, ok)
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
