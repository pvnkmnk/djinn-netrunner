package api

import (
	"context"
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

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/acquire", strings.NewReader("artist=Radiohead&album=OK+Computer"))
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

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/acquire", strings.NewReader("artist=Radiohead&title=Paranoid+Android"))
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
