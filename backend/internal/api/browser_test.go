package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DJI-547.
//
// The bug was never the 401 itself. Every guard in this app answers with JSON,
// which is correct for a machine and wrong for a person: a browser that follows
// a link, a bookmark, a refresh or a shared deep link should land on a page.
//
// So these tests pin both halves. The browser half is easy to break by
// accident later - it is one condition in a middleware - and the machine half
// is the one that must never break: silently starting to return HTML to an API
// caller or a Subsonic client would break them in a way no test in the repo
// would otherwise notice.

// browserPageRoutes are the full-page routes the app serves. Kept here so the
// deep-link sweep below covers the real navigation surface rather than one
// hand-picked URL: the bug report was "a page URL returns JSON", and a test
// that only proves it about /libraries would not notice /jobs regressing.
var browserPageRoutes = []string{
	"/",
	"/watchlists",
	"/libraries",
	"/profiles",
	"/schedules",
	"/artists",
	"/playlists",
	"/jobs",
	"/admin",
}

// machineRoutes are the machine surfaces that must keep answering with JSON
// whatever the caller claims to want. A Subsonic client handed a login page
// fails far more confusingly than one handed a 401.
var machineRoutes = []string{
	"/api/admin/users",
	"/api/admin/audit",
	"/api/watchlists",
	"/api/health",
	"/partials/stats",
	"/partials/admin/users",
}

func browserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	return db
}

// browserTestApp builds an app with the real guards in front of the real page
// and machine routes, plus a stand-in for the login page the redirect lands on.
func browserTestApp(t *testing.T, db *gorm.DB, user *database.User) *fiber.App {
	t.Helper()

	// The 403 page and every page route render a real template, so the engine
	// belongs to the app under test rather than being bolted on per test.
	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	app := fiber.New(fiber.Config{Views: engine})
	auth := NewAuthHandler(db)
	admin := NewAdminHandler(db)

	// The sign-in page. In the real app this is the dashboard rendering its
	// login form; here it is just enough to prove where the redirect went.
	app.Get("/", auth.OptionalAuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString(`<html><body data-next="` + safeNextPath(c.Query("next")) + `">sign in</body></html>`)
	})

	page := func(c *fiber.Ctx) error {
		if _, ok, err := requirePageUser(c); !ok {
			return err
		}
		return RenderPage(c, "test", "pages/watchlists", fiber.Map{})
	}

	for _, r := range browserPageRoutes {
		if r == "/" {
			continue
		}
		if r == "/admin" {
			app.Get(r, auth.AuthMiddleware, admin.AdminOnly, page)
			continue
		}
		app.Get(r, auth.AuthMiddleware, page)
	}

	// Machine surfaces, each behind the same guards as production.
	apiGroup := app.Group("/api", auth.AuthMiddleware)
	apiGroup.Get("/watchlists", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })
	apiGroup.Get("/health", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })
	adminGroup := app.Group("/api/admin", auth.AuthMiddleware, admin.AdminOnly)
	adminGroup.Get("/users", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })
	adminGroup.Get("/audit", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })
	app.Get("/partials/stats", auth.AuthMiddleware, func(c *fiber.Ctx) error {
		return c.SendString("<div>stats</div>")
	})
	app.Get("/partials/admin/users", auth.AuthMiddleware, admin.AdminOnly, func(c *fiber.Ctx) error {
		return c.SendString("<div>users</div>")
	})

	// A middleware cannot read a user that was never signed in; for the
	// signed-in cases the session row is what AuthMiddleware looks up.
	if user != nil {
		require.NoError(t, db.Create(user).Error)
		require.NoError(t, db.Create(&database.Session{
			SessionID: SessionCookie + "-test",
			UserID:    user.ID,
			ExpiresAt: timeInFuture(),
		}).Error)
	}
	return app
}

