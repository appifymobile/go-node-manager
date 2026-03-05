package health

import (
	"errors"
	"testing"
	"time"
)

func TestComputeStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		dbErr        error
		totalErrors  int64
		totalCreated int64
		lastMaint    time.Time
		expected     string
	}{
		{
			name:         "database error",
			dbErr:        errors.New("connection refused"),
			totalErrors:  0,
			totalCreated: 100,
			lastMaint:    now,
			expected:     StatusDown,
		},
		{
			name:         "error rate >= 10%",
			dbErr:        nil,
			totalErrors:  15,
			totalCreated: 100,
			lastMaint:    now,
			expected:     StatusDown,
		},
		{
			name:         "error rate between 1% and 10%",
			dbErr:        nil,
			totalErrors:  5,
			totalCreated: 100,
			lastMaint:    now,
			expected:     StatusDegraded,
		},
		{
			name:         "maintenance stale > 2 hours",
			dbErr:        nil,
			totalErrors:  0,
			totalCreated: 100,
			lastMaint:    now.Add(-3 * time.Hour),
			expected:     StatusDegraded,
		},
		{
			name:         "healthy status",
			dbErr:        nil,
			totalErrors:  0,
			totalCreated: 100,
			lastMaint:    now,
			expected:     StatusHealthy,
		},
		{
			name:         "healthy with low error rate",
			dbErr:        nil,
			totalErrors:  0,
			totalCreated: 1000,
			lastMaint:    now,
			expected:     StatusHealthy,
		},
		{
			name:         "zero clients created",
			dbErr:        nil,
			totalErrors:  0,
			totalCreated: 0,
			lastMaint:    now,
			expected:     StatusHealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeStatus(tt.dbErr, tt.totalErrors, tt.totalCreated, tt.lastMaint)
			if result != tt.expected {
				t.Fatalf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
