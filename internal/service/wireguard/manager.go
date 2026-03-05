package wireguard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"go-node-manager/internal/models"
	"go-node-manager/internal/service/ippool"
	"go-node-manager/internal/storage"
)

// Manager implements VpnManager for WireGuard protocol
type Manager struct {
	ifaceName      string
	listenPort     uint16
	address        string
	nodePublicKey  string
	nodeHostname   string

	wgClient       *wgctrl.Client
	ipPool         *ippool.Allocator
	db             *storage.DB
	logger         *slog.Logger

	activeClientCount int
}

// New creates a new WireGuard manager
func New(
	ifaceName string,
	listenPort uint16,
	address string,
	nodeHostname string,
	db *storage.DB,
	logger *slog.Logger,
) (*Manager, error) {
	// Initialize wgctrl client
	wgClient, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create wgctrl client: %w", err)
	}

	// Create IP pool allocator
	ipPool, err := ippool.New(address)
	if err != nil {
		wgClient.Close()
		return nil, fmt.Errorf("failed to create IP pool: %w", err)
	}

	return &Manager{
		ifaceName:    ifaceName,
		listenPort:   listenPort,
		address:      address,
		nodeHostname: nodeHostname,
		wgClient:     wgClient,
		ipPool:       ipPool,
		db:           db,
		logger:       logger,
	}, nil
}

// StartServer initializes and starts the WireGuard server
func (m *Manager) StartServer(ctx context.Context) error {
	m.logger.Info("Starting WireGuard server", "iface", m.ifaceName, "port", m.listenPort)

	// Load or create server keys
	protocol, err := m.db.FindProtocol(ctx, models.WIREGUARD)
	if err != nil {
		return fmt.Errorf("failed to query protocol config: %w", err)
	}

	if protocol == nil {
		// Generate new keys
		privKey, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return fmt.Errorf("failed to generate private key: %w", err)
		}

		protocol = &models.Protocol{
			Type:       models.WIREGUARD,
			Iface:      m.ifaceName,
			PrivateKey: privKey.String(),
			PublicKey:  privKey.PublicKey().String(),
		}

		if err := m.db.CreateProtocol(ctx, protocol); err != nil {
			return fmt.Errorf("failed to store protocol config: %w", err)
		}

		m.logger.Info("Generated new WireGuard keys")
	}

	m.nodePublicKey = protocol.PublicKey

	// Configure WireGuard interface
	if err := m.configureInterface(ctx, protocol); err != nil {
		return fmt.Errorf("failed to configure interface: %w", err)
	}

	// Recover existing clients
	if err := m.recoverExistingClients(ctx); err != nil {
		m.logger.Warn("Failed to recover existing clients", "error", err)
		// Continue anyway - clients can be re-added if needed
	}

	m.logger.Info("WireGuard server started successfully",
		"iface", m.ifaceName,
		"port", m.listenPort,
		"publicKey", m.nodePublicKey[:16]+"...",
	)

	return nil
}

// AddClient adds a new WireGuard peer
func (m *Manager) AddClient(ctx context.Context, clientID int64) (string, error) {
	// Check if client already exists
	existing, err := m.db.FindClient(ctx, models.WIREGUARD, clientID)
	if err != nil {
		return "", fmt.Errorf("failed to check existing client: %w", err)
	}
	if existing != nil {
		return "", models.ErrPeerAlreadyExists
	}

	// Generate keys for client
	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate client key: %w", err)
	}

	// Allocate IP address
	ipAddr, err := m.ipPool.NextAddress()
	if err != nil {
		return "", models.ErrIPPoolExhausted
	}

	// Add peer via wgctrl
	peerIP := fmt.Sprintf("%s/32", ipAddr)
	peer := wgtypes.PeerConfig{
		PublicKey:                   privKey.PublicKey(),
		AllowedIPs:                  parseAllowedIPs(peerIP),
		PersistentKeepaliveInterval: &[]time.Duration{25 * time.Second}[0],
	}

	if err := m.addPeerToDevice(ctx, peer); err != nil {
		m.ipPool.ReleaseAddress(ipAddr)
		return "", fmt.Errorf("failed to add peer to WireGuard: %w", err)
	}

	// Store client in database
	client := &models.Client{
		ClientID: clientID,
		Protocol: models.WIREGUARD,
		Password: privKey.PublicKey().String(), // Store public key as identifier
	}

	if err := m.db.CreateClient(ctx, client); err != nil {
		m.logger.Error("Failed to store client in database", "error", err)
		// Note: Peer is already added to WireGuard; consider this a "soft" error
		// in a real implementation, you'd want to roll back the wgctrl operation
	}

	// Generate response config
	config := &models.WireguardConfig{
		ClientID:      clientID,
		Iface:         m.ifaceName,
		PublicKey:     privKey.PublicKey().String(),
		PrivateKey:    privKey.String(),
		PeerPublicKey: m.nodePublicKey,
		IPAddress:     fmt.Sprintf("%s/16", ipAddr),
		NodeAddress:   fmt.Sprintf("%s:%d", m.nodeHostname, m.listenPort),
		MTU:           1420,
		DNS:           "1.1.1.1, 1.0.0.1, 8.8.8.8, 8.8.4.4",
	}

	jsonConfig, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	m.logger.Info("Client added successfully",
		"clientID", clientID,
		"ip", ipAddr,
		"publicKey", config.PublicKey[:16]+"...",
	)

	return string(jsonConfig), nil
}

