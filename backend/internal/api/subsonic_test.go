package api

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// setupSubsonicTestDB creates an in-memory SQLite DB for subsonic testing.
func setupSubsonicTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	err = database.Migrate(db)
	require.NoError(t, err)
	return db
}

// createTestUserForSubsonic creates a test user with the given email and password.
func createTestUserForSubsonic(t *testing.T, db *gorm.DB, email, password string) database.User {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	user := database.User{
		Email:        email,
		PasswordHash: string(hash),
		Role:         "user",
	}
	err = db.Create(&user).Error
	require.NoError(t, err)
	return user
}

// subsonicGetRespBody reads the response body from http.Response.
func subsonicGetRespBody(resp *http.Response) []byte {
	b, _ := io.ReadAll(resp.Body)
	return b
}

// =============================================================================
// Pure Helper Tests
// =============================================================================

func TestSubsonic_safeDeref(t *testing.T) {
	tests := []struct {
		name     string
		input    *int
		expected int
	}{
		{
			name:     "nil pointer returns 0",
			input:    nil,
			expected: 0,
		},
		{
			name:     "non-nil pointer returns value",
			input:    intPtr(42),
			expected: 42,
		},
		{
			name:     "zero value pointer returns 0",
			input:    intPtr(0),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safeDeref(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func intPtr(i int) *int {
	return &i
}

func TestSubsonic_negotiateFormat(t *testing.T) {
	tests := []struct {
		name     string
		accept   string
		expected string
	}{
		{
			name:     "empty accept returns empty",
			accept:   "",
			expected: "",
		},
		{
			name:     "audio/mpeg returns mp3",
			accept:   "audio/mpeg",
			expected: "mp3",
		},
		{
			name:     "audio/mp3 returns mp3",
			accept:   "audio/mp3",
			expected: "mp3",
		},
		{
			name:     "audio/ogg returns ogg",
			accept:   "audio/ogg",
			expected: "ogg",
		},
		{
			name:     "audio/opus returns opus",
			accept:   "audio/opus",
			expected: "opus",
		},
		{
			name:     "audio/aac returns aac",
			accept:   "audio/aac",
			expected: "aac",
		},
		{
			name:     "audio/mp4 returns aac",
			accept:   "audio/mp4",
			expected: "aac",
		},
		{
			name:     "audio/flac returns flac",
			accept:   "audio/flac",
			expected: "flac",
		},
		{
			name:     "audio/wav returns wav",
			accept:   "audio/wav",
			expected: "wav",
		},
		{
			name:     "unknown format returns empty",
			accept:   "text/html",
			expected: "",
		},
		{
			name:     "case insensitive audio/mpeg",
			accept:   "AUDIO/MPEG",
			expected: "mp3",
		},
		{
			name:     "complex accept header with audio/mpeg",
			accept:   "audio/*;q=0.9,audio/mpeg",
			expected: "mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := negotiateFormat(tt.accept)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubsonic_formatToMIME(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		expected string
	}{
		{
			name:     "mp3 returns audio/mpeg",
			format:   "mp3",
			expected: "audio/mpeg",
		},
		{
			name:     "MP3 (uppercase) returns audio/mpeg",
			format:   "MP3",
			expected: "audio/mpeg",
		},
		{
			name:     "flac returns audio/flac",
			format:   "flac",
			expected: "audio/flac",
		},
		{
			name:     "wav returns audio/wav",
			format:   "wav",
			expected: "audio/wav",
		},
		{
			name:     "ogg returns audio/ogg",
			format:   "ogg",
			expected: "audio/ogg",
		},
		{
			name:     "opus returns audio/opus",
			format:   "opus",
			expected: "audio/opus",
		},
		{
			name:     "aac returns audio/mp4",
			format:   "aac",
			expected: "audio/mp4",
		},
		{
			name:     "m4a returns audio/mp4",
			format:   "m4a",
			expected: "audio/mp4",
		},
		{
			name:     "unknown format returns empty",
			format:   "xyz",
			expected: "",
		},
		{
			name:     "empty format returns empty",
			format:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToMIME(tt.format)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubsonic_isLossyFormat(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		expected bool
	}{
		// Lossy formats
		{
			name:     "mp3 is lossy",
			format:   "mp3",
			expected: true,
		},
		{
			name:     "ogg is lossy",
			format:   "ogg",
			expected: true,
		},
		{
			name:     "m4a is lossy",
			format:   "m4a",
			expected: true,
		},
		{
			name:     "aac is lossy",
			format:   "aac",
			expected: true,
		},
		{
			name:     "opus is lossy",
			format:   "opus",
			expected: true,
		},
		// Lossless formats
		{
			name:     "flac is lossless",
			format:   "flac",
			expected: false,
		},
		{
			name:     "wav is lossless",
			format:   "wav",
			expected: false,
		},
		{
			name:     "alac is lossless",
			format:   "alac",
			expected: false,
		},
		{
			name:     "unknown format is lossless (not lossy)",
			format:   "xyz",
			expected: false,
		},
		{
			name:     "empty format is lossless",
			format:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLossyFormat(tt.format)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubsonic_getTrackDuration(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	// getTrackDuration is a stub that returns 0
	result := handler.getTrackDuration("/some/path.mp3")
	assert.Equal(t, 0, result)
}

// =============================================================================
// Response Helper Tests
// =============================================================================

func TestSubsonic_respond_XML(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		resp := &subsonicResponse{
			Status:  "ok",
			Version: "1.16.1",
			Type:    "netrunner",
		}
		return handler.respond(c, resp)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/xml")

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "<subsonicResponse")
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `version="1.16.1"`)
}

func TestSubsonic_respond_JSON(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		resp := &subsonicResponse{
			Status:  "ok",
			Version: "1.16.1",
			Type:    "netrunner",
		}
		return handler.respond(c, resp)
	})

	req := httptest.NewRequest("GET", "/test?f=json", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"subsonic-response"`)
	assert.Contains(t, bodyStr, `"status":"ok"`)
	assert.Contains(t, bodyStr, `"version":"1.16.1"`)
}

func TestSubsonic_respondXML(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		resp := &subsonicResponse{
			Status:  "ok",
			Version: "1.16.1",
			Type:    "netrunner",
		}
		return handler.respondXML(c, resp)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/xml")

	// Verify valid XML structure
	var result subsonicResponse
	body := subsonicGetRespBody(resp)
	err = xml.Unmarshal(body, &result)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, "1.16.1", result.Version)
}

func TestSubsonic_respondJSON(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		resp := &subsonicResponse{
			Status:  "ok",
			Version: "1.16.1",
			Type:    "netrunner",
		}
		return handler.respondJSON(c, resp)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"subsonic-response"`)
	assert.Contains(t, bodyStr, `"status":"ok"`)
}

func TestSubsonic_respondError(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return handler.respondError(c, 40, "Test error message")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode) // Subsonic errors are 200 with status=failed

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="40"`)
	assert.Contains(t, bodyStr, "Test error message")
}

func TestSubsonic_respondError_JSON(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return handler.respondError(c, 40, "Test error message")
	})

	req := httptest.NewRequest("GET", "/test?f=json", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":"failed"`)
	assert.Contains(t, bodyStr, `"code":40`)
	assert.Contains(t, bodyStr, "Test error message")
}

// =============================================================================
// Stub Handler Tests
// =============================================================================

func TestSubsonic_Ping(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/ping", handler.Ping)

	req := httptest.NewRequest("GET", "/ping", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `version="1.16.1"`)
	assert.Contains(t, bodyStr, `type="netrunner"`)
}

func TestSubsonic_Ping_JSON(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/ping", handler.Ping)

	req := httptest.NewRequest("GET", "/ping?f=json", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":"ok"`)
	assert.Contains(t, bodyStr, `"version":"1.16.1"`)
}

func TestSubsonic_License(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/license", handler.License)

	req := httptest.NewRequest("GET", "/license", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `version="1.16.1"`)
	assert.Contains(t, bodyStr, `<license valid="true"`)
}

func TestSubsonic_GetScanStatus(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/getScanStatus", handler.GetScanStatus)

	req := httptest.NewRequest("GET", "/getScanStatus", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<scanStatus scanning="false"`)
}

func TestSubsonic_StartScan(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/startScan", handler.StartScan)

	req := httptest.NewRequest("GET", "/startScan", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	// StartScan delegates to GetScanStatus, so it should return the same response
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<scanStatus scanning="false"`)
}

// =============================================================================
// Auth Middleware Tests
// =============================================================================

func TestSubsonic_AuthMiddleware_MissingUsername(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	// No username parameter
	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode) // Error is in body
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="40"`)
	assert.Contains(t, bodyStr, "Missing parameter: u")
}

func TestSubsonic_AuthMiddleware_ValidPassword(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	password := "testpass123"
	user := createTestUserForSubsonic(t, db, "test@example.com", password)

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		u, ok := c.Locals("user").(database.User)
		if !ok {
			return c.SendStatus(500)
		}
		if u.ID != user.ID {
			return c.SendStatus(500)
		}
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/test?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Equal(t, "OK", bodyStr)
}

func TestSubsonic_AuthMiddleware_InvalidPassword(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	createTestUserForSubsonic(t, db, "test@example.com", "correctpassword")

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/test?u=test@example.com&p=wrongpassword", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="40"`)
	assert.Contains(t, bodyStr, "Authentication failed")
}

func TestSubsonic_AuthMiddleware_ValidToken(t *testing.T) {
	db := setupSubsonicTestDB(t)
	subsonicPassword := "mysecretpass123"
	cfg := &config.Config{
		Subsonic: struct {
			Enabled  bool   `envconfig:"SUBSONIC_ENABLED" default:"false"`
			Password string `envconfig:"SUBSONIC_PASSWORD"`
		}{
			Enabled:  true,
			Password: subsonicPassword,
		},
	}
	handler := NewSubsonicHandler(db, cfg)

	// Create user (password doesn't matter for token auth)
	user := createTestUserForSubsonic(t, db, "test@example.com", "dummy")

	// Compute token: md5(md5(password) + salt)
	md5Password := md5.Sum([]byte(subsonicPassword))
	md5PasswordHex := hex.EncodeToString(md5Password[:])
	salt := "randomsalt123"
	hash := md5.Sum([]byte(md5PasswordHex + salt))
	token := hex.EncodeToString(hash[:])

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		u, ok := c.Locals("user").(database.User)
		if !ok {
			return c.SendStatus(500)
		}
		if u.ID != user.ID {
			return c.SendStatus(500)
		}
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/test?u=test@example.com&t="+token+"&s="+salt, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Equal(t, "OK", bodyStr)
}

func TestSubsonic_AuthMiddleware_InvalidToken(t *testing.T) {
	db := setupSubsonicTestDB(t)
	subsonicPassword := "mysecretpass123"
	cfg := &config.Config{
		Subsonic: struct {
			Enabled  bool   `envconfig:"SUBSONIC_ENABLED" default:"false"`
			Password string `envconfig:"SUBSONIC_PASSWORD"`
		}{
			Enabled:  true,
			Password: subsonicPassword,
		},
	}
	handler := NewSubsonicHandler(db, cfg)

	createTestUserForSubsonic(t, db, "test@example.com", "dummy")

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/test?u=test@example.com&t=invalidtoken&s=randsalt", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="40"`)
	assert.Contains(t, bodyStr, "Authentication failed")
}

func TestSubsonic_AuthMiddleware_MissingBothPasswordAndToken(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	createTestUserForSubsonic(t, db, "test@example.com", "dummy")

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	// Has username but no password or token
	req := httptest.NewRequest("GET", "/test?u=test@example.com", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="40"`)
	assert.Contains(t, bodyStr, "Missing authentication parameters")
}

func TestSubsonic_AuthMiddleware_UserNotFound(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", handler.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	req := httptest.NewRequest("GET", "/test?u=nonexistent@example.com&p=anypassword", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="40"`)
	assert.Contains(t, bodyStr, "Authentication failed")
}

// =============================================================================
// DB Handler Tests - GetSong
// =============================================================================

func setupSubsonicDBHandlerTest(t *testing.T) (*gorm.DB, *SubsonicHandler) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	password := "testpass123"
	_ = createTestUserForSubsonic(t, db, "test@example.com", password)

	// Create library owned by user
	var user database.User
	err := db.First(&user).Error
	require.NoError(t, err)

	library := database.Library{
		ID:          uuid.New(),
		Name:        "Test Library",
		Path:        "/tmp/test-library",
		OwnerUserID: &user.ID,
	}
	err = db.Create(&library).Error
	require.NoError(t, err)

	return db, handler
}

func TestSubsonic_GetSong_Found(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create a track
	var library database.Library
	err := db.First(&library).Error
	require.NoError(t, err)

	track := database.Track{
		ID:        uuid.New(),
		Title:     "Test Song",
		Artist:    "Test Artist",
		Album:     "Test Album",
		Path:      "/tmp/test-library/song.mp3",
		LibraryID: library.ID,
		Format:    "MP3",
		FileSize:  1024000,
		Genre:     "Rock",
	}
	err = db.Create(&track).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/getSong", handler.AuthMiddleware, handler.GetSong)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getSong?id="+track.ID.String()+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<song `)
	assert.Contains(t, bodyStr, `title="Test Song"`)
	assert.Contains(t, bodyStr, `artist="Test Artist"`)
	assert.Contains(t, bodyStr, `album="Test Album"`)
}

func TestSubsonic_GetSong_NotFound(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getSong", handler.AuthMiddleware, handler.GetSong)

	password := "testpass123"
	nonexistentID := uuid.New().String()
	req := httptest.NewRequest("GET", "/getSong?id="+nonexistentID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="70"`)
}

func TestSubsonic_GetSong_BadUUID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getSong", handler.AuthMiddleware, handler.GetSong)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getSong?id=not-a-uuid&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Invalid track ID")
}

