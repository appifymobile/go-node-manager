-- VPN Node Manager Schema

-- Protocols table: stores server configuration for each protocol type
CREATE TABLE IF NOT EXISTS protocols (
    type VARCHAR(32) PRIMARY KEY,
    iface VARCHAR(16) NOT NULL,
    private_key TEXT NOT NULL,
    public_key TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Clients table: stores client connections
CREATE TABLE IF NOT EXISTS clients (
    id SERIAL PRIMARY KEY,
    client_id BIGINT NOT NULL,
    protocol VARCHAR(32) NOT NULL,
    password TEXT NOT NULL,           -- Public key for WireGuard, password for SingBox
    connected_time BIGINT NOT NULL,    -- Unix timestamp
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(protocol, client_id),
    FOREIGN KEY (protocol) REFERENCES protocols(type)
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_clients_protocol_client_id ON clients(protocol, client_id);
CREATE INDEX IF NOT EXISTS idx_clients_connected_time ON clients(connected_time);

-- IP Allocations table (optional, for persistent IP tracking)
CREATE TABLE IF NOT EXISTS ip_allocations (
    ip_address INET PRIMARY KEY,
    protocol VARCHAR(32) NOT NULL,
    allocated BOOLEAN DEFAULT TRUE,
    allocated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (protocol) REFERENCES protocols(type)
);

CREATE INDEX IF NOT EXISTS idx_ip_allocations_protocol ON ip_allocations(protocol);
CREATE INDEX IF NOT EXISTS idx_ip_allocations_allocated ON ip_allocations(allocated);
