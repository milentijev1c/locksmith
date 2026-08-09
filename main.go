package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/milentijev1c/locksmith/card"
	"github.com/milentijev1c/locksmith/config"
	"github.com/milentijev1c/locksmith/server"
)

const (
	version = "1.1.1"
)

func main() {
	configPath := flag.String("config", "", "Path to config file")
	flag.Parse()

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	logger := log.New(os.Stdout, "[locksmith] ", log.LstdFlags|log.Lshortfile)
	logger.Printf("Locksmith v%s starting...", version)

	// Initialize card service
	cardService, err := card.NewCardService(logger)
	if err != nil {
		logger.Fatalf("Failed to initialize card service: %v", err)
	}
	defer cardService.Close()

	// Start card service polling
	go cardService.Start(context.Background())
	logger.Println("Card service started")

	// Initialize signing service (lazy — PKCS#11 module loaded on first request)
	signService, err := card.NewSignService(logger, cfg.PKCS11Module)
	if err != nil {
		logger.Printf("Warning: signing unavailable: %v", err)
	}
	if signService != nil {
		defer signService.Close()
		// Try to eagerly initialize if possible (card already inserted)
		if initErr := signService.Init(); initErr != nil {
			logger.Printf("Sign service will initialize on first request: %v", initErr)
		}
	}

	// Initialize HTTP server
	srv := server.NewServer(cfg, cardService, signService, logger, version)

	// Start HTTP server in goroutine
	go func() {
		addr := fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port)
		logger.Printf("Starting server on %s", addr)
		if err := srv.ListenAndServe(addr); err != nil {
			logger.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Println("Shutdown signal received, gracefully shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf("Server shutdown error: %v", err)
	}

	logger.Println("Locksmith stopped")
}
