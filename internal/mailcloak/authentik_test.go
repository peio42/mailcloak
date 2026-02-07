package mailcloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestAuthentik(t *testing.T, handler http.HandlerFunc) (*Authentik, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	cfg := AuthentikConfig{
		BaseURL:         srv.URL,
		APIToken:        "token",
		CacheTTLSeconds: 1,
	}
	return NewAuthentikWithTokenProvider(cfg, &staticTokenProvider{token: "token"}), srv
}

func TestAuthentikResolveUserEmail(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/core/users/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		if q.Get("username") != "bob" || q.Get("is_active") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"username":  "bob",
				"email":     "Bob@Example.com",
				"is_active": true,
			}},
		})
	}

	idp, srv := newTestAuthentik(t, handler)
	defer srv.Close()

	email, ok, err := idp.ResolveUserEmail(context.Background(), "bob")
	if err != nil {
		t.Fatalf("ResolveUserEmail error: %v", err)
	}
	if !ok || email != "bob@example.com" {
		t.Fatalf("expected bob@example.com ok true, got email=%q ok=%v", email, ok)
	}
}

func TestAuthentikEmailExists(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/core/users/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		if q.Get("email") != "alice@example.com" || q.Get("is_active") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"username":  "alice",
				"email":     "alice@example.com",
				"is_active": true,
			}},
		})
	}

	idp, srv := newTestAuthentik(t, handler)
	defer srv.Close()

	exists, err := idp.EmailExists(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("EmailExists error: %v", err)
	}
	if !exists {
		t.Fatalf("expected email to exist")
	}
}
