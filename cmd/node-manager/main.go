package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"google.golang.org/grpc"
	"go-node-manager/internal/api"
	"go-node-manager/internal/config"
	grpcinternal "go-node-manager/internal/grpc"
	"go-node-manager/internal/health"
	"go-node-manager/internal/models"
	"go-node-manager/internal/service"
	"go-node-manager/internal/service/singbox"
	"go-node-manager/internal/service/wireguard"
	"go-node-manager/internal/storage"
	pb "go-node-manager/proto/nodemgr"
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

	// Initialize metrics collector
	metricsCollector := health.NewMetricsCollector(db, logger)

	// Initialize gRPC server if enabled
	var grpcServer *grpc.Server
	grpcErrors := make(chan error, 1)

	if cfg.GRPC.Enabled {
		healthService := grpcinternal.NewHealthService(metricsCollector, db, logger, cfg.GRPC.HealthCheckInterval)

		var grpcServerOpts []grpc.ServerOption
		if cfg.GRPC.Auth.Enabled {
			authInterceptor := grpcinternal.NewStreamAuthInterceptor(cfg.GRPC.Auth.Username, cfg.GRPC.Auth.Password)
			grpcServerOpts = append(grpcServerOpts, grpc.StreamInterceptor(authInterceptor))
		}

		grpcServer = grpc.NewServer(grpcServerOpts...)
		pb.RegisterNodeHealthServiceServer(grpcServer, healthService)

		// Start gRPC server in a goroutine
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPC.Port))
		if err != nil {
			logger.Error("Failed to create gRPC listener", "error", err)
			os.Exit(1)
		}

		go func() {
			logger.Info("Starting gRPC server", "port", cfg.GRPC.Port)
			grpcErrors <- grpcServer.Serve(listener)
		}()
	}

	// Initialize SingBox manager if enabled
	if cfg.VPN.SingBox.Enabled {
		sbManager, err := singbox.New(
			cfg.VPN.SingBox.ConfigPath,
			getNodeHostname(),
			db,
			map[models.ProtocolType]*singbox.ProtocolConfig{
				models.SHADOWSOCKS: {
					Enabled: cfg.VPN.SingBox.ShadowSocks.Enabled,
					Port:    cfg.VPN.SingBox.ShadowSocks.Port,
					Method:  cfg.VPN.SingBox.ShadowSocks.EncryptionMethod,
				},
				models.VLESS: {
					Enabled: cfg.VPN.SingBox.VLESS.Enabled,
					Port:    cfg.VPN.SingBox.VLESS.Port,
					ShortID: cfg.VPN.SingBox.VLESS.ShortID,
				},
				models.HYSTERIA2: {
					Enabled: cfg.VPN.SingBox.Hysteria2.Enabled,
					Port:    cfg.VPN.SingBox.Hysteria2.Port,
					Obfs:    cfg.VPN.SingBox.Hysteria2.Obfs,
				},
			},
			logger,
		)
		if err != nil {
			logger.Error("Failed to initialize SingBox manager", "error", err)
			os.Exit(1)
		}

		// Start SingBox server
		if err := sbManager.StartServer(ctx); err != nil {
			logger.Error("Failed to start SingBox server", "error", err)
			os.Exit(1)
		}

		if cfg.VPN.SingBox.ShadowSocks.Enabled {
			managers[models.SHADOWSOCKS] = sbManager
			logger.Info("ShadowSocks manager initialized",
				"port", cfg.VPN.SingBox.ShadowSocks.Port,
			)
		}

		if cfg.VPN.SingBox.VLESS.Enabled {
			managers[models.VLESS] = sbManager
			logger.Info("VLESS manager initialized",
				"port", cfg.VPN.SingBox.VLESS.Port,
			)
		}

		if cfg.VPN.SingBox.Hysteria2.Enabled {
			managers[models.HYSTERIA2] = sbManager
			logger.Info("Hysteria2 manager initialized",
				"port", cfg.VPN.SingBox.Hysteria2.Port,
			)
		}

		// Start housekeeping for SingBox
		go startHousekeepingTicker(ctx, sbManager, 24*time.Hour, logger)
	}

	// Initialize HTTP router
	router := mux.NewRouter()
	handler := api.New(managers, metricsCollector, logger)
	handler.RegisterRoutes(router)

	// Register Prometheus metrics endpoint
	prometheusHandler := health.NewMetricsHandler(metricsCollector)
	router.Handle("/metrics", prometheusHandler)

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
			logger.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	case err := <-grpcErrors:
		if err != nil {
			logger.Error("gRPC server error", "error", err)
			os.Exit(1)
		}
	case sig := <-sigChan:
		logger.Info("Received signal, shutting down", "signal", sig)

		// Graceful shutdown with timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Shutdown HTTP server first
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP server shutdown error", "error", err)
			os.Exit(1)
		}

		// Gracefully shutdown gRPC server
		if grpcServer != nil {
			grpcServer.GracefulStop()
			logger.Info("gRPC server shut down successfully")
		}

		logger.Info("All servers shut down successfully")
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
