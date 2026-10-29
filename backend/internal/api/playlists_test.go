package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ─────────────────────────────────────────────
// Setup helpers
// ─────────────────────────────────────────────

func setupPlaylistTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test DB: %v", err)
	}
	database.Migrate(db)
	return db
}

func createTestUser(t *testing.T, db *gorm.DB, email, role string) database.User {
	t.Helper()
	user := database.User{
		Email:        email,
		PasswordHash: "hashed",
		Role:         role,
	}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func createTestPlaylist(t *testing.T, db *gorm.DB, ownerID uint64, name string) database.Playlist {
	t.Helper()
	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        name,
		Description: "test description",
		Public:      false,
		OwnerUserID: &ownerID,
	}
	require.NoError(t, db.Create(&playlist).Error)
	return playlist
}

func createTestLibraryAndTrack(t *testing.T, db *gorm.DB, ownerID uint64) (database.Library, database.Track) {
	t.Helper()
	lib := database.Library{
		ID:          uuid.New(),
		Name:        "Test Library",
		Path:        t.TempDir(),
		OwnerUserID: &ownerID,
	}
	require.NoError(t, db.Create(&lib).Error)

	track := database.Track{
		ID:        uuid.New(),
		LibraryID: lib.ID,
		Title:     "Test Track",
		Artist:    "Test Artist",
		Album:     "Test Album",
		Path:      t.TempDir() + "/track.mp3",
	}
	require.NoError(t, db.Create(&track).Error)
	return lib, track
}

func withAuthUser(c fiber.Ctx, user database.User) {
	c.Locals("user", user)
}

func getRespBody(resp *http.Response) []byte {
	b, _ := io.ReadAll(resp.Body)
	return b
}

// ─────────────────────────────────────────────
// Auth enforcement tests
// ─────────────────────────────────────────────

func TestPlaylistHandler_List_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists", handler.List)

	req := httptest.NewRequest("GET", "/api/playlists", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestPlaylistHandler_List_BoundedPagination(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "paged@example.com", "user")
	createTestPlaylist(t, db, user.ID, "first")
	createTestPlaylist(t, db, user.ID, "second")
	createTestPlaylist(t, db, user.ID, "third")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.List(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/playlists?page=2&limit=2", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))

	var playlists []database.Playlist
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&playlists))
	require.Len(t, playlists, 1)
	// "third" is newest and lands on page 1, so page 2 must be exactly "first".
	// A length-only assertion passes even when the handler returns a row from
	// the wrong page, which is what this pins.
	assert.Equal(t, "first", playlists[0].Name)
}

func TestPlaylistHandler_List_StableOrderAcrossPages(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "stable@example.com", "user")
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"first", "second", "third"} {
		playlist := createTestPlaylist(t, db, user.ID, name)
		require.NoError(t, db.Model(&database.Playlist{}).Where("id = ?", playlist.ID).
			Update("created_at", created.Add(time.Duration(i)*time.Minute)).Error)
	}

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.List(c)
	})

	names := func(query string) []string {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/playlists"+query, nil))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var playlists []database.Playlist
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&playlists))
		out := make([]string, 0, len(playlists))
		for _, p := range playlists {
			out = append(out, p.Name)
		}
		return out
	}

	assert.Equal(t, []string{"third", "second"}, names("?page=1&limit=2"))
	assert.Equal(t, []string{"first"}, names("?page=2&limit=2"))
	assert.Empty(t, names("?page=3&limit=2"))
}

func TestPlaylistHandler_List_TiedTimestampsKeepAStableBoundary(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "tied@example.com", "user")
	tied := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	createdIDs := make([]uuid.UUID, 0, 4)
	for _, name := range []string{"a", "b", "c", "d"} {
		playlist := createTestPlaylist(t, db, user.ID, name)
		require.NoError(t, db.Model(&database.Playlist{}).Where("id = ?", playlist.ID).
			Update("created_at", tied).Error)
		createdIDs = append(createdIDs, playlist.ID)
	}

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.List(c)
	})

	seen := map[uuid.UUID]int{}
	for page := 1; page <= 2; page++ {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/api/playlists?page=%d&limit=2", page), nil))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var playlists []database.Playlist
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&playlists))
		require.Len(t, playlists, 2)
		for _, p := range playlists {
			seen[p.ID]++
		}
	}

	// Every row appears exactly once across the two pages: ties must not
	// duplicate or drop a playlist at the page boundary.
	require.Len(t, seen, len(createdIDs))
	for id, count := range seen {
		assert.Equal(t, 1, count, "playlist %s appeared %d times across pages", id, count)
	}
}

