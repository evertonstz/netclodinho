package main

import (
	"log/slog"
	"os"

	"github.com/angristan/netclode/services/secret-proxy/internal/certs"
	"github.com/angristan/netclode/services/secret-proxy/internal/config"
	"github.com/angristan/netclode/services/secret-proxy/internal/proxy"
)

func main() {
	// Configure structured logging
	logLevel := slog.LevelInfo
	if os.Getenv("VERBOSE") == "true" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("Fatal error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	// Load configuration
	cfg := config.Load()
	logger.Info("Configuration loaded",
		"listenAddr", cfg.ListenAddr,
		"controlPlaneURL", cfg.ControlPlaneURL,
		"caPath", cfg.CAPath,
		"secretsPath", cfg.SecretsPath,
		"verbose", cfg.Verbose,
	)

	// Load secrets from file (not env var - prevents /proc/*/environ exposure)
	secrets, err := config.LoadSecrets(cfg.SecretsPath)
	if err != nil {
		return err
	}
	logger.Info("Secrets loaded", "count", len(secrets))

	// Load or generate CA certificate
	ca, err := certs.LoadOrGenerateCA(cfg.CAPath, cfg.CAKeyPath)
	if err != nil {
		return err
	}
	logger.Info("CA certificate loaded")

	// Create and start proxy
	p := proxy.New(proxy.Config{
		ListenAddr:      cfg.ListenAddr,
		ControlPlaneURL: cfg.ControlPlaneURL,
		Secrets:         secrets,
		CA:              ca,
		Verbose:         cfg.Verbose,
	}, logger)

	return p.ListenAndServe()
}
