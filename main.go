package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/api"
	"github.com/PeterTerpe/MeshBan/internal/config"
	"github.com/PeterTerpe/MeshBan/internal/database"
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
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger.Info("configuration loaded", "config", *configPath)

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

	// Create the local API server.
	apiServer := api.NewServer(api.Options{
		ListenAddr: cfg.API.Listen,
		Version:    Version,
		Database:   db,
		Logger:     logger,
	})

	// Start the local API server in the background.
	go func() {
		logger.Info("starting local API server", "listen", cfg.API.Listen)

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
