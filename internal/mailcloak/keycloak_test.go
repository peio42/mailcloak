package mailcloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newTestKeycloak(t *testing.T, handler http.HandlerFunc) (*Keycloak, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	cfg := &Config{}
	cfg.Keycloak.BaseURL = srv.URL
	cfg.Keycloak.Realm = "realm"
	cfg.Keycloak.ClientID = "client"
	cfg.Keycloak.ClientSecret = "secret"
	return NewKeycloak(cfg), srv
}

func TestKeycloakResolveUserEmailExact(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/realm/protocol/openid-connect/token":
			_ = r.ParseForm()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "token",
				"expires_in":   300,
			})
		case "/admin/realms/realm/users":
			q := r.URL.Query()
			if q.Get("username") != "bob" || q.Get("exact") != "true" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"username": "bob",
				"email":    "Bob@Example.com",
				"enabled":  true,
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}

	kc, srv := newTestKeycloak(t, handler)
	defer srv.Close()

	email, ok, err := kc.ResolveUserEmail(context.Background(), "bob")
	if err != nil {
		t.Fatalf("ResolveUserEmail error: %v", err)
	}
	if !ok || email != "bob@example.com" {
		t.Fatalf("expected bob@example.com ok true, got email=%q ok=%v", email, ok)
	}
}

func TestKeycloakResolveUserEmailFallbackSearch(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/realm/protocol/openid-connect/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "token",
				"expires_in":   300,
			})
		case "/admin/realms/realm/users":
			q := r.URL.Query()
			if q.Get("username") == "bob" && q.Get("exact") == "true" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if q.Get("search") != "bob" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"username": "bob",
				"email":    "bob@example.com",
				"enabled":  true,
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}

	kc, srv := newTestKeycloak(t, handler)
	defer srv.Close()

	email, ok, err := kc.ResolveUserEmail(context.Background(), "bob")
	if err != nil {
		t.Fatalf("ResolveUserEmail error: %v", err)
	}
	if !ok || email != "bob@example.com" {
		t.Fatalf("expected bob@example.com ok true, got email=%q ok=%v", email, ok)
	}
}

func TestKeycloakEmailExists(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/realm/protocol/openid-connect/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "token",
				"expires_in":   300,
			})
		case "/admin/realms/realm/users":
			q := r.URL.Query()
			if q.Get("email") != "alice@example.com" || q.Get("exact") != "true" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"username": "alice",
				"email":    "alice@example.com",
				"enabled":  true,
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}

	kc, srv := newTestKeycloak(t, handler)
	defer srv.Close()

	exists, err := kc.EmailExists(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("EmailExists error: %v", err)
	}
	if !exists {
		t.Fatalf("expected email to exist")
	}
}

func TestKeycloakTokenMissingAccessToken(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/realm/protocol/openid-connect/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"expires_in": 300,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}

	kc, srv := newTestKeycloak(t, handler)
	defer srv.Close()

	_, err := kc.EmailExists(context.Background(), "alice@example.com")
	if err == nil {
		t.Fatalf("expected error for missing access_token")
	}
}

func TestKeycloakAdminError(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/realm/protocol/openid-connect/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "token",
				"expires_in":   300,
			})
		case "/admin/realms/realm/users":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}

	kc, srv := newTestKeycloak(t, handler)
	defer srv.Close()

	exists, err := kc.EmailExists(context.Background(), "alice@example.com")
	if err == nil {
		t.Fatalf("expected error, got exists=%v", exists)
	}
}

func TestKeycloakTokenFormValues(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/realms/realm/protocol/openid-connect/token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "client" || r.Form.Get("client_secret") != "secret" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token",
			"expires_in":   300,
		})
	}

	kc, srv := newTestKeycloak(t, handler)
	defer srv.Close()

	_, err := kc.token(context.Background())
	if err != nil {
		t.Fatalf("token error: %v", err)
	}
}

func TestKeycloakAdminQueryEncoding(t *testing.T) {
	var gotQuery url.Values
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/realm/protocol/openid-connect/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "token",
				"expires_in":   300,
			})
		case "/admin/realms/realm/users":
			gotQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}

	kc, srv := newTestKeycloak(t, handler)
	defer srv.Close()

	_, _, err := kc.ResolveUserEmail(context.Background(), "bob")
	if err != nil {
		t.Fatalf("ResolveUserEmail error: %v", err)
	}
	if gotQuery.Get("username") != "bob" || gotQuery.Get("exact") != "true" {
		t.Fatalf("unexpected query: %v", gotQuery)
	}
}
