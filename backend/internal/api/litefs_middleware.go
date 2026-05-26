package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// LiteFSNodeDetector abstracts primary-node detection for LiteFS.
type LiteFSNodeDetector interface {
	IsPrimary() bool
	GetPrimaryHostname() string
}

// LiteFSWriteForward returns Fiber middleware that forwards write requests
// (POST, PUT, PATCH, DELETE) to the LiteFS primary node when the current
// node is a read-only replica. GET/HEAD/OPTIONS requests are always served
// locally. When no LiteFS primary file exists (single-node deployment),
// the middleware is a no-op pass-through.
func LiteFSWriteForward(guard LiteFSNodeDetector, scheme string, port string) fiber.Handler {
	if scheme == "" {
		scheme = "http"
	}
	if port == "" {
		port = "8080"
	}

	return func(c *fiber.Ctx) error {
		method := c.Method()
		if method == fiber.MethodGet || method == fiber.MethodHead || method == fiber.MethodOptions {
			return c.Next()
		}

		if guard.IsPrimary() {
			return c.Next()
		}

		primary := guard.GetPrimaryHostname()
		if primary == "" {
			return c.Next()
		}

		var target string
		if strings.Contains(primary, ":") {
			target = fmt.Sprintf("%s://%s%s", scheme, primary, c.OriginalURL())
		} else {
			target = fmt.Sprintf("%s://%s:%s%s", scheme, primary, port, c.OriginalURL())
		}
		slog.Info("LiteFS forwarding write to primary", "method", method, "target", target)

		req, err := http.NewRequestWithContext(context.Background(), method, target, bytes.NewReader(c.Body()))
		if err != nil {
			slog.Error("LiteFS forward: failed to create request", "error", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "failed to forward write to primary node",
			})
		}

		c.Request().Header.VisitAll(func(key, val []byte) {
			req.Header.Set(string(key), string(val))
		})
		req.Header.Set("X-Forwarded-For", c.IP())
		req.Header.Set("X-LiteFS-Forward", "true")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Error("LiteFS forward: primary unreachable", "primary", primary, "error", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "primary node unreachable",
			})
		}
		defer resp.Body.Close()

		for k, vals := range resp.Header {
			for _, v := range vals {
				c.Set(k, v)
			}
		}
		c.Status(resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("LiteFS forward: failed to read response", "error", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "failed to read primary response",
			})
		}
		return c.Send(body)
	}
}
