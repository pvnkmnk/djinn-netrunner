package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
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

// Library.Path carries a global unique index while every list is owner-scoped.
// A row with no owner therefore sits in the worst place: it holds a path nobody
// can see, so the user who most wants that path cannot create it, and the empty
// state tells them they have no libraries at all. Before this, the collision came
// back as a 409 identical to a foreign-owned row's, carrying nothing that said
// the row was claimable.
//
// These tests pin the three answers apart, and the adoption write that has to be
// atomic: the WHERE clause that claims the row carries owner_user_id IS NULL, so
// two operators racing for the same orphan cannot both win it.

// adoptionTestApp builds an app with the create and adopt routes, the template
// engine, and a non-admin caller — admin would see every library and could not
// reproduce the invisibility that makes this defect.
func adoptionTestApp(t *testing.T) (*fiber.App, *gorm.DB, database.User) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	user := database.User{Email: "adopter@example.com", PasswordHash: "hashed", Role: "user"}
	require.NoError(t, db.Create(&user).Error)

	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	require.NoError(t, engine.LoadFromDir())

	handler := NewLibraryHandler(db)
	app := fiber.New(fiber.Config{Views: engine})
	inject := func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}
	app.Post("/api/libraries", inject, handler.CreateLibrary)
	app.Post("/api/libraries/:id/adopt", inject, handler.AdoptLibrary)

	return app, db, user
}

func postJSON(t *testing.T, app *fiber.App, target string, payload map[string]string, htmx bool) *http.Response {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()

	var out map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

// An owner-less row must be reported as adoptable, with the route to adopt it.
// A conflict that cannot be acted on is what left the documented path stuck.
func TestCreateLibrary_OwnerlessPathIsOfferedForAdoption(t *testing.T) {
	app, db, user := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-adopt-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	orphan := database.Library{Name: "Legacy", Path: filepath.Clean(tmpDir)}
	require.NoError(t, db.Create(&orphan).Error)

	resp := postJSON(t, app, "/api/libraries", map[string]string{"name": "Mine", "path": tmpDir}, false)
	assert.Equal(t, 409, resp.StatusCode, "an owner-less row still occupies the path")

	result := decodeJSON(t, resp)
	assert.Equal(t, true, result["adoptable"],
		"an owner-less row is claimable, and the caller has to be told so")
	assert.Contains(t, result["error"], "no owner")
	assert.Equal(t, "/api/libraries/"+orphan.ID.String()+"/adopt", result["adopt_path"],
		"the offer must name the route that performs it")

	// And it must not have been silently claimed by the create attempt.
	var after database.Library
	require.NoError(t, db.First(&after, "id = ?", orphan.ID).Error)
	assert.Nil(t, after.OwnerUserID, "adoption is a deliberate second step, not a side effect of creating")
	assert.NotNil(t, user.ID)
}

// A row someone else owns is a real conflict and must not look adoptable.
func TestCreateLibrary_ForeignOwnedPathIsNotAdoptable(t *testing.T) {
	app, db, _ := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-foreign-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	other := database.User{Email: "other@example.com", PasswordHash: "hashed", Role: "user"}
	require.NoError(t, db.Create(&other).Error)
	existing := database.Library{Name: "Theirs", Path: filepath.Clean(tmpDir), OwnerUserID: &other.ID}
	require.NoError(t, db.Create(&existing).Error)

	resp := postJSON(t, app, "/api/libraries", map[string]string{"name": "Mine", "path": tmpDir}, false)
	assert.Equal(t, 409, resp.StatusCode)

	result := decodeJSON(t, resp)
	assert.Equal(t, false, result["adoptable"],
		"another account's library is not up for adoption")
	assert.NotContains(t, result["error"], "no owner")
	assert.NotContains(t, result, "adopt_path",
		"do not hand back a route that can only refuse this caller")
}

// The adoption write itself: the row becomes the caller's and shows up in their
// owner-scoped list, which is the whole point of the slice.
func TestAdoptLibrary_ClaimsTheRowAndMakesItVisible(t *testing.T) {
	app, db, user := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-claim-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	orphan := database.Library{Name: "Legacy", Path: filepath.Clean(tmpDir)}
	require.NoError(t, db.Create(&orphan).Error)

	var visible []database.Library
	require.NoError(t, db.Where("owner_user_id = ?", user.ID).Find(&visible).Error)
	assert.Empty(t, visible, "an owner-less row is invisible to its would-be owner")

	resp := postJSON(t, app, "/api/libraries/"+orphan.ID.String()+"/adopt", map[string]string{}, false)
	require.Equal(t, 200, resp.StatusCode)

	var adopted database.Library
	require.NoError(t, db.First(&adopted, "id = ?", orphan.ID).Error)
	require.NotNil(t, adopted.OwnerUserID, "the row must have an owner after adoption")
	assert.Equal(t, user.ID, *adopted.OwnerUserID)
	assert.Equal(t, "Legacy", adopted.Name, "adoption claims the row; it does not replace it")

	visible = nil
	require.NoError(t, db.Where("owner_user_id = ?", user.ID).Find(&visible).Error)
	require.Len(t, visible, 1, "the adopted library must appear in the new owner's list")
	assert.Equal(t, orphan.ID, visible[0].ID)
}

// Losing the owner that left a row unowned is unrecoverable — the row is
// invisible to every non-admin — so the trail has to name who took it and what.
func TestAdoptLibrary_WritesAnAuditEntry(t *testing.T) {
	app, db, user := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-audit-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	clean := filepath.Clean(tmpDir)

	orphan := database.Library{Name: "Legacy", Path: clean}
	require.NoError(t, db.Create(&orphan).Error)

	resp := postJSON(t, app, "/api/libraries/"+orphan.ID.String()+"/adopt", map[string]string{}, false)
	require.Equal(t, 200, resp.StatusCode)

	var entry database.AuditLog
	require.NoError(t, db.Where("action = ?", "library_adopted").First(&entry).Error)
	assert.Equal(t, user.ID, entry.ActorID, "the audit trail must name the adopting user")
	assert.Equal(t, "library", entry.TargetType)
	assert.Equal(t, orphan.ID.String(), entry.TargetID, "the audit trail must name the library")
	// Decode the metadata rather than substring-matching the raw blob: the path is
	// JSON-escaped, so a literal search for it only passes on POSIX and fails on
	// Windows. Parsing also proves the column is valid JSON rather than prose.
	var meta struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal([]byte(entry.Metadata), &meta),
		"audit metadata must be valid JSON: %s", entry.Metadata)
	assert.Equal(t, clean, meta.Path, "the audit trail must record the path claimed")
	assert.Equal(t, "Legacy", meta.Name)
}