func TestPlaylistHandler_List_InvalidPaginationUsesAPIError(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "invalid-page@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.List(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/playlists?limit=101", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "invalid_pagination", body.Code)
	assert.Equal(t, "invalid pagination", body.Error)
}

func TestPlaylistHandler_Get_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists/:id", handler.Get)

	req := httptest.NewRequest("GET", "/api/playlists/"+uuid.New().String(), nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestPlaylistHandler_Create_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists", handler.Create)

	body, _ := json.Marshal(map[string]interface{}{"name": "Test", "description": "", "public": false})
	req := httptest.NewRequest("POST", "/api/playlists", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestPlaylistHandler_Update_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Patch("/api/playlists/:id", handler.Update)

	body, _ := json.Marshal(map[string]interface{}{"name": "Updated"})
	req := httptest.NewRequest("PATCH", "/api/playlists/"+uuid.New().String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestPlaylistHandler_Delete_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Delete("/api/playlists/:id", handler.Delete)

	req := httptest.NewRequest("DELETE", "/api/playlists/"+uuid.New().String(), nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestPlaylistHandler_AddTrack_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists/:id/tracks", handler.AddTrack)

	body, _ := json.Marshal(map[string]string{"track_id": uuid.New().String()})
	req := httptest.NewRequest("POST", "/api/playlists/"+uuid.New().String()+"/tracks", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestPlaylistHandler_RemoveTrack_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Delete("/api/playlists/:id/tracks/:trackId", handler.RemoveTrack)

	req := httptest.NewRequest("DELETE", "/api/playlists/"+uuid.New().String()+"/tracks/"+uuid.New().String(), nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestPlaylistHandler_Reorder_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Put("/api/playlists/:id/tracks/reorder", handler.Reorder)

	body, _ := json.Marshal(map[string][]string{"track_ids": {uuid.New().String()}})
	req := httptest.NewRequest("PUT", "/api/playlists/"+uuid.New().String()+"/tracks/reorder", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 401, resp.StatusCode)
}

func TestPlaylistHandler_PlaylistsPage_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/playlists", handler.PlaylistsPage)

	req := httptest.NewRequest("GET", "/playlists", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 302, resp.StatusCode)
}

func TestPlaylistHandler_RenderPlaylistsPartial_Unauthenticated(t *testing.T) {
	db := setupPlaylistTestDB(t)
	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/partials/playlists", handler.RenderPlaylistsPartial)

	req := httptest.NewRequest("GET", "/partials/playlists", nil)
	resp, _ := app.Test(req)
	// requirePartialUser redirects to "/" for non-HTMX unauthenticated requests
	assert.Equal(t, 302, resp.StatusCode)
}

// ─────────────────────────────────────────────
// Playlist CRUD tests
// ─────────────────────────────────────────────

func TestPlaylistHandler_Create_Success(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Create(c)
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "My Playlist", "description": "A test playlist", "public": true})
	req := httptest.NewRequest("POST", "/api/playlists", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 201, resp.StatusCode)

	var playlist database.Playlist
	require.NoError(t, json.Unmarshal(getRespBody(resp), &playlist))
	assert.Equal(t, "My Playlist", playlist.Name)
	assert.Equal(t, "A test playlist", playlist.Description)
	assert.True(t, playlist.Public)
	assert.NotEqual(t, uuid.Nil, playlist.ID)
}

func TestPlaylistHandler_Create_MissingName(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Create(c)
	})

	body, _ := json.Marshal(map[string]interface{}{"description": "No name"})
	req := httptest.NewRequest("POST", "/api/playlists", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 400, resp.StatusCode)
}

