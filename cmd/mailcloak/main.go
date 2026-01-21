package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mailcloak/internal/mailcloak"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}

	cfgPath := "/etc/mailcloak/config.yaml"
	if len(os.Args) >= 2 {
		cfgPath = os.Args[1]
	}

	cfg, err := mailcloak.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	policyListener, err := mailcloak.OpenPolicyListener(cfg)
	if err != nil {
		log.Fatalf("policy listener: %v", err)
	}

	socketmapListener, err := mailcloak.OpenSocketmapListener(cfg)
	if err != nil {
		_ = policyListener.Close()
		log.Fatalf("socketmap listener: %v", err)
	}

	if err := mailcloak.DropPrivileges(cfg); err != nil {
		_ = policyListener.Close()
		_ = socketmapListener.Close()
		log.Fatalf("privileges: %v", err)
	}

	db, err := mailcloak.OpenAliasDB(cfg.SQLite.Path)
	if err != nil {
		log.Fatalf("sqlite: %v", err)
	}
	defer db.Close()

	kc := mailcloak.NewKeycloak(cfg)
	cache := mailcloak.NewCache(time.Duration(cfg.Policy.CacheTTLSeconds) * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start socketmap server
	go func() {
		if err := mailcloak.ServeSocketmap(ctx, cfg, db, socketmapListener); err != nil {
			log.Fatalf("socketmap: %v", err)
		}
	}()

	// Start policy server
	go func() {
		if err := mailcloak.ServePolicy(ctx, cfg, db, kc, cache, policyListener); err != nil {
			log.Fatalf("policy: %v", err)
		}
	}()

	log.Printf("mailcloak started")

	// Handle signals
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Printf("shutdown")
	cancel()
	time.Sleep(300 * time.Millisecond)
}
