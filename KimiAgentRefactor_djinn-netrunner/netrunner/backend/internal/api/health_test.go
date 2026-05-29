package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

func TestHealthHandler_GetHealthReturnsServiceUnavailableForDatabaseFailure(t *testing.T) {
	app := fiber.New()
	handler := NewHealthHandler(nil, &config.Config{MusicLibraryPath: t.TempDir()})
	app.Get("/api/health", handler.GetHealth)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, fiber.StatusServiceUnavailable)
	}

	var body HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", body.Status)
	}
	if body.Checks["database"].Status != "error" {
		t.Fatalf("database status = %q, want error", body.Checks["database"].Status)
	}
}

func TestHealthHandler_GetHealthReturnsOKWithHealthyDatabase(t *testing.T) {
	db := setupAPITestDB(t)
	app := fiber.New()
	handler := NewHealthHandler(db, &config.Config{MusicLibraryPath: t.TempDir()})
	app.Get("/api/health", handler.GetHealth)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	var body HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status = %q, want ok", body.Status)
	}
	if body.Checks["disk"].Message == "" {
		t.Fatal("disk check did not include a success message")
	}
	if body.Checks["disk"].Error != "" {
		t.Fatalf("disk error = %q, want empty", body.Checks["disk"].Error)
	}
}

func TestHealthHandler_CheckHTTPRequiresOK(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantStatus string
	}{
		{name: "ok", statusCode: http.StatusOK, wantStatus: "ok"},
		{name: "not found", statusCode: http.StatusNotFound, wantStatus: "error"},
		{name: "server error", statusCode: http.StatusInternalServerError, wantStatus: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			handler := NewHealthHandler(nil, &config.Config{})
			got := handler.checkHTTP(server.URL, time.Second)
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
		})
	}
}
