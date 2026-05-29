package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGonicClient_Search3(t *testing.T) {
	// 1. Setup Mock Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		resp := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"version": "1.16.1",
				"searchResult3": map[string]interface{}{
					"song": []map[string]interface{}{
						{
							"id": "123",
							"title": "Test Song",
							"artist": "Test Artist",
							"album": "Test Album",
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// 2. Initialize Client
	c := NewGonicClient(ts.URL, "user", "pass")

	// 3. Call Search3
	songs, err := c.Search3("test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 4. Verify
	if len(songs) != 1 {
		t.Errorf("expected 1 song, got %d", len(songs))
	} else if songs[0].Title != "Test Song" {
		t.Errorf("expected 'Test Song', got %s", songs[0].Title)
	}
}

func TestGonicClient_GetSong(t *testing.T) {
	// 1. Setup Mock Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		resp := map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"version": "1.16.1",
				"song": map[string]interface{}{
					"id": "123",
					"title": "Test Song",
					"artist": "Test Artist",
					"album": "Test Album",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// 2. Initialize Client
	c := NewGonicClient(ts.URL, "user", "pass")

	// 3. Call GetSong
	song, err := c.GetSong("123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 4. Verify
	if song == nil {
		t.Fatal("expected song, got nil")
	} else if song.ID != "123" {
		t.Errorf("expected ID '123', got %s", song.ID)
	}
}
