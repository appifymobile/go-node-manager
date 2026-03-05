package service

import (
	"context"
	"go-node-manager/internal/models"
)

// VpnManager defines the interface for VPN protocol implementations
type VpnManager interface {
	// StartServer initializes and starts the protocol server
	StartServer(ctx context.Context) error

	// AddClient generates credentials and adds a new client to the server
	// Returns JSON configuration as string
	AddClient(ctx context.Context, clientID int64) (string, error)

	// DeleteClient removes a client from the server
	DeleteClient(ctx context.Context, clientID int64) error
}

// Housekeeping defines the interface for cleanup and maintenance operations
type Housekeeping interface {
	// PerformMaintenance removes inactive peers and performs cleanup
	PerformMaintenance(ctx context.Context) error
}
