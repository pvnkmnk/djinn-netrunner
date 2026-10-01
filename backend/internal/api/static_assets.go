package api

import (
	"github.com/gofiber/fiber/v2"
)

// DJI-556.
//
// Static assets used to be served with no Cache-Control at all, only a
// Last-Modified. With no explicit freshness information a browser applies
// *heuristic* caching: it reuses a stored copy without asking, for a fraction
// of the time since Last-Modified. So a deploy did not reach anyone who already
// had the app open. The symptom is the worst kind — the page renders, the
// server is correct, the code is deployed, and the browser is quietly running
// last week's JavaScript. It was caught while proving DJI-547 in a browser: the
// deep-link round trip failed while curl showed the server rendering exactly
// the right markup, because the page was running a stale app.js.
//
// The fix is Cache-Control: no-cache. Not max-age=0 and not a removal: those
// leave the decision to a default. no-cache states it plainly — the browser may
// store this, and must not use the stored copy without asking.
//
// That is the whole design, and the rest of this file is the part worth
// knowing: the cheap path.
//
// Deliberately no ETag. Measured against this exact stack (Fiber v2.52 static
// over fasthttp v1.68), If-Modified-Since is honoured and answered with a 304
// carrying no body, while If-None-Match is ignored entirely — even "*" comes
// back 200 with the full file. Adding an ETag would therefore make caching
// strictly worse: per RFC 9110 13.1.3 a browser prefers If-None-Match over
// If-Modified-Since, so every load would send a validator the server ignores
// and re-download the whole asset. A validator nobody returns is the same bug
// wearing a hat.
//
// Last-Modified is the validator, and it works. Unchanged assets revalidate to
// a bodiless 304; changed assets come back 200 with the new bytes. Verified in
// both directions in a warm browser profile, which is the only way to tell a
// caching fix from a header that merely looks right.

// StaticAssetRevalidation marks static responses as must-revalidate.
//
// Scoped to the static prefix by the caller, so nothing about the API,
// Subsonic or page routes changes. It sets the header before calling Next
// because the file handler appends to the response rather than replacing it.
func StaticAssetRevalidation() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderCacheControl, "no-cache")
		return c.Next()
	}
}
