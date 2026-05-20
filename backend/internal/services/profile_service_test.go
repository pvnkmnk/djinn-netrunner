package services

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupProfileDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&database.QualityProfile{}))
	return db
}

func TestProfileService_GetDefaultProfile(t *testing.T) {
	t.Run("returns default when exists", func(t *testing.T) {
		db := setupProfileDB(t)
		svc := NewProfileService(db)

		defaultProfile := database.QualityProfile{
			ID:          uuid.New(),
			Name:        "My Default",
			IsDefault:   true,
		}
		require.NoError(t, db.Create(&defaultProfile).Error)

		got, err := svc.GetDefaultProfile()
		assert.NoError(t, err)
		assert.Equal(t, defaultProfile.ID, got.ID)
		assert.True(t, got.IsDefault)
	})

	t.Run("error when no default exists", func(t *testing.T) {
		db := setupProfileDB(t)
		svc := NewProfileService(db)

		nonDefault := database.QualityProfile{
			ID:   uuid.New(),
			Name: "Not Default",
		}
		require.NoError(t, db.Create(&nonDefault).Error)

		_, err := svc.GetDefaultProfile()
		assert.Error(t, err)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})
}

func TestProfileService_GetProfileByID(t *testing.T) {
	t.Run("finds by id", func(t *testing.T) {
		db := setupProfileDB(t)
		svc := NewProfileService(db)

		profile := database.QualityProfile{
			ID:   uuid.New(),
			Name: "Test Profile",
		}
		require.NoError(t, db.Create(&profile).Error)

		got, err := svc.GetProfileByID(profile.ID.String())
		assert.NoError(t, err)
		assert.Equal(t, profile.ID, got.ID)
		assert.Equal(t, profile.Name, got.Name)
	})

	t.Run("not found", func(t *testing.T) {
		db := setupProfileDB(t)
		svc := NewProfileService(db)
		_, err := svc.GetProfileByID(uuid.New().String())
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})
}

func TestProfileService_ListProfiles(t *testing.T) {
	db := setupProfileDB(t)
	svc := NewProfileService(db)

	p1 := database.QualityProfile{ID: uuid.New(), Name: "Beta"}
	p2 := database.QualityProfile{ID: uuid.New(), Name: "Alpha"}
	require.NoError(t, db.Create(&p1).Error)
	require.NoError(t, db.Create(&p2).Error)

	profiles, err := svc.ListProfiles()
	assert.NoError(t, err)
	assert.Len(t, profiles, 2)
	assert.Equal(t, "Alpha", profiles[0].Name)
	assert.Equal(t, "Beta", profiles[1].Name)
}

func TestProfileService_EnsureDefaultProfile(t *testing.T) {
	t.Run("creates default when none exists", func(t *testing.T) {
		db := setupProfileDB(t)
		svc := NewProfileService(db)

		profile, err := svc.EnsureDefaultProfile()
		assert.NoError(t, err)
		assert.NotNil(t, profile)
		assert.True(t, profile.IsDefault)
		assert.Equal(t, "Default", profile.Name)
		assert.NotEmpty(t, profile.AllowedFormats)
		assert.True(t, profile.PreferLossless)

		// Verify only one default
		var count int64
		db.Model(&database.QualityProfile{}).Where("is_default = ?", true).Count(&count)
		assert.Equal(t, int64(1), count)
	})

	t.Run("returns existing default", func(t *testing.T) {
		db := setupProfileDB(t)
		svc := NewProfileService(db)

		existing := database.QualityProfile{
			ID:        uuid.New(),
			Name:      "Existing Default",
			IsDefault: true,
		}
		require.NoError(t, db.Create(&existing).Error)

		profile, err := svc.EnsureDefaultProfile()
		assert.NoError(t, err)
		assert.Equal(t, existing.ID, profile.ID)
		assert.Equal(t, "Existing Default", profile.Name)
	})
}

func TestProfileService_ValidateFull(t *testing.T) {
	t.Run("accepts allowed format", func(t *testing.T) {
		svc := NewProfileService(nil) // DB not needed for validation
		profile := &database.QualityProfile{
			AllowedFormats: "FLAC,ALAC,WAV",
			MinBitrate:     0,
		}
		result := svc.ValidateFull(profile, "FLAC", 1411)
		assert.True(t, result.Valid)
		assert.Empty(t, result.Issues)
	})

	t.Run("rejects disallowed format", func(t *testing.T) {
		svc := NewProfileService(nil)
		profile := &database.QualityProfile{
			AllowedFormats: "FLAC,ALAC,WAV",
			MinBitrate:     0,
		}
		result := svc.ValidateFull(profile, "MP3", 320)
		assert.False(t, result.Valid)
		assert.Contains(t, result.Issues[0], "MP3")
	})

	t.Run("rejects below minimum bitrate", func(t *testing.T) {
		svc := NewProfileService(nil)
		profile := &database.QualityProfile{
			AllowedFormats: "FLAC,MP3,WAV",
			MinBitrate:     256,
		}
		result := svc.ValidateFull(profile, "MP3", 128)
		assert.False(t, result.Valid)
		assert.Contains(t, result.Issues[0], "bitrate")
	})

	t.Run("case insensitive format matching", func(t *testing.T) {
		svc := NewProfileService(nil)
		profile := &database.QualityProfile{
			AllowedFormats: "FLAC,MP3,WAV",
			MinBitrate:     0,
		}
		result := svc.ValidateFull(profile, "flac", 1411)
		assert.True(t, result.Valid)
	})

	t.Run("no allowed formats allows any", func(t *testing.T) {
		svc := NewProfileService(nil)
		profile := &database.QualityProfile{
			AllowedFormats: "",
			MinBitrate:     0,
		}
		result := svc.ValidateFull(profile, "AAC", 256)
		assert.True(t, result.Valid)
	})

	t.Run("format and bitrate combo", func(t *testing.T) {
		svc := NewProfileService(nil)
		profile := &database.QualityProfile{
			AllowedFormats: "FLAC,ALAC,WAV",
			MinBitrate:     500,
		}
		// MP3 not in allowed list, regardless of bitrate
		result := svc.ValidateFull(profile, "MP3", 320)
		assert.False(t, result.Valid)
		assert.Contains(t, result.Issues[0], "not in allowed")
	})

	t.Run("bitrate only validation", func(t *testing.T) {
		svc := NewProfileService(nil)
		profile := &database.QualityProfile{
			AllowedFormats: "",
			MinBitrate:     256,
		}
		result := svc.ValidateFull(profile, "MP3", 128)
		assert.False(t, result.Valid)
		assert.True(t, len(result.Issues) > 0)
	})
}
