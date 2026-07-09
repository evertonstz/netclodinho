package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	slogtrace "github.com/DataDog/dd-trace-go/contrib/log/slog/v2"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"github.com/DataDog/dd-trace-go/v2/profiler"

	"github.com/angristan/netclode/services/control-plane/internal/api"
	"github.com/angristan/netclode/services/control-plane/internal/boxlite"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/docker"
	"github.com/angristan/netclode/services/control-plane/internal/github"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
	"github.com/angristan/netclode/services/control-plane/internal/metrics"
	"github.com/angristan/netclode/services/control-plane/internal/session"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
)

func main() {
	// Start Datadog tracer (reads DD_SERVICE, DD_ENV, DD_VERSION from env)
	tracer.Start(tracer.WithRuntimeMetrics())
	defer tracer.Stop()

	// Start continuous profiler
	if err := profiler.Start(
		profiler.WithProfileTypes(
			profiler.CPUProfile,
			profiler.HeapProfile,
			profiler.GoroutineProfile,
		),
	); err != nil {
		slog.Warn("Failed to start profiler", "error", err)
	}
	defer profiler.Stop()

	// Configure structured logging with Datadog trace correlation
	// LOG_LEVEL env var: debug, info, warn, error (default: info)
	logLevel := slog.LevelInfo
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		switch strings.ToLower(lvl) {
		case "debug":
			logLevel = slog.LevelDebug
		case "warn", "warning":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}
	logger := slog.New(slogtrace.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Initialize DogStatsD metrics client
	if err := metrics.Init(); err != nil {
		slog.Warn("Failed to init metrics client", "error", err)
	}
	defer metrics.Close()

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
	if err := cfg.Validate(); err != nil {
		return err
	}
	slog.Info("Configuration loaded",
		"port", cfg.Port,
		"namespace", cfg.K8sNamespace,
		"agentImage", cfg.AgentImage,
		"redisURL", storage.ParseRedisURL(cfg.RedisURL),
		"idleTimeout", cfg.IdleTimeout,
		"runtimeMode", cfg.RuntimeMode,
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

	// Create session manager (needed before Docker runtime to satisfy TokenIssuer).
	manager := session.NewManager(store, nil, cfg, nil) // runtime injected below

	// Initialize runtime based on RUNTIME_MODE.
	// cfg.Validate() above ensures only known values reach this switch.
	var runtime k8s.Runtime
	switch cfg.RuntimeMode {
	case config.RuntimeModeBoxlite:
		slog.Info("Runtime mode: boxlite")
		// Force warm pool off in BoxLite mode.
		cfg.UseWarmPool = false
		boxliteRuntime, err := boxlite.NewRuntime(cfg, manager)
		if err != nil {
			return fmt.Errorf("init boxlite runtime: %w", err)
		}
		defer func() {
			slog.Info("Closing Boxlite runtime")
			boxliteRuntime.Close()
		}()
		runtime = boxliteRuntime
	case config.RuntimeModeDocker:
		// D-01: RUNTIME_MODE=docker now selects the Docker Engine container runtime.
		// Legacy BoxLite users who previously set RUNTIME_MODE=docker must update
		// to RUNTIME_MODE=boxlite to continue using the BoxLite microVM backend.
		slog.Warn("Runtime mode: docker (Docker Engine)",
			"notice", "RUNTIME_MODE=docker now selects the Docker Engine container runtime; "+
				"legacy BoxLite users must set RUNTIME_MODE=boxlite instead")
		// Force warm pool off — Docker Engine runtime does not support warm pool.
		cfg.UseWarmPool = false
		dockerRuntime, err := docker.NewRuntime(cfg)
		if err != nil {
			return fmt.Errorf("init docker runtime: %w", err)
		}
		defer func() {
			slog.Info("Closing Docker Engine runtime")
			dockerRuntime.Close()
		}()
		runtime = dockerRuntime
	default:
		// Kubernetes is the default runtime; Validate() ensures only "kubernetes" reaches here.
		slog.Info("Runtime mode: kubernetes")
		k8sRuntime, err := k8s.NewRuntime(cfg)
		if err != nil {
			return fmt.Errorf("init k8s: %w", err)
		}
		defer func() {
			slog.Info("Stopping K8s informer")
			k8sRuntime.Close()
		}()
		runtime = k8sRuntime
	}

	// Inject the runtime into the manager now that it's ready.
	manager.SetRuntime(runtime)

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

	// Set GitHub client on manager.
	manager.SetGitHubClient(githubClient)

	defer func() {
		slog.Info("Closing session manager")
		manager.Close()
	}()

	// Initialize manager (load sessions, reconcile with K8s)
	if err := manager.Initialize(ctx); err != nil {
		return fmt.Errorf("init manager: %w", err)
	}

	// Startup orphan cleanup for the Docker Engine runtime (DEVX-02, D-08).
	// CleanupOrphans is not on the k8s.Runtime interface — reached via concrete-type assertion
	// so the interface is not modified (project constraint).
	// Runs AFTER manager.Initialize so all storage sessions are already loaded.
	if dr, ok := runtime.(*docker.Runtime); ok {
		sessions, err := store.GetAllSessions(ctx)
		if err != nil {
			slog.Warn("Docker orphan cleanup: failed to list sessions, skipping", "error", err)
		} else {
			knownSessionIDs := make(map[string]bool, len(sessions))
			for _, s := range sessions {
				knownSessionIDs[s.Id] = true
			}
			if err := dr.CleanupOrphans(ctx, knownSessionIDs); err != nil {
				slog.Warn("Docker orphan cleanup failed", "error", err)
			}
		}
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
