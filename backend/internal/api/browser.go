package api

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// DJI-547.
//
// Every guard in this app answers with JSON. That is right for a machine
// caller and wrong for a person: a browser that follows a link, a bookmark or a
// refresh should land on a page, and "{"error":"not authenticated"}" rendered by
// the browser's JSON pretty-printer is not a page.
//
// So the guards decide what they are talking to before they answer. A request
// that wants HTML and is not an htmx swap gets a rendered page; everything else
// keeps the JSON body it always had, which is what keeps API clients, the
// Subsonic surface and htmx working untouched.

// machinePathPrefixes are the route families a person never navigates to. They
// are machine surfaces: JSON APIs, the Subsonic protocol, htmx fragments,
// websockets, media streaming and the metrics scrape. A refusal on any of them
// stays JSON whatever the Accept header says — a Subsonic client that got a
// login page would fail in a far more confusing way than one that got a 401.
var machinePathPrefixes = []string{
	"/api/",
	"/rest/",
	"/partials/",
	"/ws/",
	"/tracks/",
	"/console/",
	"/metrics",
	"/static/",
}

// wantsHTMLPage reports whether this request is a browser asking for a
// document. htmx is excluded deliberately: it sends HX-Request on every swap
// and asks for "text/html, */*", so the Accept header alone would classify an
// htmx fragment fetch as a page navigation and swap a login page into a panel.
func wantsHTMLPage(c fiber.Ctx) bool {
	if isHTMXRequest(c) {
		return false
	}
	return strings.Contains(c.Get(fiber.HeaderAccept), fiber.MIMETextHTML)
}

// isPageRoute reports whether the matched route is a full page a person can
// arrive at by clicking, typing a URL or following a shared link.
func isPageRoute(c fiber.Ctx) bool {
	path := c.Route().Path
	for _, prefix := range machinePathPrefixes {
		if underPrefix(path, prefix) {
			return false
		}
	}
	return true
}

// underPrefix matches a route path against a path prefix, counting the bare
// prefix as a match.
//
// The bare case is not cosmetic. Every /api route in this app is registered
// through app.Group, and Fiber reports the group prefix as the matched path
// there, so a plain strings.HasPrefix(path, "/api/") compared "/api" against
// "/api/" and said no. Every JSON endpoint then counted as a page, and a
// signed-out browser hitting one was redirected to the login page instead of
// being answered - the bug this file exists to fix, sitting inside the fix.
func underPrefix(path, prefix string) bool {
	trimmed := strings.TrimSuffix(prefix, "/")
	return path == trimmed || strings.HasPrefix(path, trimmed+"/")
}

// shouldRenderPage is the single question both guards ask: is this refusal
// going to be read by a person in a browser?
func shouldRenderPage(c fiber.Ctx) bool {
	return isPageRoute(c) && wantsHTMLPage(c)
}

// redirectToSignIn sends a browser to the sign-in page, remembering where it
// was going so login can send it back. c.OriginalURL is the path the person
// actually asked for, query string included, which is what makes a deep link
// survive the round trip.
func redirectToSignIn(c fiber.Ctx) error {
	return c.Redirect().Status(fiber.StatusFound).To("/?next=" + url.QueryEscape(c.OriginalURL()))
}

// renderForbiddenPage answers a browser that reached a page it has no role
// for. It renders the real 403 template through RenderPage so the refusal
// arrives in the app's own chrome - same header, same nav, same footer - with
// a 403 status the browser and any proxy in front of it can both see.
//
// The status matters as much as the page: it is what tells a screen reader
// this is a refusal rather than a page that failed to load, and it is what
// keeps this honest for the non-browser callers that never see the HTML.
func renderForbiddenPage(c fiber.Ctx) error {
	c.Status(fiber.StatusForbidden)
	return RenderPage(c, "forbidden", "pages/forbidden", fiber.Map{})
}

// safeNextPath validates the return path carried through sign-in.
//
// The value is attacker-supplied: it arrives in the query string of the page
// the redirect lands on, so anything that is not a plain local path is dropped
// and the user goes to the dashboard. Without this, "?next=//evil.example"
// would turn the sign-in page into an open redirect the moment login succeeded.
func safeNextPath(raw string) string {
	if raw == "" {
		return ""
	}
	// Scheme-relative ("//host") and absolute ("https://host") URLs both leave
	// the origin, and a backslash is normalised to "/" by some browsers, so it
	// is rejected too.
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return ""
	}
	return raw
}
