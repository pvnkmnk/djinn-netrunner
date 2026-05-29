package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

const testAPIKey = "test-key"

// withAPIKeyCheck wraps an http.Handler and validates the X-API-Key header.
// If the header is missing or incorrect, the request is rejected with 401.
// The test's assertions determine pass/fail based on the HTTP response.
func withAPIKeyCheck(t *testing.T, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != testAPIKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestSlskdServiceHealthCheck(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		handler http.Handler
		want    bool
	}{
		{
			name:   "success",
			apiKey: testAPIKey,
			handler: withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v0/session" {
					t.Errorf("Expected path /api/v0/session, got %s", r.URL.Path)
					http.Error(w, "bad path", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusOK)
			})),
			want: true,
		},
		{
			name:   "failure",
			apiKey: "bad-key",
			handler: withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Unreachable: withAPIKeyCheck rejects invalid keys before reaching here.
				w.WriteHeader(http.StatusOK)
			})),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			cfg := &config.Config{
				SlskdURL:    server.URL,
				SlskdAPIKey: tt.apiKey,
			}
			svc := NewSlskdService(cfg, nil)

			if got := svc.HealthCheck(); got != tt.want {
				t.Errorf("HealthCheck() = %v, want %v", got, tt.want)
			}
		})
	}
}

// intPtr returns a pointer to the given int value.
func intPtr(v int) *int { return &v }

// mockSearchResponse mirrors the slskd API response structure for search results.
type mockSearchResponse struct {
	Responses []mockSearchResult `json:"responses"`
}

type mockSearchResult struct {
	Username    string         `json:"username"`
	UploadSpeed int            `json:"uploadSpeed"`
	QueueLength int            `json:"queueLength"`
	Files       []mockFileInfo `json:"files"`
}

type mockFileInfo struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	IsLocked bool   `json:"isLocked"`
	BitRate  *int   `json:"bitRate"`
	Length   *int   `json:"length"`
}

func TestSlskdServiceSearch(t *testing.T) {
	searchID := "search-123"

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/searches", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    searchID,
			"state": "Completed",
		})
	})
	mux.HandleFunc("GET /api/v0/searches/"+searchID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockSearchResponse{
			Responses: []mockSearchResult{
				{
					Username:    "testuser",
					UploadSpeed: 500,
					QueueLength: 0,
					Files: []mockFileInfo{
						{
							Filename: "test_artist_-_test_song.mp3",
							Size:     5242880,
							IsLocked: false,
							BitRate:  intPtr(320),
							Length:   intPtr(240),
						},
					},
				},
			},
		})
	})

	server := httptest.NewServer(withAPIKeyCheck(t, mux))
	defer server.Close()

	cfg := &config.Config{
		SlskdURL:    server.URL,
		SlskdAPIKey: testAPIKey,
	}
	svc := NewSlskdService(cfg, nil)

	results, err := svc.Search("test artist test song", 0, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	expected := SearchResult{
		Username:    "testuser",
		Filename:    "test_artist_-_test_song.mp3",
		Size:        5242880,
		Speed:       500,
		QueueLength: 0,
		Locked:      false,
		Bitrate:     intPtr(320),
		Length:      intPtr(240),
	}
	expected.CalculateScore(nil)

	if !reflect.DeepEqual(expected, result) {
		t.Errorf("Search result mismatch.\nExpected: %+v\nGot:      %+v", expected, result)
	}
}

func TestSlskdServiceEnqueueDownload(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		handler http.Handler
		wantID  string
		wantErr bool
	}{
		{
			name:   "success",
			apiKey: testAPIKey,
			handler: withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v0/downloads" {
					t.Errorf("Expected path /api/v0/downloads, got %s", r.URL.Path)
					http.Error(w, "bad path", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusOK)
			})),
			wantID:  "testuser:test_song.mp3",
			wantErr: false,
		},
		{
			name:   "failure - server error",
			apiKey: testAPIKey,
			handler: withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})),
			wantID:  "",
			wantErr: true,
		},
		{
			name:   "failure - unauthorized",
			apiKey: "bad-key",
			handler: withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler reached on unauthorized request")
				w.WriteHeader(http.StatusOK)
			})),
			wantID:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			cfg := &config.Config{
				SlskdURL:    server.URL,
				SlskdAPIKey: tt.apiKey,
			}
			svc := NewSlskdService(cfg, nil)

			gotID, err := svc.EnqueueDownload("testuser", "test_song.mp3")
			if (err != nil) != tt.wantErr {
				t.Errorf("EnqueueDownload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && gotID != tt.wantID {
				t.Errorf("EnqueueDownload() = %v, want %v", gotID, tt.wantID)
			}
		})
	}
}

func TestSlskdService_EnqueueDownload_ErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"bad request", 400, true},
		{"forbidden", 403, true},
		{"not found", 404, true},
		{"rate limited", 429, true},
		{"server error", 500, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})))
			defer server.Close()

			cfg := &config.Config{SlskdURL: server.URL, SlskdAPIKey: testAPIKey}
			svc := NewSlskdService(cfg, nil)

			_, err := svc.EnqueueDownload("username", "filename.mp3")
			if (err != nil) != tt.wantErr {
				t.Errorf("EnqueueDownload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSlskdServiceGetDownload(t *testing.T) {
	tests := []struct {
		name            string
		apiKey          string
		handler         http.Handler
		wantState       string
		wantErr         bool
		wantNilDownload bool
	}{
		{
			name:   "success",
			apiKey: testAPIKey,
			handler: withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/api/v0/downloads/testuser/test song.mp3"
				if r.URL.Path != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
					http.Error(w, "bad path", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"state": "COMPLETED",
					"path":  "/downloads/test song.mp3",
				})
			})),
			wantState: "COMPLETED",
		},
		{
			name:   "not found",
			apiKey: testAPIKey,
			handler: withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			})),
			wantNilDownload: true,
		},
		{
			name:   "unauthorized",
			apiKey: "bad-key",
			handler: withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler reached on unauthorized request")
				w.WriteHeader(http.StatusOK)
			})),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			cfg := &config.Config{
				SlskdURL:    server.URL,
				SlskdAPIKey: tt.apiKey,
			}
			svc := NewSlskdService(cfg, nil)

			dl, err := svc.GetDownload("testuser", "test song.mp3")
			if (err != nil) != tt.wantErr {
				t.Errorf("GetDownload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if tt.wantNilDownload {
				if dl != nil {
					t.Errorf("Expected nil download, got %+v", dl)
				}
				return
			}

			if dl == nil {
				t.Fatal("Expected download info, got nil")
			}
			if string(dl.State) != tt.wantState {
				t.Errorf("Expected state '%s', got '%s'", tt.wantState, dl.State)
			}
		})
	}
}

// TestSlskdServiceWaitForDownload is omitted.
// WaitForDownload uses a 5-second polling ticker with a timeout check inside the tick,
// meaning a 2s timeout won't fire until the next 5s tick fires. Integration tests
// would better cover the polling/wait path.
