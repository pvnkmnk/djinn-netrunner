//go:build integration

// Smoke tests for NetRunner integration verification.
//
// These tests validate the core service layer directly (no HTTP):
//   - Config loading
//   - Database migration
//   - Basic CRUD operations
//
// Run with: go test ./backend/internal/integration/... -v -tags=integration
// Requires dockerized services (see docker-compose.integration.yml).
package integration

import (
	"os"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func skipIfShort(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration smoke test in short mode")
	}
}

func TestSmoke_ConfigLoading(t *testing.T) {
	skipIfShort(t)

	// Set minimum required env vars
	os.Setenv("DATABASE_URL", GetEnvOrDefault("INTEGRATION_DATABASE_URL", defaultDBURL))
	os.Setenv("SLSKD_API_KEY", GetEnvOrDefault("INTEGRATION_SLSKD_API_KEY", defaultSlskdAPIKey))
	os.Setenv("SLSKD_URL", GetEnvOrDefault("INTEGRATION_SLSKD_URL", defaultSlskdURL))
	os.Setenv("JWT_SECRET", "smoke-test-secret")
	os.Setenv("MUSIC_LIBRARY", "/tmp/smoke-music")
	os.Setenv("TEMPLATES_PATH", "../../ops/web/templates")
	os.Setenv("STATIC_FILES_PATH", "../../ops/web/static")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Config loading failed: %v", err)
	}

	if cfg.DatabaseURL == "" {
		t.Fatal("DatabaseURL should not be empty")
	}
	t.Logf("Config loaded: DatabaseURL=%s, SlskdURL=%s", cfg.DatabaseURL, cfg.SlskdURL)
}

func TestSmoke_DatabaseMigration(t *testing.T) {
	skipIfShort(t)
	SkipIfNoDocker(t)

	os.Setenv("DATABASE_URL", GetEnvOrDefault("INTEGRATION_DATABASE_URL", defaultDBURL))
	os.Setenv("JWT_SECRET", "smoke-test-secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Config loading failed: %v", err)
	}

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("Database connection failed: %v", err)
	}
	defer func() {
		sql, _ := db.DB()
		if sql != nil {
			sql.Close()
		}
	}()

	if err := database.Migrate(db); err != nil {
		t.Fatalf("Database migration failed: %v", err)
	}

	// Verify tables exist
	var tables []string
	db.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = 'public'").Scan(&tables)
	if len(tables) == 0 {
		t.Fatal("No tables found after migration")
	}
	t.Logf("Migration complete: %d tables created", len(tables))
}

func TestSmoke_CRUD(t *testing.T) {
	skipIfShort(t)
	SkipIfNoDocker(t)

	os.Setenv("DATABASE_URL", GetEnvOrDefault("INTEGRATION_DATABASE_URL", defaultDBURL))
	os.Setenv("JWT_SECRET", "smoke-test-secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Config loading failed: %v", err)
	}

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("Database connection failed: %v", err)
	}
	defer func() {
		sql, _ := db.DB()
		if sql != nil {
			sql.Close()
		}
	}()

	if err := database.Migrate(db); err != nil {
		t.Fatalf("Database migration failed: %v", err)
	}

	// Create a user
	user := database.User{
		Email:    "smoke-" + time.Now().Format("150405") + "@test.local",
		Password: "$2a$10$hashedpassword",
		Role:     "user",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("Create user failed: %v", err)
	}
	t.Logf("Created user: %s (ID: %d)", user.Email, user.ID)

	// Create a quality profile
	profile := database.QualityProfile{
		Name:           "smoke-test-profile",
		PreferLossless: true,
		MinBitrate:     320,
		OwnerUserID:    user.ID,
	}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}
	t.Logf("Created profile: %s (ID: %d)", profile.Name, profile.ID)

	// Create a library
	lib := database.Library{
		Name:        "smoke-test-library",
		Path:        "/tmp/smoke-music",
		OwnerUserID: user.ID,
	}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatalf("Create library failed: %v", err)
	}
	t.Logf("Created library: %s (ID: %d)", lib.Name, lib.ID)

	// Read back
	var fetched database.Library
	if err := db.First(&fetched, lib.ID).Error; err != nil {
		t.Fatalf("Read library failed: %v", err)
	}
	if fetched.Name != lib.Name {
		t.Fatalf("Library name mismatch: got %q, want %q", fetched.Name, lib.Name)
	}

	// Update
	if err := db.Model(&fetched).Update("name", "smoke-test-library-updated").Error; err != nil {
		t.Fatalf("Update library failed: %v", err)
	}

	// Delete
	if err := db.Delete(&fetched).Error; err != nil {
		t.Fatalf("Delete library failed: %v", err)
	}

	// Verify deletion
	var count int64
	db.Model(&database.Library{}).Where("id = ?", lib.ID).Count(&count)
	if count != 0 {
		t.Fatal("Library should have been deleted")
	}

	// Cleanup profile and user
	db.Delete(&profile)
	db.Delete(&user)

	t.Log("CRUD smoke test completed successfully")
}
