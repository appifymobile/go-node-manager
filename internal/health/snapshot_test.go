package health

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"go-node-manager/internal/models"
)

func TestGetHealthSnapshot(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	collector := NewMetricsCollector(nil, logger)

	// Initial snapshot should have zero values
	snapshot := collector.GetHealthSnapshot()
	if snapshot.TotalActiveConnections != 0 {
		t.Fatalf("Expected 0 active connections, got %d", snapshot.TotalActiveConnections)
	}
	if snapshot.TotalErrors != 0 {
		t.Fatalf("Expected 0 errors, got %d", snapshot.TotalErrors)
	}
	if snapshot.TotalClientsCreated != 0 {
		t.Fatalf("Expected 0 clients created, got %d", snapshot.TotalClientsCreated)
	}

	// Record some client activity
	collector.RecordClientAdded(models.WIREGUARD)
	collector.RecordClientAdded(models.SHADOWSOCKS)
	collector.RecordError()
	collector.RecordError()

	snapshot = collector.GetHealthSnapshot()
	if snapshot.TotalActiveConnections != 2 {
		t.Fatalf("Expected 2 active connections, got %d", snapshot.TotalActiveConnections)
	}
	if snapshot.TotalClientsCreated != 2 {
		t.Fatalf("Expected 2 clients created, got %d", snapshot.TotalClientsCreated)
	}
	if snapshot.TotalErrors != 2 {
		t.Fatalf("Expected 2 errors, got %d", snapshot.TotalErrors)
	}

	// Record removal
	collector.RecordClientRemoved(models.WIREGUARD)

	snapshot = collector.GetHealthSnapshot()
	if snapshot.TotalActiveConnections != 1 {
		t.Fatalf("Expected 1 active connection after removal, got %d", snapshot.TotalActiveConnections)
	}

	// Record maintenance
	beforeMaint := time.Now()
	collector.RecordMaintenanceCompleted()
	afterMaint := time.Now()

	snapshot = collector.GetHealthSnapshot()
	if snapshot.LastMaintenanceTime.Before(beforeMaint) || snapshot.LastMaintenanceTime.After(afterMaint) {
		t.Fatalf("Maintenance time not in expected range")
	}
}

func TestGetHealthSnapshotConcurrency(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	collector := NewMetricsCollector(nil, logger)

	// Concurrent writes and reads
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			collector.RecordClientAdded(models.WIREGUARD)
			collector.RecordError()
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = collector.GetHealthSnapshot()
		}
		done <- true
	}()

	<-done
	<-done

	snapshot := collector.GetHealthSnapshot()
	if snapshot.TotalClientsCreated != 100 {
		t.Fatalf("Expected 100 clients created, got %d", snapshot.TotalClientsCreated)
	}
	if snapshot.TotalErrors != 100 {
		t.Fatalf("Expected 100 errors, got %d", snapshot.TotalErrors)
	}
}
