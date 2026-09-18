package services

import (
	"context"
	"os"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// The downloaded bytes are the audio that was requested
// ---------------------------------------------------------------------------

// The two gates that existed before this one ask whether a file *is* playable
// audio — a plausible advertised size, then a successful ffprobe — and neither
// asks whether it is the audio that was *asked for*. Soulseek search is fuzzy,
// so a peer routinely serves an unrelated track whose filename happens to match
// the query. This is the case the acceptance run hit (DJI-495): requesting
// `PUP`/`PUP`/`PUP` imported an Ace Attorney soundtrack track tagged "Noriyuki
// Iwadare", filed under that artist in the library and streaming normally.
func TestIdentityMismatch(t *testing.T) {
	tests := []struct {
		name     string
		item     database.JobItem
		meta     AudioMetadata
		reject   bool
		contains string
	}{
		{
			// The exact request and the exact tags the run recorded. Note the
			// junk title contains the word "pup" — whole-word matching is why a
			// title-only check would not have caught this on its own.
			name: "the acceptance run's case: an unrelated track under a matching query",
			item: database.JobItem{Artist: "PUP", Album: "PUP", TrackTitle: "PUP"},
			meta: AudioMetadata{
				Artist:      "Noriyuki Iwadare",
				AlbumArtist: "Noriyuki Iwadare",
				Album:       "Ace Attorney Investigations: Miles Edgeworth Original Soundtrack",
				Title:       "Shi-Long Lang - Speak Up, Pup!",
			},
			reject:   true,
			contains: "Noriyuki Iwadare",
		},
		{
			name: "guest credit on the album's own track is not a different recording",
			item: database.JobItem{Artist: "Every Time I Die", Album: "Gutter Phenomenon", TrackTitle: "The New Black"},
			meta: AudioMetadata{
				Artist:      "Every Time I Die & Daryl Palumbo",
				AlbumArtist: "Every Time I Die & Daryl Palumbo",
				Album:       "Gutter Phenomenon",
				Title:       "The New Black",
			},
		},
		{
			name: "case-only drift across both axes",
			item: database.JobItem{Artist: "PUP", Album: "Morbid Stuff", TrackTitle: "Closure"},
			meta: AudioMetadata{
				Artist:      "Pup",
				AlbumArtist: "Pup",
				Album:       "MORBID STUFF",
				Title:       "Closure",
			},
		},
		{
			name: "release qualifiers and punctuation drift",
			item: database.JobItem{Artist: "PUP", Album: "Who Will Look After the Dogs?", TrackTitle: "Morbid Stuff"},
			meta: AudioMetadata{
				Artist:      "PUP",
				AlbumArtist: "PUP",
				Album:       "Who Will Look After the Dogs",
				Title:       "Morbid Stuff (Live)",
			},
		},
		{
			// A various-artists layout: the artist axis differs because the peer
			// tagged the track's own artist, but the album still identifies it.
			name: "various-artists track whose album still agrees",
			item: database.JobItem{Artist: "Various Artists", Album: "Tony Hawk's Pro Skater", TrackTitle: "Superman"},
			meta: AudioMetadata{
				Artist:      "Goldfinger",
				AlbumArtist: "Goldfinger",
				Album:       "Tony Hawk's Pro Skater",
				Title:       "Superman",
			},
		},
		{
			// The deliberate limit of the rule: one axis agreeing in full is
			// taken as identification. A peer's wrong album tag is far commoner
			// than a peer serving a different artist's track, and rejecting here
			// would throw away valid downloads.
			name: "artist agrees in full, so a differing album tag is not a mismatch",
			item: database.JobItem{Artist: "PUP", Album: "Morbid Stuff", TrackTitle: "Closure"},
			meta: AudioMetadata{
				Artist:      "PUP",
				AlbumArtist: "PUP",
				Album:       "Totally Different Record",
				Title:       "Closure",
			},
		},
		{
			name: "album-less item is identified by its title",
			item: database.JobItem{Artist: "Converge", Album: "", TrackTitle: "Concubine"},
			meta: AudioMetadata{
				Artist:      "Converge",
				AlbumArtist: "Converge",
				Album:       "Jane Doe",
				Title:       "Concubine",
			},
		},
		{
			name: "album-less item whose title disagrees too",
			item: database.JobItem{Artist: "Converge", Album: "", TrackTitle: "Concubine"},
			meta: AudioMetadata{
				Artist:      "Noriyuki Iwadare",
				AlbumArtist: "Noriyuki Iwadare",
				Album:       "Ace Attorney Investigations: Miles Edgeworth Original Soundtrack",
				Title:       "Shi-Long Lang - Speak Up, Pup!",
			},
			reject:   true,
			contains: "asked for artist",
		},
		{
			// An untagged peer file is not evidence of anything. Rejecting it
			// would throw away real downloads over a missing tag.
			name: "untagged file is never a mismatch",
			item: database.JobItem{Artist: "PUP", Album: "PUP", TrackTitle: "PUP"},
			meta: AudioMetadata{},
		},
		{
			name: "an item with nothing to compare cannot reject anything",
			item: database.JobItem{},
			meta: AudioMetadata{
				Artist:      "Noriyuki Iwadare",
				AlbumArtist: "Noriyuki Iwadare",
				Album:       "Ace Attorney Investigations: Miles Edgeworth Original Soundtrack",
				Title:       "Shi-Long Lang - Speak Up, Pup!",
			},
		},
		{
			// The artist disagrees, but the item carries no album and no title,
			// so there is no second axis to confirm it — and one axis alone is
			// never enough to reject a download.
			name: "one disagreeing axis and nothing to confirm it is not enough",
			item: database.JobItem{Artist: "Live"},
			meta: AudioMetadata{
				Artist:      "Noriyuki Iwadare",
				AlbumArtist: "Noriyuki Iwadare",
				Album:       "Ace Attorney Investigations: Miles Edgeworth Original Soundtrack",
				Title:       "Shi-Long Lang - Speak Up, Pup!",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := identityMismatch(&tt.item, &tt.meta)

			if !tt.reject {
				assert.Empty(t, reason, "a legitimate download must not be rejected")
				return
			}
			require.NotEmpty(t, reason, "a confidently unrelated file must be rejected")
			assert.Contains(t, reason, tt.contains)
		})
	}
}

func TestIdentityTokens(t *testing.T) {
	assert.Equal(t, []string{"shi", "long", "lang", "speak", "up", "pup"},
		identityTokens("Shi-Long Lang - Speak Up, Pup!"))
	// Qualifiers are kept, deliberately: they are words a legitimate tag is
	// allowed to carry on top of the words the request used.
	assert.Equal(t, []string{"morbid", "stuff", "live"}, identityTokens("Morbid Stuff (Live)"))
	assert.Equal(t, []string{"who", "will", "look", "after", "the", "dogs", "deluxe", "edition"},
		identityTokens("Who Will Look After the Dogs? (Deluxe Edition)"))
	assert.Empty(t, identityTokens("   "), "a blank name carries no words to compare")
}

// The whole gate, on real bytes: a peer serving an unrelated track (tagged with
// what the acceptance run recorded) must be discarded, and the item must walk on
// to a peer that has the requested audio rather than failing.
func TestAcquisitionHandler_StageDownloadFile_RejectsMismatchedFileAndTriesNext(t *testing.T) {
	requireProbeTools(t)

	db := setupPipelineTestDB(t)
	// The rejected bytes are discarded, and the discard refuses any path outside
	// the staging root — so the fixture has to sit inside the configured one.
	staging := t.TempDir()
	handler := NewAcquisitionHandler(db, &config.Config{DownloadStagingPath: staging},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.ext = NewMetadataExtractor() // real ffprobe and real tag reads

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)
	// The request the acceptance run recorded: thin enough that "Pup" in a
	// filename satisfies the search.
	item := database.JobItem{
		JobID: job.ID, Status: "running", NormalizedQuery: "PUP",
		Artist: "PUP", Album: "PUP", TrackTitle: "PUP", Sequence: 1,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.First(&item, item.ID).Error)

	wanted := generateTestAudio(t, staging, "wanted.flac",
		"-metadata", "artist=PUP", "-metadata", "album_artist=PUP",
		"-metadata", "album=PUP", "-metadata", "title=PUP")
	offTarget := generateTestAudio(t, staging, "off-target.flac",
		"-metadata", "artist=Noriyuki Iwadare",
		"-metadata", "album_artist=Noriyuki Iwadare",
		"-metadata", "album=Ace Attorney Investigations: Miles Edgeworth Original Soundtrack",
		"-metadata", "title=Shi-Long Lang - Speak Up, Pup!")

	handler.slskd = &mockSlskd{
		EnqueueDownloadFunc: func(username, filename string, size int64) (string, error) {
			return "id-" + username, nil
		},
		WaitForDownloadFunc: func(ctx context.Context, username, downloadID string, opts DownloadWaitOptions) (*Download, error) {
			local := offTarget
			if username == "good-peer" {
				local = wanted
			}
			return &Download{ID: downloadID, Username: username, LocalPath: local}, nil
		},
	}

	p := &acquisitionPipeline{
		ctx:  context.Background(),
		item: item,
		candidates: []SearchResult{
			{Username: "junk-peer", Filename: "music/PUP/01 - PUP.flac", Size: 30_000_000},
			{Username: "good-peer", Filename: "music/PUP/01 - PUP.flac", Size: 30_000_000},
		},
	}

	skip, err := handler.stageDownloadFile(p)
	require.NoError(t, err)
	assert.False(t, skip, "a peer serving the wrong track must not fail the item while another candidate remains")
	assert.Equal(t, wanted, p.download)

	_, statErr := os.Stat(offTarget)
	assert.True(t, os.IsNotExist(statErr),
		"a file that is not the requested recording must be removed so it can never be imported")

	logs := jobLogMessages(t, db, job.ID)
	assert.Contains(t, logs, "delivered a file that does not match the request — rejected")
	assert.Contains(t, logs, "Noriyuki Iwadare")
	assert.Contains(t, logs, "junk-peer")
}
