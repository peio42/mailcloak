package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mailcloak/internal/mailcloak"
)

var version = "dev-" + time.Now().Format("20060102")

func main() {
	log.Printf("mailcloak %s\n", version)
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		return
	}

	cfgPath := "/etc/mailcloak/config.yaml"
	if len(os.Args) >= 2 {
		cfgPath = os.Args[1]
	}

	log.Printf("loading config from %s", cfgPath)
	cfg, err := mailcloak.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := mailcloak.Start(ctx, cfg)
	if err != nil {
		log.Fatalf("start: %v", err)
	}

	// Handle signals
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Printf("shutdown")
	cancel()

	<-svc.Done()
}