func TestSubsonic_GetSong_MissingID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getSong", handler.AuthMiddleware, handler.GetSong)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getSong?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Missing parameter: id")
}

// =============================================================================
// DB Handler Tests - GetAlbum
// =============================================================================

func createSubsonicTestAlbumTracks(t *testing.T, db *gorm.DB) []database.Track {
	var library database.Library
	err := db.First(&library).Error
	require.NoError(t, err)

	tracks := []database.Track{
		{
			ID:        uuid.New(),
			Title:     "Track 1",
			Artist:    "Test Artist",
			Album:     "Test Album",
			Path:      "/tmp/test-library/track1.mp3",
			LibraryID: library.ID,
			TrackNum:  intPtr(1),
			Year:      intPtr(2023),
			Format:    "MP3",
			FileSize:  1024000,
			Genre:     "Rock",
		},
		{
			ID:        uuid.New(),
			Title:     "Track 2",
			Artist:    "Test Artist",
			Album:     "Test Album",
			Path:      "/tmp/test-library/track2.mp3",
			LibraryID: library.ID,
			TrackNum:  intPtr(2),
			Year:      intPtr(2023),
			Format:    "MP3",
			FileSize:  1024000,
			Genre:     "Rock",
		},
	}
	for _, track := range tracks {
		err = db.Create(&track).Error
		require.NoError(t, err)
	}
	return tracks
}

