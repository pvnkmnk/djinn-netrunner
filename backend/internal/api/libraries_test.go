package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
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
	// Use temp dir pattern to create a valid absolute path that doesn't exist
	tmpDir := os.TempDir() + "/netrunner-nonexistent-test-" + uuid.New().String()
	err := validateLibraryPath(tmpDir)
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
	tmpDir := os.TempDir()
	traversalPath := tmpDir + "/netrunner-nonexistent-xyz987/../.."
	// Use filepath.Join to create valid absolute path with traversal
	resolvedPath := filepath.Join(traversalPath, "this-does-not-exist-abc123")
	err := validateLibraryPath(resolvedPath)
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

	// Use temp dir pattern to create a valid absolute path that doesn't exist
	tmpDir := os.TempDir() + "/netrunner-nonexistent-lib-test-" + uuid.New().String()

	body, _ := json.Marshal(map[string]string{
		"name": "My Music",
		"path": tmpDir,
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
	assert.Equal(t, "My Music", result["name"])
	// Stored path must be the cleaned canonical form
	assert.Equal(t, filepath.Clean(tmpDir), result["path"])

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

	// Use temp dir pattern to create a valid absolute path that doesn't exist
	newPath := os.TempDir() + "/netrunner-nonexistent-update-test-" + uuid.New().String()
	body, _ := json.Marshal(map[string]string{"path": newPath})

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

// ---- Duplicate-path handling (the bring-up used to 500 here) ----

// A second library on a path the caller already owns is the documented
// bring-up re-running against an existing volume: it must hand back the
// existing library instead of colliding on the unique path index.
func TestCreateLibrary_DuplicatePathSameOwnerIsIdempotent(t *testing.T) {
	app, db, user := setupLibraryTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-lib-dup-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	body, _ := json.Marshal(map[string]string{"name": "First", "path": tmpDir})
	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)

	var first database.Library
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&first))

	body, _ = json.Marshal(map[string]string{"name": "Second", "path": tmpDir})
	req = httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var returned database.Library
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&returned))
	assert.Equal(t, first.ID, returned.ID, "must return the existing library, not a new one")
	assert.Equal(t, "First", returned.Name)
	require.NotNil(t, returned.OwnerUserID)
	assert.Equal(t, user.ID, *returned.OwnerUserID)

	var count int64
	require.NoError(t, db.Model(&database.Library{}).Where("path = ?", filepath.Clean(tmpDir)).Count(&count).Error)
	assert.EqualValues(t, 1, count, "no second row may be created")
}

// A path already registered by someone else is a genuine conflict, and the
// error must name the existing library so the caller can act on it.
func TestCreateLibrary_DuplicatePathOtherOwnerConflicts(t *testing.T) {
	app, db, _ := setupLibraryTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-lib-conflict-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	other := database.User{Email: "other@example.com", PasswordHash: "hashed", Role: "user"}
	require.NoError(t, db.Create(&other).Error)
	existing := database.Library{Name: "Someone Elses", Path: filepath.Clean(tmpDir), OwnerUserID: &other.ID}
	require.NoError(t, db.Create(&existing).Error)

	body, _ := json.Marshal(map[string]string{"name": "Mine", "path": tmpDir})
	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result["error"], "already exists at this path")
	existingInfo, ok := result["existing_library"].(map[string]interface{})
	require.True(t, ok, "the conflict must identify the existing library")
	assert.Equal(t, existing.ID.String(), existingInfo["id"])
}

// A library owned by nobody (owner_user_id NULL) is still a path conflict for
// a non-admin caller: the row exists and the unique index will not budge.
func TestCreateLibrary_DuplicatePathUnownedConflicts(t *testing.T) {
	app, db, _ := setupLibraryTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-lib-unowned-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	orphan := database.Library{Name: "Legacy", Path: filepath.Clean(tmpDir)}
	require.NoError(t, db.Create(&orphan).Error)

	body, _ := json.Marshal(map[string]string{"name": "Mine", "path": tmpDir})
	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode, "a NULL owner must not be treated as this user's library")
}

// The duplicate-path answer has to match the success path's response shape. The
// earlier version always returned JSON, so the UI submitted the form, got a body
// it could not swap into the modal, and appeared to do nothing.
func TestCreateLibrary_DuplicatePathHtmxReturnsPartial(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	user := database.User{Email: "htmx@example.com", PasswordHash: "hashed", Role: "admin"}
	require.NoError(t, db.Create(&user).Error)

	tmpDir, err := os.MkdirTemp("", "netrunner-lib-htmx-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	clean := filepath.Clean(tmpDir)
	require.NoError(t, db.Create(&database.Library{
		Name: "Existing", Path: clean, OwnerUserID: &user.ID,
	}).Error)

	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	require.NoError(t, engine.LoadFromDir())
	app := fiber.New(fiber.Config{Views: engine})
	handler := NewLibraryHandler(db)
	app.Post("/api/libraries", func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return handler.CreateLibrary(c)
	})

	body, _ := json.Marshal(map[string]string{"name": "Again", "path": tmpDir})
	req := httptest.NewRequest("POST", "/api/libraries", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HX-Request", "true")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode, "a re-submission must not 500")

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"existing_library"`, "HTMX must get the partial, not the JSON conflict")
	assert.Equal(t, "closeModal", resp.Header.Get("HX-Trigger"), "the modal still has to close")

	var count int64
	require.NoError(t, db.Model(&database.Library{}).Where("path = ?", clean).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
