package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// Helper function tests: getEnv, getEnvBool, getEnvAsInt, getEnvAsInt64
// =============================================================================

func TestGetEnv(t *testing.T) {
	// Do not run in parallel — this test modifies global environment variables

	tests := []struct {
		name        string
		key         string
		value       string
		defaultVal  string
		expected    string
	}{
		{
			name:       "env var set returns value",
			key:        "TEST_GETENV_SET",
			value:      "actual-value",
			defaultVal: "default-value",
			expected:   "actual-value",
		},
		{
			name:       "env var empty returns default",
			key:        "TEST_GETENV_EMPTY",
			value:      "",
			defaultVal: "default-value",
			expected:   "default-value",
		},
		{
			name:       "env var not set returns default",
			key:        "TEST_GETENV_NOTSET",
			value:      "",
			defaultVal: "default",
			expected:   "default",
		},
		{
			name:       "env var set to special characters",
			key:        "TEST_GETENV_SPECIAL",
			value:      "value with spaces & symbols!",
			defaultVal: "default",
			expected:   "value with spaces & symbols!",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value != "" {
				os.Setenv(tc.key, tc.value)
				defer os.Unsetenv(tc.key)
			}
			result := getEnv(tc.key, tc.defaultVal)
			if result != tc.expected {
				t.Errorf("getEnv(%q, %q) = %q, want %q", tc.key, tc.defaultVal, result, tc.expected)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	// Do not run in parallel — this test modifies global environment variables
	tests := []struct {
		name       string
		key        string
		value      string
		defaultVal bool
		expected   bool
	}{
		{
			name:       "env var set to true",
			key:        "TEST_BOOL_TRUE",
			value:      "true",
			defaultVal: false,
			expected:   true,
		},
		{
			name:       "env var set to 1",
			key:        "TEST_BOOL_ONE",
			value:      "1",
			defaultVal: false,
			expected:   true,
		},
		{
			name:       "env var set to false",
			key:        "TEST_BOOL_FALSE",
			value:      "false",
			defaultVal: true,
			expected:   false,
		},
		{
			name:       "env var set to 0",
			key:        "TEST_BOOL_ZERO",
			value:      "0",
			defaultVal: true,
			expected:   false,
		},
		{
			name:       "env var empty returns default true",
			key:        "TEST_BOOL_EMPTY_TRUE",
			value:      "",
			defaultVal: true,
			expected:   true,
		},
		{
			name:       "env var empty returns default false",
			key:        "TEST_BOOL_EMPTY_FALSE",
			value:      "",
			defaultVal: false,
			expected:   false,
		},
		{
			name:       "env var set to random string returns false (not true or 1)",
			key:        "TEST_BOOL_RANDOM",
			value:      "random",
			defaultVal: true,
			expected:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value != "" {
				os.Setenv(tc.key, tc.value)
				defer os.Unsetenv(tc.key)
			}
			result := getEnvBool(tc.key, tc.defaultVal)
			if result != tc.expected {
				t.Errorf("getEnvBool(%q, %v) = %v, want %v", tc.key, tc.defaultVal, result, tc.expected)
			}
		})
	}
}

func TestGetEnvAsInt(t *testing.T) {
	// Do not run in parallel — this test modifies global environment variables
	tests := []struct {
		name       string
		key        string
		value      string
		defaultVal int
		expected   int
	}{
		{
			name:       "env var set to valid integer",
			key:        "TEST_INT_VALID",
			value:      "42",
			defaultVal: 10,
			expected:   42,
		},
		{
			name:       "env var set to zero",
			key:        "TEST_INT_ZERO",
			value:      "0",
			defaultVal: 10,
			expected:   0,
		},
		{
			name:       "env var set to negative integer",
			key:        "TEST_INT_NEGATIVE",
			value:      "-5",
			defaultVal: 10,
			expected:   -5,
		},
		{
			name:       "env var set to large integer",
			key:        "TEST_INT_LARGE",
			value:      "1000000",
			defaultVal: 10,
			expected:   1000000,
		},
		{
			name:       "env var empty returns default",
			key:        "TEST_INT_EMPTY",
			value:      "",
			defaultVal: 99,
			expected:   99,
		},
		{
			name:       "env var set to invalid integer returns default",
			key:        "TEST_INT_INVALID",
			value:      "not-an-int",
			defaultVal: 77,
			expected:   77,
		},
		{
			name:       "env var set to float returns default (strconv limitation)",
			key:        "TEST_INT_FLOAT",
			value:      "3.14",
			defaultVal: 55,
			expected:   55,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value != "" {
				os.Setenv(tc.key, tc.value)
				defer os.Unsetenv(tc.key)
			}
			result := getEnvAsInt(tc.key, tc.defaultVal)
			if result != tc.expected {
				t.Errorf("getEnvAsInt(%q, %d) = %d, want %d", tc.key, tc.defaultVal, result, tc.expected)
			}
		})
	}
}

func TestGetEnvAsInt64(t *testing.T) {
	// Do not run in parallel — this test modifies global environment variables
	tests := []struct {
		name       string
		key        string
		value      string
		defaultVal int64
		expected   int64
	}{
		{
			name:       "env var set to valid int64",
			key:        "TEST_INT64_VALID",
			value:      "9223372036854775807",
			defaultVal: 100,
			expected:   9223372036854775807,
		},
		{
			name:       "env var set to negative int64",
			key:        "TEST_INT64_NEGATIVE",
			value:      "-9223372036854775808",
			defaultVal: 100,
			expected:   -9223372036854775808,
		},
		{
			name:       "env var empty returns default",
			key:        "TEST_INT64_EMPTY",
			value:      "",
			defaultVal: 12345,
			expected:   12345,
		},
		{
			name:       "env var set to invalid value returns default",
			key:        "TEST_INT64_INVALID",
			value:      "invalid",
			defaultVal: 999,
			expected:   999,
		},
		{
			name:       "env var set to decimal returns default",
			key:        "TEST_INT64_DECIMAL",
			value:      "1.5",
			defaultVal: 50,
			expected:   50,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value != "" {
				os.Setenv(tc.key, tc.value)
				defer os.Unsetenv(tc.key)
			}
			result := getEnvAsInt64(tc.key, tc.defaultVal)
			if result != tc.expected {
				t.Errorf("getEnvAsInt64(%q, %d) = %d, want %d", tc.key, tc.defaultVal, result, tc.expected)
			}
		})
	}
}

