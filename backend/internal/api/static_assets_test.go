package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DJI-556.
//
// Static assets used to ship with no Cache-Control at all, only a Last-Modified.
// With no explicit freshness information a browser applies *heuristic* caching:
// it reuses a stored copy without asking, for a fraction of the time since
// Last-Modified. So a deploy did not reach anyone who already had the app open.
// The symptom is the worst kind - the page renders, the server is correct, the
// code is deployed, and the browser is quietly running last week's JavaScript.
// It was caught while proving DJI-547: the deep-link round trip failed while
// curl showed the server rendering exactly the right markup, because the page
// was running a stale app.js.
//
// The fix is Cache-Control: no-cache - the browser may store this, and must not
// use the stored copy without asking. The point of these tests is that the
// *other* half is not broken by it, because "no-cache" that quietly becomes
// "re-download everything every load" is the same trade wearing a different hat.
// Both halves are asserted.

// staticTestRoot creates a directory the file handler may hold open.
//
// fasthttp keeps a file handle per served asset until its cache cleaner runs,
// so on Windows t.TempDir()'s RemoveAll fails with "being used by another
// process" and the suite goes red for a reason that has nothing to do with the
// assertions. Best-effort cleanup, documented, beats a test that is green on
// Linux and red on Windows.
func staticTestRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "netrunner-static-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeAsset(t *testing.T, root, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
}

// serveAssets starts a fresh handler over root, the way a deploy starts a fresh
// process. Each call is its own server, which is what makes the "changed
// asset" case below a faithful model of a deploy rather than a fiction.
func serveAssets(root string) *fiber.App {
	app := fiber.New()
	app.Use("/static", StaticAssetRevalidation())
	app.Static("/static", root)
	return app
}

func getAsset(t *testing.T, app *fiber.App, headers map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest("GET", "/static/app.js", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, 5000)
	require.NoError(t, err)
	return resp
}

// Criterion: a deploy must never leave a browser running last week's
// JavaScript. no-cache is the instruction that makes the browser ask at all.
func TestStaticAssets_CarryAnExplicitRevalidationHeader(t *testing.T) {
	root := staticTestRoot(t)
	writeAsset(t, root, "app.js", "console.log('v1')")

	resp := getAsset(t, serveAssets(root), nil)
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"),
		"without an explicit freshness instruction the browser reuses a stored copy "+
			"heuristically, which is how a deploy silently failed to reach returning users")
	assert.NotEmpty(t, resp.Header.Get("Last-Modified"),
		"the validator is the point; no-cache is only the instruction to use it")
}

// The cheap half, and the half a lazy fix would break: an unchanged asset must
// revalidate to an empty 304 rather than be downloaded again.
func TestStaticAssets_UnchangedAssetRevalidatesToAnEmpty304(t *testing.T) {
	root := staticTestRoot(t)
	writeAsset(t, root, "app.js", "console.log('v1')")
	app := serveAssets(root)

	first := getAsset(t, app, nil)
	require.Equal(t, fiber.StatusOK, first.StatusCode)
	firstBody, err := io.ReadAll(first.Body)
	require.NoError(t, err)
	first.Body.Close()
	require.NotEmpty(t, firstBody, "sanity: the first fetch really did transfer the asset")

	second := getAsset(t, app, map[string]string{
		"If-Modified-Since": first.Header.Get("Last-Modified"),
	})
	defer second.Body.Close()

	assert.Equal(t, fiber.StatusNotModified, second.StatusCode,
		"an unchanged asset must revalidate cheaply, not be re-downloaded wholesale")
	secondBody, err := io.ReadAll(second.Body)
	require.NoError(t, err)
	assert.Empty(t, secondBody, "a 304 must not carry the asset again")
}

// The other half: a deploy that changed the asset must reach a browser that
// asks. Modelled as a fresh handler over changed bytes, because that is what a
// deploy is - a new process with a cold file cache serving new files.
func TestStaticAssets_DeployWithAChangedAssetServesTheNewBytes(t *testing.T) {
	root := staticTestRoot(t)
	writeAsset(t, root, "app.js", "console.log('v1')")

	before := getAsset(t, serveAssets(root), nil)
	beforeBody, err := io.ReadAll(before.Body)
	require.NoError(t, err)
	before.Body.Close()
	require.Equal(t, "console.log('v1')", string(beforeBody))
	staleValidator := before.Header.Get("Last-Modified")
	require.NotEmpty(t, staleValidator)

	// The deploy. The sleep crosses a second boundary because Last-Modified is
	// an HTTP date with one-second resolution, so a change inside the same
	// second is indistinguishable from no change at all. TestStaticAssets_
	// PinThatTheValidatorHasOneSecondResolution asserts that limit rather than
	// hiding it, and it is the reason a content-based ETag would be the next
	// step if this ever needed to be tighter.
	time.Sleep(1100 * time.Millisecond)
	writeAsset(t, root, "app.js", "console.log('v2')")

	after := getAsset(t, serveAssets(root), map[string]string{
		"If-Modified-Since": staleValidator,
	})
	defer after.Body.Close()

	assert.Equal(t, fiber.StatusOK, after.StatusCode,
		"a changed asset must not be answered 304 - serving stale bytes with a correct-looking "+
			"status is the failure mode this slice exists to remove")
	afterBody, err := io.ReadAll(after.Body)
	require.NoError(t, err)
	assert.Equal(t, "console.log('v2')", string(afterBody), "a deploy must reach a browser that asks")
}

