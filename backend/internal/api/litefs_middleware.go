package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

		ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(c.Body()))
		if err != nil {
			slog.Error("LiteFS forward: failed to create request", "error", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "failed to forward write to primary node",
			})
		}

		c.Request().Header.VisitAll(func(key, val []byte) {
			k := string(key)
			if strings.EqualFold(k, "Connection") ||
				strings.EqualFold(k, "Keep-Alive") ||
				strings.EqualFold(k, "Proxy-Authenticate") ||
				strings.EqualFold(k, "Proxy-Authorization") ||
				strings.EqualFold(k, "TE") ||
				strings.EqualFold(k, "Trailers") ||
				strings.EqualFold(k, "Transfer-Encoding") ||
				strings.EqualFold(k, "Upgrade") ||
				strings.EqualFold(k, "Content-Length") {
				return
			}
			req.Header.Add(k, string(val))
		})
		req.Header.Set("X-Forwarded-For", c.IP())
		req.Header.Set("X-LiteFS-Forward", "true")

		client := &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			slog.Error("LiteFS forward: primary unreachable", "primary", primary, "error", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "primary node unreachable",
			})
		}
		defer resp.Body.Close()

		for k, vals := range resp.Header {
			if len(vals) > 0 {
				c.Set(k, vals[0])
				for _, v := range vals[1:] {
					c.Append(k, v)
				}
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
