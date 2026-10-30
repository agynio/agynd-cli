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
	// SIGCONT is the runner's wake after hibernation. It has to be asked for:
	// as a container's init, agynd never sees a signal it has no handler for.
	wake := make(chan os.Signal, 1)
	signal.Notify(wake, syscall.SIGCONT)
	defer signal.Stop(wake)

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	agynd, err := daemon.New(ctx, cfg, version)
	if err != nil {
		log.Fatalf("daemon init failed: %v", err)
	}
	defer agynd.Close()

	if err := agynd.Run(ctx, wake); err != nil {
		if errors.Is(err, context.Canceled) {
			log.Printf("daemon stopped: %v", err)
			return
		}
		log.Fatalf("daemon exited: %v", err)
	}
}