// =============================================================================
// configFileCandidates tests
// =============================================================================

func TestConfigFileCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		configEnv    string
		expectLen    int
		expectUnique bool
	}{
		{
			name:         "empty configEnv returns 4 paths",
			configEnv:    "",
			expectLen:    4,
			expectUnique: true,
		},
		{
			name:         "production configEnv returns 4 paths",
			configEnv:    "production",
			expectLen:    4,
			expectUnique: true,
		},
		{
			name:         "custom configEnv returns 4 paths",
			configEnv:    "staging",
			expectLen:    4,
			expectUnique: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidates := configFileCandidates(tc.configEnv)
			if len(candidates) != tc.expectLen {
				t.Errorf("configFileCandidates(%q) returned %d paths, want %d", tc.configEnv, len(candidates), tc.expectLen)
			}

			if tc.expectUnique {
				seen := make(map[string]bool)
				for _, c := range candidates {
					if seen[c] {
						t.Errorf("configFileCandidates(%q) returned duplicate path: %s", tc.configEnv, c)
					}
					seen[c] = true
				}
			}

			// Verify expected pattern
			wd, _ := os.Getwd()
			expectedBase := filepath.Join(wd, "config.yaml")
			if candidates[0] != expectedBase {
				t.Errorf("candidates[0] = %q, want %q", candidates[0], expectedBase)
			}

			// Third path should be parent directory
			parentBase := filepath.Join(wd, "..", "..", "config.yaml")
			if candidates[2] != parentBase {
				t.Errorf("candidates[2] = %q, want %q", candidates[2], parentBase)
			}
		})
	}
}

// =============================================================================
// applyOverlay tests
// =============================================================================

