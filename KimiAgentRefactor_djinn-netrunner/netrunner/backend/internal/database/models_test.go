package database

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	Migrate(db)
	return db
}

func TestMigrate_CreatesTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = Migrate(db)
	assert.NoError(t, err)

	// Verify tables exist by querying them
	var count int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count)
	assert.Greater(t, count, int64(0))
}

func TestTableLockManager_AcquireRelease(t *testing.T) {
	db := setupTestDB(t)
	lm := &TableLockManager{db: db}

	key, err := lm.GetScopeLockKey(nil, "watchlist", "test-uuid")
	require.NoError(t, err)
	assert.NotZero(t, key)

	acquired, err := lm.AcquireTryLock(nil, key)
	require.NoError(t, err)
	assert.True(t, acquired)

	err = lm.ReleaseLock(nil, key)
	assert.NoError(t, err)
}

func TestTableLockManager_DoubleAcquire(t *testing.T) {
	db := setupTestDB(t)
	lm := &TableLockManager{db: db}

	key, err := lm.GetScopeLockKey(nil, "artist", "test-1")
	require.NoError(t, err)

	acquired1, err := lm.AcquireTryLock(nil, key)
	require.NoError(t, err)
	assert.True(t, acquired1)

	// Second acquire should fail (already locked)
	acquired2, err := lm.AcquireTryLock(nil, key)
	require.NoError(t, err)
	assert.False(t, acquired2)

	err = lm.ReleaseLock(nil, key)
	assert.NoError(t, err)
}

func TestTableLockManager_DifferentKeys(t *testing.T) {
	db := setupTestDB(t)
	lm := &TableLockManager{db: db}

	key1, _ := lm.GetScopeLockKey(nil, "watchlist", "uuid-1")
	key2, _ := lm.GetScopeLockKey(nil, "watchlist", "uuid-2")

	acquired1, err := lm.AcquireTryLock(nil, key1)
	require.NoError(t, err)
	assert.True(t, acquired1)

	// Different key should succeed
	acquired2, err := lm.AcquireTryLock(nil, key2)
	require.NoError(t, err)
	assert.True(t, acquired2)

	lm.ReleaseLock(nil, key1)
	lm.ReleaseLock(nil, key2)
}

func TestTableLockManager_ReleaseUnlocked(t *testing.T) {
	db := setupTestDB(t)
	lm := &TableLockManager{db: db}

	key, _ := lm.GetScopeLockKey(nil, "test", "uuid")
	// Releasing a lock that was never acquired should not error
	err := lm.ReleaseLock(nil, key)
	assert.NoError(t, err)
}

func TestPostgresLockManager_GetScopeLockKey(t *testing.T) {
	lm := &PostgresLockManager{}

	// Should produce consistent keys for same input
	key1, err := lm.GetScopeLockKey(nil, "watchlist", "uuid-1")
	require.NoError(t, err)
	key2, err := lm.GetScopeLockKey(nil, "watchlist", "uuid-1")
	require.NoError(t, err)
	assert.Equal(t, key1, key2)

	// Different inputs should produce different keys
	key3, err := lm.GetScopeLockKey(nil, "watchlist", "uuid-2")
	require.NoError(t, err)
	assert.NotEqual(t, key1, key3)
}

func TestNewLockManager_TableLockForSQLite(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	Migrate(db)

	lm := NewLockManager(db)
	assert.IsType(t, &TableLockManager{}, lm)
}

func TestUser_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	user := User{
		Email:        "test@example.com",
		PasswordHash: "hashed",
		Role:         "user",
	}
	err := db.Create(&user).Error
	require.NoError(t, err)
	assert.NotZero(t, user.ID)

	var found User
	err = db.Where("email = ?", "test@example.com").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "test@example.com", found.Email)
}

func TestSession_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	user := User{Email: "sess@test.com", PasswordHash: "hash", Role: "user"}
	db.Create(&user)

	session := Session{
		SessionID: "test-session-id",
		UserID:    user.ID,
	}
	err := db.Create(&session).Error
	require.NoError(t, err)

	var found Session
	err = db.Where("session_id = ?", "test-session-id").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, user.ID, found.UserID)
}

func TestQualityProfile_CreateDefault(t *testing.T) {
	db := setupTestDB(t)

	profile := QualityProfile{
		Name:      "Test Profile",
		IsDefault: true,
	}
	err := db.Create(&profile).Error
	require.NoError(t, err)
	assert.True(t, profile.IsDefault)
}

func TestWatchlist_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	wl := Watchlist{
		Name:       "Test Watchlist",
		SourceType: "spotify_playlist",
		SourceURI:  "spotify:playlist:123",
		Enabled:    true,
	}
	err := db.Create(&wl).Error
	require.NoError(t, err)
	assert.NotZero(t, wl.ID)

	var found Watchlist
	err = db.Where("name = ?", "Test Watchlist").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "spotify_playlist", found.SourceType)
}