func get(t *testing.T, app *fiber.App, path string, headers map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

// browserHeaders is what a browser sends when a person follows a link.
var browserHeaders = map[string]string{"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}

// htmxHeaders is what htmx sends on every swap. It also asks for text/html,
// which is why wantsHTMLPage has to exclude it explicitly.
var htmxHeaders = map[string]string{"Accept": "text/html, */*", "HX-Request": "true"}

func timeInFuture() time.Time { return time.Now().Add(time.Hour) }

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

// renderLayoutFor renders the base layout's chrome for a user with the given
// role ("" for signed out). These assertions are about the chrome, so the
// layout is rendered directly rather than through a page that would bury it.
func renderLayoutFor(t *testing.T, role string) string {
	t.Helper()

	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	app := fiber.New(fiber.Config{Views: engine})

	app.Get("/probe", func(c *fiber.Ctx) error {
		if role != "" {
			c.Locals("user", database.User{ID: 1, Email: "u@example.com", Role: role})
		}
		return RenderPage(c, "probe", "layouts/base.html", fiber.Map{})
	})

	resp := get(t, app, "/probe", browserHeaders)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	return readBody(t, resp)
}

// Criterion: "A signed-out visitor following any page URL is taken to the login
// page, and lands back on the page they originally asked for after signing in."
func TestAuthMiddleware_SignedOutBrowserOnAnyPageGetsTheSignInRedirect(t *testing.T) {
	db := browserTestDB(t)
	app := browserTestApp(t, db, nil)

	for _, route := range browserPageRoutes {
		if route == "/" {
			continue
		}
		t.Run(route, func(t *testing.T) {
			resp := get(t, app, route, browserHeaders)
			require.Equal(t, fiber.StatusFound, resp.StatusCode,
				"%s must redirect a browser to sign-in, not answer with JSON", route)

			location := resp.Header.Get("Location")
			require.NotEmpty(t, location)

			parsed, err := url.Parse(location)
			require.NoError(t, err)
			assert.Equal(t, "/", parsed.Path, "the sign-in page is the dashboard")

			next := parsed.Query().Get("next")
			assert.Equal(t, route, next, "the page the visitor asked for must survive the round trip")
			assert.NotContains(t, resp.Header.Get("Content-Type"), "application/json")
		})
	}
}

// The return path has to survive a query string too, or a shared link with
// parameters silently drops them.
func TestAuthMiddleware_SignInRedirectKeepsTheQueryString(t *testing.T) {
	db := browserTestDB(t)
	app := browserTestApp(t, db, nil)

	resp := get(t, app, "/watchlists?filter=recent", browserHeaders)
	require.Equal(t, fiber.StatusFound, resp.StatusCode)

	location := resp.Header.Get("Location")
	parsed, err := url.Parse(location)
	require.NoError(t, err)
	assert.Equal(t, "/watchlists?filter=recent", parsed.Query().Get("next"))
}

// Criterion: "The JSON 401 response is preserved for API and htmx callers, so
// machine clients are unaffected."
func TestAuthMiddleware_SignedOutMachineCallersStillGetJSON(t *testing.T) {
	db := browserTestDB(t)
	app := browserTestApp(t, db, nil)

	t.Run("api caller", func(t *testing.T) {
		resp := get(t, app, "/api/watchlists", nil)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "not authenticated", body["error"])
	})

	t.Run("api caller asking for html still gets json", func(t *testing.T) {
		// A browser Accept header must not be able to turn an API refusal
		// into a login page; that is what breaks machine clients.
		resp := get(t, app, "/api/watchlists", browserHeaders)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	})

	t.Run("htmx swap", func(t *testing.T) {
		resp := get(t, app, "/partials/stats", htmxHeaders)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	})
}

// Criterion: "The admin link appears only for admins."
func TestBaseLayout_AdminLinkIsAdminOnly(t *testing.T) {
	t.Run("ordinary user", func(t *testing.T) {
		body := renderLayoutFor(t, "user")
		assert.NotContains(t, body, `href="/admin"`,
			"an ordinary user must not be shown a link that refuses them")
		assert.Contains(t, body, `href="/watchlists"`, "the rest of the nav is unchanged")
	})

	t.Run("admin", func(t *testing.T) {
		body := renderLayoutFor(t, "admin")
		assert.Contains(t, body, `href="/admin"`,
			"hiding the admin link from admins would lock them out of their own page")
	})
}

// Criterion: "A sign-out control is present in the chrome on every
// authenticated page, ends the session server-side, and returns the user to the
// signed-out dashboard."
func TestBaseLayout_SignOutControlAppearsOnlyWhenSignedIn(t *testing.T) {
	signedOut := renderLayoutFor(t, "")
	assert.NotContains(t, signedOut, "signout-form",
		"there is no session to end on the signed-out dashboard")

	for _, role := range []string{"user", "admin"} {
		body := renderLayoutFor(t, role)
		assert.Contains(t, body, `action="/api/auth/logout"`, "role %s needs a way out", role)
		assert.Contains(t, body, `name="csrf_token"`,
			"role %s: the post must carry the CSRF token", role)
		assert.NotContains(t, body, `href="/api/auth/logout"`,
			"sign-out must be a POST; a GET would be a CSRF logout")
	}
}

// Sign-out has to work without JavaScript, which is why it is a form. That is
// only true if the token can arrive in a body.
func TestSignOut_FormPostEndsTheSessionServerSide(t *testing.T) {
	db := browserTestDB(t)
	user := &database.User{ID: 1, Email: "op@example.com", PasswordHash: "x", Role: "user"}
	app := browserTestApp(t, db, user)
	auth := NewAuthHandler(db)
	app.Post("/api/auth/logout", auth.Logout)

	body := &bytes.Buffer{}
	body.WriteString("csrf_token=token-from-the-form")
	req := httptest.NewRequest("POST", "/api/auth/logout", body)
	req.Header.Set("Content-Type", fiber.MIMEApplicationForm)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: SessionCookie + "-test"})

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusFound, resp.StatusCode, "a browser must land on the signed-out dashboard")
	assert.Equal(t, "/", resp.Header.Get("Location"))

	// Server-side: the session row is gone, so the cookie is worthless even if
	// something kept a copy of it.
	var count int64
	require.NoError(t, db.Model(&database.Session{}).Count(&count).Error)
	assert.Zero(t, count, "sign-out must end the session server-side, not just clear the cookie")
}