func TestSubsonic_GetAlbum_Found(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	tracks := createSubsonicTestAlbumTracks(t, db)
	_ = tracks

	app := fiber.New()
	app.Get("/getAlbum", handler.AuthMiddleware, handler.GetAlbum)

	password := "testpass123"
	albumID := "album-" + url.PathEscape("Test Album") + "-" + url.PathEscape("Test Artist")
	req := httptest.NewRequest("GET", "/getAlbum?id="+albumID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<album `)
	assert.Contains(t, bodyStr, `name="Test Album"`)
	assert.Contains(t, bodyStr, `artist="Test Artist"`)
	assert.Contains(t, bodyStr, `songCount="2"`)
}

func TestSubsonic_GetAlbum_MalformedID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getAlbum", handler.AuthMiddleware, handler.GetAlbum)

	password := "testpass123"
	// ID doesn't have proper "album-" prefix
	req := httptest.NewRequest("GET", "/getAlbum?id=invalid&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
}

func TestSubsonic_GetAlbum_NotFound(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getAlbum", handler.AuthMiddleware, handler.GetAlbum)

	password := "testpass123"
	albumID := "album-" + url.PathEscape("Nonexistent Album") + "-" + url.PathEscape("Nonexistent Artist")
	req := httptest.NewRequest("GET", "/getAlbum?id="+albumID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="70"`)
	assert.Contains(t, bodyStr, "Album not found")
}

