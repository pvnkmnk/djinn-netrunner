package services

import (
	"context"
	"log"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"golang.org/x/oauth2"
	oauthspotify "golang.org/x/oauth2/spotify"
	"gorm.io/gorm"
)

// SpotifyTokenService proactively refreshes Spotify tokens before they expire.
type SpotifyTokenService struct {
	db       *gorm.DB
	config   *oauth2.Config
	interval time.Duration
}

// NewSpotifyTokenService creates a new Spotify token refresh service.
// It requires the app config to access Spotify credentials.
func NewSpotifyTokenService(db *gorm.DB, cfg *config.Config) *SpotifyTokenService {
	oc := &oauth2.Config{
		ClientID:     cfg.SpotifyClientID,
		ClientSecret: cfg.SpotifyClientSecret,
		Endpoint:     oauthspotify.Endpoint,
	}

	return &SpotifyTokenService{
		db:       db,
		config:   oc,
		interval: 5 * time.Minute,
	}
}

// Start begins the background refresh loop.
func (s *SpotifyTokenService) Start(ctx context.Context) {
	// Perform an immediate refresh pass on startup
	s.refreshExpiringTokens()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshExpiringTokens()
		}
	}
}

func (s *SpotifyTokenService) refreshExpiringTokens() {
	var tokens []database.SpotifyToken
	// Refresh tokens that will expire in the next 10 minutes but are not yet expired
	cutoff := time.Now().Add(10 * time.Minute)
	now := time.Now()

	if err := s.db.Where("expiry <= ? AND expiry > ?", cutoff, now).Find(&tokens).Error; err != nil {
		log.Printf("[SPOTIFY] Error finding expiring tokens: %v", err)
		return
	}

	if len(tokens) == 0 {
		return
	}

	log.Printf("[SPOTIFY] Found %d tokens to refresh", len(tokens))

	for i := range tokens {
		if err := s.refreshToken(&tokens[i]); err != nil {
			log.Printf("[SPOTIFY] Failed to refresh token for user %d: %v", tokens[i].UserID, err)
		} else {
			log.Printf("[SPOTIFY] Refreshed token for user %d", tokens[i].UserID)
		}
	}
}

func (s *SpotifyTokenService) refreshToken(token *database.SpotifyToken) error {
	oauthToken := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
	}

	// Use ReuseTokenSource — when the token is expired, it automatically
	// uses the RefreshToken to obtain a new access token.
	ts := oauth2.ReuseTokenSource(oauthToken, nil)
	newToken, err := ts.Token()
	if err != nil {
		return err
	}

	// Update token in database
	token.AccessToken = newToken.AccessToken
	if newToken.RefreshToken != "" {
		token.RefreshToken = newToken.RefreshToken
	}
	token.Expiry = newToken.Expiry

	return s.db.Save(token).Error
}