func TestJob_CreateAndStateTransitions(t *testing.T) {
	db := setupTestDB(t)

	job := Job{
		Type:      "sync",
		State:     "queued",
		ScopeType: "watchlist",
		ScopeID:   uuid.New().String(),
	}
	err := db.Create(&job).Error
	require.NoError(t, err)
	assert.Equal(t, "queued", job.State)

	// Transition to running
	db.Model(&job).Update("state", "running")
	var updated Job
	db.First(&updated, job.ID)
	assert.Equal(t, "running", updated.State)

	// Transition to succeeded
	db.Model(&job).Update("state", "succeeded")
	db.First(&updated, job.ID)
	assert.Equal(t, "succeeded", updated.State)
}

func TestJobItem_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	job := Job{Type: "acquisition", State: "queued", ScopeType: "artist", ScopeID: uuid.New().String()}
	db.Create(&job)

	item := JobItem{
		JobID:           job.ID,
		Sequence:        0,
		NormalizedQuery: "test query",
		Status:          "queued",
	}
	err := db.Create(&item).Error
	require.NoError(t, err)

	var found JobItem
	err = db.Where("job_id = ?", job.ID).First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "test query", found.NormalizedQuery)
}

func TestSchedule_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	wl := Watchlist{Name: "sched-wl", SourceType: "rss_feed", SourceURI: "http://example.com", Enabled: true}
	db.Create(&wl)

	sched := Schedule{
		WatchlistID: wl.ID,
		CronExpr:    "0 */4 * * *",
		Timezone:    "UTC",
		Enabled:     true,
	}
	err := db.Create(&sched).Error
	require.NoError(t, err)

	var found Schedule
	err = db.Preload("Watchlist").First(&found, sched.ID).Error
	require.NoError(t, err)
	assert.Equal(t, "0 */4 * * *", found.CronExpr)
	assert.Equal(t, wl.ID, found.WatchlistID)
}

func TestLibrary_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	lib := Library{
		Name: "My Library",
		Path: "/music",
	}
	err := db.Create(&lib).Error
	require.NoError(t, err)

	var found Library
	err = db.Where("name = ?", "My Library").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "/music", found.Path)
}

func TestTrack_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	lib := Library{Name: "Track Lib", Path: "/tracks"}
	db.Create(&lib)

	track := Track{
		LibraryID: lib.ID,
		Title:     "Test Song",
		Artist:    "Test Artist",
		Album:     "Test Album",
		Format:    "FLAC",
	}
	err := db.Create(&track).Error
	require.NoError(t, err)

	var found Track
	err = db.Where("title = ?", "Test Song").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "FLAC", found.Format)
}

func TestMonitoredArtist_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	profile := QualityProfile{Name: "Artist Profile", IsDefault: true}
	db.Create(&profile)

	artist := MonitoredArtist{
		MusicBrainzID:    "mbid-123",
		Name:             "Test Artist",
		QualityProfileID: profile.ID,
		Monitored:        true,
	}
	err := db.Create(&artist).Error
	require.NoError(t, err)

	var found MonitoredArtist
	err = db.Where("music_brainz_id = ?", "mbid-123").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "Test Artist", found.Name)
}

func TestCalculateBackoff_New(t *testing.T) {
	tests := []struct {
		retryCount int
		want       time.Duration
	}{
		{0, 1 * time.Minute},
		{1, 5 * time.Minute},
		{2, 1 * time.Hour},
		{5, 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			d := CalculateBackoff(tt.retryCount)
			assert.Equal(t, tt.want, d)
		})
	}
}

func TestAppendJobLog(t *testing.T) {
	db := setupTestDB(t)

	job := Job{Type: "sync", State: "running", ScopeType: "watchlist", ScopeID: uuid.New().String()}
	db.Create(&job)

	itemID := uint64(1)
	err := AppendJobLog(db, job.ID, "INFO", "Test log message", &itemID)
	require.NoError(t, err)

	var logs []JobLog
	db.Where("job_id = ?", job.ID).Find(&logs)
	assert.Len(t, logs, 1)
	assert.Equal(t, "INFO", logs[0].Level)
	assert.Equal(t, "Test log message", logs[0].Message)
}

func TestAppendJobLog_NoItemID(t *testing.T) {
	db := setupTestDB(t)

	job := Job{Type: "sync", State: "running", ScopeType: "watchlist", ScopeID: uuid.New().String()}
	db.Create(&job)

	err := AppendJobLog(db, job.ID, "WARN", "Warning without item", nil)
	require.NoError(t, err)

	var logs []JobLog
	db.Where("job_id = ?", job.ID).Find(&logs)
	assert.Len(t, logs, 1)
	assert.Nil(t, logs[0].JobItemID)
}
