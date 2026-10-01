package api

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type DashboardHandler struct {
	db *gorm.DB

	// minPasswordLength is shown on the registration form. Zero falls back to
	// DefaultMinPasswordLength so a zero-valued handler never renders "at
	// least 0 characters".
	minPasswordLength int
}

func NewDashboardHandler(db *gorm.DB) *DashboardHandler {
	return &DashboardHandler{db: db}
}

// NewDashboardHandlerWithPolicy returns a dashboard handler that states
// minLength on its registration form. Pass 0 for the default.
func NewDashboardHandlerWithPolicy(db *gorm.DB, minLength int) *DashboardHandler {
	if minLength < 1 {
		minLength = DefaultMinPasswordLength
	}
	return &DashboardHandler{db: db, minPasswordLength: minLength}
}

func (h *DashboardHandler) RenderIndex(c *fiber.Ctx) error {
	// Try to get user from middleware locals (optional auth for landing page).
	var user database.User
	var authUserID string
	if localUser, ok := currentUserFromLocals(c); ok {
		user = localUser
		authUserID = strconv.FormatUint(user.ID, 10)
	}

	data := fiber.Map{
		"User":       user,
		"authUserID": authUserID,
		// The registration form states the floor rather than leaving a person to
		// discover it by being rejected. The server is what enforces it; see
		// AuthHandler.Register.
		"MinPasswordLength": h.minPasswordLength,
		// Where to send the user once they sign in. Validated here rather
		// than in the browser, so a hostile ?next= cannot turn the sign-in
		// page into an open redirect.
		"NextPath": safeNextPath(c.Query("next")),
	}

	// Server-render both regions for a signed-in user. The page shipped a
	// hard-coded "Loading stats..." / "Loading watchlists..." placeholder and
	// triggered only on "every 30s" / "every 60s" — no load trigger — so the
	// first real content arrived half a minute to a minute after the page. To
	// a new user that is indistinguishable from a broken account: they cannot
	// tell "you have nothing yet" from "nothing is ever coming", and the only
	// way to find out was to reload and wait again.
	//
	// Only for a signed-in user. These contexts are owner-scoped, so asking
	// them for a zero-valued user would render somebody else's empty account.
	//
	// h.db == nil is the unwired-handler case: a DashboardHandler built as a
	// struct literal instead of through NewDashboardHandler. Every query
	// builder below nil-derefs, so serve the sign-in screen rather than panic
	// on a live request.
	if authUserID != "" && h.db != nil {
		for k, v := range statsRegionContext(h.db, user) {
			data[k] = v
		}
		for k, v := range watchlistsRegionContext(h.db, user) {
			data[k] = v
		}
		data["DashboardReady"] = true
	}

	// RenderPage, not a bare c.Render: the base layout's footer renders
	// {{ Version }}, and RenderPage is what supplies it from AppVersion.
	// Rendering directly left the variable empty, so the one page every
	// signed-in user lands on showed "NetRunner v" with no version while
	// every other page showed it.
	return RenderPage(c, "dashboard", "index", data)
}
