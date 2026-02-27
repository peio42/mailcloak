package mailcloak

import "testing"

func TestDropPrivileges(t *testing.T) {
	t.Run("no configured user", func(t *testing.T) {
		cfg := &Config{}
		if err := DropPrivileges(cfg); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("unknown configured user", func(t *testing.T) {
		cfg := &Config{}
		cfg.Daemon.User = "mailcloak-user-definitely-does-not-exist"
		err := DropPrivileges(cfg)
		if err == nil {
			t.Fatal("expected lookup error")
		}
	})
}