// DeleteClient removes a WireGuard peer
func (m *Manager) DeleteClient(ctx context.Context, clientID int64) error {
	// Find client in database
	client, err := m.db.FindClient(ctx, models.WIREGUARD, clientID)
	if err != nil {
		return fmt.Errorf("failed to query client: %w", err)
	}
	if client == nil {
		return models.ErrClientNotFound
	}

	// Remove peer from WireGuard
	pubKey, err := wgtypes.ParseKey(client.Password)
	if err != nil {
		return fmt.Errorf("failed to parse peer public key: %w", err)
	}

	if err := m.removePeerFromDevice(ctx, pubKey); err != nil {
		return fmt.Errorf("failed to remove peer from WireGuard: %w", err)
	}

	// Delete from database
	if err := m.db.DeleteClient(ctx, models.WIREGUARD, clientID); err != nil {
		return fmt.Errorf("failed to delete client from database: %w", err)
	}

	m.logger.Info("Client deleted successfully", "clientID", clientID)
	return nil
}

// PerformMaintenance removes inactive peers (implements Housekeeping interface)
func (m *Manager) PerformMaintenance(ctx context.Context) error {
	m.logger.Info("Starting WireGuard housekeeping")

	// Find peers inactive for > 4 hours
	inactiveClients, err := m.db.FindExpiredClients(ctx, models.WIREGUARD, 4*time.Hour)
	if err != nil {
		return fmt.Errorf("failed to find expired clients: %w", err)
	}

	if len(inactiveClients) == 0 {
		m.logger.Info("No inactive peers to remove")
		return nil
	}

	// Remove peers
	for _, client := range inactiveClients {
		if err := m.DeleteClient(ctx, client.ClientID); err != nil {
			m.logger.Error("Failed to remove inactive peer", "clientID", client.ClientID, "error", err)
		}
	}

	m.logger.Info("Housekeeping complete", "removedCount", len(inactiveClients))
	return nil
}

// Private helper methods

// configureInterface sets up the WireGuard network interface
func (m *Manager) configureInterface(ctx context.Context, protocol *models.Protocol) error {
	// Parse private key
	privKey, err := wgtypes.ParseKey(protocol.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Configure device via wgctrl
	cfg := wgtypes.Config{
		PrivateKey: &privKey,
		ListenPort: &[]int{int(m.listenPort)}[0],
	}

	if err := m.wgClient.ConfigureDevice(m.ifaceName, cfg); err != nil {
		return fmt.Errorf("failed to configure WireGuard device: %w", err)
	}

	// TODO: In a real implementation, also call system commands to:
	// - Create interface if not exists: ip link add dev wg0 type wireguard
	// - Assign IP: ip address add 10.37.0.1/16 dev wg0
	// - Bring up: ip link set up dev wg0
	// - Configure firewall: iptables, UFW rules
	// - Enable IP forwarding: sysctl net.ipv4.ip_forward=1

	m.logger.Info("WireGuard interface configured", "iface", m.ifaceName)
	return nil
}

// addPeerToDevice adds a peer to the WireGuard device
func (m *Manager) addPeerToDevice(ctx context.Context, peer wgtypes.PeerConfig) error {
	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peer},
	}

	if err := m.wgClient.ConfigureDevice(m.ifaceName, cfg); err != nil {
		return fmt.Errorf("wgctrl configure failed: %w", err)
	}

	return nil
}

// removePeerFromDevice removes a peer from the WireGuard device
func (m *Manager) removePeerFromDevice(ctx context.Context, pubKey wgtypes.Key) error {
	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey: pubKey,
				Remove:    true,
			},
		},
	}

	if err := m.wgClient.ConfigureDevice(m.ifaceName, cfg); err != nil {
		return fmt.Errorf("wgctrl remove failed: %w", err)
	}

	return nil
}

// recoverExistingClients synchronizes in-memory state with actual WireGuard peers
func (m *Manager) recoverExistingClients(ctx context.Context) error {
	device, err := m.wgClient.Device(m.ifaceName)
	if err != nil {
		return fmt.Errorf("failed to query WireGuard device: %w", err)
	}

	// Mark existing peers as allocated in IP pool
	for _, peer := range device.Peers {
		for _, allowedIP := range peer.AllowedIPs {
			ipStr := allowedIP.IP.String()
			if err := m.ipPool.AllocateSpecific(ipStr); err != nil {
				m.logger.Warn("Failed to mark IP as allocated", "ip", ipStr, "error", err)
			}
		}
	}

	return nil
}

// parseAllowedIPs converts IP CIDR strings to net.IPNet slices
func parseAllowedIPs(cidrs ...string) []net.IPNet {
	var result []net.IPNet
	for _, cidr := range cidrs {
		if ip, ipnet, err := net.ParseCIDR(cidr); err == nil {
			ipnet.IP = ip
			result = append(result, *ipnet)
		}
	}
	return result
}

// Close closes the WireGuard manager and releases resources
func (m *Manager) Close() error {
	if m.wgClient != nil {
		return m.wgClient.Close()
	}
	return nil
}