// The claim is guarded by owner_user_id IS NULL in the same statement as the
// write, so a row taken between the read and the update is not stolen.
func TestAdoptLibrary_RefusesAnAlreadyOwnedRow(t *testing.T) {
	app, db, user := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-raced-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	other := database.User{Email: "racer@example.com", PasswordHash: "hashed", Role: "user"}
	require.NoError(t, db.Create(&other).Error)
	taken := database.Library{Name: "Taken", Path: filepath.Clean(tmpDir), OwnerUserID: &other.ID}
	require.NoError(t, db.Create(&taken).Error)

	resp := postJSON(t, app, "/api/libraries/"+taken.ID.String()+"/adopt", map[string]string{}, false)
	assert.Equal(t, 409, resp.StatusCode, "adoption must not take a row that already has an owner")

	var after database.Library
	require.NoError(t, db.First(&after, "id = ?", taken.ID).Error)
	require.NotNil(t, after.OwnerUserID)
	assert.Equal(t, other.ID, *after.OwnerUserID, "the original owner must be untouched")
	assert.NotEqual(t, user.ID, *after.OwnerUserID)

	var entries int64
	require.NoError(t, db.Model(&database.AuditLog{}).Count(&entries).Error)
	assert.Zero(t, entries, "a refused adoption must not write an audit entry")
}

// Re-adopting your own library is the idempotent case, matching the create
// path's treatment of a same-owner collision.
func TestAdoptLibrary_AlreadyMineIsIdempotent(t *testing.T) {
	app, db, user := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-idem-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	mine := database.Library{Name: "Mine", Path: filepath.Clean(tmpDir), OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&mine).Error)

	resp := postJSON(t, app, "/api/libraries/"+mine.ID.String()+"/adopt", map[string]string{}, false)
	assert.Equal(t, 200, resp.StatusCode)

	var count int64
	require.NoError(t, db.Model(&database.Library{}).Where("path = ?", filepath.Clean(tmpDir)).Count(&count).Error)
	assert.EqualValues(t, 1, count, "adoption must not clone the row")
}

func TestAdoptLibrary_UnknownLibraryIsNotFound(t *testing.T) {
	app, _, _ := adoptionTestApp(t)

	resp := postJSON(t, app, "/api/libraries/"+uuid.New().String()+"/adopt", map[string]string{}, false)
	assert.Equal(t, 404, resp.StatusCode)
}

// The browser half, and the one that decides whether any of this is visible:
// htmx does not swap a 4xx, so the collision has to come back at 200 carrying
// the adopt control. Returning the 409 unchanged leaves Save doing nothing at
// all with the modal still open, which is the defect as a user meets it.
func TestCreateLibrary_HtmxOwnerlessRendersAdoptionOfferAt200(t *testing.T) {
	app, db, _ := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-htmx-adopt-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	orphan := database.Library{Name: "Legacy", Path: filepath.Clean(tmpDir)}
	require.NoError(t, db.Create(&orphan).Error)

	resp := postJSON(t, app, "/api/libraries", map[string]string{"name": "Mine", "path": tmpDir}, true)
	require.Equal(t, 200, resp.StatusCode,
		"a 409 is not swapped by htmx, so the offer must arrive at 200")

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	body := string(raw)

	assert.Contains(t, body, "Adopt this library", "the offer must carry the action")
	assert.Contains(t, body, `/api/libraries/`+orphan.ID.String()+`/adopt`)
	assert.Contains(t, body, `role="alert"`, "the message must be announced, not only drawn")
	assert.NotContains(t, body, `"existing_library"`, "the UI gets a partial, not the JSON conflict")

	var after database.Library
	require.NoError(t, db.First(&after, "id = ?", orphan.ID).Error)
	assert.Nil(t, after.OwnerUserID, "the offer must not adopt on the caller's behalf")
}

