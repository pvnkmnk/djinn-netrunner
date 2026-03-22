package services

import (
	"encoding/json"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type CacheService struct {
	db *gorm.DB
}

func NewCacheService(db *gorm.DB) *CacheService {
	return &CacheService{db: db}
}

// Get retrieves an item from the cache
func (s *CacheService) Get(source, key string, target interface{}) (bool, error) {
	var item database.MetadataCache
	err := s.db.Where("source = ? AND key = ? AND expires_at > ?", source, key, time.Now()).First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}

	err = json.Unmarshal(item.Value, target)
	if err != nil {
		return false, err
	}

	return true, nil
}

// Set stores an item in the cache
func (s *CacheService) Set(source, key string, value interface{}, ttl time.Duration) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	item := database.MetadataCache{
		Source:    source,
		Key:       key,
		Value:     bytes,
		ExpiresAt: time.Now().Add(ttl),
	}

	// Upsert
	return s.db.Where("source = ? AND key = ?", source, key).
		Assign(database.MetadataCache{Value: bytes, ExpiresAt: item.ExpiresAt}).
		FirstOrCreate(&item).Error
}

// Delete removes an item from the cache
func (s *CacheService) Delete(source, key string) error {
	return s.db.Where("source = ? AND key = ?", source, key).Delete(&database.MetadataCache{}).Error
}

// GetBytes retrieves raw bytes from the cache.
// Returns the bytes and true if found, or nil and false if not found/expired.
func (s *CacheService) GetBytes(source, key string) ([]byte, bool, error) {
	var item database.MetadataCache
	err := s.db.Where("source = ? AND key = ? AND expires_at > ?", source, key, time.Now()).First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	return item.Value, true, nil
}

// SetBytes stores raw bytes in the cache.
func (s *CacheService) SetBytes(source, key string, data []byte, ttl time.Duration) error {
	item := database.MetadataCache{
		Source:    source,
		Key:       key,
		Value:     data,
		ExpiresAt: time.Now().Add(ttl),
	}
	return s.db.Where("source = ? AND key = ?", source, key).
		Assign(database.MetadataCache{Value: data, ExpiresAt: item.ExpiresAt}).
		FirstOrCreate(&item).Error
}

// Cleanup removes expired items
func (s *CacheService) Cleanup() error {
	return s.db.Where("expires_at < ?", time.Now()).Delete(&database.MetadataCache{}).Error
}
