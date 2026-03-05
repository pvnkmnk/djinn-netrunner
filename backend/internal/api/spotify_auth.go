package api

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

type SpotifyAuthHandler struct {
	db   *gorm.DB
	auth *spotifyauth.Authenticator
}

func NewSpotifyAuthHandler(db *gorm.DB) *SpotifyAuthHandler {
	redirectURL := os.Getenv("SPOTIFY_REDIRECT_URI")
	if redirectURL == "" {
		// Default for local development
		redirectURL = "http://localhost:8080/api/auth/spotify/callback"
	}

	auth := spotifyauth.New(
		spotifyauth.WithRedirectURL(redirectURL),
		spotifyauth.WithScopes(
			spotifyauth.ScopeUserLibraryRead,
			spotifyauth.ScopePlaylistReadPrivate,
			spotifyauth.ScopePlaylistReadCollaborative,
		),
	)

	return &SpotifyAuthHandler{
		db:   db,
		auth: auth,
	}
}

// Login redirects the user to Spotify for authentication
func (h *SpotifyAuthHandler) Login(c *fiber.Ctx) error {
	state := "netrunner-spotify-state" // In production, use a secure random state
	url := h.auth.AuthURL(state)
	return c.Redirect(url)
}

// Callback handles the redirect from Spotify
func (h *SpotifyAuthHandler) Callback(c *fiber.Ctx) error {
	state := c.Query("state")
	if state != "netrunner-spotify-state" {
		return c.Status(400).SendString("State mismatch")
	}

	code := c.Query("code")
	if code == "" {
		return c.Status(400).SendString("Code missing")
	}

	token, err := h.auth.Exchange(context.Background(), code)
	if err != nil {
		return c.Status(500).SendString(fmt.Sprintf("Token exchange failed: %v", err))
	}

	// Get user from context (set by AuthMiddleware)
	u := c.Locals("user")
	if u == nil {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}
	user := u.(database.User)

	// Save or update token
	var spotifyToken database.SpotifyToken
	err = h.db.Where("user_id = ?", user.ID).First(&spotifyToken).Error

	if err != nil {
		// Create new
		spotifyToken = database.SpotifyToken{
			UserID:       user.ID,
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			TokenType:    token.TokenType,
			Expiry:       token.Expiry,
		}
		if err := h.db.Create(&spotifyToken).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to save token"})
		}
	} else {
		// Update existing
		spotifyToken.AccessToken = token.AccessToken
		if token.RefreshToken != "" {
			spotifyToken.RefreshToken = token.RefreshToken
		}
		spotifyToken.TokenType = token.TokenType
		spotifyToken.Expiry = token.Expiry
		if err := h.db.Save(&spotifyToken).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to update token"})
		}
	}

	// Redirect back to dashboard or source manager
	return c.Redirect("/#sources")
}

// GetClient returns an authenticated Spotify client for a user
func (h *SpotifyAuthHandler) GetClient(ctx context.Context, userID uint64) (*spotify.Client, error) {
	var token database.SpotifyToken
	if err := h.db.Where("user_id = ?", userID).First(&token).Error; err != nil {
		return nil, fmt.Errorf("spotify token not found for user: %w", err)
	}

	oauthToken := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
	}

	// Create a token source that automatically refreshes
	ts := h.auth.TokenSource(ctx, oauthToken)
	
	// Get current token from source (might be refreshed)
	newToken, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get valid token: %w", err)
	}

	// If token was refreshed, update database
	if newToken.AccessToken != token.AccessToken {
		token.AccessToken = newToken.AccessToken
		if newToken.RefreshToken != "" {
			token.RefreshToken = newToken.RefreshToken
		}
		token.Expiry = newToken.Expiry
		h.db.Save(&token)
	}

	return spotify.New(h.auth.Client(ctx, newToken)), nil
}
