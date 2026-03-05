package ippool

import (
	"testing"
)

func TestAllocatorCreation(t *testing.T) {
	allocator, err := New("10.37.0.1/16")
	if err != nil {
		t.Fatalf("Failed to create allocator: %v", err)
	}

	if allocator == nil {
		t.Fatal("Allocator is nil")
	}
}

func TestNextAddress(t *testing.T) {
	allocator, _ := New("10.37.0.1/16")

	ip1, err := allocator.NextAddress()
	if err != nil {
		t.Fatalf("Failed to get first IP: %v", err)
	}

	if ip1 == "" {
		t.Fatal("Got empty IP address")
	}

	// Second call should give different IP
	ip2, err := allocator.NextAddress()
	if err != nil {
		t.Fatalf("Failed to get second IP: %v", err)
	}

	if ip1 == ip2 {
		t.Fatalf("Got same IP twice: %s", ip1)
	}
}

func TestReleaseAddress(t *testing.T) {
	allocator, _ := New("10.37.0.1/16")

	ip, _ := allocator.NextAddress()
	allocated := allocator.GetAllocated()
	if len(allocated) != 1 {
		t.Fatalf("Expected 1 allocated IP, got %d", len(allocated))
	}

	err := allocator.ReleaseAddress(ip)
	if err != nil {
		t.Fatalf("Failed to release IP: %v", err)
	}

	allocated = allocator.GetAllocated()
	if len(allocated) != 0 {
		t.Fatalf("Expected 0 allocated IPs after release, got %d", len(allocated))
	}
}