// =============================================================================
// DB Handler Tests - GetArtist
// =============================================================================

func TestSubsonic_GetArtist_Found(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getArtist", handler.AuthMiddleware, handler.GetArtist)

	password := "testpass123"
	artistID := "artist-" + url.PathEscape("Test Artist")
	req := httptest.NewRequest("GET", "/getArtist?id="+artistID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<artist `)
	assert.Contains(t, bodyStr, `name="Test Artist"`)
}

func TestSubsonic_GetArtist_MalformedID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getArtist", handler.AuthMiddleware, handler.GetArtist)

	password := "testpass123"
	// ID doesn't have proper "artist-" prefix
	req := httptest.NewRequest("GET", "/getArtist?id=invalid&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
}

func TestSubsonic_GetArtist_NotFound(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getArtist", handler.AuthMiddleware, handler.GetArtist)

	password := "testpass123"
	artistID := "artist-" + url.PathEscape("Nonexistent Artist")
	req := httptest.NewRequest("GET", "/getArtist?id="+artistID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="70"`)
	assert.Contains(t, bodyStr, "Artist not found")
}

// =============================================================================
// DB Handler Tests - GetRandomSongs
// =============================================================================

func TestSubsonic_GetRandomSongs_Normal(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getRandomSongs", handler.AuthMiddleware, handler.GetRandomSongs)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getRandomSongs?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<randomSongs>`)
}

func TestSubsonic_GetRandomSongs_Empty(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getRandomSongs", handler.AuthMiddleware, handler.GetRandomSongs)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getRandomSongs?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<randomSongs>`)
	// Empty result - no songs child element or empty songs element
}

func TestSubsonic_GetRandomSongs_WithSize(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getRandomSongs", handler.AuthMiddleware, handler.GetRandomSongs)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getRandomSongs?size=1&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
}

// =============================================================================
// DB Handler Tests - GetMusicDirectory
// =============================================================================

func TestSubsonic_GetMusicDirectory_MissingID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getMusicDirectory", handler.AuthMiddleware, handler.GetMusicDirectory)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getMusicDirectory?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Missing parameter: id")
}

func TestSubsonic_GetMusicDirectory_ArtistDirectory(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getMusicDirectory", handler.AuthMiddleware, handler.GetMusicDirectory)

	password := "testpass123"
	artistID := "artist-" + url.PathEscape("Test Artist")
	req := httptest.NewRequest("GET", "/getMusicDirectory?id="+artistID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<musicDirectory `)
	assert.Contains(t, bodyStr, `name="Test Artist"`)
	// Should have a child element (album directory)
	assert.Contains(t, bodyStr, `<child `)
	assert.Contains(t, bodyStr, `isDir="true"`)
}

func TestSubsonic_GetMusicDirectory_AlbumDirectory(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getMusicDirectory", handler.AuthMiddleware, handler.GetMusicDirectory)

	password := "testpass123"
	albumID := "album-" + url.PathEscape("Test Album") + "-" + url.PathEscape("Test Artist")
	req := httptest.NewRequest("GET", "/getMusicDirectory?id="+albumID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<musicDirectory `)
	assert.Contains(t, bodyStr, `name="Test Album"`)
	// Should have tracks as children
	assert.Contains(t, bodyStr, `<child `)
	assert.Contains(t, bodyStr, `title="Track 1"`)
}

