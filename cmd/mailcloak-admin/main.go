package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mailcloak/internal/mailcloak"
)

func main() {
	dbPath := flag.String("db", mailcloak.DefaultDBPath, "SQLite database path")
	configPath := flag.String("config", mailcloak.DefaultConfigPath, "Mailcloak config path")
	listenAddr := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	token := flag.String("token", "", "Bearer token required for API requests")
	initDB := flag.Bool("init-db", false, "create or initialize the SQLite database before serving")
	insecureNoAuth := flag.Bool("insecure-no-auth", false, "serve the admin API without authentication")
	flag.Parse()

	adminToken := strings.TrimSpace(*token)
	if adminToken == "" {
		adminToken = strings.TrimSpace(os.Getenv("MAILCLOAK_ADMIN_TOKEN"))
	}

	var (
		db  *mailcloak.MailcloakDB
		err error
	)
	if *initDB {
		db, err = mailcloak.InitMailcloakDB(*dbPath)
	} else {
		db, err = mailcloak.OpenExistingMailcloakDB(*dbPath)
	}
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if adminToken == "" && !*insecureNoAuth {
		log.Fatalf("missing admin token: set --token, MAILCLOAK_ADMIN_TOKEN, or --insecure-no-auth for local development")
	}
	if *insecureNoAuth {
		log.Printf("admin api auth disabled by --insecure-no-auth; bind is %s", *listenAddr)
	}

	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           mailcloak.NewAdminHTTPHandler(db, mailcloak.AdminHTTPOptions{Token: adminToken, ConfigPath: *configPath, DBPath: *dbPath}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("mailcloak admin api listening on %s", *listenAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown: %v", err)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}
}
