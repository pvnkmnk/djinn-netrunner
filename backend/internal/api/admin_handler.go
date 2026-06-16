package api

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AdminHandler struct {
	db *gorm.DB
}

func NewAdminHandler(db *gorm.DB) *AdminHandler {
	return &AdminHandler{db: db}
}

// AdminOnly middleware checks the authenticated user has admin role.
func (h *AdminHandler) AdminOnly(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}
	if user.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{"error": "forbidden: admin only"})
	}
	return c.Next()
}

// AdminPage renders the admin dashboard page
func (h *AdminHandler) AdminPage(c *fiber.Ctx) error {
	return c.Render("pages/admin", fiber.Map{
		"Page": "admin",
	})
}

// GET /api/admin/users — list all users (sensitive fields excluded)
func (h *AdminHandler) ListUsers(c *fiber.Ctx) error {
	var users []database.User
	if err := h.db.Select("id, email, role, created_at, updated_at, last_login_at").Find(&users).Error; err != nil {
		return internalServerError(c, err)
	}
	return c.JSON(users)
}

// POST /api/admin/users — create a new user
func (h *AdminHandler) CreateUser(c *fiber.Ctx) error {
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if payload.Email == "" || payload.Password == "" {
		return c.Status(400).JSON(fiber.Map{"error": "email and password required"})
	}
	if payload.Role == "" {
		payload.Role = "user"
	}
	if payload.Role != "user" && payload.Role != "admin" {
		return c.Status(400).JSON(fiber.Map{"error": "role must be 'user' or 'admin'"})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
	if err != nil {
		return internalServerError(c, err)
	}

	user := database.User{
		Email:        payload.Email,
		PasswordHash: string(hash),
		Role:         payload.Role,
	}
	if err := h.db.Create(&user).Error; err != nil {
		return c.Status(409).JSON(fiber.Map{"error": "user already exists"})
	}

	h.logAudit("user_create", c, "user", strconv.FormatUint(user.ID, 10), map[string]string{"email": user.Email, "role": user.Role})
	return c.Status(201).JSON(fiber.Map{"status": "created", "id": user.ID})
}

// DELETE /api/admin/users/:id — delete a user
func (h *AdminHandler) DeleteUser(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid user ID"})
	}
	// Prevent self-deletion
	actor := c.Locals("user").(database.User)
	if actor.ID == id {
		return c.Status(400).JSON(fiber.Map{"error": "cannot delete yourself"})
	}

	var user database.User
	if err := h.db.First(&user, id).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "user not found"})
	}
	if err := h.db.Delete(&user).Error; err != nil {
		return internalServerError(c, err)
	}

	h.logAudit("user_delete", c, "user", strconv.FormatUint(id, 10), nil)
	return c.JSON(fiber.Map{"status": "deleted"})
}

// PATCH /api/admin/users/:id/role — update user role
func (h *AdminHandler) UpdateRole(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid user ID"})
	}
	var payload struct {
		Role string `json:"role"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if payload.Role != "user" && payload.Role != "admin" {
		return c.Status(400).JSON(fiber.Map{"error": "role must be 'user' or 'admin'"})
	}

	if err := h.db.Model(&database.User{}).Where("id = ?", id).Update("role", payload.Role).Error; err != nil {
		return internalServerError(c, err)
	}

	h.logAudit("user_role_update", c, "user", strconv.FormatUint(id, 10), map[string]string{"role": payload.Role})
	return c.JSON(fiber.Map{"status": "updated"})
}

// POST /api/admin/users/:id/reset-password — admin resets a user's password
func (h *AdminHandler) ResetPassword(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid user ID"})
	}
	var payload struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if payload.Password == "" {
		return c.Status(400).JSON(fiber.Map{"error": "password required"})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
	if err != nil {
		return internalServerError(c, err)
	}
	if err := h.db.Model(&database.User{}).Where("id = ?", id).Update("password_hash", string(hash)).Error; err != nil {
		return internalServerError(c, err)
	}

	h.logAudit("password_reset", c, "user", strconv.FormatUint(id, 10), nil)
	return c.JSON(fiber.Map{"status": "password reset"})
}

// GET /api/admin/audit — paginated audit log
func (h *AdminHandler) ListAudit(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
offset := (page - 1) * limit

	var entries []database.AuditLog
	var total int64

	h.db.Model(&database.AuditLog{}).Count(&total)
	if err := h.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&entries).Error; err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(fiber.Map{
		"entries": entries,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

// GET /api/admin/config — list all Setting rows
func (h *AdminHandler) ListConfig(c *fiber.Ctx) error {
	var settings []database.Setting
	if err := h.db.Order("key ASC").Find(&settings).Error; err != nil {
		return internalServerError(c, err)
	}
	return c.JSON(settings)
}

// PATCH /api/admin/config — upsert a Setting row
func (h *AdminHandler) UpdateConfig(c *fiber.Ctx) error {
	var payload struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if payload.Key == "" {
		return c.Status(400).JSON(fiber.Map{"error": "key is required"})
	}

	if err := h.db.Where("key = ?", payload.Key).Assign(database.Setting{Value: payload.Value}).FirstOrCreate(&database.Setting{}).Error; err != nil {
		return internalServerError(c, err)
	}

	h.logAudit("config_update", c, "config", payload.Key, map[string]string{"value": payload.Value})
	return c.JSON(fiber.Map{"status": "updated"})
}

// Helper: write audit log entry asynchronously
func (h *AdminHandler) logAudit(action string, c *fiber.Ctx, targetType, targetID string, metadata map[string]string) {
	actor, ok := c.Locals("user").(database.User)
	if !ok {
		return
	}
	metaJSON := ""
	if metadata != nil {
		b, _ := json.Marshal(metadata)
		metaJSON = string(b)
	}
	entry := database.AuditLog{
		Action:     action,
		ActorID:    actor.ID,
		TargetType: targetType,
		TargetID:   targetID,
		Metadata:   metaJSON,
		CreatedAt:  time.Now(),
	}
	if err := h.db.Create(&entry).Error; err != nil {
		slog.Error("Failed to write audit log", "action", action, "error", err)
	}
}