func TestPlaylistHandler_List_UserSeesOwn(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user1 := createTestUser(t, db, "user1@example.com", "user")
	user2 := createTestUser(t, db, "user2@example.com", "user")

	// Create playlists: 2 for user1, 1 for user2
	createTestPlaylist(t, db, user1.ID, "Playlist 1")
	createTestPlaylist(t, db, user1.ID, "Playlist 2")
	createTestPlaylist(t, db, user2.ID, "Other Playlist")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user1)
		return handler.List(c)
	})

	req := httptest.NewRequest("GET", "/api/playlists", nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)

	var playlists []database.Playlist
	require.NoError(t, json.Unmarshal(getRespBody(resp), &playlists))
	assert.Len(t, playlists, 2)
}

func TestPlaylistHandler_List_AdminSeesAll(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user1 := createTestUser(t, db, "user1@example.com", "user")
	user2 := createTestUser(t, db, "user2@example.com", "user")
	admin := createTestUser(t, db, "admin@example.com", "admin")

	createTestPlaylist(t, db, user1.ID, "Playlist 1")
	createTestPlaylist(t, db, user1.ID, "Playlist 2")
	createTestPlaylist(t, db, user2.ID, "Other Playlist")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, admin)
		return handler.List(c)
	})

	req := httptest.NewRequest("GET", "/api/playlists", nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)

	var playlists []database.Playlist
	require.NoError(t, json.Unmarshal(getRespBody(resp), &playlists))
	assert.Len(t, playlists, 3)
}

func TestPlaylistHandler_Get_Success(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "My Playlist")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Get(c)
	})

	req := httptest.NewRequest("GET", "/api/playlists/"+playlist.ID.String(), nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(getRespBody(resp), &result))
	assert.NotNil(t, result["playlist"])
	assert.NotNil(t, result["tracks"])
}

func TestPlaylistHandler_Get_InvalidUUID(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Get(c)
	})

	req := httptest.NewRequest("GET", "/api/playlists/not-a-uuid", nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 400, resp.StatusCode)
}

func TestPlaylistHandler_Get_NotFound(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Get(c)
	})

	req := httptest.NewRequest("GET", "/api/playlists/"+uuid.New().String(), nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 404, resp.StatusCode)
}

func TestPlaylistHandler_AddTrack_BOLA(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user1 := createTestUser(t, db, "user1@example.com", "user")
	user2 := createTestUser(t, db, "user2@example.com", "user")
	admin := createTestUser(t, db, "admin@example.com", "admin")

	user1Playlist := createTestPlaylist(t, db, user1.ID, "User 1 Playlist")
	_, user2Track := createTestLibraryAndTrack(t, db, user2.ID)

	app := fiber.New()
	handler := NewPlaylistHandler(db)

	app.Post("/api/playlists/:id/tracks/user1", func(c fiber.Ctx) error {
		withAuthUser(c, user1)
		return handler.AddTrack(c)
	})

	body, _ := json.Marshal(map[string]string{"track_id": user2Track.ID.String()})
	req := httptest.NewRequest("POST", "/api/playlists/"+user1Playlist.ID.String()+"/tracks/user1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 404, resp.StatusCode)

	app.Post("/api/playlists/:id/tracks/admin", func(c fiber.Ctx) error {
		withAuthUser(c, admin)
		return handler.AddTrack(c)
	})

	reqAdmin := httptest.NewRequest("POST", "/api/playlists/"+user1Playlist.ID.String()+"/tracks/admin", bytes.NewBuffer(body))
	reqAdmin.Header.Set("Content-Type", "application/json")
	respAdmin, _ := app.Test(reqAdmin)

	assert.Equal(t, 201, respAdmin.StatusCode)
}

func TestPlaylistHandler_Update_Success(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "Original Name")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Patch("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Update(c)
	})

	newName := "Updated Name"
	body, _ := json.Marshal(map[string]interface{}{"name": newName})
	req := httptest.NewRequest("PATCH", "/api/playlists/"+playlist.ID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)

	var updated database.Playlist
	require.NoError(t, json.Unmarshal(getRespBody(resp), &updated))
	assert.Equal(t, newName, updated.Name)
}

