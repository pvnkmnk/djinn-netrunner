package api

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// APIError is the stable error envelope for JSON management endpoints.
// Internal details are logged server-side and never serialized here.
type APIError struct {
	Error     string         `json:"error"`
	Code      string         `json:"code,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

// Pagination is the bounded offset pagination contract used by list endpoints.
type Pagination struct {
	Page   int
	Limit  int
	Offset int
}

// writeAPIError writes a stable, JSON-only management error response.
func writeAPIError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(APIError{
		Error:     message,
		Code:      code,
		RequestID: c.Get("X-Request-ID"),
	})
}

func parsePagination(c fiber.Ctx) (Pagination, error) {
	page, err := queryPositiveInt(c, "page", 1)
	if err != nil {
		return Pagination{}, fmt.Errorf("page must be a positive integer: %w", err)
	}
	limit, err := queryPositiveInt(c, "limit", defaultPageSize)
	if err != nil {
		return Pagination{}, fmt.Errorf("limit must be a positive integer: %w", err)
	}
	if limit > maxPageSize {
		return Pagination{}, fmt.Errorf("limit must not exceed %d", maxPageSize)
	}
	if page > (math.MaxInt-limit)/limit {
		return Pagination{}, errors.New("page offset is too large")
	}
	return Pagination{Page: page, Limit: limit, Offset: (page - 1) * limit}, nil
}

func queryPositiveInt(c fiber.Ctx, name string, fallback int) (int, error) {
	value := c.Query(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, errors.New("must be positive")
	}
	return parsed, nil
}

func parseUUIDPath(c fiber.Ctx, name string) (uuid.UUID, error) {
	value := c.Params(name)
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

func parseUintPath(c fiber.Ctx, name string) (uint64, error) {
	value := c.Params(name)
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		if err == nil {
			err = errors.New("must be positive")
		}
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}
