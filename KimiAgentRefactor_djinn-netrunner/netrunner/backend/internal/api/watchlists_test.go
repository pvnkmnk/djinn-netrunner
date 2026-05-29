package api

import (
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestWatchlistHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	assert.NotNil(t, &WatchlistHandler{})
}

func TestWatchlistHandler_NewWatchlistHandler(t *testing.T) {
	// Test that NewWatchlistHandler returns a non-nil handler
	handler := NewWatchlistHandler(nil, nil)
	assert.NotNil(t, handler, "expected non-nil handler")

	// Verify the db field is set (even if nil)
	var db *gorm.DB
	assert.Equal(t, db, handler.db)

	// Verify the service field is set (even if nil)
	var svc *services.WatchlistService
	assert.Equal(t, svc, handler.service)
}
