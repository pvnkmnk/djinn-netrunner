package services

import (
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DJI-588. Monitoring an artist used to be a bare INSERT: the row appeared,
// nothing ever scanned it, and "monitored" was a claim the queue never saw. The
// scan is part of the promise now, so the promise is asserted here rather than
// described in a comment.

func TestAddMonitoredArtist_QueuesTheScanItPromises(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	profile := database.QualityProfile{Name: "dji588-profile"}
	require.NoError(t, db.Create(&profile).Error)

	svc := NewArtistTrackingService(db, nil)
	artist, err := svc.AddMonitoredArtist("dji588-queued-mbid", profile.ID, "Queued On Add", "Queued On Add", nil)
	require.NoError(t, err)
	require.NotNil(t, artist)

	var jobs []database.Job
	require.NoError(t, db.Where("job_type = ?", "artist_scan").Find(&jobs).Error)

	// Exactly one, and it is the one this artist needs. A count is asserted as
	// an equality rather than a ">= 1": two queued scans for one artist would
	// serialise on the worker's scope lock for no reason.
	require.Len(t, jobs, 1, "adding an artist must queue exactly one scan")
	assert.Equal(t, "queued", jobs[0].State)
	assert.Equal(t, "artist", jobs[0].ScopeType)
	assert.Equal(t, artist.ID.String(), jobs[0].ScopeID)
	assert.Equal(t, "user_api", jobs[0].CreatedBy)
	assert.False(t, jobs[0].RequestedAt.IsZero(), "a queued job must say when it was asked for")
}

// The insert and the job are one promise, so they commit together. Dropping the
// jobs table makes the enqueue fail; without the transaction this test would
// still pass on "an error came back" while leaving exactly the unscanned
// artist the ticket is about.
func TestAddMonitoredArtist_RollsBackTheArtistWhenTheScanCannotBeQueued(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	profile := database.QualityProfile{Name: "dji588-atomic-profile"}
	require.NoError(t, db.Create(&profile).Error)
	require.NoError(t, db.Migrator().DropTable(&database.Job{}))

	svc := NewArtistTrackingService(db, nil)
	_, err := svc.AddMonitoredArtist("dji588-rollback-mbid", profile.ID, "Never Lands", "Never Lands", nil)
	require.Error(t, err, "an enqueue that cannot be written must fail the add")

	var artists int64
	require.NoError(t, db.Model(&database.MonitoredArtist{}).
		Where("music_brainz_id = ?", "dji588-rollback-mbid").Count(&artists).Error)
	assert.Equal(t, int64(0), artists,
		"a monitored artist must never survive without the scan that was promised for it")
}

func TestQueueArtistScan_AnswersTheActiveScanInsteadOfQueueingASecond(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	profile := database.QualityProfile{Name: "dji588-idem-profile"}
	require.NoError(t, db.Create(&profile).Error)

	svc := NewArtistTrackingService(db, nil)
	artist, err := svc.AddMonitoredArtist("dji588-idempotent-mbid", profile.ID, "Pressed Twice", "Pressed Twice", nil)
	require.NoError(t, err)

	// Add already queued one. The operator pressing Sync is asking for a scan
	// they already have, so the SAME job comes back, flagged as already active,
	// rather than a second one.
	job, alreadyActive, err := svc.QueueArtistScan(artist, "user_api")
	require.NoError(t, err)
	assert.True(t, alreadyActive, "Add already queued this artist's scan, so the ask is redundant")

	var total int64
	require.NoError(t, db.Model(&database.Job{}).Where("job_type = ?", "artist_scan").Count(&total).Error)
	assert.Equal(t, int64(1), total, "asking again must not queue a second scan of the same artist")

	again, alreadyActive, err := svc.QueueArtistScan(artist, "user_api")
	require.NoError(t, err)
	assert.True(t, alreadyActive, "the second ask is told a scan is already active")
	assert.Equal(t, job.ID, again.ID, "and it is handed the job that is already active")

	// A settled job is not an active one: once it has finished, asking again is
	// a fresh request and must reach the queue.
	require.NoError(t, db.Model(&database.Job{}).Where("job_type = ?", "artist_scan").
		Update("state", "succeeded").Error)
	_, alreadyActive, err = svc.QueueArtistScan(artist, "user_api")
	require.NoError(t, err)
	assert.False(t, alreadyActive, "a finished scan is history, so a new one is queued")

	require.NoError(t, db.Model(&database.Job{}).Where("job_type = ?", "artist_scan").Count(&total).Error)
	assert.Equal(t, int64(2), total)
}

// Queueing a scan on add means "scan, then remove" is the ordinary sequence
// for a monitored artist. Measured before this fix, the second half of that
// sequence answered 500 on fk_monitored_artists_releases: the worker had left
// tracked_releases rows behind and only the artist row was deleted.
func TestDeleteMonitoredArtist_RemovesTheReleasesTheScanLeftBehind(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	profile := database.QualityProfile{Name: "dji588-delete-profile"}
	require.NoError(t, db.Create(&profile).Error)

	svc := NewArtistTrackingService(db, nil)
	artist, err := svc.AddMonitoredArtist("dji588-delete-mbid", profile.ID, "Scanned Then Removed", "Scanned Then Removed", nil)
	require.NoError(t, err)

	// What the worker's scan leaves behind.
	require.NoError(t, db.Create(&database.TrackedRelease{
		ArtistID:       artist.ID,
		ReleaseGroupID: "dji588-release-group",
		ReleaseID:      "dji588-release",
	}).Error)

	var releases int64
	require.NoError(t, db.Model(&database.TrackedRelease{}).Where("artist_id = ?", artist.ID).Count(&releases).Error)
	require.Equal(t, int64(1), releases, "the fixture must actually be a tracked release")

	require.NoError(t, svc.DeleteMonitoredArtist(artist.ID, 0, true),
		"an artist the worker has scanned must still be removable")

	var left int64
	require.NoError(t, db.Model(&database.MonitoredArtist{}).Where("id = ?", artist.ID).Count(&left).Error)
	assert.Equal(t, int64(0), left, "the artist is gone")
	require.NoError(t, db.Model(&database.TrackedRelease{}).Where("artist_id = ?", artist.ID).Count(&left).Error)
	assert.Equal(t, int64(0), left, "and so are the releases that FK-blocked it")
}

// The scoped subquery that fixes the above must not become a way to delete
// somebody else's releases: a non-admin removing their own artist leaves the
// other owner's rows alone.
func TestDeleteMonitoredArtist_LeavesAnotherOwnersReleasesAlone(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()

	profile := database.QualityProfile{Name: "dji588-owner-profile"}
	require.NoError(t, db.Create(&profile).Error)

	owner := database.User{Email: "dji588-owner@test.local", PasswordHash: "h", Role: "user"}
	require.NoError(t, db.Create(&owner).Error)
	other := database.User{Email: "dji588-other@test.local", PasswordHash: "h", Role: "user"}
	require.NoError(t, db.Create(&other).Error)

	svc := NewArtistTrackingService(db, nil)
	mine, err := svc.AddMonitoredArtist("dji588-mine", profile.ID, "Mine", "Mine", &owner.ID)
	require.NoError(t, err)
	theirs, err := svc.AddMonitoredArtist("dji588-theirs", profile.ID, "Theirs", "Theirs", &other.ID)
	require.NoError(t, err)

	require.NoError(t, db.Create(&database.TrackedRelease{ArtistID: mine.ID, ReleaseGroupID: "g-mine"}).Error)
	require.NoError(t, db.Create(&database.TrackedRelease{ArtistID: theirs.ID, ReleaseGroupID: "g-theirs"}).Error)

	require.NoError(t, svc.DeleteMonitoredArtist(mine.ID, owner.ID, false))

	var theirsLeft int64
	require.NoError(t, db.Model(&database.TrackedRelease{}).Where("artist_id = ?", theirs.ID).Count(&theirsLeft).Error)
	assert.Equal(t, int64(1), theirsLeft, "removing your own artist must not reach another owner's releases")

	var artistsLeft int64
	require.NoError(t, db.Model(&database.MonitoredArtist{}).Count(&artistsLeft).Error)
	assert.Equal(t, int64(1), artistsLeft)
}
