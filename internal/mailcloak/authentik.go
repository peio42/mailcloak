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

type AuthentikTokenProvider interface {
	Token(ctx context.Context) (string, error)
}

type staticTokenProvider struct {
	token string
}

func (p *staticTokenProvider) Token(ctx context.Context) (string, error) {
	if strings.TrimSpace(p.token) == "" {
		return "", fmt.Errorf("missing authentik api token")
	}
	return p.token, nil
}

type Authentik struct {
	cfg           AuthentikConfig
	hc            *http.Client
	cache         *Cache
	tokenProvider AuthentikTokenProvider
}

func NewAuthentik(cfg AuthentikConfig) (*Authentik, error) {
	tokenProvider, err := newAuthentikTokenProvider(cfg)
	if err != nil {
		return nil, err
	}
	return NewAuthentikWithTokenProvider(cfg, tokenProvider), nil
}

func NewAuthentikWithTokenProvider(cfg AuthentikConfig, tokenProvider AuthentikTokenProvider) *Authentik {
	ttl := time.Duration(cfg.CacheTTLSeconds) * time.Second
	return &Authentik{
		cfg:           cfg,
		hc:            &http.Client{Timeout: 5 * time.Second},
		cache:         NewCache(ttl),
		tokenProvider: tokenProvider,
	}
}

func newAuthentikTokenProvider(cfg AuthentikConfig) (AuthentikTokenProvider, error) {
	if strings.TrimSpace(cfg.APIToken) == "" {
		return nil, fmt.Errorf("missing authentik api token")
	}
	return &staticTokenProvider{token: cfg.APIToken}, nil
}

type authentikUser struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	IsActive bool   `json:"is_active"`
}

type authentikUsersResponse struct {
	Results []authentikUser `json:"results"`
}

func (a *Authentik) users(ctx context.Context, q url.Values) ([]authentikUser, error) {
	token, err := a.tokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(a.cfg.BaseURL, "/") + "/api/v3/core/users/"
	u := base
	if q != nil {
		u += "?" + q.Encode()
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := a.hc.Do(req)
	if err != nil {
		log.Printf("authentik api request error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		log.Printf("authentik api non-2xx: %d", resp.StatusCode)
		return nil, fmt.Errorf("authentik api http %d: %s", resp.StatusCode, string(b))
	}

	var respData authentikUsersResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		log.Printf("authentik api decode error: %v", err)
		return nil, err
	}
	return respData.Results, nil
}

func (a *Authentik) ResolveUserEmail(ctx context.Context, user string) (string, bool, error) {
	key := "email_by_user:" + strings.ToLower(user)
	if email, ok, hit := a.cache.Get(key); hit {
		return email, ok, nil
	}

	q := url.Values{}
	q.Set("username", user)
	q.Set("is_active", "true")
	users, err := a.users(ctx, q)
	if err != nil {
		return "", false, err
	}

	for _, u := range users {
		if strings.EqualFold(u.Username, user) && u.IsActive && u.Email != "" {
			email := strings.ToLower(u.Email)
			a.cache.Put(key, email, true)
			return email, true, nil
		}
	}
	a.cache.Put(key, "", false)
	return "", false, nil
}

func (a *Authentik) EmailExists(ctx context.Context, email string) (bool, error) {
	key := "email_exists:" + strings.ToLower(email)
	if _, ok, hit := a.cache.Get(key); hit {
		return ok, nil
	}

	q := url.Values{}
	q.Set("email", email)
	q.Set("is_active", "true")
	users, err := a.users(ctx, q)
	if err != nil {
		return false, err
	}

	for _, u := range users {
		if u.IsActive && strings.EqualFold(u.Email, email) {
			a.cache.Put(key, "", true)
			return true, nil
		}
	}
	a.cache.Put(key, "", false)
	return false, nil
}
