package services

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCacheTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	err = database.Migrate(db)
	require.NoError(t, err)
	return db
}

// TestCacheService_NewCacheService tests the constructor
func TestCacheService_NewCacheService(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)
	assert.NotNil(t, svc)
	assert.Equal(t, db, svc.db)
}

// TestCacheService_SetAndGet_RoundTrip tests Set + Get for string values
func TestCacheService_SetAndGet_RoundTrip(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set a value
	err := svc.Set("spotify", "artist:abc123", map[string]string{"name": "The Beatles"}, 1*time.Hour)
	require.NoError(t, err, "Set should not error")

	// Get the value back
	var result map[string]string
	found, err := svc.Get("spotify", "artist:abc123", &result)
	require.NoError(t, err, "Get should not error")
	assert.True(t, found, "item should be found")
	assert.Equal(t, "The Beatles", result["name"])
}

// TestCacheService_Get_NotFound tests Get for a key that doesn't exist
func TestCacheService_Get_NotFound(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	var result map[string]string
	found, err := svc.Get("spotify", "nonexistent-key", &result)
	require.NoError(t, err, "Get should not error for missing key")
	assert.False(t, found, "item should not be found")
}

// TestCacheService_Get_WrongSource tests Get with wrong source returns not found
func TestCacheService_Get_WrongSource(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set with "sourceA"
	err := svc.Set("sourceA", "key1", "value1", 1*time.Hour)
	require.NoError(t, err)

	// Get with "sourceB" - should not find
	var result string
	found, err := svc.Get("sourceB", "key1", &result)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestCacheService_Set_WithTTL tests that Set stores with correct expiry behavior
func TestCacheService_Set_WithTTL(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set with very short TTL - uses explicit future time
	err := svc.Set("testsource", "ttl-key", "short-lived", 50*time.Millisecond)
	require.NoError(t, err)

	// Immediately should be found
	var result string
	found, err := svc.Get("testsource", "ttl-key", &result)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "short-lived", result)

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Should now be expired/not found
	found, err = svc.Get("testsource", "ttl-key", &result)
	require.NoError(t, err)
	assert.False(t, found, "expired item should not be found")
}

// TestCacheService_Delete_ExistingKey tests deleting an existing key
func TestCacheService_Delete_ExistingKey(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set a value
	err := svc.Set("testsource", "deleteme", "to-be-deleted", 1*time.Hour)
	require.NoError(t, err)

	// Delete it
	err = svc.Delete("testsource", "deleteme")
	require.NoError(t, err, "Delete should not error for existing key")

	// Verify it's gone
	var result string
	found, err := svc.Get("testsource", "deleteme", &result)
	require.NoError(t, err)
	assert.False(t, found, "deleted item should not be found")
}

// TestCacheService_Delete_MissingKey tests deleting a key that doesn't exist (no error)
func TestCacheService_Delete_MissingKey(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Delete non-existent key - should NOT error
	err := svc.Delete("testsource", "nonexistent-key")
	require.NoError(t, err, "Delete should not error for missing key")
}

// TestCacheService_Delete_WrongSource tests delete with wrong source
func TestCacheService_Delete_WrongSource(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set with "sourceA"
	err := svc.Set("sourceA", "key1", "value1", 1*time.Hour)
	require.NoError(t, err)

	// Delete with "sourceB" - should not delete anything, but no error
	err = svc.Delete("sourceB", "key1")
	require.NoError(t, err)

	// Original should still exist
	var result string
	found, err := svc.Get("sourceA", "key1", &result)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value1", result)
}

// TestCacheService_Set_OverwriteExisting tests overwriting an existing key
func TestCacheService_Set_OverwriteExisting(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set initial value
	err := svc.Set("testsource", "overwrite-key", "original", 1*time.Hour)
	require.NoError(t, err)

	// Overwrite with new value
	err = svc.Set("testsource", "overwrite-key", "updated", 1*time.Hour)
	require.NoError(t, err)

	// Verify new value is returned
	var result string
	found, err := svc.Get("testsource", "overwrite-key", &result)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "updated", result)
}

// TestCacheService_GetBytes_SetBytes_RoundTrip tests raw byte storage and retrieval
func TestCacheService_GetBytes_SetBytes_RoundTrip(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	originalData := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}

	// Set bytes
	err := svc.SetBytes("rawsource", "binary-key", originalData, 1*time.Hour)
	require.NoError(t, err, "SetBytes should not error")

	// Get bytes back
	data, found, err := svc.GetBytes("rawsource", "binary-key")
	require.NoError(t, err, "GetBytes should not error")
	assert.True(t, found, "item should be found")
	assert.Equal(t, originalData, data)
}

