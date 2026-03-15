package services

import (
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"gorm.io/gorm"
)

func TestJobHandlers(t *testing.T) {
	db := &gorm.DB{}
	spotify := &SpotifyService{}
	watchlist := &WatchlistService{}
	sync := NewSyncHandler(db, spotify, watchlist)
	if sync == nil {
		t.Fatal("Expected SyncHandler to be initialized")
	}

	slskd := &SlskdService{}
	metadata := &MetadataExtractor{}
	acq := NewAcquisitionHandler(db, &config.Config{}, slskd, nil, nil, metadata, nil)
	if acq == nil {
		t.Fatal("Expected AcquisitionHandler to be initialized")
	}
}
