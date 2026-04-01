package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	// Server
	Environment string
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

	// Library
	MusicLibraryPath string

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

	// Rate Limiter
	AuthRateLimitMax        int
	AuthRateLimitExpiration string
}

// generateSecureSecret generates a cryptographically secure random secret
func generateSecureSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatal("failed to generate secure secret: ", err)
	}
	return hex.EncodeToString(b)
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
		log.Println("[WARN] JWT_SECRET not set — generated random secret. Set JWT_SECRET in .env for persistence across restarts.")
	}

	// SECURITY: Gonic credentials must be explicitly set. Never use hardcoded defaults.
	gonicUser := os.Getenv("GONIC_USER")
	gonicPass := os.Getenv("GONIC_PASS")
	if gonicUser == "" || gonicPass == "" {
		log.Println("[WARN] GONIC_USER or GONIC_PASS not set — Gonic integration will fail until credentials are configured.")
	}

	cfg := &Config{
		Environment: getEnv("ENVIRONMENT", "development"),
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

		MusicLibraryPath: getEnv("MUSIC_LIBRARY", "./music_library"),

		TemplatesPath:   getEnv("TEMPLATES_PATH", "./ops/web/templates"),
		StaticFilesPath: getEnv("STATIC_FILES_PATH", "./ops/web/static"),

		LastFMApiKey:      getEnv("LASTFM_API_KEY", ""),
		ListenBrainzToken: getEnv("LISTENBRAINZ_TOKEN", ""),
		DiscogsToken:      getEnv("DISCOGS_TOKEN", ""),

		ProxyURL: getEnv("PROXY_URL", ""),

		NotificationWebhookURL: getEnv("NOTIFICATION_WEBHOOK_URL", ""),
		NotificationEnabled:    getEnvBool("NOTIFICATION_ENABLED", false),

		// Rate Limiter
		AuthRateLimitMax:        getEnvAsInt("AUTH_RATE_LIMIT_MAX", 10),
		AuthRateLimitExpiration: getEnv("AUTH_RATE_LIMIT_EXPIRATION", "1m"),
	}

	// Validate required fields
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
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
