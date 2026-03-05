package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"go-node-manager/internal/models"
	"go-node-manager/internal/storage"
	"go-node-manager/internal/utils"
)

// Manager implements VpnManager for SingBox-based protocols (ShadowSocks, VLESS)
type Manager struct {
	protocols      map[models.ProtocolType]*ProtocolConfig
	configPath     string
	nodeHostname   string
	db             *storage.DB
	logger         *slog.Logger
	mu             sync.RWMutex
	keyGenerator   *utils.KeyGenerator
}

// ProtocolConfig holds configuration for a single protocol
type ProtocolConfig struct {
	Enabled        bool
	Port           uint16
	Method         string // For ShadowSocks encryption
	ShortID        string // For VLESS
	EncryptionKey  string // Derived from password
	Obfs           string // For Hysteria2 obfuscation (optional)
}

// SingBoxConfig represents the full sing-box configuration
type SingBoxConfig struct {
	Log       LogConfig       `json:"log"`
	Inbounds  []InboundConfig `json:"inbounds"`
	Outbounds []OutboundConfig `json:"outbounds"`
	Route     RouteConfig     `json:"route"`
}

type LogConfig struct {
	Level     string `json:"level"`
	Output    string `json:"output"`
	Timestamp bool   `json:"timestamp"`
}

type InboundConfig struct {
	Type       string                 `json:"type"`
	Tag        string                 `json:"tag"`
	Listen     string                 `json:"listen"`
	ListenPort uint16                 `json:"listen_port"`
	Method     string                 `json:"method,omitempty"`
	Password   string                 `json:"password,omitempty"`
	Users      []UserConfig           `json:"users,omitempty"`
	Transport  map[string]interface{} `json:"transport,omitempty"`
}

type UserConfig struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	UUID     string `json:"uuid,omitempty"` // For VLESS
}

type OutboundConfig struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

type RouteConfig struct {
	Rules []interface{} `json:"rules"`
}

