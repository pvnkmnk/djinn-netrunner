package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Registration used to accept a three-character password: POST /api/auth/register
// returned 201, a session was established, and nothing anywhere in the product
// said a floor existed. These pin the floor at the three points that matter -
// below it, exactly on it, and above it - and pin that the rejection names the
// minimum, so a person is told the rule rather than just refused.
func TestRegisterEnforcesMinimumPasswordLength(t *testing.T) {
	const minimum = 12

	tests := []struct {
		name       string
		password   string
		wantStatus int
	}{
		{name: "below minimum", password: "abc", wantStatus: 400},
		{name: "one below minimum", password: strings.Repeat("a", minimum-1), wantStatus: 400},
		{name: "exactly minimum", password: strings.Repeat("a", minimum), wantStatus: 201},
		{name: "above minimum", password: strings.Repeat("a", minimum+5), wantStatus: 201},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, _, _, _ := setupPartialsTestDB(t)

			app := fiber.New()
			handler := NewAuthHandlerWithPolicy(db, "", "", minimum)
			app.Post("/api/auth/register", handler.Register)

			body := fmt.Sprintf(
				`{"email":"%s@nr.test","password":%q}`,
				strings.NewReplacer(" ", "", "/", "").Replace(tc.name), tc.password,
			)
			req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			require.NoError(t, err)
			respBody := policyBody(t, resp)

			assert.Equal(t, tc.wantStatus, resp.StatusCode, "response body: %s", respBody)
			if tc.wantStatus == 400 {
				// The message names the minimum: a bare "invalid password"
				// leaves the person guessing what to type instead.
				assert.Contains(t, respBody, fmt.Sprintf("at least %d characters", minimum),
					"the rejection must name the minimum")
				assert.Contains(t, respBody, fmt.Sprintf(`"minLength":%d`, minimum),
					"the form reads the minimum from the response")

				var stored database.User
				err = db.Where("email = ?", strings.NewReplacer(" ", "", "/", "").Replace(tc.name)+"@nr.test").
					First(&stored).Error
				assert.Error(t, err, "a rejected password must not create the account")
			}
		})
	}
}