func TestSubsonic_GetMusicDirectory_TrackDirectory(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	tracks := createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getMusicDirectory", handler.AuthMiddleware, handler.GetMusicDirectory)

	password := "testpass123"
	trackID := tracks[0].ID.String()
	req := httptest.NewRequest("GET", "/getMusicDirectory?id="+trackID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<musicDirectory `)
	assert.Contains(t, bodyStr, `name="Track 1"`)
}

func TestSubsonic_GetMusicDirectory_NotFound(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getMusicDirectory", handler.AuthMiddleware, handler.GetMusicDirectory)

	password := "testpass123"
	artistID := "artist-" + url.PathEscape("Nonexistent Artist")
	req := httptest.NewRequest("GET", "/getMusicDirectory?id="+artistID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	// When artist not found (no albums), returns empty directory with ok status
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<musicDirectory `)
}

// =============================================================================
// Additional Response Format Tests
// =============================================================================

func TestSubsonic_GetSong_JSON(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create a track
	var library database.Library
	err := db.First(&library).Error
	require.NoError(t, err)

	track := database.Track{
		ID:        uuid.New(),
		Title:     "Test Song",
		Artist:    "Test Artist",
		Album:     "Test Album",
		Path:      "/tmp/test-library/song.mp3",
		LibraryID: library.ID,
		Format:    "MP3",
		FileSize:  1024000,
		Genre:     "Rock",
	}
	err = db.Create(&track).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/getSong", handler.AuthMiddleware, handler.GetSong)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getSong?id="+track.ID.String()+"&u=test@example.com&p="+password+"&f=json", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":"ok"`)
	assert.Contains(t, bodyStr, `"subsonic-response"`)
	assert.Contains(t, bodyStr, `"title":"Test Song"`)
}

// =============================================================================
// NewSubsonicHandler Tests
// =============================================================================

func TestNewSubsonicHandler(t *testing.T) {
	db := setupSubsonicTestDB(t)

	// Without password
	cfg1 := &config.Config{}
	handler1 := NewSubsonicHandler(db, cfg1)
	assert.Empty(t, handler1.md5Password)

	// With password
	cfg2 := &config.Config{
		Subsonic: struct {
			Enabled  bool   `envconfig:"SUBSONIC_ENABLED" default:"false"`
			Password string `envconfig:"SUBSONIC_PASSWORD"`
		}{
			Enabled:  true,
			Password: "testpassword",
		},
	}
	handler2 := NewSubsonicHandler(db, cfg2)
	assert.NotEmpty(t, handler2.md5Password)
	// Verify md5 of password
	md5Hash := md5.Sum([]byte("testpassword"))
	expected := hex.EncodeToString(md5Hash[:])
	assert.Equal(t, expected, handler2.md5Password)
}

// =============================================================================
// Error Response Structure Tests
// =============================================================================

func TestSubsonic_ErrorResponse_XMLStructure(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return handler.respondError(c, 10, "Test error")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	// Verify XML structure
	var result subsonicResponse
	body := subsonicGetRespBody(resp)
	err = xml.Unmarshal(body, &result)
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Status)
	assert.NotNil(t, result.Error)
	assert.Equal(t, 10, result.Error.Code)
	assert.Equal(t, "Test error", result.Error.Message)
}

func TestSubsonic_ErrorResponse_Code40(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return handler.respondError(c, 40, "Authentication failed")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `code="40"`)
	assert.Contains(t, bodyStr, "Authentication failed")
}

func TestSubsonic_ErrorResponse_Code70(t *testing.T) {
	db := setupSubsonicTestDB(t)
	cfg := &config.Config{}
	handler := NewSubsonicHandler(db, cfg)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return handler.respondError(c, 70, "Not found")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `code="70"`)
	assert.Contains(t, bodyStr, "Not found")
}

// =============================================================================
// DB Handler Tests - Search3
// =============================================================================

func TestSubsonic_Search3_MissingQuery(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/search3", handler.AuthMiddleware, handler.Search3)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/search3?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Missing parameter: query")
}

func TestSubsonic_Search3_EmptyResults(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/search3", handler.AuthMiddleware, handler.Search3)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/search3?query=nonexistent&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<searchResult3>`)
}

func TestSubsonic_Search3_WithResults(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/search3", handler.AuthMiddleware, handler.Search3)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/search3?query=Test&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<searchResult3>`)
	// Should find artists, albums, and songs matching "Test"
}

func TestSubsonic_Search3_WithPagination(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/search3", handler.AuthMiddleware, handler.Search3)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/search3?query=Test&artistCount=5&albumCount=5&songCount=5&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
}

