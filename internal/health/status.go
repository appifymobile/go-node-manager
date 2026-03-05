package health

import "time"

const (
	StatusHealthy  = "healthy"
	StatusDegraded = "degraded"
	StatusDown     = "down"
)

const (
	maintenanceStaleThreshold = 2 * time.Hour
	errorRateDegradedMin      = 0.01    // 1%
	errorRateDegradedMax      = 0.10    // 10%
	errorRateDownMin          = 0.10    // 10%
)

// ComputeStatus determines node health status based on metrics and database connectivity
func ComputeStatus(dbErr error, totalErrors, totalCreated int64, lastMaint time.Time) string {
	// If database is unreachable, status is down
	if dbErr != nil {
		return StatusDown
	}

	// Calculate error rate
	var errorRate float64
	if totalCreated > 0 {
		errorRate = float64(totalErrors) / float64(totalCreated)
	}

	// If error rate >= 10%, status is down
	if errorRate >= errorRateDownMin {
		return StatusDown
	}

	// Check if maintenance is stale (> 2 hours)
	maintenanceStale := time.Since(lastMaint) > maintenanceStaleThreshold

	// If error rate >= 1% or maintenance stale, status is degraded
	if errorRate >= errorRateDegradedMin || maintenanceStale {
		return StatusDegraded
	}

	// Otherwise, status is healthy
	return StatusHealthy
}