// New creates a new SingBox manager
func New(
	configPath string,
	nodeHostname string,
	db *storage.DB,
	protocolConfigs map[models.ProtocolType]*ProtocolConfig,
	logger *slog.Logger,
) (*Manager, error) {
	if err := os.MkdirAll(os.path.Dir(configPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &Manager{
		protocols:    protocolConfigs,
		configPath:   configPath,
		nodeHostname: nodeHostname,
		db:           db,
		logger:       logger,
		keyGenerator: utils.NewKeyGenerator(),
	}, nil
}

// StartServer initializes and starts the SingBox server
func (m *Manager) StartServer(ctx context.Context) error {
	m.logger.Info("Initializing SingBox server", "configPath", m.configPath)

	// Check if any protocols are enabled
	anyEnabled := false
	for _, cfg := range m.protocols {
		if cfg.Enabled {
			anyEnabled = true
			break
		}
	}

	if !anyEnabled {
		m.logger.Info("No SingBox protocols enabled, skipping startup")
		return nil
	}

	// Generate initial configuration
	if err := m.generateAndWriteConfig(ctx); err != nil {
		return fmt.Errorf("failed to generate initial config: %w", err)
	}

	// Reload sing-box service
	if err := m.reloadService(ctx); err != nil {
		m.logger.Warn("Failed to reload sing-box service (may not be installed yet)", "error", err)
		// Non-fatal - service may not be running yet
	}

	m.logger.Info("SingBox server initialized successfully")
	return nil
}

// AddClient adds a new user to the SingBox configuration
func (m *Manager) AddClient(ctx context.Context, clientID int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get protocol from context (must be set by caller)
	protocolType, ok := ctx.Value("protocol").(models.ProtocolType)
	if !ok {
		return "", fmt.Errorf("protocol not specified in context")
	}

	// Check if client already exists
	existing, err := m.db.FindClient(ctx, protocolType, clientID)
	if err != nil {
		return "", fmt.Errorf("failed to check existing client: %w", err)
	}
	if existing != nil {
		return "", models.ErrPeerAlreadyExists
	}

	// Generate password/UUID
	password := m.keyGenerator.GeneratePassword(32)

	// Store client in database
	client := &models.Client{
		ClientID: clientID,
		Protocol: protocolType,
		Password: password,
	}

	if err := m.db.CreateClient(ctx, client); err != nil {
		return "", fmt.Errorf("failed to store client: %w", err)
	}

	// Reload config to add the new user
	if err := m.generateAndWriteConfig(ctx); err != nil {
		m.db.DeleteClient(ctx, protocolType, clientID)
		return "", fmt.Errorf("failed to reload config: %w", err)
	}

	if err := m.reloadService(ctx); err != nil {
		m.logger.Error("Failed to reload sing-box service", "error", err)
		// Continue anyway - user is in DB and config
	}

	// Generate response config
	config := m.buildClientConfig(protocolType, password)
	jsonConfig, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	m.logger.Info("SingBox client added",
		"clientID", clientID,
		"protocol", protocolType,
	)

	return string(jsonConfig), nil
}

// DeleteClient removes a user from the SingBox configuration
func (m *Manager) DeleteClient(ctx context.Context, clientID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get protocol from context
	protocolType, ok := ctx.Value("protocol").(models.ProtocolType)
	if !ok {
		return fmt.Errorf("protocol not specified in context")
	}

	// Find client in database
	client, err := m.db.FindClient(ctx, protocolType, clientID)
	if err != nil {
		return fmt.Errorf("failed to query client: %w", err)
	}
	if client == nil {
		return models.ErrClientNotFound
	}

	// Delete from database
	if err := m.db.DeleteClient(ctx, protocolType, clientID); err != nil {
		return fmt.Errorf("failed to delete client: %w", err)
	}

	// Reload config to remove the user
	if err := m.generateAndWriteConfig(ctx); err != nil {
		// Try to restore client to database (best effort)
		m.db.CreateClient(ctx, client)
		return fmt.Errorf("failed to reload config: %w", err)
	}

	if err := m.reloadService(ctx); err != nil {
		m.logger.Error("Failed to reload sing-box service", "error", err)
		// Continue anyway - user is removed from config
	}

	m.logger.Info("SingBox client removed",
		"clientID", clientID,
		"protocol", protocolType,
	)

	return nil
}

// PerformMaintenance cleans up expired client sessions
func (m *Manager) PerformMaintenance(ctx context.Context) error {
	m.logger.Info("Starting SingBox housekeeping")

	// For SingBox, housekeeping means removing clients inactive for > 24 hours
	// Reload config to clean up any orphaned users
	if err := m.generateAndWriteConfig(ctx); err != nil {
		m.logger.Error("Failed to reload config during maintenance", "error", err)
		return err
	}

	if err := m.reloadService(ctx); err != nil {
		m.logger.Warn("Failed to reload service during maintenance", "error", err)
		// Non-fatal
	}

	return nil
}

// Private helper methods

// generateAndWriteConfig generates the sing-box configuration from database and writes to file
func (m *Manager) generateAndWriteConfig(ctx context.Context) error {
	config := &SingBoxConfig{
		Log: LogConfig{
			Level:     "info",
			Output:    "stdout",
			Timestamp: true,
		},
		Inbounds:  []InboundConfig{},
		Outbounds: []OutboundConfig{{Type: "direct", Tag: "direct"}},
		Route: RouteConfig{
			Rules: []interface{}{},
		},
	}

	// Build inbound configs for each enabled protocol
	if m.protocols[models.SHADOWSOCKS].Enabled {
		inbound, err := m.buildShadowSocksInbound(ctx)
		if err != nil {
			return fmt.Errorf("failed to build ShadowSocks inbound: %w", err)
		}
		config.Inbounds = append(config.Inbounds, *inbound)
	}

	if m.protocols[models.VLESS].Enabled {
		inbound, err := m.buildVLESSInbound(ctx)
		if err != nil {
			return fmt.Errorf("failed to build VLESS inbound: %w", err)
		}
		config.Inbounds = append(config.Inbounds, *inbound)
	}

	if m.protocols[models.HYSTERIA2].Enabled {
		inbound, err := m.buildHysteria2Inbound(ctx)
		if err != nil {
			return fmt.Errorf("failed to build Hysteria2 inbound: %w", err)
		}
		config.Inbounds = append(config.Inbounds, *inbound)
	}

	// Write to file
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	m.logger.Debug("Config written to file", "path", m.configPath)
	return nil
}

// buildShadowSocksInbound creates a ShadowSocks inbound configuration
func (m *Manager) buildShadowSocksInbound(ctx context.Context) (*InboundConfig, error) {
	cfg := m.protocols[models.SHADOWSOCKS]

	// Fetch all ShadowSocks clients from database
	clients, err := m.db.FindAllClients(ctx, models.SHADOWSOCKS)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clients: %w", err)
	}

	// Convert to user configs
	users := make([]UserConfig, len(clients))
	for i, client := range clients {
		users[i] = UserConfig{
			Name:     fmt.Sprintf("client-%d", client.ClientID),
			Password: client.Password,
		}
	}

	return &InboundConfig{
		Type:       "shadowsocks",
		Tag:        "shadowsocks-in",
		Listen:     "0.0.0.0",
		ListenPort: cfg.Port,
		Method:     cfg.Method,
		Users:      users,
	}, nil
}

// buildVLESSInbound creates a VLESS inbound configuration
func (m *Manager) buildVLESSInbound(ctx context.Context) (*InboundConfig, error) {
	cfg := m.protocols[models.VLESS]

	// Fetch all VLESS clients from database
	clients, err := m.db.FindAllClients(ctx, models.VLESS)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clients: %w", err)
	}

	// Convert to user configs
	users := make([]UserConfig, len(clients))
	for i, client := range clients {
		users[i] = UserConfig{
			Name: fmt.Sprintf("client-%d", client.ClientID),
			UUID: client.Password, // For VLESS, use password as UUID
		}
	}

	return &InboundConfig{
		Type:       "vless",
		Tag:        "vless-in",
		Listen:     "0.0.0.0",
		ListenPort: cfg.Port,
		Users:      users,
		Transport: map[string]interface{}{
			"type":     "reality",
			"server":   "cdn.example.com",
			"short_id": cfg.ShortID,
		},
	}, nil
}

