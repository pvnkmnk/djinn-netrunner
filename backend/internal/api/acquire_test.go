package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAcquireTestApp(t *testing.T) (*fiber.App, *gorm.DB, database.User) {
	t.Helper()
	db := setupAPITestDB(t)
	user := database.User{Email: "acquire@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user).Error)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	handler := NewAcquireHandler(db)
	app.Post("/api/acquire", handler.Create)
	return app, db, user
}

func TestAcquireHandler_CreateAlbumWorkflowQueuesArtistAlbumQuery(t *testing.T) {
	app, db, user := setupAcquireTestApp(t)

	req := httptest.NewRequest("POST", "/api/acquire", strings.NewReader("artist=Radiohead&album=OK+Computer"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)

	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var item database.JobItem
	require.NoError(t, db.First(&item).Error)
	assert.Equal(t, "Radiohead", item.Artist)
	assert.Equal(t, "OK Computer", item.Album)
	assert.Empty(t, item.TrackTitle)
	assert.Equal(t, "Radiohead OK Computer", item.NormalizedQuery)
	assert.Equal(t, &user.ID, item.OwnerUserID)
}

func TestAcquireHandler_CreateSongWorkflowQueuesArtistTitleQuery(t *testing.T) {
	app, db, user := setupAcquireTestApp(t)

	req := httptest.NewRequest("POST", "/api/acquire", strings.NewReader("artist=Radiohead&title=Paranoid+Android"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)

	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var item database.JobItem
	require.NoError(t, db.First(&item).Error)
	assert.Equal(t, "Radiohead", item.Artist)
	assert.Empty(t, item.Album)
	assert.Equal(t, "Paranoid Android", item.TrackTitle)
	assert.Equal(t, "Radiohead Paranoid Android", item.NormalizedQuery)
	assert.Equal(t, &user.ID, item.OwnerUserID)
}

func TestAcquireHandler_QualityProfileBOLA(t *testing.T) {
	db := setupAPITestDB(t)

	user1 := database.User{Email: "user1@test.local", PasswordHash: "hash", Role: "user"}
	user2 := database.User{Email: "user2@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user1).Error)
	require.NoError(t, db.Create(&user2).Error)

	// User 1's private quality profile
	qpUser1 := database.QualityProfile{Name: "User 1 Profile", OwnerUserID: &user1.ID}
	require.NoError(t, db.Create(&qpUser1).Error)

	handler := NewAcquireHandler(db)

	t.Run("User 2 cannot use User 1 private profile", func(t *testing.T) {
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user", user2)
			return c.Next()
		})
		app.Post("/api/acquire", handler.Create)

		body := "artist=Radiohead&album=OK+Computer&quality_profile_id=" + qpUser1.ID.String()
		req := httptest.NewRequest("POST", "/api/acquire", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := app.Test(req)

		require.NoError(t, err)
		assert.Equal(t, 403, resp.StatusCode)
	})

	t.Run("User 1 can use own private profile", func(t *testing.T) {
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user", user1)
			return c.Next()
		})
		app.Post("/api/acquire", handler.Create)

		body := "artist=Radiohead&album=OK+Computer&quality_profile_id=" + qpUser1.ID.String()
		req := httptest.NewRequest("POST", "/api/acquire", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := app.Test(req)

		require.NoError(t, err)
		assert.Equal(t, 201, resp.StatusCode)
	})
}
