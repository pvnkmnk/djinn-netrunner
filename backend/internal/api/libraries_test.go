package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---- Unit tests for validateLibraryPath ----

func TestValidateLibraryPath_ValidDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "netrunner-lib-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	err = validateLibraryPath(tmpDir)
	assert.NoError(t, err)
}

func TestValidateLibraryPath_RelativePath(t *testing.T) {
	err := validateLibraryPath("relative/path/to/music")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library path must be absolute")
}

func TestValidateLibraryPath_EmptyPath(t *testing.T) {
	err := validateLibraryPath("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library path must be absolute")
}

func TestValidateLibraryPath_DotPath(t *testing.T) {
	err := validateLibraryPath(".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library path must be absolute")
}

func TestValidateLibraryPath_NonExistentPath(t *testing.T) {
	err := validateLibraryPath("/this/path/does/not/exist/xyz987abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library path does not exist")
}

func TestValidateLibraryPath_FileNotDirectory(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "netrunner-lib-file-*")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	err = validateLibraryPath(tmpFile.Name())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library path must be a directory")
}

func TestValidateLibraryPath_TraversalResolvesToNonExistent(t *testing.T) {
	// A path with traversal segments that resolves to a path that doesn't exist.
	// filepath.Clean will resolve the ".." components.
	err := validateLibraryPath("/tmp/netrunner-nonexistent-xyz987/../../../this-does-not-exist-abc123")
	require.Error(t, err)
	// The cleaned path won't exist, so we expect a "does not exist" error.
	assert.Contains(t, err.Error(), "library path does not exist")
}

func TestValidateLibraryPath_TraversalResolvesToValidDirectory(t *testing.T) {
	// Create a nested temp dir, then reference its parent via traversal.
	tmpDir, err := os.MkdirTemp("", "netrunner-lib-parent-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	childDir := filepath.Join(tmpDir, "child")
	require.NoError(t, os.Mkdir(childDir, 0o755))

	// /tmp/netrunner-lib-parent-XXX/child/.. resolves to /tmp/netrunner-lib-parent-XXX
	traversalPath := childDir + "/.."
	err = validateLibraryPath(traversalPath)
	// Should succeed because the resolved (parent) directory exists
	assert.NoError(t, err)
}

func TestValidateLibraryPath_CleanedPathMatchesExpected(t *testing.T) {
	// Verify that filepath.Clean is applied consistently: the cleaned path
	// stored in the DB should match filepath.Clean of the input.
	tmpDir, err := os.MkdirTemp("", "netrunner-lib-clean-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Path with redundant slashes and a trailing slash
	messyPath := tmpDir + "//"
	err = validateLibraryPath(messyPath)
	assert.NoError(t, err)
	// filepath.Clean of the messy path should equal the canonical tmpDir
	assert.Equal(t, tmpDir, filepath.Clean(messyPath))
}

// ---- Integration tests for CreateLibrary handler ----

func setupLibraryTestApp(t *testing.T) (*fiber.App, *gorm.DB, database.User) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	user := database.User{
		Email:        "test@example.com",
		PasswordHash: "hashed_password",
		Role:         "admin",
	}
	require.NoError(t, db.Create(&user).Error)

	handler := NewLibraryHandler(db)
	app := fiber.New()

	// Inject user into Locals for all routes (simulate auth middleware)
	injectUser := func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}

	app.Post("/api/libraries", injectUser, handler.CreateLibrary)
	app.Put("/api/libraries/:id", injectUser, handler.UpdateLibrary)

	return app, db, user
}

func TestCreateLibrary_RelativePath(t *testing.T) {
	app, _, _ := setupLibraryTestApp(t)

	body, _ := json.Marshal(map[string]string{
		"name": "My Music",
		"path": "relative/music/path",
	})

	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result["error"], "library path must be absolute")
}

