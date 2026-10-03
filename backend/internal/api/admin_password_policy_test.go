package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// DJI-600 -- the two admin password routes enforced NEITHER bound.
//
//	POST /api/admin/users                      admin_handler.go CreateUser
//	POST /api/admin/users/:id/reset-password   admin_handler.go ResetPassword
//
// Both called bcrypt.GenerateFromPassword directly, so a password over 72
// BYTES came back 500 "internal server error" and a ONE-CHARACTER password was
// accepted outright -- on the same server where registration refused both,
// after DJI-559 put the floor and the ceiling there.
//
// These pin the boundary on each route rather than the happy path, because
// "long passwords are refused" is a claim a guard at 70 bytes also satisfies:
//
//   - the LONGEST password still accepted (exactly 72 bytes),
//   - the FIRST one refused (73 bytes),
//   - 40 e-acutes -- under 72 CHARACTERS, over 72 BYTES,
//   - and the floor, which no admin route enforced at all.
//
// They also assert the two routes agree with registration, which is the whole
// point of moving the policy into config.ValidatePassword: a shared helper that
// one route forgets to call still diverges, and only a cross-route comparison
// can see that.

const policyEAcute = "é"

// bcryptCompare reports whether a stored hash verifies a plaintext password.
func bcryptCompare(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// adminPolicyApp wires the two admin routes with an explicit floor and returns
// the app plus its database, so a test can inspect what was stored.
func adminPolicyApp(t *testing.T, minLength int) (*fiber.App, *gorm.DB) {
	t.Helper()
	db := setupAdminTestDB(t)
	return adminPolicyAppFor(t, db, minLength), db
}

func postPolicyJSON(t *testing.T, app *fiber.App, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, buf.String()
}

func createBody(email, password string) string {
	return fmt.Sprintf(`{"email":%q,"password":%q,"role":"user"}`, email, password)
}

func resetBody(password string) string {
	return fmt.Sprintf(`{"password":%q}`, password)
}

// seedTargetUser creates a user the reset route can be pointed at, with a
// password that satisfies the policy.
func seedTargetUser(t *testing.T, db *gorm.DB, email, password string) string {
	t.Helper()
	status, body := postPolicyJSON(t, adminPolicyAppFor(t, db), "/api/admin/users", createBody(email, password))
	require.Equal(t, 201, status, "seeding the target user: %s", body)

	var user database.User
	require.NoError(t, db.Where("email = ?", email).First(&user).Error)
	return fmt.Sprintf("%d", user.ID)
}

func adminPolicyAppFor(t *testing.T, db *gorm.DB, minLength ...int) *fiber.App {
	t.Helper()
	floor := config.DefaultMinPasswordLength
	if len(minLength) > 0 {
		floor = minLength[0]
	}
	app := fiber.New()
	handler := NewAdminHandlerWithPolicy(db, floor)
	registerAdminRoutes(app, handler, func(c *fiber.Ctx) error {
		c.Locals("user", database.User{ID: 1, Email: "admin@nr.test", Role: "admin"})
		return c.Next()
	})
	return app
}

// One table for both routes, because the two are supposed to be the same
// policy. wantOK rather than wantStatus: the routes share the policy but not the
// success code -- create answers 201, reset answers 200 -- so the table states
// intent and each test supplies its own route's code.
//
// Each entry asserts its own size first. A boundary fixture that does not
// measure itself proves nothing: an eight-word passphrase is 57 bytes and a
// test that used one passed where it meant to prove a refusal.
func policyBoundaryCases(min int) []struct {
	name      string
	password  string
	wantOK    bool
	wantBound string
} {
	ceiling := config.BcryptMaxPasswordBytes
	multiByte := strings.Repeat(policyEAcute, 40)
	passphrase := "correct horse battery staple vanilla harbor candle drawer lonely summit kitten"

	require2 := func(cond bool, msg string) {
		if !cond {
			panic(msg)
		}
	}
	require2(utf8.RuneCountInString(multiByte) == 40, "40 characters")
	require2(len(multiByte) == 80, "80 bytes")
	require2(len(passphrase) > ceiling, "the passphrase fixture must exceed the ceiling")

	return []struct {
		name      string
		password  string
		wantOK    bool
		wantBound string
	}{
		{"a one-character password", "x", false, "floor"},
		{"one below the floor", strings.Repeat("a", min-1), false, "floor"},
		{"exactly at the floor", strings.Repeat("a", min), true, ""},
		{"the longest accepted password is exactly 72 bytes", strings.Repeat("a", ceiling), true, ""},
		{"the first password past the ceiling is refused", strings.Repeat("a", ceiling+1), false, "ceiling"},
		{"a long passphrase well past the ceiling", passphrase, false, "ceiling"},
		{"under 72 characters but over 72 bytes", multiByte, false, "ceiling"},
		{"exactly at the ceiling in multi-byte text", strings.Repeat(policyEAcute, ceiling/2), true, ""},
	}
}

func TestAdminCreateUserEnforcesTheSharedPasswordPolicy(t *testing.T) {
	const min = config.DefaultMinPasswordLength

	for i, tc := range policyBoundaryCases(min) {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := adminPolicyApp(t, min)
			status, body := postPolicyJSON(t, app, "/api/admin/users",
				createBody(fmt.Sprintf("create-%d@nr.test", i), tc.password))

			if tc.wantOK {
				assert.Equal(t, 201, status, "response body: %s", body)
				return
			}
			assert.Equal(t, 400, status, "response body: %s", body)
			// The regression this ticket exists for: a 500 here is the bug.
			assert.NotEqual(t, 500, status,
				"an out-of-policy password must be a client error, never a server fault")
			assertRefusalNamesTheBound(t, body, tc.wantBound)
		})
	}
}

