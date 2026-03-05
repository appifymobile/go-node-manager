package utils

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// KeyGenerator generates cryptographic keys and passwords
type KeyGenerator struct{}

// NewKeyGenerator creates a new key generator
func NewKeyGenerator() *KeyGenerator {
	return &KeyGenerator{}
}

// GeneratePassword generates a random password of the specified length (in bytes)
func (kg *KeyGenerator) GeneratePassword(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("failed to generate random bytes: %v", err))
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

// GenerateUUID generates a UUID for VLESS protocol
func (kg *KeyGenerator) GenerateUUID() string {
	// Generate 16 random bytes
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("failed to generate UUID: %v", err))
	}

	// Format as UUID v4
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	)
}

// GenerateShortID generates a short ID for VLESS Reality transport
func (kg *KeyGenerator) GenerateShortID(length int) string {
	if length > 16 {
		length = 16
	}
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("failed to generate short ID: %v", err))
	}
	return fmt.Sprintf("%x", bytes)
}