func TestPlaylistHandler_Update_EmptyName(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "Original Name")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Patch("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Update(c)
	})

	emptyName := ""
	body, _ := json.Marshal(map[string]interface{}{"name": emptyName})
	req := httptest.NewRequest("PATCH", "/api/playlists/"+playlist.ID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 400, resp.StatusCode)
}

func TestPlaylistHandler_Update_NotFound(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Patch("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Update(c)
	})

	newName := "Updated Name"
	body, _ := json.Marshal(map[string]interface{}{"name": newName})
	req := httptest.NewRequest("PATCH", "/api/playlists/"+uuid.New().String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 404, resp.StatusCode)
}

func TestPlaylistHandler_Delete_Success(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "To Delete")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Delete("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Delete(c)
	})

	req := httptest.NewRequest("DELETE", "/api/playlists/"+playlist.ID.String(), nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 204, resp.StatusCode)

	// Verify playlist is gone
	var count int64
	db.Model(&database.Playlist{}).Where("id = ?", playlist.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestPlaylistHandler_Delete_OtherUsersPlaylist(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user1 := createTestUser(t, db, "user1@example.com", "user")
	admin := createTestUser(t, db, "admin@example.com", "admin")
	playlist := createTestPlaylist(t, db, user1.ID, "User1 Playlist")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	// Admin bypasses ownership checks - they can delete any playlist
	app.Delete("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, admin)
		return handler.Delete(c)
	})

	req := httptest.NewRequest("DELETE", "/api/playlists/"+playlist.ID.String(), nil)
	resp, _ := app.Test(req)

	// Admin can delete any playlist (no ownership check for admin role)
	assert.Equal(t, 204, resp.StatusCode)
}

func TestPlaylistHandler_Delete_NotFound(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Delete("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Delete(c)
	})

	req := httptest.NewRequest("DELETE", "/api/playlists/"+uuid.New().String(), nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 404, resp.StatusCode)
}

// ─────────────────────────────────────────────
// Track operation tests
// ─────────────────────────────────────────────

func TestPlaylistHandler_AddTrack_Success(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "My Playlist")
	_, track := createTestLibraryAndTrack(t, db, user.ID)

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists/:id/tracks", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.AddTrack(c)
	})

	body, _ := json.Marshal(map[string]string{"track_id": track.ID.String()})
	req := httptest.NewRequest("POST", "/api/playlists/"+playlist.ID.String()+"/tracks", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 201, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(getRespBody(resp), &result))
	assert.Equal(t, "track added", result["message"])
	assert.Equal(t, float64(0), result["position"])
}

func TestPlaylistHandler_AddTrack_InvalidTrackID(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "My Playlist")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists/:id/tracks", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.AddTrack(c)
	})

	body, _ := json.Marshal(map[string]string{"track_id": "not-a-uuid"})
	req := httptest.NewRequest("POST", "/api/playlists/"+playlist.ID.String()+"/tracks", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 400, resp.StatusCode)
}

func TestPlaylistHandler_AddTrack_TrackNotFound(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "My Playlist")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists/:id/tracks", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.AddTrack(c)
	})

	body, _ := json.Marshal(map[string]string{"track_id": uuid.New().String()})
	req := httptest.NewRequest("POST", "/api/playlists/"+playlist.ID.String()+"/tracks", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 404, resp.StatusCode)
}

func TestPlaylistHandler_AddTrack_PlaylistNotFound(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	_, track := createTestLibraryAndTrack(t, db, user.ID)

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists/:id/tracks", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.AddTrack(c)
	})

	body, _ := json.Marshal(map[string]string{"track_id": track.ID.String()})
	req := httptest.NewRequest("POST", "/api/playlists/"+uuid.New().String()+"/tracks", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 404, resp.StatusCode)
}

