package health

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go-node-manager/internal/models"
	"go-node-manager/internal/storage"
)

// MetricsCollector collects and tracks VPN metrics
type MetricsCollector struct {
	db     *storage.DB
	logger *slog.Logger
	mu     sync.RWMutex

	// Metrics
	activeClientsByProtocol map[models.ProtocolType]int
	totalClientsCreated     int64
	totalClientsRemoved     int64
	totalErrors             int64
	lastMaintenanceTime     time.Time
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(db *storage.DB, logger *slog.Logger) *MetricsCollector {
	return &MetricsCollector{
		db:                      db,
		logger:                  logger,
		activeClientsByProtocol: make(map[models.ProtocolType]int),
	}
}

// RecordClientAdded records a client connection
func (mc *MetricsCollector) RecordClientAdded(protocol models.ProtocolType) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.activeClientsByProtocol[protocol]++
	mc.totalClientsCreated++
}

// RecordClientRemoved records a client disconnection
func (mc *MetricsCollector) RecordClientRemoved(protocol models.ProtocolType) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.activeClientsByProtocol[protocol] > 0 {
		mc.activeClientsByProtocol[protocol]--
	}
	mc.totalClientsRemoved++
}

// RecordError records an error event
func (mc *MetricsCollector) RecordError() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.totalErrors++
}

// RecordMaintenanceCompleted records housekeeping completion
func (mc *MetricsCollector) RecordMaintenanceCompleted() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.lastMaintenanceTime = time.Now()
}

// RefreshFromDatabase updates metrics from database
func (mc *MetricsCollector) RefreshFromDatabase(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for protocol := range mc.activeClientsByProtocol {
		clients, err := mc.db.FindAllClients(ctx, protocol)
		if err != nil {
			return fmt.Errorf("failed to fetch clients for %s: %w", protocol, err)
		}
		mc.activeClientsByProtocol[protocol] = len(clients)
	}

	return nil
}

// GetSnapshot returns a snapshot of current metrics
func (mc *MetricsCollector) GetSnapshot() map[string]interface{} {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	clientsByProtocol := make(map[string]int)
	totalActive := 0
	for protocol, count := range mc.activeClientsByProtocol {
		clientsByProtocol[protocol.String()] = count
		totalActive += count
	}

	return map[string]interface{}{
		"total_active_clients":      totalActive,
		"active_clients_by_protocol": clientsByProtocol,
		"total_clients_created":      mc.totalClientsCreated,
		"total_clients_removed":      mc.totalClientsRemoved,
		"total_errors":               mc.totalErrors,
		"last_maintenance_time":      mc.lastMaintenanceTime.Format(time.RFC3339),
	}
}

// GetHealthSnapshot returns a structured snapshot for health status computation
func (mc *MetricsCollector) GetHealthSnapshot() HealthSnapshot {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	totalActive := 0
	for _, count := range mc.activeClientsByProtocol {
		totalActive += count
	}

	return HealthSnapshot{
		TotalActiveConnections: int64(totalActive),
		TotalErrors:            mc.totalErrors,
		TotalClientsCreated:    mc.totalClientsCreated,
		LastMaintenanceTime:    mc.lastMaintenanceTime,
	}
}

// PrometheusMetrics holds Prometheus metric objects
type PrometheusMetrics struct {
	ActiveClients  map[models.ProtocolType]float64
	ClientsCreated int64
	ClientsRemoved int64
	Errors         int64
}

// GetPrometheusMetrics returns metrics in Prometheus format
func (mc *MetricsCollector) GetPrometheusMetrics() *PrometheusMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	activeClients := make(map[models.ProtocolType]float64)
	for protocol, count := range mc.activeClientsByProtocol {
		activeClients[protocol] = float64(count)
	}

	return &PrometheusMetrics{
		ActiveClients:  activeClients,
		ClientsCreated: mc.totalClientsCreated,
		ClientsRemoved: mc.totalClientsRemoved,
		Errors:         mc.totalErrors,
	}
}

// MetricsHandler serves Prometheus-formatted metrics
type MetricsHandler struct {
	collector *MetricsCollector
}

// NewMetricsHandler creates a new metrics HTTP handler
func NewMetricsHandler(collector *MetricsCollector) *MetricsHandler {
	return &MetricsHandler{
		collector: collector,
	}
}

// ServeHTTP implements http.Handler for Prometheus metrics
func (mh *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/metrics" {
		http.NotFound(w, r)
		return
	}

	metrics := mh.collector.GetPrometheusMetrics()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// HELP and TYPE comments
	fmt.Fprintf(w, "# HELP vvpn_active_clients_total Active connected clients\n")
	fmt.Fprintf(w, "# TYPE vvpn_active_clients_total gauge\n")

	// Active clients per protocol
	for protocol, count := range metrics.ActiveClients {
		fmt.Fprintf(w, "vvpn_active_clients_total{protocol=\"%s\"} %f\n", protocol.String(), count)
	}

	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "# HELP vvpn_clients_created_total Total clients connected\n")
	fmt.Fprintf(w, "# TYPE vvpn_clients_created_total counter\n")
	fmt.Fprintf(w, "vvpn_clients_created_total %d\n", metrics.ClientsCreated)

	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "# HELP vvpn_clients_removed_total Total clients disconnected\n")
	fmt.Fprintf(w, "# TYPE vvpn_clients_removed_total counter\n")
	fmt.Fprintf(w, "vvpn_clients_removed_total %d\n", metrics.ClientsRemoved)

	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "# HELP vvpn_errors_total Total errors encountered\n")
	fmt.Fprintf(w, "# TYPE vvpn_errors_total counter\n")
	fmt.Fprintf(w, "vvpn_errors_total %d\n", metrics.Errors)
}
