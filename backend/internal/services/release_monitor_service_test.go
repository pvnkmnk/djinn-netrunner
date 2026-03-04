package services

import (
	"testing"

	"gorm.io/gorm"
)

func TestReleaseMonitorService(t *testing.T) {
	// Dummy test
	db := &gorm.DB{}
	at := &ArtistTrackingService{}
	s := NewReleaseMonitorService(db, at)

	if s == nil {
		t.Fatal("Expected ReleaseMonitorService to be initialized")
	}
}