// A machine caller signing out still gets JSON, for the same reason every other
// refusal keeps its JSON body.
func TestSignOut_MachineCallerStillGetsJSON(t *testing.T) {
	db := browserTestDB(t)
	user := &database.User{ID: 1, Email: "op@example.com", PasswordHash: "x", Role: "user"}
	app := browserTestApp(t, db, user)
	app.Post("/api/auth/logout", NewAuthHandler(db).Logout)

	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: SessionCookie + "-test"})

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// Criterion: "A signed-in non-admin who reaches the admin URL gets a real 403
// page in the app's own styling, not a JSON body."
func TestAdminOnly_NonAdminBrowserGetsA403Page(t *testing.T) {
	db := browserTestDB(t)
	user := &database.User{ID: 1, Email: "someone@example.com", PasswordHash: "x", Role: "user"}
	app := browserTestApp(t, db, user)

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: SessionCookie + "-test"})

	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode,
		"the status has to stay 403 so the refusal is not mistaken for a broken page")
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	assert.NotContains(t, resp.Header.Get("Content-Type"), "application/json")

	body := readBody(t, resp)
	assert.Contains(t, body, "Not permitted")
	assert.NotContains(t, body, `href="/admin"`,
		"the refusal page must not offer the link that just refused")
}

// The machine half of the same guard. An ordinary user's API client must still
// get the 403 JSON.
func TestAdminOnly_NonAdminMachineCallerStillGetsJSON(t *testing.T) {
	db := browserTestDB(t)
	user := &database.User{ID: 1, Email: "someone@example.com", PasswordHash: "x", Role: "user"}
	app := browserTestApp(t, db, user)

	for _, route := range []string{"/api/admin/users", "/partials/admin/users"} {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest("GET", route, nil)
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: SessionCookie + "-test"})

			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
			assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
		})
	}
}

