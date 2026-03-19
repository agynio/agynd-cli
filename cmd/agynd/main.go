package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/daemon"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	agynd, err := daemon.New(ctx, cfg, version)
	if err != nil {
		log.Fatalf("daemon init failed: %v", err)
	}
	defer agynd.Close()

	if err := agynd.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("daemon exited: %v", err)
	}
}
