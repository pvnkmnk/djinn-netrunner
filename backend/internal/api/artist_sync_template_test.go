package api

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The artist card exists in TWO templates: partials/artists.html inlines one
// inside the list, and partials/artist-card.html is what a Pause/Resume
// re-renders. They drifted. The list copy posted to /sync with
// hx-swap="none", so the server's answer -- "Sync triggered for X (job #N)",
// or "Sync already active" -- was fetched and thrown away, and an operator
// pressing Sync on /artists saw nothing happen at all.
//
// A copy that disagrees is invisible to a behavioural spec written against the
// other copy, so the agreement itself is what this pins.
func TestEveryArtistSyncButtonShowsWhatTheServerSaid(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "ops", "web", "templates")

	found := 0
	// Recursive on purpose. The buttons live in partials/, and a guard that only
	// reads the top level finds nothing and passes forever -- which is exactly
	// what the first version of this test did. The count assertion at the end is
	// what turns that mistake loud instead of silent.
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".html") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}

		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "/sync") || !strings.Contains(line, "<button") {
				continue
			}
			found++
			assert.Contains(t, line, `hx-target="#notice"`,
				"%s: the artist Sync button must show the server's answer, not discard it", rel)
			assert.NotContains(t, line, `hx-swap="none"`,
				"%s: swap=none throws the scan status away, so the click looks inert", rel)
		}
		return nil
	})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, found, 2,
		"expected the artist Sync button in both partials/artists.html and partials/artist-card.html; "+
			"finding fewer means this guard is no longer looking at the buttons it was written for")
}
