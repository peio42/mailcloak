package mailcloak

import "testing"

func TestChownChmodSocket(t *testing.T) {
	t.Run("noop when no ownership and mode are configured", func(t *testing.T) {
		cfg := &Config{}
		if err := ChownChmodSocket(t.TempDir(), cfg); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("invalid socket mode", func(t *testing.T) {
		cfg := &Config{}
		cfg.Sockets.SocketMode = "invalid"
		err := ChownChmodSocket(t.TempDir(), cfg)
		if err == nil {
			t.Fatal("expected invalid mode error")
		}
	})
}
