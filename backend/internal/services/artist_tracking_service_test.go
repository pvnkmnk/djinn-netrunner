package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupArtistTrackingDB(t *testing.T) (*gorm.DB, func()) {
	// Use a temp file for the database to avoid sharing issues between tests
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	cleanup := func() {
		sqlDB.Close()
		os.RemoveAll(tmpDir)
	}
	return db, cleanup
}

func TestAddMonitoredArtist(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	profile := database.QualityProfile{Name: "Test Profile"}
	require.NoError(t, db.Create(&profile).Error)

	artist, err := svc.AddMonitoredArtist(
		"mbid-123",
		profile.ID,
		"Test Artist",
		"Artist, Test",
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "mbid-123", artist.MusicBrainzID)
	assert.Equal(t, "Test Artist", artist.Name)
	assert.Equal(t, "Artist, Test", artist.SortName)
	assert.True(t, artist.Monitored)
	assert.True(t, artist.MonitorNew)
	assert.True(t, artist.MonitorAlbums)
	assert.True(t, artist.MonitorEPs)
	assert.NotEqual(t, uuid.Nil, artist.ID)
}

func TestAddMonitoredArtist_DuplicateMBID_ReturnsError(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	profile := database.QualityProfile{Name: "Test Profile"}
	require.NoError(t, db.Create(&profile).Error)

	mbid := "duplicate-mbid"
	_, err := svc.AddMonitoredArtist(mbid, profile.ID, "Artist 1", "", nil)
	require.NoError(t, err)

	_, err = svc.AddMonitoredArtist(mbid, profile.ID, "Artist 2", "", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already monitored")
}

func TestAddMonitoredArtist_WithOwnerUserID(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	user := database.User{Email: "owner@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user).Error)

	profile := database.QualityProfile{Name: "Test Profile", OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&profile).Error)

	artist, err := svc.AddMonitoredArtist(
		"mbid-owner",
		profile.ID,
		"Owned Artist",
		"",
		&user.ID,
	)
	require.NoError(t, err)
	assert.Equal(t, &user.ID, artist.OwnerUserID)
}

func TestAddMonitoredArtist_EmptyName_FallsBackToMBID(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	profile := database.QualityProfile{Name: "Test Profile"}
	require.NoError(t, db.Create(&profile).Error)

	artist, err := svc.AddMonitoredArtist("mbid-no-name", profile.ID, "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "mbid-no-name", artist.Name)
}

func TestGetMonitoredArtists(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	profile := database.QualityProfile{Name: "Test Profile"}
	require.NoError(t, db.Create(&profile).Error)

	// Add multiple artists
	for i := 1; i <= 3; i++ {
		_, err := svc.AddMonitoredArtist(
			uuid.New().String(),
			profile.ID,
			"Artist",
			"",
			nil,
		)
		require.NoError(t, err)
	}

	// GetMonitoredArtists for admin (isAdmin=true) returns all
	artists, err := svc.GetMonitoredArtists(0, true)
	require.NoError(t, err)
	assert.Len(t, artists, 3)
}

func TestGetMonitoredArtists_FiltersByUserID(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	user1 := database.User{Email: "user1@test.local", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user1).Error)
	require.NoError(t, db.Create(&user2).Error)

	profile := database.QualityProfile{Name: "Test Profile", OwnerUserID: &user1.ID}
	require.NoError(t, db.Create(&profile).Error)

	// User 1 adds 2 artists
	for i := 0; i < 2; i++ {
		_, err := svc.AddMonitoredArtist(uuid.New().String(), profile.ID, "User1Artist", "", &user1.ID)
		require.NoError(t, err)
	}

	// User 2 adds 1 artist
	_, err := svc.AddMonitoredArtist(uuid.New().String(), profile.ID, "User2Artist", "", &user2.ID)
	require.NoError(t, err)

	// User 1 only sees their artists
	artists, err := svc.GetMonitoredArtists(user1.ID, false)
	require.NoError(t, err)
	assert.Len(t, artists, 2)
	for _, a := range artists {
		assert.Equal(t, &user1.ID, a.OwnerUserID)
	}

	// User 2 only sees their artist
	artists, err = svc.GetMonitoredArtists(user2.ID, false)
	require.NoError(t, err)
	assert.Len(t, artists, 1)

	// Admin sees all
	artists, err = svc.GetMonitoredArtists(0, true)
	require.NoError(t, err)
	assert.Len(t, artists, 3)
}

func TestUpdateArtistStatus(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	profile := database.QualityProfile{Name: "Test Profile"}
	require.NoError(t, db.Create(&profile).Error)

	artist, err := svc.AddMonitoredArtist("mbid-update", profile.ID, "Update Test", "", nil)
	require.NoError(t, err)
	assert.True(t, artist.Monitored)

	// Update to not monitored
	err = svc.UpdateArtistStatus(artist.ID, false, 0, true)
	require.NoError(t, err)

	var updated database.MonitoredArtist
	require.NoError(t, db.First(&updated, artist.ID).Error)
	assert.False(t, updated.Monitored)
}

func TestUpdateArtistStatus_NonAdmin_CannotUpdateOthersArtist(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	user1 := database.User{Email: "user1@test.local", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user1).Error)
	require.NoError(t, db.Create(&user2).Error)

	profile := database.QualityProfile{Name: "Test Profile", OwnerUserID: &user1.ID}
	require.NoError(t, db.Create(&profile).Error)

	artist, err := svc.AddMonitoredArtist("mbid-protected", profile.ID, "Protected", "", &user1.ID)
	require.NoError(t, err)

	// User 2 tries to update user1's artist - should not error but update 0 rows
	err = svc.UpdateArtistStatus(artist.ID, false, user2.ID, false)
	require.NoError(t, err) // Returns no error even if nothing updated

	// Verify artist was NOT updated
	var unchanged database.MonitoredArtist
	require.NoError(t, db.First(&unchanged, artist.ID).Error)
	assert.True(t, unchanged.Monitored)
}