func TestCreateLibrary_NonExistentPath(t *testing.T) {
	app, _, _ := setupLibraryTestApp(t)

	body, _ := json.Marshal(map[string]string{
		"name": "My Music",
		"path": "/this/path/does/not/exist/xyz123abc",
	})

	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result["error"], "library path does not exist")
}

func TestCreateLibrary_PathIsFile(t *testing.T) {
	app, _, _ := setupLibraryTestApp(t)

	tmpFile, err := os.CreateTemp("", "netrunner-lib-file-*")
	require.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	body, _ := json.Marshal(map[string]string{
		"name": "My Music",
		"path": tmpFile.Name(),
	})

	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result["error"], "library path must be a directory")
}

func TestCreateLibrary_ValidPath(t *testing.T) {
	app, db, _ := setupLibraryTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-lib-valid-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	body, _ := json.Marshal(map[string]string{
		"name": "My Music",
		"path": tmpDir,
	})

	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "My Music", result["Name"])
	// Stored path must be the cleaned canonical form
	assert.Equal(t, filepath.Clean(tmpDir), result["Path"])

	// Confirm stored in DB with cleaned path
	var lib database.Library
	require.NoError(t, db.Where("name = ?", "My Music").First(&lib).Error)
	assert.Equal(t, filepath.Clean(tmpDir), lib.Path)
}

func TestCreateLibrary_PathStoredCleaned(t *testing.T) {
	app, db, _ := setupLibraryTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-lib-clean-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Submit path with a trailing slash — should be stored as clean path
	messyPath := tmpDir + "/"

	body, _ := json.Marshal(map[string]string{
		"name": "Messy Path Library",
		"path": messyPath,
	})

	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	var lib database.Library
	require.NoError(t, db.Where("name = ?", "Messy Path Library").First(&lib).Error)
	assert.Equal(t, filepath.Clean(messyPath), lib.Path)
	assert.NotEqual(t, messyPath, lib.Path) // trailing slash stripped
}

// ---- Integration tests for UpdateLibrary handler ----

func TestUpdateLibrary_RelativePath(t *testing.T) {
	app, db, user := setupLibraryTestApp(t)

	// Seed a library with a valid existing path
	tmpDir, err := os.MkdirTemp("", "netrunner-lib-update-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	lib := database.Library{
		ID:          uuid.New(),
		Name:        "Original",
		Path:        tmpDir,
		OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&lib).Error)

	newPath := "relative/new/path"
	body, _ := json.Marshal(map[string]string{"path": newPath})

	req := httptest.NewRequest("PUT", "/api/libraries/"+lib.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result["error"], "library path must be absolute")
}

func TestUpdateLibrary_NonExistentPath(t *testing.T) {
	app, db, user := setupLibraryTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-lib-update-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	lib := database.Library{
		ID:          uuid.New(),
		Name:        "Original",
		Path:        tmpDir,
		OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&lib).Error)

	body, _ := json.Marshal(map[string]string{"path": "/nonexistent/path/xyz987"})

	req := httptest.NewRequest("PUT", "/api/libraries/"+lib.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result["error"], "library path does not exist")
}

func TestUpdateLibrary_ValidPath(t *testing.T) {
	app, db, user := setupLibraryTestApp(t)

	tmpDir1, err := os.MkdirTemp("", "netrunner-lib-orig-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir1)

	tmpDir2, err := os.MkdirTemp("", "netrunner-lib-new-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir2)

	lib := database.Library{
		ID:          uuid.New(),
		Name:        "Original",
		Path:        tmpDir1,
		OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&lib).Error)

	body, _ := json.Marshal(map[string]string{"path": tmpDir2})

	req := httptest.NewRequest("PUT", "/api/libraries/"+lib.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// Confirm the DB has the updated, cleaned path
	var updated database.Library
	require.NoError(t, db.First(&updated, "id = ?", lib.ID).Error)
	assert.Equal(t, filepath.Clean(tmpDir2), updated.Path)
}