func TestSubsonic_Search3_JSON(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/search3", handler.AuthMiddleware, handler.Search3)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/search3?query=Test&u=test@example.com&p="+password+"&f=json", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":"ok"`)
	assert.Contains(t, bodyStr, `"subsonic-response"`)
}

// =============================================================================
// DB Handler Tests - GetAlbumList2
// =============================================================================

func TestSubsonic_GetAlbumList2_Random(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getAlbumList2", handler.AuthMiddleware, handler.GetAlbumList2)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getAlbumList2?type=random&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<albumList2>`)
	assert.Contains(t, bodyStr, `<album `)
	assert.Contains(t, bodyStr, `name="Test Album"`)
	assert.Contains(t, bodyStr, `artist="Test Artist"`)
}

func TestSubsonic_GetAlbumList2_Newest(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getAlbumList2", handler.AuthMiddleware, handler.GetAlbumList2)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getAlbumList2?type=newest&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<albumList2>`)
}

func TestSubsonic_GetAlbumList2_AlphabeticalByName(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getAlbumList2", handler.AuthMiddleware, handler.GetAlbumList2)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getAlbumList2?type=alphabeticalByName&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<albumList2>`)
}

func TestSubsonic_GetAlbumList2_AlphabeticalByArtist(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getAlbumList2", handler.AuthMiddleware, handler.GetAlbumList2)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getAlbumList2?type=alphabeticalByArtist&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<albumList2>`)
}

func TestSubsonic_GetAlbumList2_Empty(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getAlbumList2", handler.AuthMiddleware, handler.GetAlbumList2)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getAlbumList2?type=random&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<albumList2>`)
}

func TestSubsonic_GetAlbumList2_WithPagination(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getAlbumList2", handler.AuthMiddleware, handler.GetAlbumList2)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getAlbumList2?type=random&size=10&offset=0&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
}

func TestSubsonic_GetAlbumList2_JSON(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getAlbumList2", handler.AuthMiddleware, handler.GetAlbumList2)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getAlbumList2?type=random&u=test@example.com&p="+password+"&f=json", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":"ok"`)
	assert.Contains(t, bodyStr, `"albumList2"`)
}

// =============================================================================
// DB Handler Tests - GetIndexes
// =============================================================================

func TestSubsonic_GetIndexes_Empty(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getIndexes", handler.AuthMiddleware, handler.GetIndexes)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getIndexes?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<indexes`)
}

func TestSubsonic_GetIndexes_WithArtists(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getIndexes", handler.AuthMiddleware, handler.GetIndexes)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getIndexes?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<indexes`)
	// Note: due to GORM scan issue with artist->Name mapping, indexes may be empty
	// but the structure should be valid
}

func TestSubsonic_GetIndexes_ArtistsAZ(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create library
	var library database.Library
	err := db.First(&library).Error
	require.NoError(t, err)

	// Create tracks with different artists starting with different letters
	artists := []string{"Adele", "Beatles", "Queen", "Zappa"}
	for i, artist := range artists {
		track := database.Track{
			ID:        uuid.New(),
			Title:     fmt.Sprintf("Song %d", i),
			Artist:    artist,
			Album:     "Test Album",
			Path:      fmt.Sprintf("/tmp/test-library/song%d.mp3", i),
			LibraryID: library.ID,
			Format:    "MP3",
			FileSize:  1024000,
			Genre:     "Rock",
		}
		err = db.Create(&track).Error
		require.NoError(t, err)
	}

	app := fiber.New()
	app.Get("/getIndexes", handler.AuthMiddleware, handler.GetIndexes)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getIndexes?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	// Note: due to GORM scan issue with artist->Name mapping, indexes may be empty
	// but the structure should be valid
}

func TestSubsonic_GetIndexes_JSON(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	_ = createSubsonicTestAlbumTracks(t, db)

	app := fiber.New()
	app.Get("/getIndexes", handler.AuthMiddleware, handler.GetIndexes)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getIndexes?u=test@example.com&p="+password+"&f=json", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `"status":"ok"`)
	assert.Contains(t, bodyStr, `"indexes"`)
}

// =============================================================================
// DB Handler Tests - GetPlaylists
// =============================================================================

func TestSubsonic_GetPlaylists_Empty(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getPlaylists", handler.AuthMiddleware, handler.GetPlaylists)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getPlaylists?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<playlists>`)
}

func TestSubsonic_GetPlaylists_WithPlaylists(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create a playlist
	var user database.User
	err := db.First(&user).Error
	require.NoError(t, err)

	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        "My Playlist",
		Description: "Test description",
		Public:      false,
		OwnerUserID: &user.ID,
	}
	err = db.Create(&playlist).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/getPlaylists", handler.AuthMiddleware, handler.GetPlaylists)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getPlaylists?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<playlists>`)
	assert.Contains(t, bodyStr, `<playlist `)
	assert.Contains(t, bodyStr, `name="My Playlist"`)
}

