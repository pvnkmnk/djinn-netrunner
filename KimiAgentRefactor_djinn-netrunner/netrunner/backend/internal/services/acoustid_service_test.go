package services

import (
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
)

// TestNewAcoustIDService tests the constructor
func TestNewAcoustIDService(t *testing.T) {
	cfg := &config.Config{
		AcoustIDApiKey: "test-api-key",
	}

	service := NewAcoustIDService(cfg)

	assert.NotNil(t, service, "expected non-nil service")
	assert.Equal(t, cfg, service.cfg, "expected config to be set")
	assert.NotNil(t, service.httpClient, "expected httpClient to be set")
}

// TestAcoustIDService_SetCache tests the SetCache method
func TestAcoustIDService_SetCache(t *testing.T) {
	cfg := &config.Config{}
	service := NewAcoustIDService(cfg)

	// Initially no cache
	assert.Nil(t, service.cache, "expected nil cache initially")

	// Create a mock cache (we'll use a real in-memory one)
	// Note: This test verifies the method exists and can be called
	service.SetCache(nil)

	// Should not panic
}

// TestAcoustIDService_Lookup_NoAPIKey tests Lookup fails without API key
func TestAcoustIDService_Lookup_NoAPIKey(t *testing.T) {
	cfg := &config.Config{} // No API key
	service := NewAcoustIDService(cfg)

	results, err := service.Lookup("test-fingerprint", 180)

	assert.Error(t, err, "expected error when API key is not configured")
	assert.Nil(t, results, "expected nil results on error")

	expectedErr := "AcoustID API key is not configured"
	assert.EqualError(t, err, expectedErr, "expected specific error message")
}
