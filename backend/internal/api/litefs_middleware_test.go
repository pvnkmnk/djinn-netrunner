package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liteFSPrimaryGuard always reports as primary (single-node / no LiteFS).
type liteFSPrimaryGuard struct{}

func (g *liteFSPrimaryGuard) IsPrimary() bool            { return true }
func (g *liteFSPrimaryGuard) GetPrimaryHostname() string { return "" }

func TestLiteFSWriteForward_GETPassesThrough(t *testing.T) {
	guard := &liteFSPrimaryGuard{}
	app := fiber.New()
	app.Use(LiteFSWriteForward(guard, "http", "8080"))
	app.Get("/api/test", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"local": true})
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/api/test", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "local")
}

func TestLiteFSWriteForward_POSTPassesThroughOnPrimary(t *testing.T) {
	guard := &liteFSPrimaryGuard{}
	app := fiber.New()
	app.Use(LiteFSWriteForward(guard, "http", "8080"))
	app.Post("/api/test", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"written": true})
	})

	resp, err := app.Test(httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"key":"val"}`)))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "written")
}

func TestLiteFSWriteForward_ForwardsToPrimary(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "true", r.Header.Get("X-LiteFS-Forward"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"forwarded":true}`))
	}))
	defer primary.Close()

	// Extract host and port from the test server
	addr := primary.Listener.Addr().String()

	guard := &liteFSForwardTestGuard{primaryHost: addr}

	app := fiber.New()
	app.Use(LiteFSWriteForward(guard, "http", "8080"))
	app.Post("/api/test", func(c *fiber.Ctx) error {
		t.Fatal("should not reach local handler")
		return nil
	})

	resp, err := app.Test(httptest.NewRequest("POST", "/api/test", strings.NewReader(`{}`)))
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "forwarded")
}

// liteFSForwardTestGuard implements a guard that always says "not primary"
// and returns the given host as the primary.
type liteFSForwardTestGuard struct {
	primaryHost string
}

func (g *liteFSForwardTestGuard) IsPrimary() bool            { return false }
func (g *liteFSForwardTestGuard) GetPrimaryHostname() string { return g.primaryHost }
