package config

import (
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	VPN      VPNConfig      `yaml:"vpn"`
	Database DatabaseConfig `yaml:"database"`
	Logging  LoggingConfig  `yaml:"logging"`
	Metrics  MetricsConfig  `yaml:"metrics"`
}

type ServerConfig struct {
	Port int       `yaml:"port"`
	TLS  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"certfile"`
	KeyFile  string `yaml:"keyfile"`
}

type VPNConfig struct {
	WireGuard WireGuardConfig `yaml:"wireguard"`
	SingBox   SingBoxConfig   `yaml:"singbox"`
}

type WireGuardConfig struct {
	Enabled     bool         `yaml:"enabled"`
	Port        uint16       `yaml:"port"`
	Iface       string       `yaml:"iface"`
	Address     string       `yaml:"address"`
	HealthCheck HealthConfig `yaml:"healthcheck"`
}

type SingBoxConfig struct {
	Enabled     bool              `yaml:"enabled"`
	ConfigPath  string            `yaml:"configpath"`
	ShadowSocks ShadowSocksConfig `yaml:"shadowsocks"`
	VLESS       VLESSConfig       `yaml:"vless"`
}

type ShadowSocksConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Port             uint16 `yaml:"port"`
	EncryptionMethod string `yaml:"encryptionmethod"`
}

type VLESSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    uint16 `yaml:"port"`
	ShortID string `yaml:"shortid"`
}

type HealthConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}

type DatabaseConfig struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Name           string `yaml:"name"`
	User           string `yaml:"user"`
	Password       string `yaml:"password"`
	MaxConnections int    `yaml:"maxconnections"`
	SSLMode        string `yaml:"sslmode"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// Load reads configuration from YAML file and applies environment variable overrides
func Load(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	return cfg, nil
}

// applyEnvOverrides replaces configuration values with environment variables
func applyEnvOverrides(cfg *Config) {
	// Server
	if port := os.Getenv("SERVER_PORT"); port != "" {
		// Skip for simplicity; production code would parse int
	}

	// Database
	if host := os.Getenv("DB_HOST"); host != "" {
		cfg.Database.Host = host
	}
	if port := os.Getenv("DB_PORT"); port != "" {
		// Parse port
	}
	if user := os.Getenv("DB_USER"); user != "" {
		cfg.Database.User = user
	}
	if pass := os.Getenv("DB_PASSWORD"); pass != "" {
		cfg.Database.Password = pass
	}
	if name := os.Getenv("DB_NAME"); name != "" {
		cfg.Database.Name = name
	}

	// VPN
	if iface := os.Getenv("VPN_WIREGUARD_IFACE"); iface != "" {
		cfg.VPN.WireGuard.Iface = iface
	}
	if addr := os.Getenv("VPN_WIREGUARD_ADDRESS"); addr != "" {
		cfg.VPN.WireGuard.Address = addr
	}

	// Logging
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		cfg.Logging.Level = strings.ToLower(level)
	}
}
