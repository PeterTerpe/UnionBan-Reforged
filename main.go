package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/api"
	"github.com/PeterTerpe/MeshBan/internal/config"
	"github.com/PeterTerpe/MeshBan/internal/database"
	"github.com/PeterTerpe/MeshBan/internal/identity"
	"github.com/PeterTerpe/MeshBan/internal/secrets"
)

const (
	AppName = "MeshBan"
	Version = "0.1.0-dev"
)

func main() {
	// Parse command-line arguments.
	configPath := flag.String("config", "config.yaml", "Path to the configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s\n", AppName, Version)
		return
	}

	// Create a root context for the whole program.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize structured logging.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("starting MeshBan", "version", Version)

	// Load configuration.
	cfg, err := config.LoadOrCreate(*configPath, "example_config.yaml")
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger.Info("configuration loaded", "config", *configPath)

	// Load secrets from the env file.
	secretStore, err := secrets.LoadOrCreate(cfg.Secrets.EnvFile)
	if err != nil {
		logger.Error("failed to load secrets", "error", err)
		os.Exit(1)
	}

	// Ensure the WebUI token exists.
	if _, err := secretStore.EnsureRandom(secrets.WebTokenEnv, 32); err != nil {
		logger.Error("failed to initialize WebUI token", "error", err)
		os.Exit(1)
	}

	logger.Info("WebUI token initialized")

	// Open the local SQLite database.
	db, err := database.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	logger.Info("database opened", "path", cfg.Database.Path)

	// Run database migrations.
	if err := db.Migrate(ctx); err != nil {
		logger.Error("failed to migrate database", "error", err)
		os.Exit(1)
	}

	logger.Info("database migration completed")

	// Initialize private key protection options.
	keyPassphrase := ""

	if cfg.Security.EncryptPrivateKey {
		keyPassphrase = strings.TrimSpace(secretStore.Get(secrets.KeyPassphraseEnv))

		if keyPassphrase == "" {
			record, err := db.GetIdentity(ctx)
			if err == nil && identity.IsEncryptedPrivateKey(record.PrivateKey) {
				logger.Error("private key is encrypted but MESHBAN_KEY_PASSPHRASE is missing")
				os.Exit(1)
			}

			keyPassphrase, err = secretStore.EnsureRandom(secrets.KeyPassphraseEnv, 32)
			if err != nil {
				logger.Error("failed to initialize key passphrase", "error", err)
				os.Exit(1)
			}
		}
	}

	keyOptions := identity.KeyOptions{
		EncryptPrivateKey: cfg.Security.EncryptPrivateKey,
		Passphrase:        keyPassphrase,
	}

	// Load or create the local identity.
	identityService, err := identity.LoadOrCreate(ctx, db, cfg.Node.DisplayName, keyOptions)
	if err != nil {
		logger.Error("failed to load or create local identity", "error", err)
		os.Exit(1)
	}

	localIdentity := identityService.Current()
	logger.Info("local identity loaded", "node_id", localIdentity.NodeID)

	// Create the local API and WebUI server.
	apiServer := api.NewServer(api.Options{
		ListenAddr:      cfg.WebUI.Listen,
		Version:         Version,
		Database:        db,
		IdentityService: identityService,
		Config:          cfg,
		ConfigPath:      *configPath,
		SecretManager:   secretStore,
		Logger:          logger,
	})

	// Start the local API server in the background.
	go func() {
		logger.Info("starting local API server", "listen", cfg.WebUI.Listen)

		if err := apiServer.Start(ctx); err != nil {
			logger.Error("local API server stopped with error", "error", err)
			cancel()
		}
	}()

	// Wait for Ctrl+C or system shutdown signal.
	waitForShutdownSignal(logger)

	logger.Info("shutdown signal received")

	// Cancel all background services.
	cancel()

	// Gracefully shut down the API server.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shut down API server", "error", err)
	}

	logger.Info("MeshBan stopped")
}

func waitForShutdownSignal(logger *slog.Logger) {
	signalChannel := make(chan os.Signal, 1)

	signal.Notify(
		signalChannel,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	sig := <-signalChannel
	logger.Info("received system signal", "signal", sig.String())
}