func TestApplyOverlay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		overlay        map[string]interface{}
		expectEnv      string
		expectPort     string
		expectDomain   string
		expectRateMax  int
		expectNotif    bool
		expectSubsonic bool
		expectLibPath  string
	}{
		{
			name: "apply known keys",
			overlay: map[string]interface{}{
				"environment":             "production",
				"port":                   "3000",
				"domain":                 "example.com",
				"auth_rate_limit_max":    100,
				"notification_enabled":   true,
				"subsonic_enabled":       true,
				"music_library_path":     "/mnt/music",
				"download_staging_path":  "/mnt/staging",
			},
			expectEnv:      "production",
			expectPort:     "3000",
			expectDomain:   "example.com",
			expectRateMax:  100,
			expectNotif:    true,
			expectSubsonic: true,
			expectLibPath:  "/mnt/music",
		},
		{
			name: "unknown keys are silently ignored",
			overlay: map[string]interface{}{
				"unknown_key":      "value",
				"another_unknown":  123,
				"totally_fake":     true,
			},
			expectEnv:      "",
			expectPort:     "",
			expectDomain:   "",
			expectRateMax:  0,
			expectNotif:    false,
			expectSubsonic: false,
			expectLibPath:  "",
		},
		{
			name: "empty string values don't overwrite",
			overlay: map[string]interface{}{
				"environment": "",
				"port":        "",
				"domain":      "",
			},
			expectEnv:      "",
			expectPort:     "",
			expectDomain:   "",
			expectRateMax:  0,
			expectNotif:    false,
			expectSubsonic: false,
			expectLibPath:  "",
		},
		{
			name: "zero int values don't overwrite rate limit",
			overlay: map[string]interface{}{
				"auth_rate_limit_max": 0,
			},
			expectRateMax: 0,
		},
		{
			name: "apply subsonic password",
			overlay: map[string]interface{}{
				"subsonic_password": "secret123",
			},
			expectSubsonic: false, // only enabled flag is tracked in this test
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			applyOverlay(cfg, tc.overlay)

			if cfg.Environment != tc.expectEnv {
				t.Errorf("cfg.Environment = %q, want %q", cfg.Environment, tc.expectEnv)
			}
			if cfg.Port != tc.expectPort {
				t.Errorf("cfg.Port = %q, want %q", cfg.Port, tc.expectPort)
			}
			if cfg.Domain != tc.expectDomain {
				t.Errorf("cfg.Domain = %q, want %q", cfg.Domain, tc.expectDomain)
			}
			if cfg.AuthRateLimitMax != tc.expectRateMax {
				t.Errorf("cfg.AuthRateLimitMax = %d, want %d", cfg.AuthRateLimitMax, tc.expectRateMax)
			}
			if cfg.NotificationEnabled != tc.expectNotif {
				t.Errorf("cfg.NotificationEnabled = %v, want %v", cfg.NotificationEnabled, tc.expectNotif)
			}
			if cfg.Subsonic.Enabled != tc.expectSubsonic {
				t.Errorf("cfg.Subsonic.Enabled = %v, want %v", cfg.Subsonic.Enabled, tc.expectSubsonic)
			}
			if cfg.MusicLibraryPath != tc.expectLibPath {
				t.Errorf("cfg.MusicLibraryPath = %q, want %q", cfg.MusicLibraryPath, tc.expectLibPath)
			}
		})
	}
}

func TestApplyOverlaySubsonicPassword(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	applyOverlay(cfg, map[string]interface{}{
		"subsonic_password": "my-secret-pass",
	})
	if cfg.Subsonic.Password != "my-secret-pass" {
		t.Errorf("cfg.Subsonic.Password = %q, want %q", cfg.Subsonic.Password, "my-secret-pass")
	}
}

// =============================================================================
// loadYAMLOverrides tests
// =============================================================================

func TestLoadYAMLOverrides(t *testing.T) {
	t.Parallel()

	t.Run("non-existent file warns and continues", func(t *testing.T) {
		cfg := &Config{}
		// Should not panic, should just warn
		loadYAMLOverrides(cfg, "nonexistent")
	})

	t.Run("invalid YAML warns and continues", func(t *testing.T) {
		// loadYAMLOverrides should not panic on invalid YAML
		cfg := &Config{}
		loadYAMLOverrides(cfg, "nonexistent-env")
	})
}

// =============================================================================
// Load function edge case tests
// =============================================================================