func TestUpdateArtistStatus_WithMissingID(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	// Try to update non-existent artist
	err := svc.UpdateArtistStatus(uuid.New(), false, 0, true)
	assert.NoError(t, err) // GORM Update doesn't error on no rows affected
}

func TestDeleteMonitoredArtist(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	profile := database.QualityProfile{Name: "Test Profile"}
	require.NoError(t, db.Create(&profile).Error)

	artist, err := svc.AddMonitoredArtist("mbid-delete", profile.ID, "Delete Me", "", nil)
	require.NoError(t, err)

	// Delete the artist
	err = svc.DeleteMonitoredArtist(artist.ID, 0, true)
	require.NoError(t, err)

	// Verify deletion
	var deleted database.MonitoredArtist
	err = db.First(&deleted, artist.ID).Error
	assert.Error(t, err)
}

func TestDeleteMonitoredArtist_NonAdmin_CannotDeleteOthersArtist(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	user1 := database.User{Email: "user1@test.local", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user1).Error)
	require.NoError(t, db.Create(&user2).Error)

	profile := database.QualityProfile{Name: "Test Profile", OwnerUserID: &user1.ID}
	require.NoError(t, db.Create(&profile).Error)

	artist, err := svc.AddMonitoredArtist("mbid-protected-delete", profile.ID, "Protected Delete", "", &user1.ID)
	require.NoError(t, err)

	// User 2 tries to delete user1's artist
	err = svc.DeleteMonitoredArtist(artist.ID, user2.ID, false)
	require.NoError(t, err) // Returns no error even if nothing deleted

	// Verify artist still exists
	var stillExists database.MonitoredArtist
	require.NoError(t, db.First(&stillExists, artist.ID).Error)
}

func TestDeleteMonitoredArtist_WithMissingID(t *testing.T) {
	db, cleanup := setupArtistTrackingDB(t)
	defer cleanup()
	svc := NewArtistTrackingService(db, nil)

	// Try to delete non-existent artist - should not error
	err := svc.DeleteMonitoredArtist(uuid.New(), 0, true)
	assert.NoError(t, err)
}

// TestArtistTrackingService is the integration test that was already in the file
func TestArtistTrackingService(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := gorm.Open(sqlite.Open(dbURL), &gorm.Config{})
	if err != nil {
		t.Skipf("Failed to connect to database: %v", err)
	}

	err = database.Migrate(db)
	if err != nil {
		t.Skipf("Failed to migrate: %v", err)
	}

	cfg := &MusicBrainzService{}
	at := NewArtistTrackingService(db, cfg)

	if at == nil {
		t.Fatal("Expected ArtistTrackingService to be initialized")
	}
}
