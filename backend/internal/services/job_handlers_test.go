package services

import (
	"testing"
	"gorm.io/gorm"
)

func TestJobHandlers(t *testing.T) {
	db := &gorm.DB{}
	spotify := &SpotifyService{}
	sync := NewSyncHandler(db, spotify)
	if sync == nil {
		t.Fatal("Expected SyncHandler to be initialized")
	}

	slskd := &SlskdService{}
	metadata := &MetadataExtractor{}
	acq := NewAcquisitionHandler(db, slskd, metadata)
	if acq == nil {
		t.Fatal("Expected AcquisitionHandler to be initialized")
	}
}
