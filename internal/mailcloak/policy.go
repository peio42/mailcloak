package mailcloak

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

type IdentityResolver interface {
	ResolveUserEmail(ctx context.Context, user string) (string, bool, error)
	EmailExists(ctx context.Context, email string) (bool, error)
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
	log.Printf("policy listener ready on %s", sock)

	return l, nil
}

func ServePolicy(ctx context.Context, cfg *Config, db *MailcloakDB, idp IdentityResolver, l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("policy accept error: %v", err)
			return err
		}
		go handlePolicyConn(conn, cfg, db, idp)
	}
}

func RunPolicy(ctx context.Context, cfg *Config, db *MailcloakDB, idp IdentityResolver) error {
	l, err := OpenPolicyListener(cfg)
	if err != nil {
		return err
	}
	return ServePolicy(ctx, cfg, db, idp, l)
}

func handlePolicyConn(conn net.Conn, cfg *Config, db *MailcloakDB, idp IdentityResolver) {
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

	log.Printf("policy request: state=%s sasl=%s sender=%s rcpt=%s client=%s helo=%s", req["protocol_state"], req["sasl_username"], req["sender"], req["recipient"], req["client_address"], req["helo_name"])

	// Decide based on protocol_state
	state := req["protocol_state"] // e.g. RCPT, MAIL
	saslMethod := strings.ToLower(req["sasl_method"])
	saslUser := req["sasl_username"]
	sender := strings.ToLower(req["sender"])
	rcpt := strings.ToLower(req["recipient"])

	action := "DUNNO"

	switch state {
	case "RCPT":
		action = policy(cfg, db, idp, sender, rcpt, saslMethod, saslUser)

	// With "smtpd_delay_reject = yes" in Postfix, MAIL stage is bypassed
	// So we move all checks to RCPT stage

	default:
		action = "DUNNO"
	}

	log.Printf("policy decision: state=%s action=%s sasl=%s sender=%s rcpt=%s", state, action, saslUser, sender, rcpt)

	fmt.Fprintf(conn, "action=%s\n\n", action)
}

func policy(cfg *Config, db *MailcloakDB, idp IdentityResolver, sender, rcpt, saslMethod, saslUser string) string {
	if rcpt == "" {
		return "DUNNO"
	}

	// Check recipient against local domains
	rcptLocal, err := db.DomainFromEmailIsLocal(rcpt)
	if err != nil {
		log.Printf("sqlite domain lookup error: %v", err)
		return "451 4.3.0 Temporary internal error"
	}
	if rcptLocal {
		// Check recipient exists
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exists, err := idp.EmailExists(ctx, rcpt)
		if err != nil {
			log.Printf("idp email exists lookup error for %s: %v", rcpt, err)
			if cfg.Policy.IDPFailureMode == "dunno" {
				return "DUNNO"
			}
			return "451 4.3.0 Temporary authentication/lookup failure"
		}
		if !exists {
			_, exists, err := db.AliasOwner(rcpt)
			if err != nil {
				log.Printf("sqlite alias sender lookup error: %v", err)
				return "451 4.3.0 Temporary internal error"
			}
			if !exists {
				return "550 5.1.1 No such user"
			}
		}
	}

	if saslMethod == "" {
		// No authentication:
		// - Block recipient to non-local domains
		// - Block sending from local domains

		if !rcptLocal {
			return "550 5.7.1 Recipient domain not local"
		}

		senderLocal, err := db.DomainFromEmailIsLocal(sender)
		if err != nil {
			log.Printf("sqlite domain lookup error: %v", err)
			return "451 4.3.0 Temporary internal error"
		}

		if senderLocal {
			return "553 5.7.1 Sending from local domains requires authentication"
		}

		return "DUNNO"
	}

	if saslMethod == "xoauth2" || saslMethod == "oauthbearer" {
		// User authenticated via OIDC/OAuth2
		// - Allow all recipients
		// - Allow sending from user primary email or aliases only

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		email, ok, err := idp.ResolveUserEmail(ctx, saslUser)
		if err != nil {
			log.Printf("idp email-by-user lookup error for %s: %v", saslUser, err)
			if cfg.Policy.IDPFailureMode == "dunno" {
				return "DUNNO"
			}
			return "451 4.3.0 Temporary authentication/lookup failure"
		}

		// 1) sender == primary email
		if ok && strings.EqualFold(sender, email) {
			return "DUNNO"
		}

		// 2) sender is sqlite alias belonging to this user
		belongs, err := db.AliasBelongsTo(sender, saslUser)
		if err != nil {
			log.Printf("sqlite alias sender lookup error: %v", err)
			return "451 4.3.0 Temporary internal error"
		}
		if belongs {
			return "DUNNO"
		}

		return "553 5.7.1 Sender not owned by authenticated user"
	}

	if saslMethod == "plain" || saslMethod == "login" {
		// App authenticatied via username/password
		// - Allow all recipients
		// - Allow sending from email associated with app only

		allowed, err := db.AppFromAllowed(saslUser, sender)
		if err != nil {
			log.Printf("sqlite app sender lookup error: %v", err)
			return "451 4.3.0 Temporary internal error"
		}
		if allowed {
			return "DUNNO"
		}

		return "553 5.7.1 Sender not owned by authenticated user"
	}

	return "553 5.7.1 Unsupported authentication method"
}
