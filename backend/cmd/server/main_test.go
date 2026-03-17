package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
)

func TestHealthCheck(t *testing.T) {
	app := fiber.New()
	// Create mock or nil handlers as needed
	artistsHandler := &api.ArtistsHandler{}
	schedulesHandler := &api.SchedulesHandler{}
	setupRoutes(app, nil, &api.AuthHandler{}, &api.DashboardHandler{}, &api.WatchlistHandler{}, &api.SpotifyAuthHandler{}, &api.WebSocketManager{}, &services.ArtistTrackingService{}, &services.ScannerService{}, artistsHandler, schedulesHandler)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/health", nil))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}