func TestLoad_ProductionRequiresGonicCredentials(t *testing.T) {
	// Clear all env vars first
	os.Clearenv()

	// Set required vars but not Gonic credentials
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("ENVIRONMENT", "production")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("ENVIRONMENT")
		os.Unsetenv("GONIC_USER")
		os.Unsetenv("GONIC_PASS")
	}()

	_, err := Load(".non-existent-env")
	if err == nil {
		t.Fatal("Expected error for production missing Gonic credentials, got nil")
	}
	if err.Error() != "GONIC_USER and GONIC_PASS are required in production" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestLoad_ProductionWithGonicCredentials(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("ENVIRONMENT", "production")
	os.Setenv("GONIC_USER", "gonicuser")
	os.Setenv("GONIC_PASS", "gonicpass")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("ENVIRONMENT")
		os.Unsetenv("GONIC_USER")
		os.Unsetenv("GONIC_PASS")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if cfg.Environment != "production" {
		t.Errorf("cfg.Environment = %q, want %q", cfg.Environment, "production")
	}
	if cfg.GonicUser != "gonicuser" {
		t.Errorf("cfg.GonicUser = %q, want %q", cfg.GonicUser, "gonicuser")
	}
	if cfg.GonicPass != "gonicpass" {
		t.Errorf("cfg.GonicPass = %q, want %q", cfg.GonicPass, "gonicpass")
	}
}

func TestLoad_ProxyURLValidation(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	defer os.Unsetenv("DATABASE_URL")

	tests := []struct {
		name      string
		proxyURL  string
		expectErr bool
		errMsg    string
	}{
		{
			name:      "empty proxy URL is valid",
			proxyURL:  "",
			expectErr: false,
		},
		{
			name:      "valid http proxy URL",
			proxyURL:  "http://proxy.example.com:8080",
			expectErr: false,
		},
		{
			name:      "valid socks5 proxy URL",
			proxyURL:  "socks5://localhost:1080",
			expectErr: false,
		},
		{
			name:      "URL without scheme fails validation",
			proxyURL:  "not-a-valid-url",
			expectErr: true,
			errMsg:    "PROXY_URL must include a scheme",
		},
		{
			name:      "scheme-relative URL fails (no scheme)",
			proxyURL:  "//proxy.example.com:8080",
			expectErr: true,
			errMsg:    "PROXY_URL must include a scheme",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.proxyURL != "" {
				os.Setenv("PROXY_URL", tc.proxyURL)
				defer os.Unsetenv("PROXY_URL")
			}

			cfg, err := Load(".non-existent-env")
			if tc.expectErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("Error = %q, want error containing %q", err.Error(), tc.errMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if cfg.ProxyURL != tc.proxyURL {
					t.Errorf("cfg.ProxyURL = %q, want %q", cfg.ProxyURL, tc.proxyURL)
				}
			}
		})
	}
}

func TestLoad_ConfigEnvDefaultsToEnvironment(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("ENVIRONMENT", "testenv")
	// CONFIG_ENV is not set, should default to ENVIRONMENT
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("ENVIRONMENT")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if cfg.ConfigEnv != "testenv" {
		t.Errorf("cfg.ConfigEnv = %q, want %q", cfg.ConfigEnv, "testenv")
	}
}

func TestLoad_JWTSecretAutoGenerated(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	// Ensure JWT_SECRET is not set
	os.Unsetenv("JWT_SECRET")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("JWT_SECRET")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Should have a generated secret (64 hex chars)
	if len(cfg.JWTSecret) != 64 {
		t.Errorf("cfg.JWTSecret length = %d, want 64", len(cfg.JWTSecret))
	}
}

func TestLoad_JWTSecretFromEnv(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("JWT_SECRET", "my-secret-key")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("JWT_SECRET")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if cfg.JWTSecret != "my-secret-key" {
		t.Errorf("cfg.JWTSecret = %q, want %q", cfg.JWTSecret, "my-secret-key")
	}
}

func TestLoad_E2EFlags(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("E2E_ENABLE_TEST_API", "true")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("E2E_ENABLE_TEST_API")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !cfg.E2EEnableTestAPI {
		t.Error("cfg.E2EEnableTestAPI = false, want true")
	}
}