func TestAdminResetPasswordEnforcesTheSharedPasswordPolicy(t *testing.T) {
	const min = config.DefaultMinPasswordLength

	for i, tc := range policyBoundaryCases(min) {
		t.Run(tc.name, func(t *testing.T) {
			db := setupAdminTestDB(t)
			seeded := adminPolicyAppFor(t, db)
			targetID := seedTargetUser(t, db, fmt.Sprintf("reset-target-%d@nr.test", i),
				strings.Repeat("a", min))

			status, body := postPolicyJSON(t, seeded,
				fmt.Sprintf("/api/admin/users/%s/reset-password", targetID),
				resetBody(tc.password))

			if tc.wantOK {
				assert.Equal(t, 200, status, "response body: %s", body)
				// Accepted is not the same as STORED: the route must have
				// written the password it was given.
				var user database.User
				require.NoError(t, db.Where("email = ?",
					fmt.Sprintf("reset-target-%d@nr.test", i)).First(&user).Error)
				require.NotEmpty(t, user.PasswordHash)
				return
			}
			assert.Equal(t, 400, status, "response body: %s", body)
			assert.NotEqual(t, 500, status,
				"an out-of-policy password must be a client error, never a server fault")
			assertRefusalNamesTheBound(t, body, tc.wantBound)
		})
	}
}

// A refusal has to leave the target's existing password intact. A guard that
// validates after writing -- or a partial update -- would destroy a working
// credential on a request it refused.
func TestAdminRefusalLeavesTheExistingPasswordUntouched(t *testing.T) {
	const min = config.DefaultMinPasswordLength
	db := setupAdminTestDB(t)
	app := adminPolicyAppFor(t, db)

	const good = "correct-horse-battery-staple"
	targetID := seedTargetUser(t, db, "untouched@nr.test", good)

	var before database.User
	require.NoError(t, db.Where("email = ?", "untouched@nr.test").First(&before).Error)

	for _, bad := range []string{"x", strings.Repeat("a", min-1), strings.Repeat("a", 100), strings.Repeat(policyEAcute, 40)} {
		status, body := postPolicyJSON(t, app,
			fmt.Sprintf("/api/admin/users/%s/reset-password", targetID), resetBody(bad))
		require.Equal(t, 400, status, "password %q must be refused; got %s", truncate(bad), body)

		var after database.User
		require.NoError(t, db.Where("email = ?", "untouched@nr.test").First(&after).Error)
		assert.Equal(t, before.PasswordHash, after.PasswordHash,
			"a refused reset must not overwrite the stored hash")
	}

	// And the credential still works.
	var stored database.User
	require.NoError(t, db.Where("email = ?", "untouched@nr.test").First(&stored).Error)
	assert.True(t, bcryptCompare(stored.PasswordHash, good),
		"the original password must still verify after four refused resets")
}

