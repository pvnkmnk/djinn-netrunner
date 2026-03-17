package services

import (
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestSearchArtistByName(t *testing.T) {
	mb := NewMusicBrainzService(nil)
	results, err := mb.SearchArtist("Radiohead")
	require.NoError(t, err)
	require.Greater(t, len(results), 0)
	assert.Equal(t, "Radiohead", results[0].Name)
	assert.NotEmpty(t, results[0].ID) // MBID
}