// A zero-valued handler - which every test that builds one as a struct literal
// produces - must still enforce a policy rather than silently accepting
// anything, because that is how the original defect shipped.
func TestRegisterEnforcesTheDefaultWhenNoPolicyIsConfigured(t *testing.T) {
	db, _, _, _ := setupPartialsTestDB(t)

	app := fiber.New()
	app.Post("/api/auth/register", (&AuthHandler{db: db}).Register)

	req := httptest.NewRequest("POST", "/api/auth/register",
		bytes.NewBufferString(`{"email":"short@nr.test","password":"abc"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	assert.Contains(t, policyBody(t, resp),
		fmt.Sprintf("at least %d characters", DefaultMinPasswordLength))
}

// The floor counts characters, not bytes. Byte counting is never stricter
// than rune counting - a byte count is never below a rune count - so the only
// direction in which the two disagree is a password that is short in
// characters but long in bytes. e-acute is one character in two bytes, so six
// of them are six characters and twelve bytes: a byte count waves that through.
func TestRegisterCountsRunesNotBytes(t *testing.T) {
	const minimum = 12
	multiByte := "é"

	short := strings.Repeat(multiByte, minimum/2)
	long := strings.Repeat(multiByte, minimum)

	// The fixtures are the whole point of the test, so pin what they measure.
	require.Equal(t, 6, utf8.RuneCountInString(short), "six characters")
	require.Equal(t, minimum, len(short), "twelve bytes")
	require.Equal(t, minimum, utf8.RuneCountInString(long), "twelve characters")
	require.Greater(t, len(long), minimum, "twenty-four bytes")

	tests := []struct {
		name       string
		email      string
		password   string
		wantStatus int
	}{
		{
			name:       "under the floor in characters, at it in bytes",
			email:      "short-wide@nr.test",
			password:   short,
			wantStatus: 400,
		},
		{
			name:       "at the floor in characters, over it in bytes",
			email:      "long-wide@nr.test",
			password:   long,
			wantStatus: 201,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, _, _, _ := setupPartialsTestDB(t)

			app := fiber.New()
			app.Post("/api/auth/register", NewAuthHandlerWithPolicy(db, "", "", minimum).Register)

			body := fmt.Sprintf(`{"email":%q,"password":%q}`, tc.email, tc.password)
			req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode, "response body: %s", policyBody(t, resp))
		})
	}
}

// The Jobs page type filter must offer every job type the app creates. This is
// the user-visible half; cmd/worker holds the half that keeps the list and the
// worker's switch in step.
func TestJobsTypeFilterOffersEveryJobType(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	// One job of every type the app creates, including the ownerless
	// release_monitor the scheduler enqueues.
	for _, jt := range database.JobTypes {
		require.NoError(t, db.Create(&database.Job{
			Type:        jt.Value,
			State:       "succeeded",
			RequestedAt: time.Now(),
		}).Error)
	}

	engine := templates.NewPongo2("../../../ops/web/templates", ".html")
	app := fiber.New(fiber.Config{Views: engine})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Get("/partials/jobs", (&StatsHandler{db: db}).RenderJobsPartial)

	resp, err := app.Test(httptest.NewRequest("GET", "/partials/jobs", nil))
	require.NoError(t, err)
	html := policyBody(t, resp)

	for _, jt := range database.JobTypes {
		assert.Contains(t, html, `value="`+jt.Value+`"`,
			"the filter must offer %s", jt.Value)
	}

	// The regression this ticket exists for, named directly.
	assert.Contains(t, html, `value="release_monitor"`,
		"release_monitor is the scheduler's own job type and must be filterable")
}

// Selecting a type must return that type's jobs. An option that renders but
// filters nothing would satisfy the test above and still be broken.
func TestJobsTypeFilterActuallyFilters(t *testing.T) {
	db, user, _, _ := setupPartialsTestDB(t)

	for _, jobType := range []string{"release_monitor", "artist_scan", "acquisition"} {
		require.NoError(t, db.Create(&database.Job{
			Type:        jobType,
			State:       "succeeded",
			RequestedAt: time.Now(),
			// The list is owner-scoped for a non-admin, so a job with no
			// owner is invisible to this viewer. That is correct behaviour,
			// and it would have made this test pass for the wrong reason.
			OwnerUserID: &user.ID,
		}).Error)
	}

	engine := templates.NewPongo2("../../../ops/web/templates", ".html")
	app := fiber.New(fiber.Config{Views: engine})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Get("/partials/jobs", (&StatsHandler{db: db}).RenderJobsPartial)

	resp, err := app.Test(httptest.NewRequest("GET", "/partials/jobs?job_type=release_monitor", nil))
	require.NoError(t, err)
	html := policyBody(t, resp)

	// Scope to the list. The dropdown necessarily names every type, so
	// asserting on the whole page would match the filter, not the rows.
	list := html
	if start := strings.Index(html, `<div id="jobs-list">`); start >= 0 {
		list = html[start:]
	}

	assert.Contains(t, list, "release_monitor",
		"the selected type must return its jobs")
	assert.NotContains(t, list, "artist_scan",
		"a different type must not survive the filter")
	assert.NotContains(t, list, "acquisition")
	assert.NotContains(t, html, "No jobs found",
		"the filter matched nothing, so it proves nothing")
}

// The registration form states the floor, so a person finds out by reading it
// rather than by being rejected. The server remains the thing that enforces it.
func TestRegisterFormStatesTheMinimum(t *testing.T) {
	engine := templates.NewPongo2("../../../ops/web/templates", ".html")
	app := fiber.New(fiber.Config{Views: engine})
	app.Get("/", NewDashboardHandlerWithPolicy(nil, 14).RenderIndex)

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	html := policyBody(t, resp)

	assert.Contains(t, html, `minlength="14"`,
		"the register form must carry the configured minimum")
	assert.Contains(t, html, "At least 14 characters")
}

// A dashboard handler built by NewDashboardHandler or a struct literal has no
// policy and still renders the registration form. The form must state the
// default, not a floor of zero: the server enforces the default, so a form
// reading 0 beside a server demanding 12 is a form that lies.
func TestRegisterFormStatesTheDefaultWhenNoPolicyIsConfigured(t *testing.T) {
	engine := templates.NewPongo2("../../../ops/web/templates", ".html")
	app := fiber.New(fiber.Config{Views: engine})
	app.Get("/", NewDashboardHandler(nil).RenderIndex)

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	html := policyBody(t, resp)

	assert.NotContains(t, html, `minlength="0"`, "a zero policy must not render a floor of zero")
	assert.NotContains(t, html, "At least 0 characters")
	assert.Contains(t, html, fmt.Sprintf(`minlength="%d"`, DefaultMinPasswordLength))
	assert.Contains(t, html, fmt.Sprintf("At least %d characters", DefaultMinPasswordLength))
}

// policyBody reads a response body as text, so an assertion failure can quote
// what the endpoint actually said.
func policyBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(data)
}

// -----------------------------------------------------------------------
// The ceiling (DJI-559)
// -----------------------------------------------------------------------
// Registration hashed whatever it was given, so a password over bcrypt's
// 72-byte ceiling returned 500 "failed to hash password" -- a server fault
// for a request a person made reasonably. A browser states no length limit on
// a password field and a passphrase generator will produce 80 bytes, so the
// refusal is reachable in ordinary use.
//
// These pin the boundary itself rather than the happy path: the longest
// password still accepted, the first one refused, and the case that separates
// the two limits -- under 72 CHARACTERS but over 72 BYTES.

func TestRegisterRefusesPasswordsOverBcryptsByteLimit(t *testing.T) {
	const minimum = 12
	const ceiling = config.BcryptMaxPasswordBytes
	eAcute := "\u00e9"

	// The widest case: 40 characters, 80 bytes. It clears the 12-character
	// floor comfortably and is still under 72 characters, so a rune-counted
	// ceiling would wave it through and bcrypt would still refuse it.
	multiByte := strings.Repeat(eAcute, 40)
	require.Equal(t, 40, utf8.RuneCountInString(multiByte), "forty characters")
	require.Equal(t, 80, len(multiByte), "eighty bytes -- under 72 characters, over the ceiling")

	atCeiling := strings.Repeat("a", ceiling)
	overCeiling := strings.Repeat("a", ceiling+1)

	tests := []struct {
		name       string
		email      string
		password   string
		wantStatus int
	}{
		{"the longest accepted password is exactly the ceiling", "at-ceiling@nr.test", atCeiling, 201},
		{"one byte over the ceiling is refused", "over-ceiling@nr.test", overCeiling, 400},
		{"a long passphrase well past the ceiling is refused", "long@nr.test", strings.Repeat("a", 100), 400},
		{"under 72 characters but over 72 bytes is refused", "multibyte@nr.test", multiByte, 400},
		{"exactly at the ceiling in bytes, in multi-byte text", "wide-at@nr.test", strings.Repeat(eAcute, ceiling/2), 201},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, _, _, _ := setupPartialsTestDB(t)

			app := fiber.New()
			app.Post("/api/auth/register", NewAuthHandlerWithPolicy(db, "", "", minimum).Register)

			body := fmt.Sprintf(`{"email":%q,"password":%q}`, tc.email, tc.password)
			req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			require.NoError(t, err)
			got := policyBody(t, resp)
			require.Equal(t, tc.wantStatus, resp.StatusCode, "response body: %s", got)

			if tc.wantStatus == 400 {
				// The regression this ticket exists for: a 500 here is the bug.
				assert.NotEqual(t, 500, resp.StatusCode,
					"an over-limit password must be a client error, never a server fault")
				// The message has to name the limit AND the unit. "Password too
				// long" tells a user nothing when the rule is in bytes.
				assert.Contains(t, got, fmt.Sprintf("%d bytes", ceiling),
					"the refusal must name the ceiling in bytes")
				assert.Contains(t, got, "not characters",
					"the refusal must say the limit is not counted in characters")
				assert.Contains(t, got, fmt.Sprintf(`"maxBytes":%d`, ceiling))
			}
		})
	}
}

// The ceiling is checked BEFORE the duplicate lookup, so a refusal cannot tell
// an attacker whether the address is registered: an over-limit password must
// get the same 400 whether or not the account exists. If this ever moved after
// the duplicate branch, an existing address would answer 201 and a new one 400,
// which enumerates the user table.
func TestCeilingRefusalDoesNotRevealWhetherTheAccountExists(t *testing.T) {
	const minimum = 12
	const ceiling = config.BcryptMaxPasswordBytes
	db, _, _, _ := setupPartialsTestDB(t)

	app := fiber.New()
	app.Post("/api/auth/register", NewAuthHandlerWithPolicy(db, "", "", minimum).Register)

	register := func(email, password string) int {
		body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
		req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		_ = policyBody(t, resp)
		return resp.StatusCode
	}

	const existing = "known@nr.test"
	require.Equal(t, 201, register(existing, strings.Repeat("a", minimum)),
		"the account must exist before the comparison is meaningful")

	over := strings.Repeat("a", ceiling+1)
	require.Equal(t, 400, register(existing, over),
		"an existing account must NOT answer 201 to an over-limit password")
	require.Equal(t, 400, register("unknown@nr.test", over),
		"a new account must answer identically")
}

// The form states both limits. HTML minlength counts characters and cannot
// express a byte ceiling, so without the sentence in the hint a user satisfies
// the rule they can see and is still refused.
func TestRegisterFormStatesTheByteCeiling(t *testing.T) {
	engine := templates.NewPongo2("../../../ops/web/templates", ".html")
	app := fiber.New(fiber.Config{Views: engine})
	app.Get("/", NewDashboardHandlerWithPolicy(nil, 14).RenderIndex)

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	html := policyBody(t, resp)

	assert.Contains(t, html, fmt.Sprintf("At least 14 characters, at most %d bytes",
		config.BcryptMaxPasswordBytes),
		"the form must state the character floor AND the byte ceiling together")
}
