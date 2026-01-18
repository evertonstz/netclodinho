package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/angristan/netclode/services/control-plane/internal/api"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/github"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
	"github.com/angristan/netclode/services/control-plane/internal/session"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
)

func main() {
	// Configure structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load configuration
	cfg := config.Load()
	slog.Info("Configuration loaded",
		"port", cfg.Port,
		"namespace", cfg.K8sNamespace,
		"agentImage", cfg.AgentImage,
		"redisURL", storage.ParseRedisURL(cfg.RedisURL),
	)

	// Initialize Redis storage
	store, err := storage.NewRedisStorage(ctx, cfg)
	if err != nil {
		return fmt.Errorf("init redis: %w", err)
	}
	defer func() {
		slog.Info("Closing Redis connection")
		store.Close()
	}()

	// Initialize Kubernetes runtime with informers
	k8sRuntime, err := k8s.NewRuntime(cfg)
	if err != nil {
		return fmt.Errorf("init k8s: %w", err)
	}
	defer func() {
		slog.Info("Stopping K8s informer")
		k8sRuntime.Close()
	}()

	// Initialize GitHub client (optional - nil if not configured)
	githubClient, err := github.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("init github: %w", err)
	}
	if githubClient != nil {
		slog.Info("GitHub App integration enabled", "appID", cfg.GitHubAppID, "installationID", cfg.GitHubInstallationID)
	}

	// Create session manager
	manager := session.NewManager(store, k8sRuntime, cfg, githubClient)
	defer func() {
		slog.Info("Closing session manager")
		manager.Close()
	}()

	// Initialize manager (load sessions, reconcile with K8s)
	if err := manager.Initialize(ctx); err != nil {
		return fmt.Errorf("init manager: %w", err)
	}

	// Create HTTP/WebSocket server
	server := api.NewServer(manager)

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		slog.Info("Shutdown signal received", "signal", sig.String())
		cancel()
	}()

	// Start server (blocks until shutdown)
	addr := fmt.Sprintf(":%d", cfg.Port)
	if err := server.ListenAndServe(ctx, addr); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	slog.Info("Server stopped gracefully")
	return nil
}
