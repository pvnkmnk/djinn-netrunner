package services

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestArtistTrackingService(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		t.Skipf("Failed to connect to database: %v", err)
	}

	// Auto-migrate
	err = database.Migrate(db)
	if err != nil {
		t.Skipf("Failed to migrate: %v", err)
	}

	cfg := &MusicBrainzService{} // Dummy for now
	at := NewArtistTrackingService(db, cfg)

	if at == nil {
		t.Fatal("Expected ArtistTrackingService to be initialized")
	}
}

func TestSyncDiscographyCreatesAcquisitionJob(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	user := database.User{Email: "artist-sync@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	profile := database.QualityProfile{Name: "artist-sync-profile", OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&profile).Error)
	artist := database.MonitoredArtist{
		ID:               uuid.New(),
		MusicBrainzID:    "artist-mbid",
		Name:             "Test Artist",
		QualityProfileID: profile.ID,
		OwnerUserID:      &user.ID,
		Monitored:        true,
		MonitorAlbums:    true,
	}
	require.NoError(t, db.Create(&artist).Error)

	mb := &MusicBrainzService{
		cfg: &config.Config{MusicBrainzUserAgent: "NetRunnerTest/1.0.0"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"release-groups": [
						{"id": "release-group-1", "title": "Test Album", "primary-type": "Album"}
					]
				}`)),
			}, nil
		})},
		rateLimiter: time.NewTicker(time.Nanosecond),
	}
	defer mb.Close()

	service := NewArtistTrackingService(db, mb)
	require.NoError(t, service.SyncDiscography(artist.ID))

	var job database.Job
	require.NoError(t, db.First(&job, "job_type = ?", "acquisition").Error)
	assert.Equal(t, &user.ID, job.OwnerUserID)
	assert.Equal(t, "artist_tracking", job.CreatedBy)

	var item database.JobItem
	require.NoError(t, db.First(&item, "job_id = ?", job.ID).Error)
	assert.Equal(t, "Test Artist", item.Artist)
	assert.Equal(t, "Test Album", item.Album)
	assert.Equal(t, "Test Album", item.TrackTitle)
	assert.Equal(t, "Test Artist Test Album", item.NormalizedQuery)
}
