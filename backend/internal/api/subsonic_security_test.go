package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestSubsonic_GetCoverArt_SSRF(t *testing.T) {
	db, handler := setupSubsonicDBHandlerTest(t)
	var hit bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer ts.Close()

	var library database.Library
	db.First(&library)
	track := database.Track{
		ID: uuid.New(), LibraryID: library.ID, CoverURL: ts.URL + "/secret",
	}
	db.Create(&track)

	app := fiber.New()
	app.Get("/getCoverArt", handler.AuthMiddleware, handler.GetCoverArt)

	url := "/getCoverArt?id=" + track.ID.String() + "&u=test@example.com&p=testpass123"
	req := httptest.NewRequest("GET", url, nil)
	resp, _ := app.Test(req)

	assert.Equal(t, 200, resp.StatusCode)
	assert.False(t, hit, "Local server SHOULD NOT have been hit (SSRF protection should block it)")
	assert.Contains(t, string(subsonicGetRespBody(resp)), "Cover art not available")
}
