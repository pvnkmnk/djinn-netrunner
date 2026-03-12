package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestFileWatchlistProvider_FetchTracks(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("CSV Parsing", func(t *testing.T) {
		csvPath := filepath.Join(tempDir, "test.csv")
		content := "Artist,Title,Album\nArtist One,Track One,Album One\n\"Artist, Two\",Track Two,Album Two"
		os.WriteFile(csvPath, []byte(content), 0644)

		provider := &FileWatchlistProvider{}
		watchlist := &database.Watchlist{
			SourceType: "local_file",
			SourceURI:  csvPath,
		}

		tracks, _, err := provider.FetchTracks(context.Background(), watchlist)
		assert.NoError(t, err)
		assert.Len(t, tracks, 2)
		assert.Equal(t, "Artist One", tracks[0]["artist"])
		assert.Equal(t, "Track One", tracks[0]["title"])
		assert.Equal(t, "Artist, Two", tracks[1]["artist"])
	})

	t.Run("M3U Parsing", func(t *testing.T) {
		m3uPath := filepath.Join(tempDir, "test.m3u")
		content := "#EXTM3U\n#EXTINF:123,Artist Three - Track Three\n/path/to/file.mp3\nArtist Four - Track Four"
		os.WriteFile(m3uPath, []byte(content), 0644)

		provider := &FileWatchlistProvider{}
		watchlist := &database.Watchlist{
			SourceType: "local_file",
			SourceURI:  m3uPath,
		}

		tracks, _, err := provider.FetchTracks(context.Background(), watchlist)
		assert.NoError(t, err)
		assert.Len(t, tracks, 2)
		assert.Equal(t, "Artist Three", tracks[0]["artist"])
		assert.Equal(t, "Track Three", tracks[0]["title"])
		assert.Equal(t, "Artist Four", tracks[1]["artist"])
		assert.Equal(t, "Track Four", tracks[1]["title"])
	})

	t.Run("TXT Parsing", func(t *testing.T) {
		txtPath := filepath.Join(tempDir, "test.txt")
		content := "Artist Five - Track Five\nJust a Title\nArtist Six-Track Six"
		os.WriteFile(txtPath, []byte(content), 0644)

		provider := &FileWatchlistProvider{}
		watchlist := &database.Watchlist{
			SourceType: "local_file",
			SourceURI:  txtPath,
		}

		tracks, _, err := provider.FetchTracks(context.Background(), watchlist)
		assert.NoError(t, err)
		assert.Len(t, tracks, 3)
		assert.Equal(t, "Artist Five", tracks[0]["artist"])
		assert.Equal(t, "Artist Six", tracks[2]["artist"])
		assert.Equal(t, "Track Six", tracks[2]["title"])
	})
}
