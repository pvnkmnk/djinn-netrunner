package api

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	SessionCookie = "session_id"
	SessionTTL    = 7 * 24 * time.Hour
)

type AuthHandler struct {
	db *gorm.DB
}

func NewAuthHandler(db *gorm.DB) *AuthHandler {
	return &AuthHandler{db: db}
}

// Register handles user registration
func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if payload.Email == "" || payload.Password == "" {
		return c.Status(400).JSON(fiber.Map{"error": "email and password are required"})
	}

	// Check if user exists
	var existing database.User
	if err := h.db.Where("email = ?", payload.Email).First(&existing).Error; err == nil {
		return c.Status(200).JSON(fiber.Map{"user_id": existing.ID}) // Idempotent as per Python version
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to hash password"})
	}

	user := database.User{
		Email:        payload.Email,
		PasswordHash: string(hashedPassword),
		Role:         "user", // Hardcoded to prevent privilege escalation during registration
	}

	if err := h.db.Create(&user).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to create user"})
	}

	return c.JSON(fiber.Map{"user_id": user.ID})
}

// Login handles user login
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	var user database.User
	if err := h.db.Where("email = ?", payload.Email).First(&user).Error; err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(payload.Password)); err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
	}

	// Create session
	sessionID, err := generateSessionID()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to generate session id"})
	}

	expiresAt := time.Now().Add(SessionTTL)
	session := database.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		ExpiresAt: expiresAt,
		IP:        c.IP(),
		UserAgent: c.Get("User-Agent"),
	}

	if err := h.db.Create(&session).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to create session"})
	}

	// Update last login
	now := time.Now()
	h.db.Model(&user).Update("last_login_at", &now)

	// Set cookie
	c.Cookie(&fiber.Cookie{
		Name:     SessionCookie,
		Value:    sessionID,
		Expires:  expiresAt,
		HTTPOnly: true,
		SameSite: "Lax",
		Path:     "/",
	})

	return c.Redirect("/", 302)
}

// Logout handles user logout
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	sessionID := c.Cookies(SessionCookie)
	if sessionID != "" {
		h.db.Where("session_id = ?", sessionID).Delete(&database.Session{})
	}

	c.ClearCookie(SessionCookie)
	return c.JSON(fiber.Map{"status": "ok"})
}

func (h *AuthHandler) GetDB() *gorm.DB {
	return h.db
}

// AuthMiddleware protects routes
func (h *AuthHandler) AuthMiddleware(c *fiber.Ctx) error {
	sessionID := c.Cookies(SessionCookie)
	if sessionID == "" {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var user database.User
	err := h.db.Joins("JOIN sessions ON sessions.user_id = users.id").
		Where("sessions.session_id = ? AND sessions.expires_at > ?", sessionID, time.Now()).
		First(&user).Error

	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	// Store user in context
	c.Locals("user", user)
	return c.Next()
}

func generateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
