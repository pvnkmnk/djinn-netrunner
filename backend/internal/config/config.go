package config

import (
	"fmt"
	"os"

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

	// SLSKD
	SlskdURL    string
	SlskdAPIKey string

	// Gonic
	GonicURL  string
	GonicUser string
	GonicPass string

	// Proxy
	ProxyURL string
}

// Load reads configuration from environment variables
func Load(filenames ...string) (*Config, error) {
	// Try to load .env file if provided, otherwise default to project root
	if len(filenames) > 0 {
		_ = godotenv.Load(filenames...)
	} else {
		_ = godotenv.Load("../../.env")
	}

	cfg := &Config{
		Environment: getEnv("ENVIRONMENT", "development"),
		Port:        getEnv("PORT", "8080"),
		Domain:      getEnv("DOMAIN", "localhost"),
		JWTSecret:   getEnv("JWT_SECRET", "dev-secret-do-not-use-in-prod"),

		DatabaseURL: getEnv("DATABASE_URL", ""),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379"),

		SpotifyClientID:     getEnv("SPOTIFY_CLIENT_ID", ""),
		SpotifyClientSecret: getEnv("SPOTIFY_CLIENT_SECRET", ""),
		
		MusicBrainzUserAgent: getEnv("MUSICBRAINZ_USER_AGENT", "NetRunner/1.0.0 (contact@example.com)"),
		MusicBrainzAPIKey:    getEnv("MUSICBRAINZ_API_KEY", ""),

		SlskdURL:    getEnv("SLSKD_URL", "http://localhost:5030"),
		SlskdAPIKey: getEnv("SLSKD_API_KEY", ""),

		GonicURL:  getEnv("GONIC_URL", "http://localhost:4747"),
		GonicUser: getEnv("GONIC_USER", "admin"),
		GonicPass: getEnv("GONIC_PASS", "admin"),

		ProxyURL: getEnv("PROXY_URL", ""),
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