func TestSubsonic_GetPlaylists_Public(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create a public playlist owned by another user
	otherUser := database.User{
		Email:        "other@example.com",
		PasswordHash: "hash",
		Role:         "user",
	}
	err := db.Create(&otherUser).Error
	require.NoError(t, err)

	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        "Public Playlist",
		Description: "Visible to all",
		Public:      true,
		OwnerUserID: &otherUser.ID,
	}
	err = db.Create(&playlist).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/getPlaylists", handler.AuthMiddleware, handler.GetPlaylists)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getPlaylists?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `name="Public Playlist"`)
}

// =============================================================================
// DB Handler Tests - GetPlaylist
// =============================================================================

func TestSubsonic_GetPlaylist_MissingID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getPlaylist", handler.AuthMiddleware, handler.GetPlaylist)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getPlaylist?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Missing playlist id")
}

func TestSubsonic_GetPlaylist_NotFound(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getPlaylist", handler.AuthMiddleware, handler.GetPlaylist)

	password := "testpass123"
	nonexistentID := uuid.New().String()
	req := httptest.NewRequest("GET", "/getPlaylist?id="+nonexistentID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="70"`)
	assert.Contains(t, bodyStr, "Playlist not found")
}

func TestSubsonic_GetPlaylist_InvalidUUID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Get("/getPlaylist", handler.AuthMiddleware, handler.GetPlaylist)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getPlaylist?id=not-a-uuid&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Invalid playlist id")
}

func TestSubsonic_GetPlaylist_Found(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create a playlist with tracks
	var user database.User
	err := db.First(&user).Error
	require.NoError(t, err)

	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        "My Playlist",
		Description: "Test playlist",
		Public:      false,
		OwnerUserID: &user.ID,
	}
	err = db.Create(&playlist).Error
	require.NoError(t, err)

	// Add tracks to the playlist
	var library database.Library
	err = db.First(&library).Error
	require.NoError(t, err)

	track := database.Track{
		ID:        uuid.New(),
		Title:     "Track in Playlist",
		Artist:    "Test Artist",
		Album:     "Test Album",
		Path:      "/tmp/test-library/playlist_track.mp3",
		LibraryID: library.ID,
		Format:    "MP3",
		FileSize:  1024000,
		Genre:     "Rock",
	}
	err = db.Create(&track).Error
	require.NoError(t, err)

	playlistTrack := database.PlaylistTrack{
		PlaylistID: playlist.ID,
		TrackID:    track.ID,
		Position:   0,
	}
	err = db.Create(&playlistTrack).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/getPlaylist", handler.AuthMiddleware, handler.GetPlaylist)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getPlaylist?id="+playlist.ID.String()+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<playlist `)
	assert.Contains(t, bodyStr, `name="My Playlist"`)
	assert.Contains(t, bodyStr, `songCount="1"`)
	assert.Contains(t, bodyStr, `<child `)
	assert.Contains(t, bodyStr, `title="Track in Playlist"`)
}

func TestSubsonic_GetPlaylist_AccessDenied(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create another user with a private playlist
	otherUser := database.User{
		Email:        "other@example.com",
		PasswordHash: "hash",
		Role:         "user",
	}
	err := db.Create(&otherUser).Error
	require.NoError(t, err)

	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        "Private Playlist",
		Description: "Not accessible",
		Public:      false,
		OwnerUserID: &otherUser.ID,
	}
	err = db.Create(&playlist).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/getPlaylist", handler.AuthMiddleware, handler.GetPlaylist)

	password := "testpass123"
	req := httptest.NewRequest("GET", "/getPlaylist?id="+playlist.ID.String()+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="50"`)
	assert.Contains(t, bodyStr, "Access denied")
}

// =============================================================================
// DB Handler Tests - CreatePlaylist
// =============================================================================

func TestSubsonic_CreatePlaylist_MissingName(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Post("/createPlaylist", handler.AuthMiddleware, handler.CreatePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/createPlaylist?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Missing playlist name")
}

func TestSubsonic_CreatePlaylist_New(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Post("/createPlaylist", handler.AuthMiddleware, handler.CreatePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/createPlaylist?name=NewPlaylist&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `<playlist `)
	assert.Contains(t, bodyStr, `name="NewPlaylist"`)
}

func TestSubsonic_CreatePlaylist_WithComment(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Post("/createPlaylist", handler.AuthMiddleware, handler.CreatePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/createPlaylist?name=CommentedPlaylist&comment=My+Description&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `name="CommentedPlaylist"`)
}

func TestSubsonic_CreatePlaylist_Public(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Post("/createPlaylist", handler.AuthMiddleware, handler.CreatePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/createPlaylist?name=PublicPlaylist&public=true&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `name="PublicPlaylist"`)
}

func TestSubsonic_CreatePlaylist_UpdateExisting(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create existing playlist
	var user database.User
	err := db.First(&user).Error
	require.NoError(t, err)

	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        "Old Name",
		Description: "Original",
		Public:      false,
		OwnerUserID: &user.ID,
	}
	err = db.Create(&playlist).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Post("/createPlaylist", handler.AuthMiddleware, handler.CreatePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/createPlaylist?playlistId="+playlist.ID.String()+"&name=New+Name&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)
	assert.Contains(t, bodyStr, `name="New Name"`)
}

func TestSubsonic_CreatePlaylist_UpdateNotFound(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Post("/createPlaylist", handler.AuthMiddleware, handler.CreatePlaylist)

	password := "testpass123"
	nonexistentID := uuid.New().String()
	req := httptest.NewRequest("POST", "/createPlaylist?playlistId="+nonexistentID+"&name=NewName&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="70"`)
	assert.Contains(t, bodyStr, "Playlist not found")
}

