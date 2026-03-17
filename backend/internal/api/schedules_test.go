package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestSchedulesHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	assert.NotNil(t, &SchedulesHandler{})
}

func TestSchedulesHandler_NewSchedulesHandler(t *testing.T) {
	// Test that NewSchedulesHandler returns a non-nil handler
	handler := NewSchedulesHandler(nil)
	assert.NotNil(t, handler, "expected non-nil handler")

	// Verify the db field is set (even if nil)
	var db *gorm.DB
	assert.Equal(t, db, handler.db)
}
