package mailcloak

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Keycloak struct {
	cfg *Config
	hc  *http.Client
}

func NewKeycloak(cfg *Config) *Keycloak {
	return &Keycloak{
		cfg: cfg,
		hc:  &http.Client{Timeout: 5 * time.Second},
	}
}

type tokenResp struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (k *Keycloak) token(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", k.cfg.Keycloak.ClientID)
	form.Set("client_secret", k.cfg.Keycloak.ClientSecret)

	u := strings.TrimRight(k.cfg.Keycloak.BaseURL, "/") +
		"/realms/" + url.PathEscape(k.cfg.Keycloak.Realm) +
		"/protocol/openid-connect/token"

	req, _ := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := k.hc.Do(req)
	if err != nil {
		log.Printf("keycloak token request error: %v", err)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		log.Printf("keycloak token non-2xx: %d", resp.StatusCode)
		return "", fmt.Errorf("token http %d: %s", resp.StatusCode, string(b))
	}
	var tr tokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		log.Printf("keycloak token decode error: %v", err)
		return "", err
	}
	if tr.AccessToken == "" {
		log.Printf("keycloak token response missing access_token")
		return "", fmt.Errorf("empty access_token")
	}
	return tr.AccessToken, nil
}

type kcUser struct {
	ID       string              `json:"id"`
	Username string              `json:"username"`
	Email    string              `json:"email"`
	Enabled  bool                `json:"enabled"`
	Attrs    map[string][]string `json:"attributes"`
}

func (k *Keycloak) adminGet(ctx context.Context, bearer, path string, q url.Values) ([]kcUser, error) {
	base := strings.TrimRight(k.cfg.Keycloak.BaseURL, "/") +
		"/admin/realms/" + url.PathEscape(k.cfg.Keycloak.Realm) + path

	u := base
	if q != nil {
		u += "?" + q.Encode()
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := k.hc.Do(req)
	if err != nil {
		log.Printf("keycloak admin request error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		log.Printf("keycloak admin non-2xx: %d", resp.StatusCode)
		return nil, fmt.Errorf("admin http %d: %s", resp.StatusCode, string(b))
	}
	var users []kcUser
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		log.Printf("keycloak admin decode error: %v", err)
		return nil, err
	}
	return users, nil
}

// Find primary email by username (exact if supported)
func (k *Keycloak) EmailByUsername(ctx context.Context, username string) (string, bool, error) {
	bearer, err := k.token(ctx)
	if err != nil {
		return "", false, err
	}

	q := url.Values{}
	q.Set("username", username)
	q.Set("exact", "true")
	users, err := k.adminGet(ctx, bearer, "/users", q)
	if err != nil {
		log.Printf("keycloak admin exact username lookup failed for %s: %v", username, err)
		// fallback: search
		q2 := url.Values{}
		q2.Set("search", username)
		users, err = k.adminGet(ctx, bearer, "/users", q2)
		if err != nil {
			log.Printf("keycloak admin search username lookup failed for %s: %v", username, err)
			return "", false, err
		}
	}

	for _, u := range users {
		if strings.EqualFold(u.Username, username) && u.Enabled && u.Email != "" {
			return strings.ToLower(u.Email), true, nil
		}
	}
	return "", false, nil
}

// Check if an email exists as primary user email
func (k *Keycloak) EmailExists(ctx context.Context, email string) (bool, error) {
	bearer, err := k.token(ctx)
	if err != nil {
		return false, err
	}
	q := url.Values{}
	q.Set("email", email)
	q.Set("exact", "true")
	users, err := k.adminGet(ctx, bearer, "/users", q)
	if err != nil {
		log.Printf("keycloak admin exact email lookup failed for %s: %v", email, err)
		// fallback: search
		q2 := url.Values{}
		q2.Set("search", email)
		users, err = k.adminGet(ctx, bearer, "/users", q2)
		if err != nil {
			log.Printf("keycloak admin search email lookup failed for %s: %v", email, err)
			return false, err
		}
	}
	for _, u := range users {
		if u.Enabled && strings.EqualFold(u.Email, email) {
			return true, nil
		}
	}
	return false, nil
}
