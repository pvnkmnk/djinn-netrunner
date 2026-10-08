package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The e2e seam only works if the constructor honours MUSICBRAINZ_URL. If it did
// not, the stand-in would be wired into compose and every spec would still be
// talking to musicbrainz.org -- a failure that looks exactly like a third-party
// outage, which is the problem this whole seam exists to remove.
func TestMusicBrainzService_HonoursTheURLOverride(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artists": []map[string]any{{
				"id": "from-the-stand-in", "name": "Napalm Death",
				"country": "GB", "type": "Group",
			}},
			"count": 1,
		})
	}))
	defer srv.Close()

	t.Run("override is used", func(t *testing.T) {
		svc := NewMusicBrainzService(&config.Config{MusicBrainzURL: srv.URL, AllowPrivateTargets: true})

		artists, err := svc.SearchArtist("death")
		require.NoError(t, err)
		require.Len(t, artists, 1)

		assert.Equal(t, "from-the-stand-in", artists[0].ID,
			"the response came from the stand-in, so the override was honoured")
		assert.Equal(t, "/ws/2/artist", gotPath)
	})

	t.Run("a trailing slash does not double up", func(t *testing.T) {
		// A compose value like "http://fake-musicbrainz:8082/" would otherwise
		// produce "http://fake-musicbrainz:8082//ws/2/artist", which 404s on a
		// well-behaved server and reads as the stand-in being broken.
		svc := NewMusicBrainzService(&config.Config{MusicBrainzURL: srv.URL + "/", AllowPrivateTargets: true})

		_, err := svc.SearchArtist("death")
		require.NoError(t, err)
		assert.Equal(t, "/ws/2/artist", gotPath)
	})

	t.Run("empty override keeps the public service", func(t *testing.T) {
		// The default must not move: this is a production code path and the
		// e2e seam must be opt-in.
		svc := NewMusicBrainzService(&config.Config{})
		assert.Equal(t, defaultMusicBrainzURL, svc.baseURL)
	})

	t.Run("nil config keeps the public service", func(t *testing.T) {
		svc := NewMusicBrainzService(nil)
		assert.Equal(t, defaultMusicBrainzURL, svc.baseURL)
	})
}
