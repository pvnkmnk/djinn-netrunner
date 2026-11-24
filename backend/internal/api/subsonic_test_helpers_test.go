package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"gorm.io/gorm"
)

// app.Test uses a synthetic 0.0.0.0 peer rather than a real TLS connection.
// Existing handler tests focus on credential semantics, so this fixture models
// the test app as a trusted TLS-terminating proxy. Real transport and peer-IP
// checks are covered by subsonic_auth_tls_test.go.
func newSubsonicTestFiberApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		if len(c.Request().Header.PeekAll("X-Forwarded-Proto")) == 0 {
			c.Request().Header.Set("X-Forwarded-Proto", "https")
		}
		return c.Next()
	})
	return app
}

func newSubsonicTestHandler(db *gorm.DB, cfg *config.Config) *SubsonicHandler {
	if cfg == nil {
		cfg = &config.Config{}
	}
	configured := *cfg
	configured.SubsonicTrustedProxyCIDRs = "0.0.0.0/32"
	return NewSubsonicHandler(db, &configured)
}
