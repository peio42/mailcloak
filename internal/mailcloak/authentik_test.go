package mailcloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNewAuthentikMissingToken(t *testing.T) {
	_, err := NewAuthentik(AuthentikConfig{
		BaseURL:         "http://authentik.local",
		APIToken:        " ",
		CacheTTLSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestAuthentikEmailExistsAPINon2xx(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}
	idp, srv := newTestAuthentik(t, handler)
	defer srv.Close()

	_, err := idp.EmailExists(context.Background(), "alice@example.com")
	if err == nil {
		t.Fatal("expected non-2xx error")
	}
	if !strings.Contains(err.Error(), "http 502") {
		t.Fatalf("expected 502 error, got %v", err)
	}
}

func TestAuthentikResolveUserEmailBadJSON(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{bad-json"))
	}
	idp, srv := newTestAuthentik(t, handler)
	defer srv.Close()

	_, _, err := idp.ResolveUserEmail(context.Background(), "alice")
	if err == nil {
		t.Fatal("expected decode error")
	}
}
