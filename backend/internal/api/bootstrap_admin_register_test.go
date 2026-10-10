package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// testBootstrapSecret is the enrollment code these tests configure, so the
// assertions about the address stay separate from the assertions about the code.
const testBootstrapSecret = "correct-horse-battery-staple"

// The ordering the defect missed: a SECOND party registering the configured
// address before the operator. It must gain nothing, and it must leave no
// bootstrap marker behind - a marker written for a refused claim would spend the
// address with nothing to show for it.
//
// As shipped this was reachable: promotion compared a string against a usually
// published address, so claiming admin needed no proof of control of that
// mailbox and no knowledge beyond the address itself.
//
// The restart half is asserted here too. Fixing only the registration trigger
// deferred the defect rather than closing it - a live probe showed the account
// refused at registration promoted by the next restart of ops-web, because the
// boot compared an address and nothing else.
func TestRegister_AddressAloneDoesNotClaimTheBootstrapAdmin(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register",
		NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com", testBootstrapSecret).Register)

	resp := registerUser(t, app, "operator@example.com", "")
	require.Equal(t, 201, resp.StatusCode, "the registration itself still succeeds")

	var u database.User
	require.NoError(t, db.Where("email = ?", "operator@example.com").First(&u).Error)
	assert.Equal(t, "user", u.Role,
		"knowing the configured address must not be enough to be promoted")

	var count int64
	require.NoError(t, db.Model(&database.AuditLog{}).
		Where("action = ?", services.BootstrapAdminAction).Count(&count).Error)
	assert.Equal(t, int64(0), count,
		"a refused claim must leave no marker, or the address is spent for nothing")

	// A restart must not finish what registration refused. The boot has no code
	// to compare, so it promotes only an account that has already proved it
	// holds one - and this account never did.
	boot, err := services.BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)
	assert.True(t, boot.NotEnrolled,
		"the boot must recognise an account that never presented the code")
	assert.False(t, boot.Promoted)

	var afterRestart database.User
	require.NoError(t, db.Where("email = ?", "operator@example.com").First(&afterRestart).Error)
	assert.Equal(t, "user", afterRestart.Role,
		"a restart must not promote the account registration refused")
}

// A wrong code is no better than none.
func TestRegister_WrongEnrollmentCodeDoesNotPromote(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register",
		NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com", testBootstrapSecret).Register)

	resp := registerUser(t, app, "operator@example.com", "not-the-secret")
	require.Equal(t, 201, resp.StatusCode)

	var u database.User
	require.NoError(t, db.Where("email = ?", "operator@example.com").First(&u).Error)
	assert.Equal(t, "user", u.Role)
	assert.Nil(t, u.BootstrapEnrolledAt,
		"a wrong code must not record the proof a boot would accept")

	boot, err := services.BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)
	assert.True(t, boot.NotEnrolled)
	assert.Equal(t, "user", roleAfter(t, db, "operator@example.com"))
}

// An empty configured secret is a DISABLED bootstrap, not "any code will do".
func TestRegister_EmptyConfiguredSecretPromotesNobody(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register",
		NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com", "").Register)

	resp := registerUser(t, app, "operator@example.com", "")
	require.Equal(t, 201, resp.StatusCode)

	var u database.User
	require.NoError(t, db.Where("email = ?", "operator@example.com").First(&u).Error)
	assert.Equal(t, "user", u.Role, "no secret configured must mean no promotion")
}

// Registration still hardcodes the user role. The bootstrap is what makes an
// operator reachable, and the ordering that matters is the one an operator
// actually hits: set BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_SECRET, restart,
// then register with that code.
func TestRegister_PromotesTheBootstrapAccount(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register", NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com", testBootstrapSecret).Register)

	resp := registerUser(t, app, "operator@example.com", testBootstrapSecret)
	require.Equal(t, 201, resp.StatusCode)

	var u database.User
	require.NoError(t, db.Where("email = ?", "operator@example.com").First(&u).Error)
	assert.Equal(t, services.AdminRole, u.Role,
		"an operator who configured the address before registering must become admin")
}

// Registering as somebody else must never be escalated, whatever the bootstrap
// is configured to.
func TestRegister_DoesNotPromoteAnyoneElse(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register", NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com", testBootstrapSecret).Register)

	for _, email := range []string{
		"other@example.com",
		"operator@example.com.evil.test",
		"operator@example.co",
	} {
		resp := registerUser(t, app, email, testBootstrapSecret)
		require.Equal(t, 201, resp.StatusCode)

		var u database.User
		require.NoError(t, db.Where("email = ?", email).First(&u).Error)
		assert.Equal(t, "user", u.Role, "%s must stay an ordinary user", email)
	}
}

// With no bootstrap configured, registration is unchanged.
func TestRegister_WithoutBootstrapStaysUser(t *testing.T) {
	db := setupTestDBForAuth(t)
	app := fiber.New()
	app.Post("/register", NewAuthHandler(db).Register)

	resp := registerUser(t, app, "someone@example.com", "")
	require.Equal(t, 201, resp.StatusCode)

	var u database.User
	require.NoError(t, db.Where("email = ?", "someone@example.com").First(&u).Error)
	assert.Equal(t, "user", u.Role)
}

// The registration path must not double the audit trail. An earlier boot with
// the same address configured found no account, and registering is the moment
// the account appears, so exactly one promotion may be recorded.
func TestRegister_PromotesOnceAfterAFailedBootAttempt(t *testing.T) {
	db := setupTestDBForAuth(t)

	pending, err := services.BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)
	require.True(t, pending.NoAccount)

	app := fiber.New()
	app.Post("/register", NewAuthHandlerWithBootstrapAdmin(db, "operator@example.com", testBootstrapSecret).Register)

	resp := registerUser(t, app, "operator@example.com", testBootstrapSecret)
	require.Equal(t, 201, resp.StatusCode)

	var count int64
	require.NoError(t, db.Model(&database.AuditLog{}).
		Where("action = ?", services.BootstrapAdminAction).
		Count(&count).Error)
	assert.Equal(t, int64(1), count, "registration must complete the pending bootstrap exactly once")
}

// roleAfter re-reads the role, so an assertion cannot be satisfied by a value
// captured before the call under test.
func roleAfter(t *testing.T, db *gorm.DB, email string) string {
	t.Helper()
	var u database.User
	require.NoError(t, db.Where("email = ?", email).First(&u).Error)
	return u.Role
}

// registerUser posts a registration through the real route and returns the
// response. The enrollment code travels the way the browser sends it, so a
// handler that ignored the field could not pass by accident.
func registerUser(t *testing.T, app *fiber.App, email, enrollmentCode string) *http.Response {
	t.Helper()

	payload := map[string]string{"email": email, "password": "correct horse battery"}
	if enrollmentCode != "" {
		payload["enrollment_code"] = enrollmentCode
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)

	return resp
}
