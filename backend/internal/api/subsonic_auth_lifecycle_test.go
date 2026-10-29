package api

import (
	"crypto/md5"
	"encoding/hex"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const subsonicLifecycleSharedPassword = "shared-secret-123"
const subsonicLifecycleSalt = "randomsalt123"

// newSubsonicLifecycleHandler builds a handler with an explicit shared
// password. An empty password means token mode is deliberately unconfigured.
func newSubsonicLifecycleHandler(db *gorm.DB, sharedPassword string) *SubsonicHandler {
	cfg := &config.Config{
		Subsonic: struct {
			Enabled  bool   `envconfig:"SUBSONIC_ENABLED" default:"false"`
			Password string `envconfig:"SUBSONIC_PASSWORD"`
		}{
			Enabled:  true,
			Password: sharedPassword,
		},
	}
	return NewSubsonicHandler(db, cfg)
}

// subsonicLifecycleToken reproduces the client-side token derivation:
// md5(hex(md5(sharedPassword)) + salt).
func subsonicLifecycleToken(sharedPassword string) string {
	md5Password := md5.Sum([]byte(sharedPassword))
	sum := md5.Sum([]byte(hex.EncodeToString(md5Password[:]) + subsonicLifecycleSalt))
	return hex.EncodeToString(sum[:])
}

func subsonicLifecycleApp(handler *SubsonicHandler) *fiber.App {
	app := fiber.New()
	identity := func(c fiber.Ctx) error {
		user, ok := c.Locals("user").(database.User)
		if !ok {
			return c.SendStatus(500)
		}
		return c.SendString("OK:" + user.Email)
	}
	app.Get("/test", handler.AuthMiddleware, identity)
	app.Post("/test", handler.AuthMiddleware, identity)
	return app
}

// TestSubsonicAuth_CredentialModes pins the accepted credential modes and the
// failure envelope for every rejected shape. NR03 review focus: a conflicting
// `p` with `t`/`s` must fail rather than silently selecting a mode.
func TestSubsonicAuth_CredentialModes(t *testing.T) {
	const email = "matrix@example.com"
	const accountPassword = "accountpass123"
	token := subsonicLifecycleToken(subsonicLifecycleSharedPassword)

	tests := []struct {
		name             string
		sharedPassword   string
		disableUser      bool
		values           url.Values
		wantOK           bool
		wantBodyContains string
	}{
		{
			name:           "valid account password",
			sharedPassword: subsonicLifecycleSharedPassword,
			values:         url.Values{"u": {email}, "p": {accountPassword}},
			wantOK:         true,
		},
		{
			name:           "valid configured token",
			sharedPassword: subsonicLifecycleSharedPassword,
			values:         url.Values{"u": {email}, "t": {token}, "s": {subsonicLifecycleSalt}},
			wantOK:         true,
		},
		{
			name:             "missing username",
			sharedPassword:   subsonicLifecycleSharedPassword,
			values:           url.Values{"p": {accountPassword}},
			wantBodyContains: "Missing parameter: u",
		},
		{
			name:             "missing credential",
			sharedPassword:   subsonicLifecycleSharedPassword,
			values:           url.Values{"u": {email}},
			wantBodyContains: "Missing authentication parameters",
		},
		{
			name:             "incomplete token without salt",
			sharedPassword:   subsonicLifecycleSharedPassword,
			values:           url.Values{"u": {email}, "t": {token}},
			wantBodyContains: "Authentication failed",
		},
		{
			name:             "incomplete salt without token",
			sharedPassword:   subsonicLifecycleSharedPassword,
			values:           url.Values{"u": {email}, "s": {subsonicLifecycleSalt}},
			wantBodyContains: "Authentication failed",
		},
		{
			name:             "password and token conflict",
			sharedPassword:   subsonicLifecycleSharedPassword,
			values:           url.Values{"u": {email}, "p": {accountPassword}, "t": {token}, "s": {subsonicLifecycleSalt}},
			wantBodyContains: "Authentication failed",
		},
		{
			name:             "invalid password",
			sharedPassword:   subsonicLifecycleSharedPassword,
			values:           url.Values{"u": {email}, "p": {"wrongpassword"}},
			wantBodyContains: "Authentication failed",
		},
		{
			name:             "malformed token is not hex",
			sharedPassword:   subsonicLifecycleSharedPassword,
			values:           url.Values{"u": {email}, "t": {"zzzz"}, "s": {subsonicLifecycleSalt}},
			wantBodyContains: "Authentication failed",
		},
		{
			name:             "disabled account is rejected",
			sharedPassword:   subsonicLifecycleSharedPassword,
			disableUser:      true,
			values:           url.Values{"u": {email}, "p": {accountPassword}},
			wantBodyContains: "Authentication failed",
		},
		{
			name:             "token mode without a configured shared password",
			sharedPassword:   "",
			values:           url.Values{"u": {email}, "t": {token}, "s": {subsonicLifecycleSalt}},
			wantBodyContains: "Authentication failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupSubsonicTestDB(t)
			user := createTestUserForSubsonic(t, db, email, accountPassword)
			if tt.disableUser {
				require.NoError(t, db.Model(&database.User{}).Where("id = ?", user.ID).
					Update("disabled_at", time.Now()).Error)
			}

			app := subsonicLifecycleApp(newSubsonicLifecycleHandler(db, tt.sharedPassword))
			req := httptest.NewRequest("GET", "/test?"+tt.values.Encode(), nil)
			resp, err := app.Test(req)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode, "Subsonic errors stay inside a 200 envelope")

			body := string(subsonicGetRespBody(resp))
			if tt.wantOK {
				assert.Equal(t, "OK:"+email, body)
				return
			}
			assert.Contains(t, body, `status="failed"`)
			assert.Contains(t, body, `code="40"`)
			assert.Contains(t, body, tt.wantBodyContains)
		})
	}
}