// A validator nobody honours is the same bug wearing a hat, so this asserts the
// measurement that ruled out an ETag. If a future Fiber or fasthttp upgrade
// starts answering If-None-Match, this test is the signal to revisit that
// decision - not to assume the new behaviour is wanted.
func TestStaticAssets_NoETagBecauseThisStackIgnoresIfNoneMatch(t *testing.T) {
	root := staticTestRoot(t)
	writeAsset(t, root, "app.js", "console.log('v1')")
	app := serveAssets(root)

	resp := getAsset(t, app, nil)
	defer resp.Body.Close()
	assert.Empty(t, resp.Header.Get("ETag"),
		"emitting an ETag here would make caching strictly worse: browsers prefer "+
			"If-None-Match over If-Modified-Since (RFC 9110 13.1.3) and this stack answers "+
			"If-None-Match with the full body every time")

	// Named rather than inlined: a raw string literal nested inside a composite
	// literal is exactly where a stray quote hides.
	const someETag = `"nonsense"`
	bogus := getAsset(t, app, map[string]string{"If-None-Match": someETag})
	defer bogus.Body.Close()
	assert.Equal(t, fiber.StatusOK, bogus.StatusCode,
		"If-None-Match is not honoured by this stack, which is precisely why there is no ETag")
}

// Measured behaviour, recorded so a future reader is not surprised and so the
// suite does not assert something untrue: the file handler resets the response
// when it answers 304, so the 304 carries no headers at all. Harmless - the
// browser keeps the stored response, and the stored response is the one
// carrying no-cache - but it is not something to write a passing assertion
// about.
func TestStaticAssets_The304ItselfCarriesNoHeaders(t *testing.T) {
	root := staticTestRoot(t)
	writeAsset(t, root, "app.js", "console.log('v1')")
	app := serveAssets(root)

	first := getAsset(t, app, nil)
	first.Body.Close()

	second := getAsset(t, app, map[string]string{
		"If-Modified-Since": first.Header.Get("Last-Modified"),
	})
	defer second.Body.Close()

	require.Equal(t, fiber.StatusNotModified, second.StatusCode)
	assert.Empty(t, second.Header.Get("Cache-Control"),
		"documented consequence of fasthttp's NotModified() resetting the response; the browser "+
			"still revalidates every load because the stored 200 carries no-cache")
}

// The limit of the validator, pinned so the passing deploy test above is not
// read as "a change within the same second is handled". Last-Modified is an
// HTTP date with one-second resolution and this stack ignores If-None-Match,
// so a same-second change is answered 304 - correct behaviour for the protocol,
// and a real ceiling on the guarantee.
//
// It is narrow here because assets are baked into a container image: a deploy is
// a new process with a new set of build timestamps, not a same-second edit.
// Tightening it means implementing a content-based ETag *and honouring it*,
// which is DJI-556's follow-up rather than something to bolt on here.
func TestStaticAssets_PinThatTheValidatorHasOneSecondResolution(t *testing.T) {
	root := staticTestRoot(t)
	writeAsset(t, root, "app.js", "console.log('v1')")

	first := getAsset(t, serveAssets(root), nil)
	first.Body.Close()
	staleValidator := first.Header.Get("Last-Modified")

	// Rewritten immediately: same second, different content.
	writeAsset(t, root, "app.js", "console.log('v2')")

	sameSecond := getAsset(t, serveAssets(root), map[string]string{
		"If-Modified-Since": staleValidator,
	})
	defer sameSecond.Body.Close()
	assert.Equal(t, fiber.StatusNotModified, sameSecond.StatusCode,
		"a same-second change is indistinguishable from no change with a one-second validator; "+
			"this test exists so that ceiling is recorded rather than rediscovered")

	// One second later the same request is answered correctly.
	time.Sleep(1100 * time.Millisecond)
	writeAsset(t, root, "app.js", "console.log('v3')")
	nextSecond := getAsset(t, serveAssets(root), map[string]string{
		"If-Modified-Since": staleValidator,
	})
	defer nextSecond.Body.Close()
	assert.Equal(t, fiber.StatusOK, nextSecond.StatusCode,
		"across a second boundary the same validator is accurate")
	body, err := io.ReadAll(nextSecond.Body)
	require.NoError(t, err)
	assert.Equal(t, "console.log('v3')", string(body))
}

// The middleware is scoped to /static. Nothing else about the app's caching
// changes, and in particular the API and page routes are left alone.
func TestStaticAssetRevalidation_OnlyTouchesStaticPaths(t *testing.T) {
	root := staticTestRoot(t)
	writeAsset(t, root, "app.js", "console.log('v1')")

	app := fiber.New()
	app.Use("/static", StaticAssetRevalidation())
	app.Static("/static", root)
	app.Get("/api/thing", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	asset := getAsset(t, app, nil)
	asset.Body.Close()
	assert.Equal(t, "no-cache", asset.Header.Get("Cache-Control"))

	apiResp, err := app.Test(httptest.NewRequest("GET", "/api/thing", nil), 5000)
	require.NoError(t, err)
	defer apiResp.Body.Close()
	assert.Empty(t, apiResp.Header.Get("Cache-Control"),
		"scoping this to the static prefix is deliberate: API responses are not ours to re-cache")
}
