package services

import (
	"context"
	"os"
	"path/filepath"
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
			// "The National" and "The Beatles" share only the word "the". If a
			// generic connector could carry an axis, the artist axis would agree
			// here and the album axis would never be consulted.
			name: "a shared generic word does not make two artists the same",
			item: database.JobItem{Artist: "The National", Album: "Boxer", TrackTitle: "Fake Empire"},
			meta: AudioMetadata{
				Artist:      "The Beatles",
				AlbumArtist: "The Beatles",
				Album:       "Abbey Road",
				Title:       "Come Together",
			},
			reject:   true,
			contains: "The Beatles",
		},
		{
			// ... and the album axis still rescues a file when the artists
			// differ: the rule is "both axes disagree", not "the artist must
			// match".
			name: "an album match still identifies a file whose artist differs",
			item: database.JobItem{Artist: "The National", Album: "Boxer", TrackTitle: "Fake Empire"},
			meta: AudioMetadata{
				Artist:      "The Beatles",
				AlbumArtist: "The Beatles",
				Album:       "Boxer",
				Title:       "Fake Empire",
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
			// An album-less item has only the title left, and this file carries no
			// title tag: Extract filled it from the file's name, which the peer
			// chose. Rejecting on that would throw away a valid download because of
			// a filename, so a title that came from the name is an axis the check
			// does not have.
			name: "a title the extractor took from the filename is not evidence",
			item: database.JobItem{Artist: "Converge", TrackTitle: "Concubine"},
			meta: AudioMetadata{
				Artist:            "Noriyuki Iwadare",
				AlbumArtist:       "Noriyuki Iwadare",
				Album:             "Ace Attorney Investigations: Miles Edgeworth Original Soundtrack",
				Title:             "01 - track",
				TitleFromFilename: true,
			},
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
	// Qualifiers and connectors are removed: none of them can identify a
	// recording, and any one of them is enough to make two names "agree".
	assert.Equal(t, []string{"morbid", "stuff"}, identityTokens("Morbid Stuff (Live)"))
	assert.Equal(t, []string{"who", "will", "look", "after", "dogs"},
		identityTokens("Who Will Look After the Dogs? (Deluxe Edition)"))
	assert.Empty(t, identityTokens("The With (Live)"),
		"a name of nothing but connectors and qualifiers carries no identity")
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

// A caller with no staged path is a gap, not a pass: the gate has nothing to
// read, so it must say so rather than returning silently as though the file had
// been checked.
func TestRejectUnusableDownload_LogsWhenThereIsNoPathToCheck(t *testing.T) {
	db := setupPipelineTestDB(t)
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.ext = NewMetadataExtractor()

	_, item := createAcquisitionTestItem(t, db)
	p := &acquisitionPipeline{ctx: context.Background(), item: item}

	reason, err := handler.rejectUnusableDownload(p, "yt-dlp", "")
	require.NoError(t, err)
	assert.Empty(t, reason, "a missing path is not evidence against a download")

	assert.Contains(t, jobLogMessages(t, db, item.JobID), "No staged path to verify")
}

// ---------------------------------------------------------------------------
// The import stage has two entrances, and both are gated
// ---------------------------------------------------------------------------

// The pipeline reaches the import stage from the Soulseek candidate loop and from
// the yt-dlp fallback. The fallback used to return straight into the import stage
// with no check at all, so the same defect class the gate exists to stop — a
// playable file that is a different work — could still be imported through it
// (found by DJI-495's audit).
//
// This drives the whole pipeline through that entrance: Soulseek finds nothing,
// the fallback returns a real, playable, correctly tagged file for a different
// work, and it must be rejected, discarded, and never reach the library.
func TestAcquisitionHandler_ExecuteItem_YtdlpFallbackIsGatedToo(t *testing.T) {
	requireProbeTools(t)

	db := setupPipelineTestDB(t)
	staging := t.TempDir()
	libraryRoot := t.TempDir()

	handler := NewAcquisitionHandler(db, &config.Config{
		DownloadStagingPath: staging,
		MusicLibraryPath:    libraryRoot,
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.ext = NewMetadataExtractor() // real ffprobe and real tag reads

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)

	item := database.JobItem{
		JobID: job.ID, Status: "queued", Sequence: 1,
		NormalizedQuery: "PUP PUP",
		Artist:          "PUP", Album: "PUP", TrackTitle: "PUP",
		SourceURL: "https://example.invalid/watch?v=off-target",
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.First(&item, item.ID).Error)

	offTarget := generateTestAudio(t, staging, "off-target.flac",
		"-metadata", "artist=Noriyuki Iwadare",
		"-metadata", "album_artist=Noriyuki Iwadare",
		"-metadata", "album=Ace Attorney Investigations: Miles Edgeworth Original Soundtrack",
		"-metadata", "title=Shi-Long Lang - Speak Up, Pup!")

	// Soulseek finds nothing, which is exactly when the fallback runs.
	handler.slskd = &mockSlskd{
		SearchFunc: func(query string, timeout int, profile *database.QualityProfile) ([]SearchResult, error) {
			return nil, nil
		},
	}
	handler.ytdlp = &mockYtdlp{
		IsYtdlpAvailableFunc: func() bool { return true },
		DownloadAudioFunc: func(ctx context.Context, rawURL, outputDir, audioFormat string) (string, error) {
			return offTarget, nil
		},
	}

	require.NoError(t, handler.ExecuteItem(context.Background(), job.ID, item.ID))

	// The gate ran on this entrance, and the rejection names the entrance it came
	// through.
	var stored database.JobItem
	require.NoError(t, db.First(&stored, item.ID).Error)
	assert.Equal(t, "abandoned", stored.Status,
		"a rejected fallback download is terminal: a retry would fetch the same URL and reach the same verdict")
	assert.Nil(t, stored.NextAttemptAt,
		"a permanent verdict must not leave a retry scheduled")
	assert.NotNil(t, stored.FinishedAt)
	assert.Contains(t, stored.FailureReason, "does not match the request")
	assert.Contains(t, stored.FailureReason, "yt-dlp",
		"the reason must say which entrance the rejection came from")

	// The bytes are gone from staging, through the same owner as every other
	// rejection.
	_, statErr := os.Stat(offTarget)
	assert.True(t, os.IsNotExist(statErr),
		"a rejected fallback file must be discarded so it can never be imported")

	// And nothing reached the library.
	imported := 0
	require.NoError(t, filepath.WalkDir(libraryRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			imported++
		}
		return nil
	}))
	assert.Zero(t, imported,
		"an unrelated file must not be imported through the fallback entrance")

	// And it is terminal: the worker claims only queued items and failed ones
	// whose backoff has passed, so an abandoned item is never picked up again.
	nextID, claimErr := NewJobItemProcessor(db, handler).ClaimNextItem(job.ID)
	require.NoError(t, claimErr)
	assert.Zero(t, nextID, "a permanent verdict must not be re-claimable")
}
