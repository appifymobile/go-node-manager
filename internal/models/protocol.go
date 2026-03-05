package models

import "fmt"

type ProtocolType string

const (
	WIREGUARD   ProtocolType = "WIREGUARD"
	SHADOWSOCKS ProtocolType = "SHADOWSOCKS"
	VLESS       ProtocolType = "VLESS"
)

func (p ProtocolType) String() string {
	return string(p)
}

func (p ProtocolType) IsValid() bool {
	switch p {
	case WIREGUARD, SHADOWSOCKS, VLESS:
		return true
	default:
		return false
	}
}

// Protocol entity - stores server configuration (keys, interface)
type Protocol struct {
	Type       ProtocolType
	Iface      string
	PrivateKey string
	PublicKey  string
}

// Client entity - stores client connection info
type Client struct {
	ClientID      int64
	Protocol      ProtocolType
	Password      string // public key for WireGuard, password for SingBox protocols
	ConnectedTime int64  // Unix timestamp
}

// WireguardConfig - JSON response for /connect endpoint
type WireguardConfig struct {
	ClientID      int64  `json:"clientId"`
	Iface         string `json:"iface"`
	PublicKey     string `json:"publicKey"`
	PrivateKey    string `json:"privateKey"`
	PeerPublicKey string `json:"peerPublicKey"`
	IPAddress     string `json:"ipAddress"`
	NodeAddress   string `json:"nodeAddress"`
	MTU           int    `json:"mtu,omitempty"`
	DNS           string `json:"dns,omitempty"`
}

// SingBoxConfig - JSON response for ShadowSocks/VLESS /connect endpoint
type SingBoxConfig struct {
	ClientID    int64  `json:"clientId"`
	Protocol    string `json:"protocol"`
	Password    string `json:"password"`
	Method      string `json:"method,omitempty"` // For ShadowSocks
	NodeAddress string `json:"nodeAddress"`
	Port        uint16 `json:"port"`
}

type VPNError struct {
	Code    string
	Message string
	Err     error
}

func (e *VPNError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Common errors
var (
	ErrPeerAlreadyExists = &VPNError{Code: "PEER_EXISTS", Message: "peer already exists"}
	ErrIPPoolExhausted   = &VPNError{Code: "IP_POOL_FULL", Message: "IP pool exhausted"}
	ErrWgctlFailed       = &VPNError{Code: "WGCTRL_FAILED", Message: "WireGuard control failed"}
	ErrClientNotFound    = &VPNError{Code: "CLIENT_NOT_FOUND", Message: "client not found"}
	ErrProtocolDisabled  = &VPNError{Code: "PROTOCOL_DISABLED", Message: "protocol is disabled"}
)
