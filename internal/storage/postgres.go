package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go-node-manager/internal/models"
)

type DB struct {
	pool *pgxpool.Pool
}

// New creates a new database connection pool
func New(ctx context.Context, host string, port int, user, password, dbname string, maxConns int) (*DB, error) {
	config, err := pgxpool.ParseConfig(fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=require",
		user, password, host, port, dbname,
	))
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	config.MaxConns = int32(maxConns)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Close closes the connection pool
func (d *DB) Close() {
	d.pool.Close()
}

// PingDB checks database connectivity and returns latency in milliseconds
func (d *DB) PingDB(ctx context.Context) (latencyMs int64, err error) {
	start := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err = d.pool.Ping(pingCtx)
	latencyMs = time.Since(start).Milliseconds()
	return
}

// GetPool returns the underlying pgxpool.Pool for advanced queries
func (d *DB) GetPool() *pgxpool.Pool {
	return d.pool
}

// Protocol Repository

// FindProtocol retrieves a protocol configuration by type
func (d *DB) FindProtocol(ctx context.Context, protocolType models.ProtocolType) (*models.Protocol, error) {
	var p models.Protocol
	err := d.pool.QueryRow(ctx,
		"SELECT type, iface, private_key, public_key FROM protocols WHERE type = $1",
		protocolType.String(),
	).Scan(&p.Type, &p.Iface, &p.PrivateKey, &p.PublicKey)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query protocol: %w", err)
	}

	return &p, nil
}

// CreateProtocol stores a new protocol configuration
func (d *DB) CreateProtocol(ctx context.Context, p *models.Protocol) error {
	_, err := d.pool.Exec(ctx,
		"INSERT INTO protocols (type, iface, private_key, public_key) VALUES ($1, $2, $3, $4)",
		p.Type.String(), p.Iface, p.PrivateKey, p.PublicKey,
	)
	if err != nil {
		return fmt.Errorf("failed to create protocol: %w", err)
	}
	return nil
}

// UpdateProtocol updates an existing protocol configuration
func (d *DB) UpdateProtocol(ctx context.Context, p *models.Protocol) error {
	result, err := d.pool.Exec(ctx,
		"UPDATE protocols SET iface = $1, private_key = $2, public_key = $3 WHERE type = $4",
		p.Iface, p.PrivateKey, p.PublicKey, p.Type.String(),
	)
	if err != nil {
		return fmt.Errorf("failed to update protocol: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("protocol not found: %s", p.Type)
	}
	return nil
}

// Client Repository

// FindClient retrieves a client by protocol and client ID
func (d *DB) FindClient(ctx context.Context, protocolType models.ProtocolType, clientID int64) (*models.Client, error) {
	var c models.Client
	err := d.pool.QueryRow(ctx,
		"SELECT client_id, protocol, password, connected_time FROM clients WHERE protocol = $1 AND client_id = $2",
		protocolType.String(), clientID,
	).Scan(&c.ClientID, &c.Protocol, &c.Password, &c.ConnectedTime)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query client: %w", err)
	}

	return &c, nil
}

// FindAllClients retrieves all clients for a given protocol
func (d *DB) FindAllClients(ctx context.Context, protocolType models.ProtocolType) ([]models.Client, error) {
	rows, err := d.pool.Query(ctx,
		"SELECT client_id, protocol, password, connected_time FROM clients WHERE protocol = $1",
		protocolType.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query clients: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var c models.Client
		if err := rows.Scan(&c.ClientID, &c.Protocol, &c.Password, &c.ConnectedTime); err != nil {
			return nil, fmt.Errorf("failed to scan client: %w", err)
		}
		clients = append(clients, c)
	}

	return clients, rows.Err()
}

// FindExpiredClients retrieves clients inactive for more than the specified duration
func (d *DB) FindExpiredClients(ctx context.Context, protocolType models.ProtocolType, inactiveFor time.Duration) ([]models.Client, error) {
	cutoffTime := time.Now().Add(-inactiveFor).Unix()
	rows, err := d.pool.Query(ctx,
		"SELECT client_id, protocol, password, connected_time FROM clients WHERE protocol = $1 AND connected_time < $2",
		protocolType.String(), cutoffTime,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired clients: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var c models.Client
		if err := rows.Scan(&c.ClientID, &c.Protocol, &c.Password, &c.ConnectedTime); err != nil {
			return nil, fmt.Errorf("failed to scan client: %w", err)
		}
		clients = append(clients, c)
	}

	return clients, rows.Err()
}

// CreateClient stores a new client connection
func (d *DB) CreateClient(ctx context.Context, c *models.Client) error {
	_, err := d.pool.Exec(ctx,
		"INSERT INTO clients (client_id, protocol, password, connected_time) VALUES ($1, $2, $3, $4)",
		c.ClientID, c.Protocol.String(), c.Password, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	return nil
}

// UpdateClientConnectedTime updates the last connected timestamp
func (d *DB) UpdateClientConnectedTime(ctx context.Context, protocolType models.ProtocolType, clientID int64) error {
	result, err := d.pool.Exec(ctx,
		"UPDATE clients SET connected_time = $1 WHERE protocol = $2 AND client_id = $3",
		time.Now().Unix(), protocolType.String(), clientID,
	)
	if err != nil {
		return fmt.Errorf("failed to update client: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("client not found")
	}
	return nil
}

// DeleteClient removes a client connection
func (d *DB) DeleteClient(ctx context.Context, protocolType models.ProtocolType, clientID int64) error {
	_, err := d.pool.Exec(ctx,
		"DELETE FROM clients WHERE protocol = $1 AND client_id = $2",
		protocolType.String(), clientID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete client: %w", err)
	}
	return nil
}

// DeleteClients removes multiple clients
func (d *DB) DeleteClients(ctx context.Context, clients []models.Client) error {
	if len(clients) == 0 {
		return nil
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, c := range clients {
		if _, err := tx.Exec(ctx,
			"DELETE FROM clients WHERE protocol = $1 AND client_id = $2",
			c.Protocol.String(), c.ClientID,
		); err != nil {
			return fmt.Errorf("failed to delete client %d: %w", c.ClientID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
