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
	// maxPageOffset bounds the OFFSET a caller can force. Without it a
	// valid page number still makes the database scan every visible row
	// before returning an empty page.
	maxPageOffset = 10_000
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
	// Check the actual product for overflow; the previous form rejected pages
	// whose offset still fits.
	if page-1 > math.MaxInt/limit {
		return Pagination{}, errors.New("page offset is too large")
	}
	offset := (page - 1) * limit
	if offset > maxPageOffset {
		return Pagination{}, fmt.Errorf("page must not exceed %d with limit %d", maxPageOffset/limit+1, limit)
	}
	return Pagination{Page: page, Limit: limit, Offset: offset}, nil
}

// queryPositiveInt distinguishes an absent parameter from an empty one:
// `?page=` is a client mistake, not a request for the default.
func queryPositiveInt(c fiber.Ctx, name string, fallback int) (int, error) {
	args := c.Request().URI().QueryArgs()
	if !args.Has(name) {
		return fallback, nil
	}
	value := string(args.Peek(name))
	if value == "" {
		return 0, errors.New("must not be empty")
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
