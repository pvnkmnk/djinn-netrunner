package api

import (
	"context"
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
	oauthspotify "golang.org/x/oauth2/spotify"
	"gorm.io/gorm"
)

type SpotifyAuthHandler struct {
	db     *gorm.DB
	config *oauth2.Config
}

func NewSpotifyAuthHandler(db *gorm.DB) *SpotifyAuthHandler {
	redirectURL := os.Getenv("SPOTIFY_REDIRECT_URI")
	if redirectURL == "" {
		redirectURL = "http://localhost:8080/api/auth/spotify/callback"
	}

	config := &oauth2.Config{
		ClientID:     os.Getenv("SPOTIFY_CLIENT_ID"),
		ClientSecret: os.Getenv("SPOTIFY_CLIENT_SECRET"),
		RedirectURL:  redirectURL,
		Endpoint:     oauthspotify.Endpoint,
		Scopes: []string{
			spotifyauth.ScopeUserLibraryRead,
			spotifyauth.ScopePlaylistReadPrivate,
			spotifyauth.ScopePlaylistReadCollaborative,
		},
	}

	return &SpotifyAuthHandler{
		db:     db,
		config: config,
	}
}

// Login redirects the user to Spotify for authentication
func (h *SpotifyAuthHandler) Login(c *fiber.Ctx) error {
	state := "netrunner-spotify-state"
	url := h.config.AuthCodeURL(state)
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

	token, err := h.config.Exchange(context.Background(), code)
	if err != nil {
		return c.Status(500).SendString(fmt.Sprintf("Token exchange failed: %v", err))
	}

	u := c.Locals("user")
	if u == nil {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}
	user := u.(database.User)

	var spotifyToken database.SpotifyToken
	err = h.db.Where("user_id = ?", user.ID).First(&spotifyToken).Error

	if err != nil {
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

	ts := h.config.TokenSource(ctx, oauthToken)
	newToken, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get valid token: %w", err)
	}

	if newToken.AccessToken != token.AccessToken {
		token.AccessToken = newToken.AccessToken
		if newToken.RefreshToken != "" {
			token.RefreshToken = newToken.RefreshToken
		}
		token.Expiry = newToken.Expiry
		h.db.Save(&token)
	}

	// Use the token to create a client
	httpClient := h.config.Client(ctx, newToken)
	return spotify.New(httpClient), nil
}

// IsLinked checks if a user has linked their Spotify account
func (h *SpotifyAuthHandler) IsLinked(userID uint64) bool {
	var token database.SpotifyToken
	err := h.db.Where("user_id = ?", userID).First(&token).Error
	return err == nil
}