// TestCacheService_GetBytes_NotFound tests GetBytes for a missing key
func TestCacheService_GetBytes_NotFound(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	data, found, err := svc.GetBytes("testsource", "nonexistent")
	require.NoError(t, err, "GetBytes should not error for missing key")
	assert.False(t, found, "item should not be found")
	assert.Nil(t, data)
}

// TestCacheService_GetBytes_WrongSource tests GetBytes with wrong source
func TestCacheService_GetBytes_WrongSource(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set with "sourceA"
	err := svc.SetBytes("sourceA", "key1", []byte("data"), 1*time.Hour)
	require.NoError(t, err)

	// Get with "sourceB"
	data, found, err := svc.GetBytes("sourceB", "key1")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, data)
}

// TestCacheService_SetBytes_WithTTL tests SetBytes with TTL expiry
func TestCacheService_SetBytes_WithTTL(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set with very short TTL
	err := svc.SetBytes("testsource", "short-lived-bytes", []byte("temporary"), 50*time.Millisecond)
	require.NoError(t, err)

	// Immediately should be found
	data, found, err := svc.GetBytes("testsource", "short-lived-bytes")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("temporary"), data)

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Should now be expired
	data, found, err = svc.GetBytes("testsource", "short-lived-bytes")
	require.NoError(t, err)
	assert.False(t, found, "expired item should not be found")
}

// TestCacheService_SetBytes_OverwriteExisting tests overwriting with SetBytes
func TestCacheService_SetBytes_OverwriteExisting(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set initial bytes
	err := svc.SetBytes("testsource", "bytes-key", []byte("original"), 1*time.Hour)
	require.NoError(t, err)

	// Overwrite with new bytes
	err = svc.SetBytes("testsource", "bytes-key", []byte("updated"), 1*time.Hour)
	require.NoError(t, err)

	// Verify new value
	data, found, err := svc.GetBytes("testsource", "bytes-key")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("updated"), data)
}

// TestCacheService_Cleanup_RemovesExpired tests that Cleanup removes expired entries
func TestCacheService_Cleanup_RemovesExpired(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set two items - one expiring soon, one long-lived
	err := svc.Set("testsource", "expired-soon", "data1", 10*time.Millisecond)
	require.NoError(t, err)
	err = svc.Set("testsource", "long-lived", "data2", 1*time.Hour)
	require.NoError(t, err)

	// Wait for the short-lived one to expire
	time.Sleep(50 * time.Millisecond)

	// Run cleanup
	err = svc.Cleanup()
	require.NoError(t, err, "Cleanup should not error")

	// Expired item should be gone
	var result string
	found, err := svc.Get("testsource", "expired-soon", &result)
	require.NoError(t, err)
	assert.False(t, found, "expired item should be removed by Cleanup")

	// Long-lived item should still exist
	found, err = svc.Get("testsource", "long-lived", &result)
	require.NoError(t, err)
	assert.True(t, found, "non-expired item should still exist after Cleanup")
	assert.Equal(t, "data2", result)
}

// TestCacheService_Cleanup_PreservesNonExpired tests that Cleanup doesn't remove valid entries
func TestCacheService_Cleanup_PreservesNonExpired(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set items with long TTL
	err := svc.Set("testsource", "key1", "value1", 1*time.Hour)
	require.NoError(t, err)
	err = svc.Set("testsource", "key2", "value2", 2*time.Hour)
	require.NoError(t, err)

	// Run cleanup immediately
	err = svc.Cleanup()
	require.NoError(t, err)

	// Both items should still exist
	var result1, result2 string
	found1, err := svc.Get("testsource", "key1", &result1)
	require.NoError(t, err)
	assert.True(t, found1)
	assert.Equal(t, "value1", result1)

	found2, err := svc.Get("testsource", "key2", &result2)
	require.NoError(t, err)
	assert.True(t, found2)
	assert.Equal(t, "value2", result2)
}

// TestCacheService_Cleanup_EmptyCache tests Cleanup on empty cache (no error)
func TestCacheService_Cleanup_EmptyCache(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Cleanup on empty cache - should not error
	err := svc.Cleanup()
	require.NoError(t, err, "Cleanup should not error on empty cache")
}

