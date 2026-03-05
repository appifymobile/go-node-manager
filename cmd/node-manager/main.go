package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"go-node-manager/internal/api"
	"go-node-manager/internal/config"
	"go-node-manager/internal/models"
	"go-node-manager/internal/service/wireguard"
	"go-node-manager/internal/storage"
)

func main() {
	// Parse command-line flags
	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx := context.Background()

	// Initialize database
	db, err := storage.New(
		ctx,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Name,
		cfg.Database.MaxConnections,
	)
	if err != nil {
		logger.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	logger.Info("Connected to database",
		"host", cfg.Database.Host,
		"database", cfg.Database.Name,
	)

	// Initialize VPN managers
	managers := make(map[models.ProtocolType]service.VpnManager)

	// Initialize WireGuard manager if enabled
	if cfg.VPN.WireGuard.Enabled {
		wgManager, err := wireguard.New(
			cfg.VPN.WireGuard.Iface,
			cfg.VPN.WireGuard.Port,
			cfg.VPN.WireGuard.Address,
			getNodeHostname(),
			db,
			logger,
		)
		if err != nil {
			logger.Error("Failed to initialize WireGuard manager", "error", err)
			os.Exit(1)
		}
		defer wgManager.Close()

		// Start WireGuard server
		if err := wgManager.StartServer(ctx); err != nil {
			logger.Error("Failed to start WireGuard server", "error", err)
			os.Exit(1)
		}

		managers[models.WIREGUARD] = wgManager

		// Start housekeeping ticker
		if cfg.VPN.WireGuard.HealthCheck.Enabled {
			go startHousekeepingTicker(ctx, wgManager, cfg.VPN.WireGuard.HealthCheck.Interval, logger)
		}

		logger.Info("WireGuard manager initialized",
			"iface", cfg.VPN.WireGuard.Iface,
			"port", cfg.VPN.WireGuard.Port,
		)
	}

	// TODO: Initialize SingBox manager if enabled

	// Initialize HTTP router
	router := mux.NewRouter()
	handler := api.New(managers, logger)
	handler.RegisterRoutes(router)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Starting HTTP server", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	// Wait for shutdown signal or server error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != http.ErrServerClosed {
			logger.Error("Server error", "error", err)
			os.Exit(1)
		}
	case sig := <-sigChan:
		logger.Info("Received signal, shutting down", "signal", sig)

		// Graceful shutdown with timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Server shutdown error", "error", err)
			os.Exit(1)
		}

		logger.Info("Server shut down successfully")
	}
}

// startHousekeepingTicker runs housekeeping maintenance at regular intervals
func startHousekeepingTicker(
	ctx context.Context,
	manager interface{ PerformMaintenance(context.Context) error },
	interval time.Duration,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := manager.PerformMaintenance(ctx); err != nil {
				logger.Error("Housekeeping failed", "error", err)
			}
		}
	}
}

// getNodeHostname returns the node's public hostname for VPN configurations
func getNodeHostname() string {
	if hostname := os.Getenv("VPN_NODE_PUBLIC_HOSTNAME"); hostname != "" {
		return hostname
	}
	// Fallback to system hostname
	hostname, err := os.Hostname()
	if err != nil {
		return "vpn-node"
	}
	return hostname
}

// Add missing import
import "go-node-manager/internal/service"
