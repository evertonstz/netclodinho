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
		"idleTimeout", cfg.IdleTimeout,
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

	// Initialize GitHub client (nil if not configured)
	var githubClient *github.Client
	if cfg.HasGitHubApp() {
		var err error
		githubClient, err = github.NewClient(cfg.GitHubAppID, cfg.GitHubInstallationID, cfg.GitHubAppPrivateKey)
		if err != nil {
			slog.Error("Failed to create GitHub client", "error", err)
		} else {
			slog.Info("GitHub App client initialized")
		}
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

	// Create HTTP/Connect server
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
	httpAddr := fmt.Sprintf(":%d", cfg.Port)
	if err := server.ListenAndServe(ctx, httpAddr); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	slog.Info("Server stopped gracefully")
	return nil
}
