package templates

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The artist card is how an operator answers "is this the right Death?".
// Before DJI-589 it showed a name and nothing else -- no disambiguation, no
// country, no type -- even though the MusicBrainz service decoded all three and
// the picker displayed them. The provenance was available at the moment of
// choosing and gone at the moment of checking.
//
// The separator is the part worth pinning. These fields are independently
// optional on MusicBrainz: plenty of entities carry a country and no type. A
// naive join renders "Napalm Death · United Kingdom ·" for those, and the
// dangling separator is the kind of thing that survives review because it only
// appears on rows you happen to look at.
func TestArtistCard_RendersProvenanceAndOmitsEmptySeparators(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)
	engine := NewPongo2(templatesDir, ".html")

	card := func(overrides map[string]any) string {
		artist := map[string]any{
			"ID": "artist-1", "Name": "Napalm Death",
			"MusicBrainzID": "mbid-1", "Monitored": true,
			"AcquiredReleases": 12, "TotalReleases": 48,
			"LastScanLabel": "3 Oct 2026",
			// Country and ArtistType start empty; each case opts in.
			"Disambiguation": "", "Country": "", "ArtistType": "",
		}
		for k, v := range overrides {
			artist[k] = v
		}
		return renderPartial(t, engine, "partials/artist-card.html",
			map[string]any{"Artist": artist})
	}

	t.Run("all three fields present", func(t *testing.T) {
		body := card(map[string]any{
			"Disambiguation": "UK grindcore", "Country": "United Kingdom", "ArtistType": "Group",
		})
		assert.Contains(t, body, "UK grindcore")
		assert.Contains(t, body, "United Kingdom")
		assert.Contains(t, body, "Group")
		// Exactly two separators between three fields.
		assert.Equal(t, 2, strings.Count(body, "&middot;"),
			"expected two separators for three provenance fields, got %d", strings.Count(body, "&middot;"))
	})

	t.Run("country set, type empty", func(t *testing.T) {
		// The common case: MusicBrainz knows where the artist is from but has
		// not classified them.
		body := card(map[string]any{"Country": "United Kingdom"})
		assert.Contains(t, body, "United Kingdom")
		assert.NotContains(t, body, "Group")
		assert.Equal(t, 0, strings.Count(body, "&middot;"),
			"one field needs no separator; a leading or dangling one is the bug this pins")
	})

	t.Run("nothing set", func(t *testing.T) {
		// A row written before the backfill. It must render name and nothing
		// else -- not an empty span, and not a bare separator.
		body := card(nil)
		assert.Contains(t, body, "Napalm Death")
		assert.NotContains(t, body, "&middot;",
			"a row with no provenance must not render a separator")
	})

	t.Run("type only", func(t *testing.T) {
		// The mirror of the country-only case: if the join is written as
		// "prefix everything after the first field", this breaks.
		body := card(map[string]any{"ArtistType": "Person"})
		assert.Contains(t, body, "Person")
		assert.Equal(t, 0, strings.Count(body, "&middot;"))
	})
}

// The card existed in two templates with different variable casing --
// `artist.X` inlined in the list and `Artist.X` in the partial. A copy held in
// agreement only by a test is how it drifted in the first place, so the list
// must now render the one partial rather than carry a second copy.
func TestArtistsList_RendersTheSharedCardPartial(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)
	engine := NewPongo2(templatesDir, ".html")

	body := renderPartial(t, engine, "partials/artists.html", map[string]any{
		"artists": []map[string]any{{
			"ID": "artist-1", "Name": "Napalm Death", "MusicBrainzID": "mbid-1",
			"Monitored": true, "AcquiredReleases": 1, "TotalReleases": 2,
			"LastScanLabel": "3 Oct 2026",
			"Disambiguation": "", "Country": "United Kingdom", "ArtistType": "Group",
		}},
	})

	// If the include is wired wrongly the loop renders nothing at all, which is
	// the failure a reviewer would otherwise find by opening the page.
	assert.Contains(t, body, "Napalm Death", "the list rendered no artist: the include is not receiving Artist")
	assert.Contains(t, body, "United Kingdom", "provenance must survive the include")
	assert.Contains(t, body, "/sync", "the card's Sync button must survive the include")
}