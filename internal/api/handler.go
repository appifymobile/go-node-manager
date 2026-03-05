package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"go-node-manager/internal/health"
	"go-node-manager/internal/models"
	"go-node-manager/internal/service"
)

// Handler handles HTTP requests for VPN management
type Handler struct {
	managers map[models.ProtocolType]service.VpnManager
	metrics  *health.MetricsCollector
	logger   *slog.Logger
}

// New creates a new HTTP handler
func New(
	managers map[models.ProtocolType]service.VpnManager,
	metrics *health.MetricsCollector,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		managers: managers,
		metrics:  metrics,
		logger:   logger,
	}
}

// RegisterRoutes registers all HTTP routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Client management endpoints
	router.HandleFunc("/api/v1/clients/{clientId}/{type}/connect", h.Connect).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/clients/{clientId}/{type}/disconnect", h.Disconnect).Methods(http.MethodPost)

	// Health check endpoints
	router.HandleFunc("/manage/health", h.Health).Methods(http.MethodGet)
	router.HandleFunc("/manage/health/readiness", h.Readiness).Methods(http.MethodGet)
	router.HandleFunc("/manage/health/liveness", h.Liveness).Methods(http.MethodGet)
	router.HandleFunc("/manage/metrics", h.Metrics).Methods(http.MethodGet)
}

// Connect handles POST /api/v1/clients/{clientId}/{type}/connect
func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientIDStr := vars["clientId"]
	protocolStr := vars["type"]

	// Parse client ID
	clientID, err := strconv.ParseInt(clientIDStr, 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	// Parse protocol type
	protocolType := models.ProtocolType(protocolStr)
	if !protocolType.IsValid() {
		h.respondError(w, http.StatusBadRequest, "invalid protocol type")
		return
	}

	// Get the manager for this protocol
	manager, exists := h.managers[protocolType]
	if !exists || manager == nil {
		h.respondError(w, http.StatusBadRequest, "protocol not available")
		return
	}

	// Add client with protocol in context
	ctx := context.WithValue(r.Context(), "protocol", protocolType)
	config, err := manager.AddClient(ctx, clientID)
	if err != nil {
		h.logger.Error("Failed to add client",
			"clientID", clientID,
			"protocol", protocolType,
			"error", err,
		)

		// Determine error code
		if err == models.ErrPeerAlreadyExists {
			h.respondError(w, http.StatusConflict, "peer already exists")
			return
		}
		if err == models.ErrIPPoolExhausted {
			h.respondError(w, http.StatusServiceUnavailable, "IP pool exhausted")
			return
		}

		h.respondError(w, http.StatusInternalServerError, "failed to add client")
		return
	}

	// Record metrics
	h.metrics.RecordClientAdded(protocolType)

	// Return configuration as JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(config))

	h.logger.Info("Client connected successfully",
		"clientID", clientID,
		"protocol", protocolType,
	)
}

// Disconnect handles POST /api/v1/clients/{clientId}/{type}/disconnect
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientIDStr := vars["clientId"]
	protocolStr := vars["type"]

	// Parse client ID
	clientID, err := strconv.ParseInt(clientIDStr, 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	// Parse protocol type
	protocolType := models.ProtocolType(protocolStr)
	if !protocolType.IsValid() {
		h.respondError(w, http.StatusBadRequest, "invalid protocol type")
		return
	}

	// Get the manager for this protocol
	manager, exists := h.managers[protocolType]
	if !exists || manager == nil {
		h.respondError(w, http.StatusBadRequest, "protocol not available")
		return
	}

	// Delete client with protocol in context
	ctx := context.WithValue(r.Context(), "protocol", protocolType)
	if err := manager.DeleteClient(ctx, clientID); err != nil {
		h.logger.Error("Failed to delete client",
			"clientID", clientID,
			"protocol", protocolType,
			"error", err,
		)

		if err == models.ErrClientNotFound {
			h.respondError(w, http.StatusNotFound, "client not found")
			return
		}

		h.respondError(w, http.StatusInternalServerError, "failed to delete client")
		h.metrics.RecordError()
		return
	}

	// Record metrics
	h.metrics.RecordClientRemoved(protocolType)

	// Return 204 No Content
	w.WriteHeader(http.StatusNoContent)

	h.logger.Info("Client disconnected successfully",
		"clientID", clientID,
		"protocol", protocolType,
	)
}

// Health returns a simple health check
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "up",
	})
}

// Readiness checks if the service is ready to serve traffic
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
	})
}

// Liveness checks if the service is alive
func (h *Handler) Liveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "alive",
	})
}

// Metrics returns metrics in JSON format
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(h.metrics.GetSnapshot())
}

// respondError sends an error response in JSON format
func (h *Handler) respondError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