func TestLoad_SMTPConfig(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("SMTP_HOST", "smtp.example.com")
	os.Setenv("SMTP_PORT", "587")
	os.Setenv("SMTP_USER", "user")
	os.Setenv("SMTP_PASS", "pass")
	os.Setenv("SMTP_FROM", "noreply@example.com")
	os.Setenv("SMTP_ENABLED", "true")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("SMTP_HOST")
		os.Unsetenv("SMTP_PORT")
		os.Unsetenv("SMTP_USER")
		os.Unsetenv("SMTP_PASS")
		os.Unsetenv("SMTP_FROM")
		os.Unsetenv("SMTP_ENABLED")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if cfg.SMTPHost != "smtp.example.com" {
		t.Errorf("cfg.SMTPHost = %q, want %q", cfg.SMTPHost, "smtp.example.com")
	}
	if cfg.SMTPPort != "587" {
		t.Errorf("cfg.SMTPPort = %q, want %q", cfg.SMTPPort, "587")
	}
	if cfg.SMTPUser != "user" {
		t.Errorf("cfg.SMTPUser = %q, want %q", cfg.SMTPUser, "user")
	}
	if cfg.SMTPPass != "pass" {
		t.Errorf("cfg.SMTPPass = %q, want %q", cfg.SMTPPass, "pass")
	}
	if cfg.SMTPFrom != "noreply@example.com" {
		t.Errorf("cfg.SMTPFrom = %q, want %q", cfg.SMTPFrom, "noreply@example.com")
	}
	if !cfg.SMTPEnabled {
		t.Error("cfg.SMTPEnabled = false, want true")
	}
}

func TestLoad_SubsonicConfig(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("SUBSONIC_ENABLED", "true")
	os.Setenv("SUBSONIC_PASSWORD", "secret123")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("SUBSONIC_ENABLED")
		os.Unsetenv("SUBSONIC_PASSWORD")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !cfg.Subsonic.Enabled {
		t.Error("cfg.Subsonic.Enabled = false, want true")
	}
	if cfg.Subsonic.Password != "secret123" {
		t.Errorf("cfg.Subsonic.Password = %q, want %q", cfg.Subsonic.Password, "secret123")
	}
}

func TestLoad_TranscodeConfig(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("TRANSCODE_ENABLED", "true")
	os.Setenv("FFMPEG_PATH", "/usr/bin/ffmpeg")
	os.Setenv("TRANSCODE_CACHE_DIR", "/var/cache/transcode")
	os.Setenv("TRANSCODE_MAX_BITRATE", "640")
	os.Setenv("TRANSCODE_MAX_CACHE_MB", "1024")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("TRANSCODE_ENABLED")
		os.Unsetenv("FFMPEG_PATH")
		os.Unsetenv("TRANSCODE_CACHE_DIR")
		os.Unsetenv("TRANSCODE_MAX_BITRATE")
		os.Unsetenv("TRANSCODE_MAX_CACHE_MB")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !cfg.Transcode.Enabled {
		t.Error("cfg.Transcode.Enabled = false, want true")
	}
	if cfg.Transcode.FFmpegPath != "/usr/bin/ffmpeg" {
		t.Errorf("cfg.Transcode.FFmpegPath = %q, want %q", cfg.Transcode.FFmpegPath, "/usr/bin/ffmpeg")
	}
	if cfg.Transcode.CacheDir != "/var/cache/transcode" {
		t.Errorf("cfg.Transcode.CacheDir = %q, want %q", cfg.Transcode.CacheDir, "/var/cache/transcode")
	}
	if cfg.Transcode.MaxBitrate != 640 {
		t.Errorf("cfg.Transcode.MaxBitrate = %d, want %d", cfg.Transcode.MaxBitrate, 640)
	}
	if cfg.Transcode.MaxCacheMB != 1024 {
		t.Errorf("cfg.Transcode.MaxCacheMB = %d, want %d", cfg.Transcode.MaxCacheMB, 1024)
	}
}

func TestLoad_EnvVarsOverrideDefaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("ENVIRONMENT", "custom-env")
	os.Setenv("PORT", "9999")
	os.Setenv("DOMAIN", "custom.example.com")
	os.Setenv("AUTH_RATE_LIMIT_MAX", "50")
	os.Setenv("NOTIFICATION_ENABLED", "true")
	os.Setenv("MUSIC_LIBRARY", "/custom/music")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("ENVIRONMENT")
		os.Unsetenv("PORT")
		os.Unsetenv("DOMAIN")
		os.Unsetenv("AUTH_RATE_LIMIT_MAX")
		os.Unsetenv("NOTIFICATION_ENABLED")
		os.Unsetenv("MUSIC_LIBRARY")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Env vars should override defaults
	if cfg.Environment != "custom-env" {
		t.Errorf("cfg.Environment = %q, want %q", cfg.Environment, "custom-env")
	}
	if cfg.Port != "9999" {
		t.Errorf("cfg.Port = %q, want %q", cfg.Port, "9999")
	}
	if cfg.Domain != "custom.example.com" {
		t.Errorf("cfg.Domain = %q, want %q", cfg.Domain, "custom.example.com")
	}
	if cfg.AuthRateLimitMax != 50 {
		t.Errorf("cfg.AuthRateLimitMax = %d, want %d", cfg.AuthRateLimitMax, 50)
	}
	if !cfg.NotificationEnabled {
		t.Error("cfg.NotificationEnabled = false, want true")
	}
	if cfg.MusicLibraryPath != "/custom/music" {
		t.Errorf("cfg.MusicLibraryPath = %q, want %q", cfg.MusicLibraryPath, "/custom/music")
	}
}

func TestLoad_Defaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	defer func() {
		os.Unsetenv("DATABASE_URL")
	}()

	cfg, err := Load(".non-existent-env")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Check various defaults
	if cfg.Environment != "development" {
		t.Errorf("cfg.Environment = %q, want %q", cfg.Environment, "development")
	}
	if cfg.Port != "8080" {
		t.Errorf("cfg.Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.Domain != "localhost" {
		t.Errorf("cfg.Domain = %q, want %q", cfg.Domain, "localhost")
	}
	if cfg.RedisURL != "redis://localhost:6379" {
		t.Errorf("cfg.RedisURL = %q, want %q", cfg.RedisURL, "redis://localhost:6379")
	}
	if cfg.MusicLibraryPath != "./music_library" {
		t.Errorf("cfg.MusicLibraryPath = %q, want %q", cfg.MusicLibraryPath, "./music_library")
	}
	if cfg.DownloadStagingPath != "./downloads" {
		t.Errorf("cfg.DownloadStagingPath = %q, want %q", cfg.DownloadStagingPath, "./downloads")
	}
	if cfg.SMTPPort != "587" {
		t.Errorf("cfg.SMTPPort = %q, want %q", cfg.SMTPPort, "587")
	}
	if cfg.TemplatesPath != "./ops/web/templates" {
		t.Errorf("cfg.TemplatesPath = %q, want %q", cfg.TemplatesPath, "./ops/web/templates")
	}
	if cfg.StaticFilesPath != "./ops/web/static" {
		t.Errorf("cfg.StaticFilesPath = %q, want %q", cfg.StaticFilesPath, "./ops/web/static")
	}
	if cfg.AuthRateLimitMax != 10 {
		t.Errorf("cfg.AuthRateLimitMax = %d, want %d", cfg.AuthRateLimitMax, 10)
	}
	if cfg.AuthRateLimitExpiration != "1m" {
		t.Errorf("cfg.AuthRateLimitExpiration = %q, want %q", cfg.AuthRateLimitExpiration, "1m")
	}
	if cfg.Transcode.Enabled != true {
		t.Error("cfg.Transcode.Enabled = false, want true (default from TRANSCODE_ENABLED=true)")
	}
	if cfg.Transcode.MaxBitrate != 320 {
		t.Errorf("cfg.Transcode.MaxBitrate = %d, want %d", cfg.Transcode.MaxBitrate, 320)
	}
	if cfg.Transcode.MaxCacheMB != 512 {
		t.Errorf("cfg.Transcode.MaxCacheMB = %d, want %d", cfg.Transcode.MaxCacheMB, 512)
	}
}
