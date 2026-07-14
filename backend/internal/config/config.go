package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the application
type Config struct {
	// Server
	Environment string
	ConfigEnv   string
	Port        string
	Domain      string

	// Security
	JWTSecret string

	// Database
	DatabaseURL string

	// Redis
	RedisURL string

	// Spotify
	SpotifyClientID     string
	SpotifyClientSecret string

	// MusicBrainz
	MusicBrainzUserAgent string
	MusicBrainzAPIKey    string

	// AcoustID
	AcoustIDApiKey string

	// SLSKD
	SlskdURL    string
	SlskdAPIKey string

	// Gonic
	GonicURL  string
	GonicUser string
	GonicPass string

	// Lidarr
	LidarrURL    string
	LidarrAPIKey string

	// Navidrome
	NavidromeURL  string
	NavidromeUser string
	NavidromePass string

	// SMTP
	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPass     string
	SMTPFrom     string
	SMTPEnabled  bool

	// Library
	MusicLibraryPath     string
	DownloadStagingPath  string

	// Templates
	TemplatesPath   string
	StaticFilesPath string

	// Last.fm
	LastFMApiKey string

	// ListenBrainz
	ListenBrainzToken string

	// Discogs
	DiscogsToken string

	// Proxy
	ProxyURL string

	// Notifications
	NotificationWebhookURL string
	NotificationEnabled    bool

	// Subsonic
	Subsonic struct {
		Enabled  bool   `envconfig:"SUBSONIC_ENABLED" default:"false"`
		Password string `envconfig:"SUBSONIC_PASSWORD"`
	}

	// Transcode
	Transcode struct {
		Enabled    bool
		FFmpegPath string
		CacheDir   string
		MaxBitrate int
		MaxCacheMB int64
	}

	// Rate Limiter
	AuthRateLimitMax        int
	AuthRateLimitExpiration string

	// E2E
	E2EEnableTestAPI bool
}

// generateSecureSecret generates a cryptographically secure random secret
func generateSecureSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		slog.Error("Failed to generate secure secret", "error", err)
		os.Exit(1)
	}
	return hex.EncodeToString(b)
}

// configFileCandidates returns file paths to search for config YAML overlays,
// preferring the current working directory first, then the project root (../../).
func configFileCandidates(configEnv string) []string {
	wd, _ := os.Getwd()
	base := filepath.Join(wd, "config.yaml")
	envFile := filepath.Join(wd, fmt.Sprintf("config.%s.yaml", configEnv))

	parentBase := filepath.Join(wd, "..", "..", "config.yaml")
	parentEnv := filepath.Join(wd, "..", "..", fmt.Sprintf("config.%s.yaml", configEnv))

	return []string{base, envFile, parentBase, parentEnv}
}

// loadYAMLOverrides reads YAML config overlays and applies them to cfg.
// Env vars still take highest precedence (loaded after this runs at startup).
// Only known keys are applied to prevent arbitrary config injection.
func loadYAMLOverrides(cfg *Config, configEnv string) {
	for _, file := range configFileCandidates(configEnv) {
		data, err := os.ReadFile(file)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("Failed to read config file", "file", file, "error", err)
			}
			continue
		}
		var overlay map[string]interface{}
		if err := yaml.Unmarshal(data, &overlay); err != nil {
			slog.Warn("Failed to parse config file", "file", file, "error", err)
			continue
		}
		applyOverlay(cfg, overlay)
		slog.Info("Loaded config overlay", "file", file)
	}
}

// applyOverlay applies known YAML config keys to the Config struct.
// This is a whitelist — unknown keys are silently ignored.
func applyOverlay(cfg *Config, overlay map[string]interface{}) {
	if v, ok := overlay["environment"].(string); ok && v != "" {
		cfg.Environment = v
	}
	// Rate limiter
	if v, ok := overlay["auth_rate_limit_max"].(int); ok && v > 0 {
		cfg.AuthRateLimitMax = v
	}
	if v, ok := overlay["auth_rate_limit_expiration"].(string); ok && v != "" {
		cfg.AuthRateLimitExpiration = v
	}
	// Server
	if v, ok := overlay["port"].(string); ok && v != "" {
		cfg.Port = v
	}
	if v, ok := overlay["domain"].(string); ok && v != "" {
		cfg.Domain = v
	}
	// Notifications
	if v, ok := overlay["notification_enabled"].(bool); ok {
		cfg.NotificationEnabled = v
	}
	if v, ok := overlay["notification_webhook_url"].(string); ok && v != "" {
		cfg.NotificationWebhookURL = v
	}
	// Subsonic
	if v, ok := overlay["subsonic_enabled"].(bool); ok {
		cfg.Subsonic.Enabled = v
	}
	if v, ok := overlay["subsonic_password"].(string); ok && v != "" {
		cfg.Subsonic.Password = v
	}
	// Library
	if v, ok := overlay["music_library_path"].(string); ok && v != "" {
		cfg.MusicLibraryPath = v
	}
	if v, ok := overlay["download_staging_path"].(string); ok && v != "" {
		cfg.DownloadStagingPath = v
	}
}