// The other half of "do not lock admins out of their own pages".
func TestAdminOnly_AdminStillReachesTheAdminPage(t *testing.T) {
	db := browserTestDB(t)
	user := &database.User{ID: 1, Email: "boss@example.com", PasswordHash: "x", Role: services.AdminRole}
	app := browserTestApp(t, db, user)

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: SessionCookie + "-test"})

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Contains(t, readBody(t, resp), "Watchlists", "the admin page renders its own content")
}

// isPageRoute is the rule that keeps /api, /rest, /partials, /ws, /tracks,
// /console and /metrics out of the page treatment. Pinned explicitly so a
// future path cannot drift into being answered with HTML.
func TestIsPageRoute_CarvesOutTheMachineSurfaces(t *testing.T) {
	// Every route is registered before anything is requested, so each decision
	// comes from a real matched route rather than a guess.
	decision := map[string]bool{}
	app := fiber.New()
	record := func(c *fiber.Ctx) error {
		decision[c.Route().Path] = isPageRoute(c)
		return c.SendStatus(fiber.StatusOK)
	}
	all := append(append([]string{}, browserPageRoutes...), machineRoutes...)
	for _, route := range all {
		app.Get(route, record)
	}
	for _, route := range all {
		resp, err := app.Test(httptest.NewRequest("GET", route, nil))
		require.NoError(t, err, route)
		_ = resp.Body.Close()
	}

	for _, route := range browserPageRoutes {
		if route == "/" {
			continue
		}
		assert.True(t, decision[route], "%s is a page a person navigates to", route)
	}
	for _, route := range machineRoutes {
		assert.False(t, decision[route], "%s is a machine surface", route)
	}
}

// The whole /api surface is registered through app.Group, and Fiber reports
// the group prefix as the matched path there. If underPrefix stops counting the
// bare prefix as a match, every API route counts as a page and a signed-out
// browser is redirected to the login page instead of getting its 401 - the bug
// this file fixes, reintroduced inside the fix.
func TestIsPageRoute_GroupedRoutesKeepTheirPrefix(t *testing.T) {
	decision := map[string]bool{}
	app := fiber.New()

	record := func(c *fiber.Ctx) error {
		decision[c.Route().Path] = isPageRoute(c)
		return c.SendStatus(fiber.StatusOK)
	}
	group := app.Group("/api", record)
	group.Get("/watchlists", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	admin := group.Group("/admin", record)
	admin.Get("/users", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for _, path := range []string{"/api/watchlists", "/api/admin/users"} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		require.NoError(t, err, path)
		_ = resp.Body.Close()
	}

	require.NotEmpty(t, decision, "the probe must have run")
	for path, isPage := range decision {
		assert.False(t, isPage, "%s is a machine surface", path)
	}
}

// A hostile ?next must not survive validation, or the sign-in page becomes an
// open redirect the moment login succeeds.
func TestSafeNextPath(t *testing.T) {
	for _, raw := range []string{"/", "/watchlists", "/watchlists?filter=recent", "/admin?section=users"} {
		assert.Equal(t, raw, safeNextPath(raw), "a local path must survive")
	}
	for _, raw := range []string{
		"", "//evil.example", "/\\evil.example", "https://evil.example",
		"http://evil.example/x", "javascript:alert(1)", "watchlists", "\n//evil.example",
	} {
		assert.Equal(t, "", safeNextPath(raw), "%q must be dropped", raw)
	}
}
