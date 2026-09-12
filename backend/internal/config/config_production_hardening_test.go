package config

import (
	"os"
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
