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
	cfg   KeycloakConfig
	hc    *http.Client
	cache *Cache
}

func NewKeycloak(cfg *Config) *Keycloak {
	ttl := time.Duration(cfg.IDP.Keycloak.CacheTTLSeconds) * time.Second
	return &Keycloak{
		cfg:   cfg.IDP.Keycloak,
		hc:    &http.Client{Timeout: 5 * time.Second},
		cache: NewCache(ttl),
	}
}

type tokenResp struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (k *Keycloak) token(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", k.cfg.ClientID)
	form.Set("client_secret", k.cfg.ClientSecret)

	u := strings.TrimRight(k.cfg.BaseURL, "/") +
		"/realms/" + url.PathEscape(k.cfg.Realm) +
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
	base := strings.TrimRight(k.cfg.BaseURL, "/") +
		"/admin/realms/" + url.PathEscape(k.cfg.Realm) + path

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

// Find primary email of user (username/uuid)
func (k *Keycloak) ResolveUserEmail(ctx context.Context, user string) (string, bool, error) {
	key := "email_by_user:" + strings.ToLower(user)
	if email, ok, hit := k.cache.Get(key); hit {
		return email, ok, nil
	}

	bearer, err := k.token(ctx)
	if err != nil {
		return "", false, err
	}

	// Exact username match - Expect future option using uuid
	q := url.Values{}
	q.Set("username", user)
	q.Set("exact", "true")
	users, err := k.adminGet(ctx, bearer, "/users", q)
	if err != nil {
		log.Printf("keycloak admin exact username lookup failed for %s: %v", user, err)
		// fallback: search
		q2 := url.Values{}
		q2.Set("search", user)
		users, err = k.adminGet(ctx, bearer, "/users", q2)
		if err != nil {
			log.Printf("keycloak admin search username lookup failed for %s: %v", user, err)
			return "", false, err
		}
	}

	for _, u := range users {
		if strings.EqualFold(u.Username, user) && u.Enabled && u.Email != "" {
			email := strings.ToLower(u.Email)
			k.cache.Put(key, email, true)
			return email, true, nil
		}
	}
	k.cache.Put(key, "", false)
	return "", false, nil
}

// Check if an email exists as primary user email
func (k *Keycloak) EmailExists(ctx context.Context, email string) (bool, error) {
	key := "email_exists:" + strings.ToLower(email)
	if _, ok, hit := k.cache.Get(key); hit {
		return ok, nil
	}

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
			k.cache.Put(key, "", true)
			return true, nil
		}
	}
	k.cache.Put(key, "", false)
	return false, nil
}