func TestPlaylistHandler_RemoveTrack_Success(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "My Playlist")
	_, track := createTestLibraryAndTrack(t, db, user.ID)

	// Add track first
	playlistTrack := database.PlaylistTrack{
		PlaylistID: playlist.ID,
		TrackID:    track.ID,
		Position:   0,
	}
	require.NoError(t, db.Create(&playlistTrack).Error)

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Delete("/api/playlists/:id/tracks/:trackId", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.RemoveTrack(c)
	})

	req := httptest.NewRequest("DELETE", "/api/playlists/"+playlist.ID.String()+"/tracks/"+track.ID.String(), nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 204, resp.StatusCode)

	// Verify track is removed
	var count int64
	db.Model(&database.PlaylistTrack{}).Where("playlist_id = ? AND track_id = ?", playlist.ID, track.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestPlaylistHandler_Reorder_Success(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "My Playlist")
	_, track1 := createTestLibraryAndTrack(t, db, user.ID)
	_, track2 := createTestLibraryAndTrack(t, db, user.ID)

	// Add tracks
	pt1 := database.PlaylistTrack{PlaylistID: playlist.ID, TrackID: track1.ID, Position: 0}
	pt2 := database.PlaylistTrack{PlaylistID: playlist.ID, TrackID: track2.ID, Position: 1}
	require.NoError(t, db.Create(&pt1).Error)
	require.NoError(t, db.Create(&pt2).Error)

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Put("/api/playlists/:id/tracks/reorder", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Reorder(c)
	})

	body, _ := json.Marshal(map[string][]string{"track_ids": {track2.ID.String(), track1.ID.String()}})
	req := httptest.NewRequest("PUT", "/api/playlists/"+playlist.ID.String()+"/tracks/reorder", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)

	// Verify new order
	var pts []database.PlaylistTrack
	db.Where("playlist_id = ?", playlist.ID).Order("position ASC").Find(&pts)
	require.Len(t, pts, 2)
	assert.Equal(t, track2.ID, pts[0].TrackID)
	assert.Equal(t, track1.ID, pts[1].TrackID)
}

// ─────────────────────────────────────────────
// HTMX behavior tests
// ─────────────────────────────────────────────

func TestPlaylistHandler_Create_HTMXRequest(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Post("/api/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Create(c)
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "HTMX Playlist"})
	req := httptest.NewRequest("POST", "/api/playlists", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HX-Request", "true")
	resp, _ := app.Test(req)

	// HTMX requests to Create call RenderPlaylistsPartial which tries to render a template.
	// Without template engine configured, this returns 500. This test verifies the
	// HTMX code path is exercised; integration tests with template config would pass.
	assert.Equal(t, 500, resp.StatusCode)
}

func TestPlaylistHandler_Delete_HTMXRequest(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	playlist := createTestPlaylist(t, db, user.ID, "To Delete")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Delete("/api/playlists/:id", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.Delete(c)
	})

	req := httptest.NewRequest("DELETE", "/api/playlists/"+playlist.ID.String(), nil)
	req.Header.Set("HX-Request", "true")
	resp, _ := app.Test(req)

	// HTMX requests to Delete call RenderPlaylistsPartial which tries to render a template.
	// Without template engine configured, this returns 500. This test verifies the
	// HTMX code path is exercised; integration tests with template config would pass.
	assert.Equal(t, 500, resp.StatusCode)
}

// ─────────────────────────────────────────────
// Page/Partial rendering tests
// ─────────────────────────────────────────────

func TestPlaylistHandler_PlaylistsPage_WithUser(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.PlaylistsPage(c)
	})

	req := httptest.NewRequest("GET", "/playlists", nil)
	resp, _ := app.Test(req)

	// RenderPage tries to render template which won't work in test
	// but the important thing is it passes auth check
	assert.NotEqual(t, 302, resp.StatusCode)
}

func TestPlaylistHandler_RenderPlaylistsPartial_WithUser(t *testing.T) {
	db := setupPlaylistTestDB(t)
	user := createTestUser(t, db, "test@example.com", "user")
	createTestPlaylist(t, db, user.ID, "Playlist 1")

	app := fiber.New()
	handler := NewPlaylistHandler(db)
	app.Get("/partials/playlists", func(c fiber.Ctx) error {
		withAuthUser(c, user)
		return handler.RenderPlaylistsPartial(c)
	})

	req := httptest.NewRequest("GET", "/partials/playlists", nil)
	resp, _ := app.Test(req)

	// RenderPlaylistsPartial calls c.Render which needs template engine configured.
	// Without it, this returns 500. This test verifies auth passes;
	// integration tests with template config would verify rendering.
	assert.Equal(t, 500, resp.StatusCode)
}
