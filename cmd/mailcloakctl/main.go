package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"mailcloak/internal/mailcloak"

	"golang.org/x/term"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mailcloakctl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", mailcloak.DefaultDBPath, "SQLite database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		usage(stderr)
		return fmt.Errorf("missing command")
	}

	ctx := context.Background()
	if rest[0] == "init" {
		db, err := mailcloak.InitMailcloakDB(*dbPath)
		if err != nil {
			return err
		}
		return db.Close()
	}

	db, err := mailcloak.OpenExistingMailcloakDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	switch rest[0] {
	case "domains":
		return runDomains(ctx, db, rest[1:], stdout, stderr)
	case "aliases":
		return runAliases(ctx, db, rest[1:], stdout, stderr)
	case "apps":
		return runApps(ctx, db, rest[1:], stdout, stderr)
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", rest[0])
	}
}

func runDomains(ctx context.Context, db *mailcloak.MailcloakDB, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		domainsUsage(stderr)
		return fmt.Errorf("missing domains command")
	}
	switch args[0] {
	case "list":
		domains, err := db.ListDomains(ctx)
		if err != nil {
			return err
		}
		for _, domain := range domains {
			fmt.Fprintf(stdout, "%s\t%s\n", domain.DomainName, enabledLabel(domain.Enabled))
		}
		return nil
	case "add":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl domains add <domain>")
		}
		return db.UpsertDomain(ctx, args[1])
	case "del":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl domains del <domain>")
		}
		return db.DeleteDomain(ctx, args[1])
	case "enable":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl domains enable <domain>")
		}
		return db.SetDomainEnabled(ctx, args[1], true)
	case "disable":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl domains disable <domain>")
		}
		return db.SetDomainEnabled(ctx, args[1], false)
	default:
		domainsUsage(stderr)
		return fmt.Errorf("unknown domains command %q", args[0])
	}
}

func runAliases(ctx context.Context, db *mailcloak.MailcloakDB, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		aliasesUsage(stderr)
		return fmt.Errorf("missing aliases command")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("aliases list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		user := fs.String("user", "", "filter by target user")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: mailcloakctl aliases list [--user <username>]")
		}
		aliases, err := db.ListAliases(ctx, *user)
		if err != nil {
			return err
		}
		for _, alias := range aliases {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", alias.AliasEmail, alias.TargetUser, enabledLabel(alias.Enabled))
		}
		return nil
	case "add":
		if len(args) != 3 {
			return fmt.Errorf("usage: mailcloakctl aliases add <alias_email> <username>")
		}
		return db.UpsertAlias(ctx, args[1], args[2])
	case "del":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl aliases del <alias_email>")
		}
		return db.DeleteAlias(ctx, args[1])
	case "enable":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl aliases enable <alias_email>")
		}
		return db.SetAliasEnabled(ctx, args[1], true)
	case "disable":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl aliases disable <alias_email>")
		}
		return db.SetAliasEnabled(ctx, args[1], false)
	default:
		aliasesUsage(stderr)
		return fmt.Errorf("unknown aliases command %q", args[0])
	}
}

func runApps(ctx context.Context, db *mailcloak.MailcloakDB, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		appsUsage(stderr)
		return fmt.Errorf("missing apps command")
	}
	switch args[0] {
	case "list":
		apps, err := db.ListApps(ctx)
		if err != nil {
			return err
		}
		for _, app := range apps {
			fmt.Fprintf(stdout, "%s\t%s\t%d\n", app.AppID, enabledLabel(app.Enabled), app.UpdatedAt)
			if len(app.Senders) == 0 {
				fmt.Fprintln(stdout, "\t\t-")
				continue
			}
			parts := make([]string, 0, len(app.Senders))
			for _, sender := range app.Senders {
				part := sender.FromAddr
				if !sender.Enabled {
					part += " (disabled)"
				}
				parts = append(parts, part)
			}
			fmt.Fprintf(stdout, "\t\t%s\n", strings.Join(parts, ", "))
		}
		return nil
	case "add":
		appID, password, err := parseAppAddArgs(args[1:], stderr)
		if err != nil {
			return err
		}
		return db.UpsertAppPassword(ctx, appID, password)
	case "del":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl apps del <app_id>")
		}
		return db.DeleteApp(ctx, args[1])
	case "enable":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl apps enable <app_id>")
		}
		return db.SetAppEnabled(ctx, args[1], true)
	case "disable":
		if len(args) != 2 {
			return fmt.Errorf("usage: mailcloakctl apps disable <app_id>")
		}
		return db.SetAppEnabled(ctx, args[1], false)
	case "allow":
		if len(args) != 3 {
			return fmt.Errorf("usage: mailcloakctl apps allow <app_id> <from_addr>")
		}
		return db.AllowAppSender(ctx, args[1], args[2])
	case "disallow":
		if len(args) != 3 {
			return fmt.Errorf("usage: mailcloakctl apps disallow <app_id> <from_addr>")
		}
		return db.DeleteAppSender(ctx, args[1], args[2])
	default:
		appsUsage(stderr)
		return fmt.Errorf("unknown apps command %q", args[0])
	}
}

func parseAppAddArgs(args []string, stderr io.Writer) (string, string, error) {
	passwordStdin := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--password-stdin" {
			passwordStdin = true
			continue
		}
		filtered = append(filtered, arg)
	}

	if passwordStdin {
		if len(filtered) != 1 {
			return "", "", fmt.Errorf("usage: mailcloakctl apps add <app_id> --password-stdin")
		}
		password, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", err
		}
		value := trimOneTrailingNewline(string(password))
		if value == "" {
			return "", "", fmt.Errorf("missing password on stdin")
		}
		return filtered[0], value, nil
	}
	if len(filtered) != 2 {
		if len(filtered) == 1 && term.IsTerminal(int(os.Stdin.Fd())) {
			password, err := promptForPassword(stderr)
			if err != nil {
				return "", "", err
			}
			return filtered[0], password, nil
		}
		return "", "", fmt.Errorf("missing password: pass it as an argument, use --password-stdin, or run interactively")
	}
	return filtered[0], filtered[1], nil
}

func promptForPassword(stderr io.Writer) (string, error) {
	fmt.Fprint(stderr, "App password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(stderr, "Confirm password: ")
	confirm, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read password confirmation: %w", err)
	}
	if string(password) != string(confirm) {
		return "", fmt.Errorf("passwords do not match")
	}
	if strings.TrimSpace(string(password)) == "" {
		return "", fmt.Errorf("empty password not allowed")
	}
	return string(password), nil
}

func trimOneTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\r\n") {
		return value[:len(value)-2]
	}
	if strings.HasSuffix(value, "\n") || strings.HasSuffix(value, "\r") {
		return value[:len(value)-1]
	}
	return value
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: mailcloakctl [--db <path>] <init|domains|aliases|apps> ...")
}

func domainsUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: mailcloakctl domains <list|add|del|enable|disable> ...")
}

func aliasesUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: mailcloakctl aliases <list|add|del|enable|disable> ...")
}

func appsUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: mailcloakctl apps <list|add|del|enable|disable|allow|disallow> ...")
}
