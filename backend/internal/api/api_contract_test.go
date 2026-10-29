package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    Pagination
		wantErr bool
	}{
		{name: "defaults", want: Pagination{Page: 1, Limit: defaultPageSize, Offset: 0}},
		{name: "valid page and limit", query: "?page=3&limit=20", want: Pagination{Page: 3, Limit: 20, Offset: 40}},
		{name: "zero page rejected", query: "?page=0", wantErr: true},
		{name: "limit above maximum rejected", query: "?limit=101", wantErr: true},
		{name: "non numeric limit rejected", query: "?limit=large", wantErr: true},
		{name: "empty page is rejected rather than defaulted", query: "?page=", wantErr: true},
		{name: "empty limit is rejected rather than defaulted", query: "?limit=", wantErr: true},
		{name: "maximum representable page rejected", query: "?page=2147483647&limit=1", wantErr: true},
		{name: "offset above the cap rejected", query: "?page=10002&limit=1", wantErr: true},
		{name: "offset at the cap accepted", query: "?page=101&limit=100", want: Pagination{Page: 101, Limit: 100, Offset: 10000}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			var got Pagination
			var gotErr error
			app.Get("/items", func(c fiber.Ctx) error {
				got, gotErr = parsePagination(c)
				return c.SendStatus(http.StatusNoContent)
			})

			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/items"+tt.query, nil))
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			if tt.wantErr {
				assert.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWriteAPIError(t *testing.T) {
	app := fiber.New()
	app.Get("/error", func(c fiber.Ctx) error {
		return writeAPIError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/error", nil))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	var body APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "internal_error", body.Code)
	assert.Equal(t, "internal server error", body.Error)
	assert.NotContains(t, body.Error, "password")
}

func TestParsePathIdentifiers(t *testing.T) {
	app := fiber.New()
	app.Get("/uuid/:id/number/:job_id", func(c fiber.Ctx) error {
		id, err := parseUUIDPath(c, "id")
		if err != nil {
			return writeAPIError(c, fiber.StatusBadRequest, "invalid_identifier", "invalid id")
		}
		jobID, err := parseUintPath(c, "job_id")
		if err != nil {
			return writeAPIError(c, fiber.StatusBadRequest, "invalid_identifier", "invalid job_id")
		}
		return c.JSON(fiber.Map{"id": id.String(), "job_id": jobID})
	})

	for _, tt := range []struct {
		name    string
		path    string
		code    int
		wantID  string
		wantJob uint64
	}{
		{name: "valid opaque uuid and numeric id", path: "/uuid/550e8400-e29b-41d4-a716-446655440000/number/12", code: http.StatusOK, wantID: "550e8400-e29b-41d4-a716-446655440000", wantJob: 12},
		{name: "invalid uuid", path: "/uuid/not-a-uuid/number/12", code: http.StatusBadRequest},
		{name: "invalid numeric id", path: "/uuid/550e8400-e29b-41d4-a716-446655440000/number/nope", code: http.StatusBadRequest},
		{name: "zero numeric id is rejected", path: "/uuid/550e8400-e29b-41d4-a716-446655440000/number/0", code: http.StatusBadRequest},
		{name: "uppercase uuid parses to the same value", path: "/uuid/550E8400-E29B-41D4-A716-446655440000/number/12", code: http.StatusOK, wantID: "550e8400-e29b-41d4-a716-446655440000", wantJob: 12},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, tt.path, nil))
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tt.code, resp.StatusCode)
			if tt.code != http.StatusOK {
				return
			}
			var body struct {
				ID    string `json:"id"`
				JobID uint64 `json:"job_id"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, tt.wantID, body.ID, "a parse that returns the wrong identifier must not pass")
			assert.Equal(t, tt.wantJob, body.JobID)
		})
	}
}
