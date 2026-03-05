package grpc

import (
	"context"
	"log/slog"
	"time"

	pb "go-node-manager/proto/nodemgr"

	"go-node-manager/internal/health"
	"go-node-manager/internal/storage"
)

// HealthService implements the NodeHealthServiceServer
type HealthService struct {
	pb.UnimplementedNodeHealthServiceServer
	metricsCollector    *health.MetricsCollector
	db                  *storage.DB
	logger              *slog.Logger
	healthCheckInterval time.Duration
}

// NewHealthService creates a new health service
func NewHealthService(
	metricsCollector *health.MetricsCollector,
	db *storage.DB,
	logger *slog.Logger,
	healthCheckInterval time.Duration,
) *HealthService {
	return &HealthService{
		metricsCollector:    metricsCollector,
		db:                  db,
		logger:              logger,
		healthCheckInterval: healthCheckInterval,
	}
}

// StreamHealth implements the streaming health check RPC
func (hs *HealthService) StreamHealth(
	req *pb.HealthStreamRequest,
	stream pb.NodeHealthService_StreamHealthServer,
) error {
	hs.logger.Debug("StreamHealth started", "node_id", req.NodeId)
	defer hs.logger.Debug("StreamHealth ended", "node_id", req.NodeId)

	ctx := stream.Context()
	ticker := time.NewTicker(hs.healthCheckInterval)
	defer ticker.Stop()

	// Send initial event immediately
	if err := hs.sendHealthEvent(ctx, stream, req.NodeId); err != nil {
		return err
	}

	// Stream health events at configured interval
	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ticker.C:
			if err := hs.sendHealthEvent(ctx, stream, req.NodeId); err != nil {
				return err
			}
		}
	}
}

// sendHealthEvent sends a single health event to the stream
func (hs *HealthService) sendHealthEvent(
	ctx context.Context,
	stream pb.NodeHealthService_StreamHealthServer,
	nodeID string,
) error {
	// Get metrics snapshot
	snapshot := hs.metricsCollector.GetHealthSnapshot()

	// Check database connectivity
	latencyMs, dbErr := hs.db.PingDB(ctx)

	// Compute status
	status := health.ComputeStatus(
		dbErr,
		snapshot.TotalErrors,
		snapshot.TotalClientsCreated,
		snapshot.LastMaintenanceTime,
	)

	event := &pb.HealthEvent{
		NodeId:             nodeID,
		Status:             status,
		LatencyMs:          latencyMs,
		ActiveConnections:  snapshot.TotalActiveConnections,
		Timestamp:          time.Now().Unix(),
	}

	return stream.Send(event)
}
