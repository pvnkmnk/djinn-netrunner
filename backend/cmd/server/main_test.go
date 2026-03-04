package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
)

func TestHealthCheck(t *testing.T) {
	app := fiber.New()
	// Mock services for route setup
	setupRoutes(app, &services.ArtistTrackingService{}, &services.ScannerService{})

	resp, err := app.Test(httptest.NewRequest("GET", "/api/health", nil))

	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}