// Load reads configuration from environment variables
func Load(filenames ...string) (*Config, error) {
	// Try to load .env file if provided, otherwise default to project root
	if len(filenames) > 0 {
		_ = godotenv.Load(filenames...)
	} else {
		_ = godotenv.Load("../../.env")
	}

	// SECURITY: JWT_SECRET must be explicitly set. Never use a hardcoded default.
	// In development, auto-generate a random secret if not provided.
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = generateSecureSecret()
		slog.Warn("JWT_SECRET not set — generated random secret. Set JWT_SECRET in .env for persistence across restarts.")
	}

	// SECURITY: Gonic credentials must be explicitly set. Never use hardcoded defaults.
	gonicUser := os.Getenv("GONIC_USER")
	gonicPass := os.Getenv("GONIC_PASS")
	env := getEnv("ENVIRONMENT", "development")
	if gonicUser == "" || gonicPass == "" {
		if env == "production" {
			return nil, fmt.Errorf("GONIC_USER and GONIC_PASS are required in production")
		}
		slog.Warn("GONIC_USER or GONIC_PASS not set — Gonic integration will fail until credentials are configured.")
	}

	// Determine config environment (defaults to ENVIRONMENT if CONFIG_ENV not set)
	configEnv := getEnv("CONFIG_ENV", "")
	if configEnv == "" {
		configEnv = getEnv("ENVIRONMENT", "development")
	}

	cfg := &Config{
		Environment: getEnv("ENVIRONMENT", "development"),
		ConfigEnv:   configEnv,
		Port:        getEnv("PORT", "8080"),
		Domain:      getEnv("DOMAIN", "localhost"),
		JWTSecret:   jwtSecret,

		DatabaseURL: getEnv("DATABASE_URL", ""),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379"),

		SpotifyClientID:     getEnv("SPOTIFY_CLIENT_ID", ""),
		SpotifyClientSecret: getEnv("SPOTIFY_CLIENT_SECRET", ""),

		MusicBrainzUserAgent: getEnv("MUSICBRAINZ_USER_AGENT", "NetRunner/1.0.0 (contact@example.com)"),
		MusicBrainzAPIKey:    getEnv("MUSICBRAINZ_API_KEY", ""),
		AcoustIDApiKey:       getEnv("ACOUSTID_API_KEY", ""),

		SlskdURL:    getEnv("SLSKD_URL", "http://localhost:5030"),
		SlskdAPIKey: getEnv("SLSKD_API_KEY", ""),

		GonicURL:  getEnv("GONIC_URL", "http://localhost:4747"),
		GonicUser: gonicUser,
		GonicPass: gonicPass,

		LidarrURL:    getEnv("LIDARR_URL", ""),
		LidarrAPIKey: getEnv("LIDARR_API_KEY", ""),

		NavidromeURL:  getEnv("NAVIDROME_URL", ""),
		NavidromeUser: getEnv("NAVIDROME_USER", ""),
		NavidromePass: getEnv("NAVIDROME_PASS", ""),

		SMTPHost:    getEnv("SMTP_HOST", ""),
		SMTPPort:    getEnv("SMTP_PORT", "587"),
		SMTPUser:    getEnv("SMTP_USER", ""),
		SMTPPass:    getEnv("SMTP_PASS", ""),
		SMTPFrom:    getEnv("SMTP_FROM", ""),
		SMTPEnabled: getEnvBool("SMTP_ENABLED", false),

		MusicLibraryPath:    getEnv("MUSIC_LIBRARY", "./music_library"),
		DownloadStagingPath: getEnv("DOWNLOAD_STAGING", "./downloads"),

		TemplatesPath:   getEnv("TEMPLATES_PATH", "./ops/web/templates"),
		StaticFilesPath: getEnv("STATIC_FILES_PATH", "./ops/web/static"),

		LastFMApiKey:      getEnv("LASTFM_API_KEY", ""),
		ListenBrainzToken: getEnv("LISTENBRAINZ_TOKEN", ""),
		DiscogsToken:      getEnv("DISCOGS_TOKEN", ""),

		ProxyURL: getEnv("PROXY_URL", ""),

		NotificationWebhookURL: getEnv("NOTIFICATION_WEBHOOK_URL", ""),
		NotificationEnabled:    getEnvBool("NOTIFICATION_ENABLED", false),

		// Transcode
		Transcode: struct {
			Enabled    bool
			FFmpegPath string
			CacheDir   string
			MaxBitrate int
			MaxCacheMB int64
		}{
			Enabled:    getEnv("TRANSCODE_ENABLED", "true") == "true",
			FFmpegPath: getEnv("FFMPEG_PATH", "ffmpeg"),
			CacheDir:   getEnv("TRANSCODE_CACHE_DIR", "/tmp/netrunner-transcode"),
			MaxBitrate: getEnvAsInt("TRANSCODE_MAX_BITRATE", 320),
			MaxCacheMB: getEnvAsInt64("TRANSCODE_MAX_CACHE_MB", 512),
		},

		// Rate Limiter
		AuthRateLimitMax:        getEnvAsInt("AUTH_RATE_LIMIT_MAX", 10),
		AuthRateLimitExpiration: getEnv("AUTH_RATE_LIMIT_EXPIRATION", "1m"),

		// Subsonic
		Subsonic: struct {
			Enabled  bool   `envconfig:"SUBSONIC_ENABLED" default:"false"`
			Password string `envconfig:"SUBSONIC_PASSWORD"`
		}{
			Enabled:  getEnvBool("SUBSONIC_ENABLED", false),
			Password: getEnv("SUBSONIC_PASSWORD", ""),
		},

		// E2E
		E2EEnableTestAPI: getEnvBool("E2E_ENABLE_TEST_API", false),
	}

	// Load YAML config overlays
	loadYAMLOverrides(cfg, configEnv)

	// Env vars take highest precedence — re-override fields that YAML may have set
	cfg.Environment = getEnv("ENVIRONMENT", cfg.Environment)
	cfg.Port = getEnv("PORT", cfg.Port)
	cfg.Domain = getEnv("DOMAIN", cfg.Domain)
	cfg.AuthRateLimitMax = getEnvAsInt("AUTH_RATE_LIMIT_MAX", cfg.AuthRateLimitMax)
	cfg.AuthRateLimitExpiration = getEnv("AUTH_RATE_LIMIT_EXPIRATION", cfg.AuthRateLimitExpiration)
	cfg.NotificationEnabled = getEnvBool("NOTIFICATION_ENABLED", cfg.NotificationEnabled)
	cfg.NotificationWebhookURL = getEnv("NOTIFICATION_WEBHOOK_URL", cfg.NotificationWebhookURL)
	cfg.MusicLibraryPath = getEnv("MUSIC_LIBRARY", cfg.MusicLibraryPath)
	cfg.DownloadStagingPath = getEnv("DOWNLOAD_STAGING", cfg.DownloadStagingPath)

	// Validate required fields
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	// Validate proxy URL if set
	if cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("PROXY_URL is not a valid URL: %w", err)
		}
		if proxyURL.Scheme == "" {
			return nil, fmt.Errorf("PROXY_URL must include a scheme (e.g., http://, socks5://)")
		}
	}

	return cfg, nil
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool retrieves a boolean environment variable or returns a default value
func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1"
	}
	return defaultVal
}

// getEnvAsInt retrieves an integer environment variable or returns a default value
func getEnvAsInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			return v
		}
	}
	return defaultVal
}

// getEnvAsInt64 retrieves a 64-bit integer environment variable or returns a default value
func getEnvAsInt64(key string, defaultVal int64) int64 {
	if val := os.Getenv(key); val != "" {
		if v, err := strconv.ParseInt(val, 10, 64); err == nil {
			return v
		}
	}
	return defaultVal
}