// CreateUser answers 409 when the address exists. If the policy check ran after
// that, an existing address and a new one would answer differently for the same
// over-limit password, which enumerates the user table.
func TestAdminCreateUserRefusalDoesNotRevealWhetherTheAddressExists(t *testing.T) {
	const min = config.DefaultMinPasswordLength
	db := setupAdminTestDB(t)
	app := adminPolicyAppFor(t, db)

	existing := "existing@nr.test"
	status, body := postPolicyJSON(t, app, "/api/admin/users", createBody(existing, "a-valid-password"))
	require.Equal(t, 201, status, "the account must exist before the comparison means anything: %s", body)

	over := strings.Repeat("a", config.BcryptMaxPasswordBytes+1)
	knownStatus, knownBody := postPolicyJSON(t, app, "/api/admin/users", createBody(existing, over))
	unknownStatus, unknownBody := postPolicyJSON(t, app, "/api/admin/users",
		createBody("unknown@nr.test", over))

	assert.Equal(t, 400, knownStatus, "an existing address must not answer 409 to an over-limit password")
	assert.Equal(t, knownStatus, unknownStatus)
	assert.JSONEq(t, unknownBody, knownBody,
		"the two answers must be indistinguishable, or the pair enumerates users")

	below := "x"
	knownFloor, knownFloorBody := postPolicyJSON(t, app, "/api/admin/users", createBody(existing, below))
	unknownFloor, unknownFloorBody := postPolicyJSON(t, app, "/api/admin/users",
		createBody("another@nr.test", below))
	assert.Equal(t, 400, knownFloor)
	assert.Equal(t, unknownFloor, knownFloor)
	assert.JSONEq(t, unknownFloorBody, knownFloorBody)
}

// The three routes must answer identically. This is the assertion the whole
// refactor exists to enable: a shared helper that one route forgets to call is
// invisible to per-route tests and obvious here.
func TestAllThreePasswordRoutesAnswerIdentically(t *testing.T) {
	const min = config.DefaultMinPasswordLength
	passwords := map[string]string{
		"one character":    "x",
		"below the floor":  strings.Repeat("a", min-1),
		"one over ceiling": strings.Repeat("a", config.BcryptMaxPasswordBytes+1),
		"multibyte over":   strings.Repeat(policyEAcute, 40),
		"exactly at floor": strings.Repeat("a", min),
		"exactly at bytes": strings.Repeat("a", config.BcryptMaxPasswordBytes),
	}

	for label, password := range passwords {
		t.Run(label, func(t *testing.T) {
			// Registration.
			regDB, _, _, _ := setupPartialsTestDB(t)
			regApp := fiber.New()
			regApp.Post("/api/auth/register",
				NewAuthHandlerWithPolicy(regDB, "", min).Register)
			regStatus, regBody := postPolicyJSON(t, regApp, "/api/auth/register",
				fmt.Sprintf(`{"email":"reg@nr.test","password":%q}`, password))

			// Admin create-user.
			adminDB := setupAdminTestDB(t)
			adminStatus, adminBody := postPolicyJSON(t, adminPolicyAppFor(t, adminDB),
				"/api/admin/users", createBody("admin-create@nr.test", password))

			// Admin reset-password.
			resetDB := setupAdminTestDB(t)
			resetApp := adminPolicyAppFor(t, resetDB)
			targetID := seedTargetUser(t, resetDB, "reset@nr.test", strings.Repeat("a", min))
			resetStatus, resetBodyOut := postPolicyJSON(t, resetApp,
				fmt.Sprintf("/api/admin/users/%s/reset-password", targetID), resetBody(password))

			// Normalise the success codes: 201 for register and create, 200 for
			// reset. What must match is the REFUSAL and its body.
			if regStatus != 201 {
				assert.Equal(t, 400, regStatus, "registration refused: %s", regBody)
				assert.Equal(t, 400, adminStatus, "admin create must refuse the same password: %s", adminBody)
				assert.Equal(t, 400, resetStatus, "admin reset must refuse the same password: %s", resetBodyOut)
				assert.JSONEq(t, regBody, adminBody,
					"registration and admin create must return the same body")
				assert.JSONEq(t, regBody, resetBodyOut,
					"registration and admin reset must return the same body")
				return
			}
			assert.Equal(t, 201, regStatus)
			assert.Equal(t, 201, adminStatus, "admin create must accept the same password: %s", adminBody)
			assert.Equal(t, 200, resetStatus, "admin reset must accept the same password: %s", resetBodyOut)
		})
	}
}

// --- helpers -------------------------------------------------------------

func assertRefusalNamesTheBound(t *testing.T, body, wantBound string) {
	t.Helper()
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &fields), "body: %s", body)

	message, _ := fields["error"].(string)
	require.NotEmpty(t, message, "a refusal must carry a message: %s", body)

	switch wantBound {
	case "floor":
		assert.Contains(t, message, "characters")
		assert.Equal(t, float64(config.DefaultMinPasswordLength), fields["minLength"])
	case "ceiling":
		// The unit is the whole point: 40 e-acutes fail this bound while
		// clearing the character floor.
		assert.Contains(t, message, fmt.Sprintf("%d bytes", config.BcryptMaxPasswordBytes))
		assert.Contains(t, message, "not characters")
		assert.Equal(t, float64(config.BcryptMaxPasswordBytes), fields["maxBytes"])
	default:
		t.Fatalf("unknown bound %q", wantBound)
	}
}

func truncate(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12] + "..."
}
