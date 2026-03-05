package services

import (
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

func TestMusicBrainzService(t *testing.T) {
	cfg := &config.Config{
		MusicBrainzUserAgent: "NetRunnerTest/1.0.0",
	}
	s := NewMusicBrainzService(cfg)
	defer s.Close()

	if s == nil {
		t.Fatal("Expected MusicBrainzService to be initialized")
	}
}
