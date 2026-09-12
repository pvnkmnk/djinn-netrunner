package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Production must fail closed on configuration that degrades silently at
// runtime: an auto-generated session secret (sessions die on every restart)
// and an enabled Subsonic endpoint with no shared password (its token path
// becomes forgeable). Development keeps warning-only behaviour.

func TestLoad_ProductionRequiresJWTSecret(t *testing.T) {
	cleanup := saveRestoreEnv()
	defer cleanup()

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("ENVIRONMENT", "production")
	os.Unsetenv("JWT_SECRET")

	_, err := Load(".non-existent-env")
	if err == nil {
		t.Fatal("Expected error for production without JWT_SECRET, got nil")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET is required in production") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestLoad_DevelopmentWithoutJWTSecretStillLoads(t *testing.T) {
	cleanup := saveRestoreEnv()
	defer cleanup()

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Unsetenv("ENVIRONMENT")
	os.Unsetenv("JWT_SECRET")

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if cfg.JWTSecret == "" {
		t.Error("cfg.JWTSecret is empty: development should still generate an ephemeral secret")
	}
}

func TestLoad_ProductionRequiresSubsonicPasswordWhenEnabled(t *testing.T) {
	cleanup := saveRestoreEnv()
	defer cleanup()

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("ENVIRONMENT", "production")
	os.Setenv("JWT_SECRET", "production-secret")
	os.Setenv("SUBSONIC_ENABLED", "true")
	os.Unsetenv("SUBSONIC_PASSWORD")

	_, err := Load(".non-existent-env")
	if err == nil {
		t.Fatal("Expected error for production with SUBSONIC_ENABLED=true and no SUBSONIC_PASSWORD, got nil")
	}
	if !strings.Contains(err.Error(), "SUBSONIC_PASSWORD is required in production") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestLoad_ProductionSubsonicEnabledWithPasswordSucceeds(t *testing.T) {
	cleanup := saveRestoreEnv()
	defer cleanup()

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("ENVIRONMENT", "production")
	os.Setenv("JWT_SECRET", "production-secret")
	os.Setenv("SUBSONIC_ENABLED", "true")
	os.Setenv("SUBSONIC_PASSWORD", "streaming-secret")

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !cfg.Subsonic.Enabled {
		t.Error("cfg.Subsonic.Enabled = false, want true")
	}
	if cfg.Subsonic.Password != "streaming-secret" {
		t.Errorf("cfg.Subsonic.Password = %q, want %q", cfg.Subsonic.Password, "streaming-secret")
	}
}

func TestLoad_DevelopmentSubsonicWithoutPasswordLoadsWithTokenAuthDisabled(t *testing.T) {
	cleanup := saveRestoreEnv()
	defer cleanup()

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Unsetenv("ENVIRONMENT")
	os.Setenv("SUBSONIC_ENABLED", "true")
	os.Unsetenv("SUBSONIC_PASSWORD")

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !cfg.Subsonic.Enabled {
		t.Error("cfg.Subsonic.Enabled = false, want true (development only warns)")
	}
	if cfg.Subsonic.Password != "" {
		t.Errorf("cfg.Subsonic.Password = %q, want empty", cfg.Subsonic.Password)
	}
}

// A YAML overlay can set `environment: production`, and overlays are applied
// after the env vars are read. Deciding the JWT_SECRET fail-fast from the raw
// ENVIRONMENT variable therefore let a production deployment boot with an
// ephemeral secret (sessions invalidated on every restart) instead of refusing.
func TestLoad_ProductionFromYAMLStillRequiresJWTSecret(t *testing.T) {
	cleanup := saveRestoreEnv()
	defer cleanup()

	// Load only reads config files from the working directory, so serve the
	// overlay from an isolated one. godotenv's relative .env lookup then misses
	// too, which is what keeps this test independent of the developer's .env.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte(`environment: production
`), 0o644); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Unsetenv("ENVIRONMENT")
	os.Unsetenv("CONFIG_ENV")
	os.Unsetenv("JWT_SECRET")

	_, err = Load()
	if err == nil {
		t.Fatal("expected production - from YAML - to demand JWT_SECRET, got nil error")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET is required in production") {
		t.Fatalf("expected the JWT_SECRET production error, got: %v", err)
	}
}

// The mirror case: a YAML overlay that says production must not fail when a real
// secret is configured, so the check keys off the missing secret, not the env name.
func TestLoad_ProductionFromYAMLPassesWithJWTSecret(t *testing.T) {
	cleanup := saveRestoreEnv()
	defer cleanup()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte(`environment: production
`), 0o644); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("JWT_SECRET", "a-real-configured-secret")
	os.Unsetenv("ENVIRONMENT")
	os.Unsetenv("CONFIG_ENV")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error with JWT_SECRET configured: %v", err)
	}
	if cfg.Environment != "production" {
		t.Fatalf("expected the YAML overlay to win, got environment %q", cfg.Environment)
	}
}
