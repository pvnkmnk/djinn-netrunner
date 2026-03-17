package services

import (
	"os"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// setupTestCacheDB creates a test database for cache testing
func setupTestCacheDB(t *testing.T) *gorm.DB {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping test")
	}

	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	require.NoError(t, err, "failed to create test database")

	// Auto migrate
	err = db.AutoMigrate(&database.MetadataCache{})
	require.NoError(t, err, "failed to migrate")

	return db
}

// TestNewCacheService tests the constructor
func TestNewCacheService(t *testing.T) {
	db := setupTestCacheDB(t)

	service := NewCacheService(db)

	assert.NotNil(t, service, "expected non-nil service")
	assert.Equal(t, db, service.db, "expected db to be set")
}

// TestCacheService_SetAndGet tests setting and getting cache items
func TestCacheService_SetAndGet(t *testing.T) {
	db := setupTestCacheDB(t)

	service := NewCacheService(db)

	// Set a value
	err := service.Set("test-source", "test-key", map[string]string{"foo": "bar"}, 1*time.Hour)
	require.NoError(t, err, "failed to set cache")

	// Get the value
	var result map[string]string
	found, err := service.Get("test-source", "test-key", &result)
	require.NoError(t, err, "failed to get cache")
	assert.True(t, found, "expected to find cached item")
	assert.Equal(t, "bar", result["foo"], "expected foo=bar")
}

// TestCacheService_Get_NotFound tests getting a non-existent key
func TestCacheService_Get_NotFound(t *testing.T) {
	db := setupTestCacheDB(t)

	service := NewCacheService(db)

	var result map[string]string
	found, err := service.Get("test-source", "non-existent-key", &result)
	require.NoError(t, err, "failed to get cache")
	assert.False(t, found, "expected not to find cached item")
}

// TestCacheService_Delete tests deleting cache items
func TestCacheService_Delete(t *testing.T) {
	db := setupTestCacheDB(t)

	service := NewCacheService(db)

	// Set a value
	err := service.Set("test-source", "test-key", map[string]string{"foo": "bar"}, 1*time.Hour)
	require.NoError(t, err, "failed to set cache")

	// Delete the value
	err = service.Delete("test-source", "test-key")
	require.NoError(t, err, "failed to delete cache")

	// Get the value - should not find it
	var result map[string]string
	found, err := service.Get("test-source", "test-key", &result)
	require.NoError(t, err, "failed to get cache")
	assert.False(t, found, "expected not to find deleted cached item")
}
