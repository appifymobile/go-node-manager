package health

import "time"

// HealthSnapshot represents a point-in-time snapshot of node health metrics
type HealthSnapshot struct {
	TotalActiveConnections int64
	TotalErrors            int64
	TotalClientsCreated    int64
	LastMaintenanceTime    time.Time
}