// The other half of the same rule: a foreign-owned row produces a message a
// person can act on, and no adopt control to click at something unclaimable.
func TestCreateLibrary_HtmxForeignOwnedExplainsWithoutOfferingAdoption(t *testing.T) {
	app, db, _ := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-htmx-foreign-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	other := database.User{Email: "other@example.com", PasswordHash: "hashed", Role: "user"}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&database.Library{
		Name: "Theirs", Path: filepath.Clean(tmpDir), OwnerUserID: &other.ID,
	}).Error)

	resp := postJSON(t, app, "/api/libraries", map[string]string{"name": "Mine", "path": tmpDir}, true)
	require.Equal(t, 200, resp.StatusCode, "the UI needs a body it will swap")

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	body := string(raw)

	assert.Contains(t, body, "another account", "the message must say who has it")
	assert.NotContains(t, body, "Adopt this library",
		"do not offer to adopt a library that has an owner")
}

// Adopting from the browser returns the refreshed region and closes the modal,
// so the user sees their new library rather than a silent change.
func TestAdoptLibrary_HtmxReturnsRegionAndClosesModal(t *testing.T) {
	app, db, user := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-htmx-claim-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	orphan := database.Library{Name: "Legacy", Path: filepath.Clean(tmpDir)}
	require.NoError(t, db.Create(&orphan).Error)

	resp := postJSON(t, app, "/api/libraries/"+orphan.ID.String()+"/adopt", map[string]string{}, true)
	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "closeModal", resp.Header.Get("HX-Trigger"))

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Legacy", "the region must list the newly owned library")

	var count int64
	require.NoError(t, db.Where("owner_user_id = ?", user.ID).Model(&database.Library{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

// The race the WHERE clause exists for: the handler reads "unowned", and another
// operator claims the row before the write lands. Removing
// `AND owner_user_id IS NULL` leaves every other test green, because
// TestAdoptLibrary_RefusesAnAlreadyOwnedRow is rejected by the in-code check
// before the UPDATE is ever reached. Only a real interleaving exercises it.
func TestAdoptLibrary_LosesTheRaceAndDoesNotStealTheRow(t *testing.T) {
	app, db, user := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-interleave-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	orphan := database.Library{Name: "Legacy", Path: filepath.Clean(tmpDir)}
	require.NoError(t, db.Create(&orphan).Error)

	// Stand in for the other operator: claim the row at the moment the handler
	// has finished reading it and not yet written.
	racer := database.User{Email: "racer@example.com", PasswordHash: "hashed", Role: "user"}
	require.NoError(t, db.Create(&racer).Error)

	var fired bool
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(
		"dji543:racer", func(d *gorm.DB) {
			if fired {
				return
			}
			if _, ok := d.Statement.Dest.(*database.Library); !ok {
				return
			}
			fired = true
			require.NoError(t, db.Model(&database.Library{}).
				Where("id = ?", orphan.ID).
				Update("owner_user_id", racer.ID).Error)
		}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove("dji543:racer")
	})

	resp := postJSON(t, app, "/api/libraries/"+orphan.ID.String()+"/adopt", map[string]string{}, false)
	require.Equal(t, 409, resp.StatusCode,
		"losing the race is a conflict, not a success")

	var after database.Library
	require.NoError(t, db.First(&after, "id = ?", orphan.ID).Error)
	require.NotNil(t, after.OwnerUserID)
	assert.Equal(t, racer.ID, *after.OwnerUserID,
		"the row that was claimed first must stay with whoever claimed it")
	assert.NotEqual(t, user.ID, *after.OwnerUserID)
}

// Creating at a free path is untouched: no adoption, no conflict, 201.
func TestCreateLibrary_FreePathIsUnchanged(t *testing.T) {
	app, db, user := adoptionTestApp(t)

	tmpDir, err := os.MkdirTemp("", "netrunner-free-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	resp := postJSON(t, app, "/api/libraries", map[string]string{"name": "Brand New", "path": tmpDir}, false)
	assert.Equal(t, 201, resp.StatusCode)

	var created database.Library
	require.NoError(t, db.First(&created, "path = ?", filepath.Clean(tmpDir)).Error)
	require.NotNil(t, created.OwnerUserID)
	assert.Equal(t, user.ID, *created.OwnerUserID, "a free path is claimed outright, not adopted")
}
