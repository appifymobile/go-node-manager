# VVPN Node Manager (Go)

High-performance VPN node manager written in Go, replacing the Kotlin/Spring Boot implementation.

## Architecture

- **WireGuard Management**: Uses `wgctrl` library for efficient peer management (no shell commands)
- **Database**: PostgreSQL for client and protocol configuration storage
- **HTTP API**: Mirrors Kotlin implementation (POST /api/v1/clients/{id}/{type}/{connect|disconnect})
- **Housekeeping**: Goroutine-based periodic cleanup of inactive peers

## Project Structure

```
cmd/node-manager/           # Entry point
internal/
  api/                      # HTTP request handlers
  config/                   # Configuration management
  models/                   # Data models and error types
  service/
    wireguard/              # WireGuard protocol implementation
    singbox/                # SingBox/ShadowSocks implementation (Phase 2b)
    ippool/                 # IP address allocation
  storage/                  # PostgreSQL operations
migrations/                 # Database schema
config.yaml                 # Default configuration
Dockerfile                  # Container build
```

## Quick Start

### Prerequisites

- Go 1.22+
- PostgreSQL 12+
- WireGuard kernel module (Linux)

### Build

```bash
go mod download
CGO_ENABLED=1 go build -o node-manager ./cmd/node-manager
```

### Run

```bash
# Create database and schema
psql -h localhost -U postgres -c "CREATE DATABASE vvpn"
psql -h localhost -U vvpn -d vvpn < migrations/schema.sql

# Start server
export VPN_NODE_PUBLIC_HOSTNAME=vpn-nl-1.vvpn.pro
export DB_USER=vvpn
export DB_PASSWORD=secure_password
./node-manager -config config.yaml
```

### Docker Build

```bash
docker build -t vvpn/node-manager:latest .
docker run -d \
  -e DB_HOST=postgres \
  -e DB_USER=vvpn \
  -e DB_PASSWORD=secure_password \
  -e VPN_NODE_PUBLIC_HOSTNAME=vpn-nl-1.vvpn.pro \
  -p 8080:8080 \
  vvpn/node-manager:latest
```

## API Endpoints

### Add Client
```
POST /api/v1/clients/{clientId}/{type}/connect
Content-Type: application/json

Response (WireGuard):
{
  "clientId": 12345,
  "iface": "wg0",
  "publicKey": "...",
  "privateKey": "...",
  "peerPublicKey": "...",
  "ipAddress": "10.37.0.5/16",
  "nodeAddress": "vpn-nl-1.vvpn.pro:51820",
  "mtu": 1420,
  "dns": "1.1.1.1, 1.0.0.1"
}
```

### Remove Client
```
POST /api/v1/clients/{clientId}/{type}/disconnect

Response: 204 No Content
```

### Health Checks
```
GET /manage/health
GET /manage/health/readiness
GET /manage/health/liveness
```

## Configuration

See `config.yaml` for all available options. Environment variables override YAML values.

Key settings:
- `VPN_WIREGUARD_IFACE`: Interface name (default: wg0)
- `VPN_WIREGUARD_PORT`: Listen port (default: 51820)
- `VPN_WIREGUARD_ADDRESS`: IP pool CIDR (default: 10.37.0.1/16)
- `DB_HOST`, `DB_USER`, `DB_PASSWORD`: Database credentials
- `VPN_NODE_PUBLIC_HOSTNAME`: Public hostname for client configs

## Development

### Run Tests
```bash
go test ./...
```

### Generate WireGuard Keys
```bash
go run ./cmd/node-manager -generate-keys
```

### Database Migrations
Migrations are applied automatically on startup if not already applied.

## Performance Characteristics

| Metric | Target | Notes |
|--------|--------|-------|
| Binary Size | <50MB | Static binary |
| Startup RAM | <10MB | Go runtime overhead |
| Idle RAM | 30-50MB | Includes connection pool |
| Handshake Latency | <10ms | HTTP + DB query |
| Max Clients | 65k | IP pool size (/16) |

## Deployment (DigitalOcean 1vCPU-2GB)

### Initial Setup
```bash
# Create vvpn user
useradd -m vvpn

# Copy binary
scp node-manager root@vpn-nl-1:/usr/local/bin/
chmod +x /usr/local/bin/node-manager

# Create systemd unit
cat > /etc/systemd/system/node-manager.service << EOF
[Unit]
Description=VVPN Node Manager
After=network.target postgresql.service

[Service]
Type=simple
User=vvpn
ExecStart=/usr/local/bin/node-manager -config /etc/node-manager/config.yaml
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable node-manager
systemctl start node-manager
```

### Rolling Updates (Blue-Green Deployment)
1. Build and test on canary node (1 node)
2. Monitor metrics for 1 hour
3. Route 5 nodes to Go version (25% traffic)
4. Monitor for 2 hours
5. Expand to 50%, then 100%

## Monitoring

### Prometheus Metrics (Phase 2b)
- `active_clients_total` (gauge) - Connected clients per protocol
- `ip_pool_utilization` (gauge) - Allocated IPs / total pool size
- `request_duration_seconds` (histogram) - HTTP handler latencies
- `database_connections_active` (gauge) - Active DB connections

### Logs
- JSON structured logs to stdout
- Log level configurable via `LOG_LEVEL` env var
- Sample logs: `{"time":"2024-01-15T10:23:45Z","level":"INFO","msg":"Client connected","clientID":12345}`

## Migration from Kotlin

### Phase 2a (Weeks 1-2): Current
- WireGuard manager with wgctrl
- HTTP API handlers
- PostgreSQL integration
- Housekeeping ticker

### Phase 2b (Week 3): Next
- SingBox/ShadowSocks manager
- Prometheus metrics export
- Complete API parity
- Canary deployment

### Phase 2c (Week 4): Later
- Integration tests with real WireGuard
- Load testing
- Production rollout (5 → 17 nodes)

## Troubleshooting

### "wgctrl: permission denied"
Ensure the process runs as root or with CAP_NET_ADMIN capability:
```bash
setcap cap_net_admin=ep /usr/local/bin/node-manager
```

### "database connection refused"
Check PostgreSQL is running and credentials are correct:
```bash
psql -h $DB_HOST -U $DB_USER -d vvpn -c "SELECT 1"
```

### "IP pool exhausted"
The default /16 network provides 65k addresses. If exhausted, expand:
```yaml
vpn:
  wireguard:
    address: 10.37.0.1/15  # 131k addresses
```

## License

Same as VVPN project (see parent README)

## Authors

Rewritten from Kotlin/Spring Boot to Go for VVPN 2.0 (Phase 2, Q1 2024)