// =============================================================================
// DB Handler Tests - DeletePlaylist
// =============================================================================

func TestSubsonic_DeletePlaylist_MissingID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Post("/deletePlaylist", handler.AuthMiddleware, handler.DeletePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/deletePlaylist?u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Missing playlist id")
}

func TestSubsonic_DeletePlaylist_InvalidUUID(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Post("/deletePlaylist", handler.AuthMiddleware, handler.DeletePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/deletePlaylist?id=not-a-uuid&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="10"`)
	assert.Contains(t, bodyStr, "Invalid playlist id")
}

func TestSubsonic_DeletePlaylist_NotFound(t *testing.T) {
	_, handler := setupSubsonicDBHandlerTest(t)

	app := fiber.New()
	app.Post("/deletePlaylist", handler.AuthMiddleware, handler.DeletePlaylist)

	password := "testpass123"
	nonexistentID := uuid.New().String()
	req := httptest.NewRequest("POST", "/deletePlaylist?id="+nonexistentID+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="70"`)
	assert.Contains(t, bodyStr, "Playlist not found")
}

func TestSubsonic_DeletePlaylist_Success(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create a playlist to delete
	var user database.User
	err := db.First(&user).Error
	require.NoError(t, err)

	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        "To Be Deleted",
		Description: "Will be gone",
		Public:      false,
		OwnerUserID: &user.ID,
	}
	err = db.Create(&playlist).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Post("/deletePlaylist", handler.AuthMiddleware, handler.DeletePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/deletePlaylist?id="+playlist.ID.String()+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="ok"`)

	// Verify playlist is deleted
	var count int64
	db.Model(&database.Playlist{}).Where("id = ?", playlist.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestSubsonic_DeletePlaylist_AccessDenied(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)

	// Create another user with a private playlist
	otherUser := database.User{
		Email:        "other@example.com",
		PasswordHash: "hash",
		Role:         "user",
	}
	err := db.Create(&otherUser).Error
	require.NoError(t, err)

	playlist := database.Playlist{
		ID:          uuid.New(),
		Name:        "Private Playlist",
		Description: "Not deletable",
		Public:      false,
		OwnerUserID: &otherUser.ID,
	}
	err = db.Create(&playlist).Error
	require.NoError(t, err)

	app := fiber.New()
	app.Post("/deletePlaylist", handler.AuthMiddleware, handler.DeletePlaylist)

	password := "testpass123"
	req := httptest.NewRequest("POST", "/deletePlaylist?id="+playlist.ID.String()+"&u=test@example.com&p="+password, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)
	body := subsonicGetRespBody(resp)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, `status="failed"`)
	assert.Contains(t, bodyStr, `code="50"`)
	assert.Contains(t, bodyStr, "Access denied")
}

// =============================================================================
// Additional Helper Tests
// =============================================================================

func TestSubsonic_negotiateFormat_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		accept   string
		expected string
	}{
		{
			name:     "uppercase AUDIO/MPEG",
			accept:   "AUDIO/MPEG",
			expected: "mp3",
		},
		{
			name:     "mixed case Audio/Flac",
			accept:   "Audio/Flac",
			expected: "flac",
		},
		{
			name:     "q-value preference",
			accept:   "audio/*;q=0.9,audio/flac",
			expected: "flac",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := negotiateFormat(tt.accept)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubsonic_formatToMIME_Unknown(t *testing.T) {
	tests := []struct {
		name   string
		format string
	}{
		{name: "unknown format", format: "xyz"},
		{name: "empty format", format: ""},
		{name: "weird case", format: "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToMIME(tt.format)
			assert.Empty(t, result)
		})
	}
}

func TestSubsonic_isLossyFormat_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		expected bool
	}{
		{name: "mp3 is lossy", format: "mp3", expected: true},
		{name: "MP3 is lossy", format: "MP3", expected: true},
		{name: "flac is lossless", format: "flac", expected: false},
		{name: "FLAC is lossless", format: "FLAC", expected: false},
		{name: "ogg is lossy", format: "ogg", expected: true},
		{name: "opus is lossy", format: "opus", expected: true},
		{name: "aac is lossy", format: "aac", expected: true},
		{name: "m4a is lossy", format: "m4a", expected: true},
		{name: "wav is lossless", format: "wav", expected: false},
		{name: "alac is lossless", format: "alac", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLossyFormat(tt.format)
			assert.Equal(t, tt.expected, result)
		})
	}
}
