package mailcloak

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

type Cache struct {
	ttl time.Duration
	m   map[string]cacheItem
}

type cacheItem struct {
	val     string
	expires time.Time
	ok      bool
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, m: make(map[string]cacheItem)}
}

func (c *Cache) Get(key string) (string, bool, bool) {
	it, ok := c.m[key]
	if !ok || time.Now().After(it.expires) {
		return "", false, false
	}
	return it.val, it.ok, true
}

func (c *Cache) Put(key, val string, ok bool) {
	c.m[key] = cacheItem{val: val, ok: ok, expires: time.Now().Add(c.ttl)}
}

func OpenPolicyListener(cfg *Config) (net.Listener, error) {
	sock := cfg.Sockets.PolicySocket
	_ = os.Remove(sock)

	l, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}

	if err := ChownChmodSocket(sock, cfg); err != nil {
		_ = l.Close()
		return nil, err
	}

	return l, nil
}

func ServePolicy(ctx context.Context, cfg *Config, db *AliasDB, kc *Keycloak, cache *Cache, l net.Listener) error {
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go handlePolicyConn(conn, cfg, db, kc, cache)
	}
}

func RunPolicy(ctx context.Context, cfg *Config, db *AliasDB, kc *Keycloak, cache *Cache) error {
	l, err := OpenPolicyListener(cfg)
	if err != nil {
		return err
	}
	return ServePolicy(ctx, cfg, db, kc, cache, l)
}

func handlePolicyConn(conn net.Conn, cfg *Config, db *AliasDB, kc *Keycloak, cache *Cache) {
	defer conn.Close()
	r := bufio.NewReader(conn)

	req := map[string]string{}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if i := strings.IndexByte(line, '='); i > 0 {
			req[line[:i]] = line[i+1:]
		}
	}

	// Decide based on protocol_state
	state := req["protocol_state"] // e.g. RCPT, MAIL
	saslUser := req["sasl_username"]
	sender := strings.ToLower(req["sender"])
	rcpt := strings.ToLower(req["recipient"])

	action := "DUNNO"

	switch state {
	case "RCPT":
		action = policyRCPT(cfg, db, kc, cache, rcpt)
	case "MAIL":
		// On MAIL stage we can validate sender if authenticated (submission)
		if saslUser != "" && sender != "" {
			action = policyMAIL(cfg, db, kc, cache, saslUser, sender)
		}
	default:
		action = "DUNNO"
	}

	fmt.Fprintf(conn, "action=%s\n\n", action)
}

func policyRCPT(cfg *Config, db *AliasDB, kc *Keycloak, cache *Cache, rcpt string) string {
	if rcpt == "" {
		return "DUNNO"
	}
	// Only enforce for our domain
	if !strings.HasSuffix(rcpt, "@"+strings.ToLower(cfg.Policy.Domain)) {
		return "DUNNO"
	}

	// 1) exists in keycloak primary email?
	key := "email_exists:" + rcpt
	if _, ok, hit := cache.Get(key); hit {
		if ok {
			return "DUNNO"
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exists, err := kc.EmailExists(ctx, rcpt)
		if err != nil {
			if cfg.Policy.KeycloakFailureMode == "dunno" {
				return "DUNNO"
			}
			return "451 4.3.0 Temporary authentication/lookup failure"
		}
		cache.Put(key, "", exists)
		if exists {
			return "DUNNO"
		}
	}

	// 2) exists as sqlite alias?
	_, ok, err := db.AliasOwner(rcpt)
	if err != nil {
		log.Printf("sqlite rcpt lookup error: %v", err)
		return "451 4.3.0 Temporary internal error"
	}
	if ok {
		return "DUNNO"
	}

	return "550 5.1.1 No such user"
}

func policyMAIL(cfg *Config, db *AliasDB, kc *Keycloak, cache *Cache, saslUser, sender string) string {
	// Allow empty sender (bounce)
	if sender == "" || sender == "<>" {
		return "DUNNO"
	}
	// Only enforce our domain senders (optional)
	if !strings.HasSuffix(sender, "@"+strings.ToLower(cfg.Policy.Domain)) {
		return "DUNNO"
	}

	// primary email from keycloak (cached)
	key := "email_by_username:" + strings.ToLower(saslUser)
	email, ok, hit := cache.Get(key)
	if !hit {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e, exists, err := kc.EmailByUsername(ctx, saslUser)
		if err != nil {
			if cfg.Policy.KeycloakFailureMode == "dunno" {
				return "DUNNO"
			}
			return "451 4.3.0 Temporary authentication/lookup failure"
		}
		cache.Put(key, e, exists)
		email, ok = e, exists
	}

	// 1) sender == primary email
	if ok && strings.EqualFold(sender, email) {
		return "DUNNO"
	}

	// 2) sender is sqlite alias belonging to this user
	belongs, err := db.AliasBelongsTo(sender, saslUser)
	if err != nil {
		log.Printf("sqlite sender lookup error: %v", err)
		return "451 4.3.0 Temporary internal error"
	}
	if belongs {
		return "DUNNO"
	}

	return "553 5.7.1 Sender not owned by authenticated user"
}
