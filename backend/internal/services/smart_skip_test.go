package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
)

func TestAcquisitionHandler_SmartSkip(t *testing.T) {
	// 1. Setup Mock Gonic Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		resp := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"searchResult3": map[string]interface{}{
					"song": []map[string]interface{}{
						{
							"id":     "123",
							"title":  "Test Song",
							"artist": "Test Artist",
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// 2. Setup DB
	db, err := database.Connect(&config.Config{DatabaseURL: ":memory:"})
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	database.Migrate(db)

	gonic := NewGonicClient(ts.URL, "user", "pass")
	handler := NewAcquisitionHandler(db, nil, nil, nil, nil, nil, gonic, nil)

	// 3. Create job and item
	job := database.Job{Type: "acquisition"}
	db.Create(&job)

	item := database.JobItem{
		JobID:           job.ID,
		Status:          "queued",
		Artist:          "Test Artist",
		TrackTitle:      "Test Song",
		NormalizedQuery: "Test Artist Test Song",
		Sequence:        1,
	}
	db.Create(&item)

	// 4. Execute Item
	err = handler.ExecuteItem(context.Background(), job.ID, item.ID)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// 5. Verify status is 'completed (already indexed)'
	var updatedItem database.JobItem
	db.First(&updatedItem, item.ID)

	if updatedItem.Status != "completed (already indexed)" {
		t.Errorf("expected status 'completed (already indexed)', got %s", updatedItem.Status)
	}
}
