package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kc-policy/internal/kcpolicy"
)

func main() {
	cfgPath := "/etc/kc-policy/config.yaml"
	if len(os.Args) >= 2 {
		cfgPath = os.Args[1]
	}

	cfg, err := kcpolicy.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := kcpolicy.OpenAliasDB(cfg.SQLite.Path)
	if err != nil {
		log.Fatalf("sqlite: %v", err)
	}
	defer db.Close()

	kc := kcpolicy.NewKeycloak(cfg)
	cache := kcpolicy.NewCache(time.Duration(cfg.Policy.CacheTTLSeconds) * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start socketmap server
	go func() {
		if err := kcpolicy.RunSocketmap(ctx, cfg, db); err != nil {
			log.Fatalf("socketmap: %v", err)
		}
	}()

	// Start policy server
	go func() {
		if err := kcpolicy.RunPolicy(ctx, cfg, db, kc, cache); err != nil {
			log.Fatalf("policy: %v", err)
		}
	}()

	log.Printf("kc-policy started")

	// Handle signals
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Printf("shutdown")
	cancel()
	time.Sleep(300 * time.Millisecond)
}
