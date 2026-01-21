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
		if err := mailcloak.RunSocketmap(ctx, cfg, db); err != nil {
			log.Fatalf("socketmap: %v", err)
		}
	}()

	// Start policy server
	go func() {
		if err := mailcloak.RunPolicy(ctx, cfg, db, kc, cache); err != nil {
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