// TestCacheService_Cleanup_AllExpired tests Cleanup when all entries are expired
func TestCacheService_Cleanup_AllExpired(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set items that will all expire quickly
	err := svc.Set("testsource", "key1", "value1", 10*time.Millisecond)
	require.NoError(t, err)
	err = svc.Set("testsource", "key2", "value2", 10*time.Millisecond)
	require.NoError(t, err)

	// Wait for expiry
	time.Sleep(50 * time.Millisecond)

	// Run cleanup
	err = svc.Cleanup()
	require.NoError(t, err)

	// Both should be gone
	var result string
	found, err := svc.Get("testsource", "key1", &result)
	require.NoError(t, err)
	assert.False(t, found)

	found, err = svc.Get("testsource", "key2", &result)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestCacheService_MixedSources tests that operations only affect the specified source
func TestCacheService_MixedSources(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set items with different sources
	err := svc.Set("sourceA", "key1", "A-value1", 1*time.Hour)
	require.NoError(t, err)
	err = svc.Set("sourceB", "key1", "B-value1", 1*time.Hour)
	require.NoError(t, err)

	// Each source's item should be retrievable independently
	var resultA, resultB string
	foundA, err := svc.Get("sourceA", "key1", &resultA)
	require.NoError(t, err)
	assert.True(t, foundA)
	assert.Equal(t, "A-value1", resultA)

	foundB, err := svc.Get("sourceB", "key1", &resultB)
	require.NoError(t, err)
	assert.True(t, foundB)
	assert.Equal(t, "B-value1", resultB)

	// Deleting from one source should not affect the other
	err = svc.Delete("sourceA", "key1")
	require.NoError(t, err)

	foundA, err = svc.Get("sourceA", "key1", &resultA)
	require.NoError(t, err)
	assert.False(t, foundA)

	// sourceB should still be intact
	foundB, err = svc.Get("sourceB", "key1", &resultB)
	require.NoError(t, err)
	assert.True(t, foundB)
	assert.Equal(t, "B-value1", resultB)
}

// TestCacheService_SetComplexTypes tests storing complex Go types
func TestCacheService_SetComplexTypes(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Test with a struct
	artist := struct {
		Name  string   `json:"name"`
		MBID  string   `json:"mbid"`
		Tags  []string `json:"tags"`
	}{Name: "Pink Floyd", MBID: "f5913c06-79c4-436f-b2cd-0f2354cb1d7e", Tags: []string{"rock", "progressive"}}

	err := svc.Set("musicbrainz", "artist:f5913c06", artist, 1*time.Hour)
	require.NoError(t, err)

	// Retrieve and verify
	var result struct {
		Name  string   `json:"name"`
		MBID  string   `json:"mbid"`
		Tags  []string `json:"tags"`
	}
	found, err := svc.Get("musicbrainz", "artist:f5913c06", &result)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "Pink Floyd", result.Name)
	assert.Equal(t, "f5913c06-79c4-436f-b2cd-0f2354cb1d7e", result.MBID)
	assert.Equal(t, []string{"rock", "progressive"}, result.Tags)
}

// TestCacheService_SetZeroTTL tests Set with zero TTL (item is immediately expired)
func TestCacheService_SetZeroTTL(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set with zero TTL - item is stored but immediately expired since
	// ExpiresAt = time.Now().Add(0) = time.Now(), and query checks expires_at > time.Now()
	err := svc.Set("testsource", "no-expiry", "permanent", 0)
	require.NoError(t, err)

	// Should NOT be found because zero TTL = immediately expired
	var result string
	found, err := svc.Get("testsource", "no-expiry", &result)
	require.NoError(t, err)
	assert.False(t, found, "zero TTL item should be immediately expired")
}

// TestCacheService_DeleteAllInSource tests deleting all entries for a source
func TestCacheService_DeleteAllInSource(t *testing.T) {
	db := setupCacheTestDB(t)
	svc := NewCacheService(db)

	// Set multiple keys in same source
	err := svc.Set("todelete", "key1", "value1", 1*time.Hour)
	require.NoError(t, err)
	err = svc.Set("todelete", "key2", "value2", 1*time.Hour)
	require.NoError(t, err)
	err = svc.Set("todelete", "key3", "value3", 1*time.Hour)
	require.NoError(t, err)

	// Also set in different source (should not be affected)
	err = svc.Set("keep", "key1", "keep-value", 1*time.Hour)
	require.NoError(t, err)

	// Delete all in "todelete" source (must delete individually)
	err = svc.Delete("todelete", "key1")
	require.NoError(t, err, "Delete key1 should not error")
	err = svc.Delete("todelete", "key2")
	require.NoError(t, err, "Delete key2 should not error")
	err = svc.Delete("todelete", "key3")
	require.NoError(t, err, "Delete key3 should not error")

	// All "todelete" keys should be gone
	var result string
	found, err := svc.Get("todelete", "key1", &result)
	require.NoError(t, err)
	assert.False(t, found, "key1 should be deleted")

	found, err = svc.Get("todelete", "key2", &result)
	require.NoError(t, err)
	assert.False(t, found, "key2 should be deleted")

	found, err = svc.Get("todelete", "key3", &result)
	require.NoError(t, err)
	assert.False(t, found, "key3 should be deleted")

	// "keep" source should be unaffected
	found, err = svc.Get("keep", "key1", &result)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "keep-value", result)
}
