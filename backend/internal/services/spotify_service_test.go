package services

import (
	"testing"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

func TestSpotifyService(t *testing.T) {
	cfg := &config.Config{
		SpotifyClientID: "test-id",
		SpotifyClientSecret: "test-secret",
	}
	s := NewSpotifyService(cfg)
	if s == nil {
		t.Fatal("Expected SpotifyService to be initialized")
	}
}