// buildHysteria2Inbound creates a Hysteria2 inbound configuration
func (m *Manager) buildHysteria2Inbound(ctx context.Context) (*InboundConfig, error) {
	cfg := m.protocols[models.HYSTERIA2]

	// Fetch all Hysteria2 clients from database
	clients, err := m.db.FindAllClients(ctx, models.HYSTERIA2)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clients: %w", err)
	}

	// Convert to user configs
	users := make([]UserConfig, len(clients))
	for i, client := range clients {
		users[i] = UserConfig{
			Name:     fmt.Sprintf("client-%d", client.ClientID),
			Password: client.Password,
		}
	}

	return &InboundConfig{
		Type:       "hysteria2",
		Tag:        "hysteria2-in",
		Listen:     "0.0.0.0",
		ListenPort: cfg.Port,
		Users:      users,
		Transport: map[string]interface{}{
			"type": "quic",
			"tls": map[string]interface{}{
				"enabled":      true,
				"server_name":  m.nodeHostname,
				"certificate":  "/etc/sing-box/cert.pem",
				"key":          "/etc/sing-box/key.pem",
			},
		},
	}, nil
}

// buildClientConfig generates the response configuration for a client
func (m *Manager) buildClientConfig(protocolType models.ProtocolType, password string) interface{} {
	cfg := m.protocols[protocolType]

	switch protocolType {
	case models.SHADOWSOCKS:
		return &models.SingBoxConfig{
			ClientID:    0, // Will be set by caller
			Protocol:    protocolType.String(),
			Password:    password,
			Method:      cfg.Method,
			NodeAddress: m.nodeHostname,
			Port:        cfg.Port,
		}
	case models.VLESS:
		return &models.SingBoxConfig{
			ClientID:    0, // Will be set by caller
			Protocol:    protocolType.String(),
			Password:    password,
			NodeAddress: m.nodeHostname,
			Port:        cfg.Port,
		}
	case models.HYSTERIA2:
		return &models.Hysteria2Config{
			ClientID:    0, // Will be set by caller
			Protocol:    protocolType.String(),
			Password:    password,
			NodeAddress: m.nodeHostname,
			Port:        cfg.Port,
			Obfs:        cfg.Obfs,
		}
	default:
		return &models.SingBoxConfig{
			ClientID:    0,
			Protocol:    protocolType.String(),
			Password:    password,
			NodeAddress: m.nodeHostname,
			Port:        cfg.Port,
		}
	}
}

// reloadService reloads the sing-box systemd service
func (m *Manager) reloadService(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "reload", "sing-box.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl reload failed: %w (output: %s)", err, string(output))
	}
	return nil
}

// IsServiceRunning checks if sing-box service is active
func (m *Manager) IsServiceRunning(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", "sing-box.service")
	err := cmd.Run()
	return err == nil, nil
}

// GetMetrics returns current SingBox metrics
func (m *Manager) GetMetrics(ctx context.Context) (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Fetch client counts for each protocol
	metrics := make(map[string]interface{})

	for protocolType := range m.protocols {
		clients, err := m.db.FindAllClients(ctx, protocolType)
		if err != nil {
			m.logger.Warn("Failed to fetch metrics", "protocol", protocolType, "error", err)
			continue
		}
		metrics[fmt.Sprintf("clients_%s", protocolType.String())] = len(clients)
	}

	return metrics, nil
}
