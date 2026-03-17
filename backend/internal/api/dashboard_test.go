package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestDashboardHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	assert.NotNil(t, &DashboardHandler{})
}

func TestDashboardHandler_NewDashboardHandler(t *testing.T) {
	// Test that NewDashboardHandler returns a non-nil handler
	handler := NewDashboardHandler(nil)
	assert.NotNil(t, handler, "expected non-nil handler")

	// Verify the db field is set (even if nil)
	var db *gorm.DB
	assert.Equal(t, db, handler.db)
}
