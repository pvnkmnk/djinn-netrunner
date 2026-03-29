package services

import (
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"testing"
)

func TestSpotifyService(t *testing.T) {
	cfg := &config.Config{
		SpotifyClientID:     "test-id",
		SpotifyClientSecret: "test-secret",
	}
	s := NewSpotifyService(cfg)
	if s == nil {
		t.Fatal("Expected SpotifyService to be initialized")
	}
}
