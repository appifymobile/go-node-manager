package ippool

import (
	"context"
	"fmt"
	"net"
	"sync"
)

// Allocator manages IP address allocation from a pool
type Allocator struct {
	baseIP    net.IP
	maxIP     net.IP
	allocated map[string]bool
	mu        sync.Mutex
}

// New creates a new IP allocator for the given CIDR block
// Example: NewAllocator("10.37.0.1/16") allocates from 10.37.0.2 to 10.37.255.254
func New(cidr string) (*Allocator, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}

	// Start from the second IP in the network (first is gateway)
	baseIP := net.ParseIP(ipnet.IP.String())
	if baseIP == nil {
		return nil, fmt.Errorf("failed to parse IP")
	}

	// Increment to second address
	incrementIP(baseIP)

	// Calculate max IP (broadcast - 1)
	maxIP := net.ParseIP(ipnet.IP.String())
	maskSize, _ := ipnet.Mask.Size()
	for i := 0; i < 32-maskSize-1; i++ {
		incrementIP(maxIP)
	}

	return &Allocator{
		baseIP:    baseIP,
		maxIP:     maxIP,
		allocated: make(map[string]bool),
	}, nil
}

// NextAddress returns the next unallocated IP address
func (a *Allocator) NextAddress() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	current := net.ParseIP(a.baseIP.String())
	for {
		addr := current.String()

		// Check bounds
		if current.Equal(a.maxIP) || ipGreaterThan(current, a.maxIP) {
			return "", fmt.Errorf("IP pool exhausted")
		}

		// Check allocation
		if !a.allocated[addr] {
			a.allocated[addr] = true
			return addr, nil
		}

		incrementIP(current)
	}
}

// ReleaseAddress marks an IP address as available for reuse
func (a *Allocator) ReleaseAddress(addr string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.allocated[addr] {
		return fmt.Errorf("address not allocated: %s", addr)
	}

	delete(a.allocated, addr)
	return nil
}

// AllocateSpecific marks a specific IP address as allocated
func (a *Allocator) AllocateSpecific(addr string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	ip := net.ParseIP(addr)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", addr)
	}

	if a.allocated[addr] {
		return fmt.Errorf("address already allocated: %s", addr)
	}

	a.allocated[addr] = true
	return nil
}

// GetAllocated returns a snapshot of all allocated addresses
func (a *Allocator) GetAllocated() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	var addrs []string
	for addr := range a.allocated {
		addrs = append(addrs, addr)
	}
	return addrs
}

// Helper functions

// incrementIP increments the given IP address by 1
func incrementIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			return
		}
	}
}

// ipGreaterThan returns true if ip1 > ip2
func ipGreaterThan(ip1, ip2 net.IP) bool {
	return ip1.String() > ip2.String()
}
