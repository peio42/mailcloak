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
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("policy accept error: %v", err)
				return err
			}
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
	saslUser := req["sasl_username"]
	sender := strings.ToLower(req["sender"])
	rcpt := strings.ToLower(req["recipient"])

	action := "DUNNO"

	switch state {
	case "RCPT":
		action = policyRCPT(cfg, db, idp, rcpt)

		// With "smtpd_delay_reject = yes" in Postfix, MAIL stage is bypassed
		// So we move all checks to RCPT stage
		if action == "DUNNO" {
			action = policyMAIL(cfg, db, idp, saslUser, sender)
		}

	// case "MAIL":
	//   // On MAIL stage we can validate sender if authenticated (submission)
	//	   if saslUser != "" && sender != "" {
	//	    action = policyMAIL(cfg, db, idp, saslUser, sender)
	//    }

	default:
		action = "DUNNO"
	}

	log.Printf("policy decision: state=%s action=%s sasl=%s sender=%s rcpt=%s", state, action, saslUser, sender, rcpt)

	fmt.Fprintf(conn, "action=%s\n\n", action)
}

func policyRCPT(cfg *Config, db *MailcloakDB, idp IdentityResolver, rcpt string) string {
	if rcpt == "" {
		return "DUNNO"
	}
	// Only enforce for our local domains
	domain, ok := domainFromEmail(rcpt)
	if !ok {
		return "DUNNO"
	}
	local, err := db.DomainEnabled(domain)
	if err != nil {
		log.Printf("sqlite domain lookup error: %v", err)
		return "451 4.3.0 Temporary internal error"
	}
	if !local {
		return "DUNNO"
	}

	// 1) exists in keycloak primary email?
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exists, err := idp.EmailExists(ctx, rcpt)
	if err != nil {
		log.Printf("keycloak email exists lookup error for %s: %v", rcpt, err)
		if cfg.Policy.KeycloakFailureMode == "dunno" {
			return "DUNNO"
		}
		return "451 4.3.0 Temporary authentication/lookup failure"
	}
	if exists {
		return "DUNNO"
	}

	// 2) exists as sqlite alias?
	_, aliasOk, err := db.AliasOwner(rcpt)
	if err != nil {
		log.Printf("sqlite rcpt lookup error: %v", err)
		return "451 4.3.0 Temporary internal error"
	}
	if aliasOk {
		return "DUNNO"
	}

	return "550 5.1.1 No such user"
}

func policyMAIL(cfg *Config, db *MailcloakDB, idp IdentityResolver, saslUser, sender string) string {
	// Allow empty sender (bounce)
	if sender == "" || sender == "<>" {
		return "DUNNO"
	}
	// Only enforce our domain senders (optional)
	domain, ok := domainFromEmail(sender)
	if !ok {
		return "DUNNO"
	}
	local, err := db.DomainEnabled(domain)
	if err != nil {
		log.Printf("sqlite domain lookup error: %v", err)
		return "451 4.3.0 Temporary internal error"
	}
	if !local {
		return "DUNNO"
	}

	// primary email from keycloak
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	email, ok, err := idp.ResolveUserEmail(ctx, saslUser)
	if err != nil {
		log.Printf("keycloak email-by-user lookup error for %s: %v", saslUser, err)
		if cfg.Policy.KeycloakFailureMode == "dunno" {
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
		log.Printf("sqlite sender lookup error: %v", err)
		return "451 4.3.0 Temporary internal error"
	}
	if belongs {
		return "DUNNO"
	}

	// 3) sender is allowed for app (saslUser = app_id)
	if saslUser != "" {
		allowed, err := db.AppFromAllowed(saslUser, sender)
		if err != nil {
			log.Printf("sqlite app sender lookup error: %v", err)
			return "451 4.3.0 Temporary internal error"
		}
		if allowed {
			return "DUNNO"
		}
	}

	return "553 5.7.1 Sender not owned by authenticated user"
}
