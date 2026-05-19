package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"gorm.io/gorm"
)

// HealthHandler provides dependency-checked health responses.
type HealthHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

// HealthCheck represents the status of a single dependency.
type HealthCheck struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HealthResponse is the top-level health payload.
type HealthResponse struct {
	Status string                 `json:"status"`
	Checks map[string]HealthCheck `json:"checks"`
}

// NewHealthHandler creates a handler that can probe internal and external
// dependencies and report overall health.
func NewHealthHandler(db *gorm.DB, cfg *config.Config) *HealthHandler {
	return &HealthHandler{db: db, cfg: cfg}
}

// GetHealth performs a live check against all configured dependencies and
// returns a summary. The overall status is "ok" when all critical checks
// (database) pass; optional checks (slskd, gonic, disk) are reported but
// do not degrade the top-level status.
func (h *HealthHandler) GetHealth(c *fiber.Ctx) error {
	checks := make(map[string]HealthCheck)

	// Database (critical)
	checks["database"] = h.checkDatabase()

	// slskd (optional, reported only when configured)
	if h.cfg.SlskdAPIKey != "" {
		checks["slskd"] = h.checkHTTP(h.cfg.SlskdURL+"/health", 5*time.Second)
	}

	// Gonic (optional)
	if h.cfg.GonicURL != "" {
		checks["gonic"] = h.checkHTTP(h.cfg.GonicURL+"/ping", 5*time.Second)
	}

	// Disk (optional, reported only when path exists)
	if info, err := os.Stat(h.cfg.MusicLibraryPath); err == nil && info.IsDir() {
		checks["disk"] = h.checkDisk(h.cfg.MusicLibraryPath)
	}

	status := "ok"
	code := fiber.StatusOK
	if dbCheck := checks["database"]; dbCheck.Status != "ok" {
		status = "degraded"
		code = fiber.StatusServiceUnavailable
	}

	return c.Status(code).JSON(HealthResponse{
		Status: status,
		Checks: checks,
	})
}

func (h *HealthHandler) checkDatabase() HealthCheck {
	if h.db == nil {
		return HealthCheck{Status: "error", Error: "database not configured"}
	}
	raw, err := h.db.DB()
	if err != nil {
		return HealthCheck{Status: "error", Error: err.Error()}
	}
	if err := raw.Ping(); err != nil {
		return HealthCheck{Status: "error", Error: err.Error()}
	}
	return HealthCheck{Status: "ok"}
}

func (h *HealthHandler) checkHTTP(url string, timeout time.Duration) HealthCheck {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return HealthCheck{Status: "error", Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return HealthCheck{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	return HealthCheck{Status: "ok"}
}

func (h *HealthHandler) checkDisk(path string) HealthCheck {
	dir, err := os.Open(path)
	if err != nil {
		return HealthCheck{Status: "error", Error: err.Error()}
	}
	defer dir.Close()

	if _, err := dir.Readdirnames(1); err != nil && err != io.EOF {
		return HealthCheck{Status: "error", Error: err.Error()}
	}
	return HealthCheck{Status: "ok", Message: "directory accessible"}
}