// TestSubsonicAuth_CredentialParameters_GetAndPost pins that the query string
// (GET) and a form body (POST) produce identical principal and failure results.
func TestSubsonicAuth_CredentialParameters_GetAndPost(t *testing.T) {
	const email = "parity@example.com"
	const accountPassword = "accountpass123"
	token := subsonicLifecycleToken(subsonicLifecycleSharedPassword)

	tests := []struct {
		name     string
		values   url.Values
		wantOK   bool
		wantText string
	}{
		{
			name:   "valid account password",
			values: url.Values{"u": {email}, "p": {accountPassword}},
			wantOK: true,
		},
		{
			name:   "valid configured token",
			values: url.Values{"u": {email}, "t": {token}, "s": {subsonicLifecycleSalt}},
			wantOK: true,
		},
		{
			name:     "conflicting credential modes",
			values:   url.Values{"u": {email}, "p": {accountPassword}, "t": {token}, "s": {subsonicLifecycleSalt}},
			wantText: "Authentication failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupSubsonicTestDB(t)
			createTestUserForSubsonic(t, db, email, accountPassword)
			app := subsonicLifecycleApp(newSubsonicLifecycleHandler(db, subsonicLifecycleSharedPassword))

			getResp, err := app.Test(httptest.NewRequest("GET", "/test?"+tt.values.Encode(), nil))
			require.NoError(t, err)
			getBody := string(subsonicGetRespBody(getResp))

			form := httptest.NewRequest("POST", "/test", strings.NewReader(tt.values.Encode()))
			form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			postResp, err := app.Test(form)
			require.NoError(t, err)
			postBody := string(subsonicGetRespBody(postResp))

			assert.Equal(t, getResp.StatusCode, postResp.StatusCode)
			assert.Equal(t, getBody, postBody, "GET and POST must agree for the same credentials")
			if tt.wantOK {
				assert.Equal(t, "OK:"+email, getBody)
				return
			}
			assert.Contains(t, getBody, tt.wantText)
		})
	}
}

// TestSubsonic_GetSong_FormBodyMatchesQueryString pins the endpoint half of the
// GET/POST parity: POST registers the same handler as GET, so any parameter the
// handler reads only from the query string still authenticates and then fails
// with "Missing parameter" - a 200 envelope that looks like a client bug.
func TestSubsonic_GetSong_FormBodyMatchesQueryString(t *testing.T) {
	db := setupSubsonicTestDB(t)
	user := createTestUserForSubsonic(t, db, "formsong@example.com", "accountpass123")

	library := database.Library{
		ID:          uuid.New(),
		Name:        "Form Library",
		Path:        "/tmp/form-library",
		OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&library).Error)
	track := database.Track{
		ID:        uuid.New(),
		LibraryID: library.ID,
		Title:     "Form Song",
		Artist:    "Form Artist",
		Album:     "Form Album",
		Path:      "/tmp/form-library/form.mp3",
		Format:    "MP3",
		FileSize:  1024,
	}
	require.NoError(t, db.Create(&track).Error)

	handler := newSubsonicLifecycleHandler(db, subsonicLifecycleSharedPassword)
	app := fiber.New()
	app.Get("/getSong.view", handler.AuthMiddleware, handler.GetSong)
	app.Post("/getSong.view", handler.AuthMiddleware, handler.GetSong)

	values := url.Values{
		"u":  {"formsong@example.com"},
		"p":  {"accountpass123"},
		"id": {track.ID.String()},
	}
	getResp, err := app.Test(httptest.NewRequest("GET", "/getSong.view?"+values.Encode(), nil))
	require.NoError(t, err)
	getBody := string(subsonicGetRespBody(getResp))

	form := httptest.NewRequest("POST", "/getSong.view", strings.NewReader(values.Encode()))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postResp, err := app.Test(form)
	require.NoError(t, err)
	postBody := string(subsonicGetRespBody(postResp))

	assert.Equal(t, getBody, postBody, "a form POST must return the same song as the GET form")
	assert.Contains(t, getBody, `title="Form Song"`)
}
