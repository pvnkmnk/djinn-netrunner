package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func lifecycleFixture(t *testing.T, expires time.Time) (*fiber.App, *database.Session, *database.Session) {
	t.Helper()
	db := setupInMemoryDB(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-battery-staple"), bcrypt.MinCost)
	require.NoError(t, err)
	user := database.User{Email: "lifecycle@example.com", PasswordHash: string(hash), Role: "admin"}
	require.NoError(t, db.Create(&user).Error)
	first := &database.Session{SessionID: "lifecycle-first", UserID: user.ID, ExpiresAt: expires}
	second := &database.Session{SessionID: "lifecycle-second", UserID: user.ID, ExpiresAt: expires}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)
	auth := NewAuthHandler(db)
	app := fiber.New()
	app.Get("/api/auth/me", auth.AuthMiddleware, auth.Me)
	app.Post("/api/auth/logout", auth.Logout)
	app.Get("/api/protected", auth.AuthMiddleware, func(c fiber.Ctx) error { return c.SendStatus(204) })
	return app, first, second
}

func TestAuthMe_ReturnsPrincipalWithoutSecrets(t *testing.T) {
	app, session, _ := lifecycleFixture(t, time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: session.SessionID})
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "lifecycle@example.com", body["email"])
	require.Equal(t, "admin", body["role"])
	require.Equal(t, float64(session.UserID), body["id"])
	require.NotEmpty(t, body["expires_at"])
	require.NotContains(t, body, "PasswordHash")
	require.NotContains(t, body, "SessionID")
	require.NotContains(t, body, "password")
}

func TestAuthMe_RejectsMissingSession(t *testing.T) {
	app, _, _ := lifecycleFixture(t, time.Now().Add(time.Hour))
	resp, err := app.Test(httptest.NewRequest("GET", "/api/auth/me", nil))
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)
	var body APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "not authenticated", body.Error)
	require.Equal(t, "unauthorized", body.Code)
}

func TestAuthLifecycle_LogoutRevokesSession(t *testing.T) {
	app, session, _ := lifecycleFixture(t, time.Now().Add(time.Hour))
	logout := httptest.NewRequest("POST", "/api/auth/logout", nil)
	logout.AddCookie(&http.Cookie{Name: SessionCookie, Value: session.SessionID})
	resp, err := app.Test(logout)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Contains(t, strings.Join(resp.Header.Values("Set-Cookie"), ";"), SessionCookie+"=")
	me := httptest.NewRequest("GET", "/api/auth/me", nil)
	me.AddCookie(&http.Cookie{Name: SessionCookie, Value: session.SessionID})
	resp, err = app.Test(me)
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)
	protected := httptest.NewRequest("GET", "/api/protected", nil)
	protected.AddCookie(&http.Cookie{Name: SessionCookie, Value: session.SessionID})
	resp, err = app.Test(protected)
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)
}

func TestAuthLifecycle_ExpiredSessionRejected(t *testing.T) {
	app, session, _ := lifecycleFixture(t, time.Now().Add(-time.Minute))
	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: session.SessionID})
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)
}

func TestAuthLifecycle_ConcurrentSessionsRemainIndependent(t *testing.T) {
	app, first, second := lifecycleFixture(t, time.Now().Add(time.Hour))
	logout := httptest.NewRequest("POST", "/api/auth/logout", nil)
	logout.AddCookie(&http.Cookie{Name: SessionCookie, Value: first.SessionID})
	_, err := app.Test(logout)
	require.NoError(t, err)
	me := httptest.NewRequest("GET", "/api/auth/me", nil)
	me.AddCookie(&http.Cookie{Name: SessionCookie, Value: second.SessionID})
	resp, err := app.Test(me)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	protected := httptest.NewRequest("GET", "/api/protected", nil)
	protected.AddCookie(&http.Cookie{Name: SessionCookie, Value: second.SessionID})
	resp, err = app.Test(protected)
	require.NoError(t, err)
	require.Equal(t, 204, resp.StatusCode)
}

func TestAuthLogin_SessionCookieSecurityAttributes(t *testing.T) {
	db := setupInMemoryDB(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-battery-staple"), bcrypt.MinCost)
	require.NoError(t, err)
	require.NoError(t, db.Create(&database.User{Email: "login@example.com", PasswordHash: string(hash), Role: "user"}).Error)
	app := fiber.New()
	app.Post("/api/auth/login", NewAuthHandler(db).Login)
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"email":"login@example.com","password":"correct-horse-battery-staple"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	cookie := strings.Join(resp.Header.Values("Set-Cookie"), ";")
	require.Contains(t, cookie, SessionCookie+"=")
	require.Contains(t, cookie, "HttpOnly")
	require.Contains(t, cookie, "SameSite=Strict")
	require.Contains(t, cookie, "expires=")
}
