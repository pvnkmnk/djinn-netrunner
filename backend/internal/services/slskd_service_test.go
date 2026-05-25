package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
		// Verify includeResponses=true is passed
		if r.URL.Query().Get("includeResponses") != "true" {
			t.Error("Expected includeResponses=true query parameter")
		}
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
	testDownloadID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

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
				if r.URL.Path != "/api/v0/transfers/downloads/testuser" {
					t.Errorf("Expected path /api/v0/transfers/downloads/testuser, got %s", r.URL.Path)
					http.Error(w, "bad path", http.StatusBadRequest)
					return
				}
				// Verify the payload is an array of download requests
				var payload []struct {
					Filename string `json:"filename"`
					Size     int64  `json:"size"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("Failed to decode request body: %v", err)
					http.Error(w, "bad body", http.StatusBadRequest)
					return
				}
				if len(payload) != 1 || payload[0].Filename != "test_song.mp3" {
					t.Errorf("Unexpected payload: %+v", payload)
				}
				if payload[0].Size != 5242880 {
					t.Errorf("Expected size 5242880, got %d", payload[0].Size)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"enqueued": []map[string]interface{}{
						{"id": testDownloadID, "filename": "test_song.mp3", "state": "Requested"},
					},
					"failed": []interface{}{},
				})
			})),
			wantID:  testDownloadID,
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

			gotID, err := svc.EnqueueDownload("testuser", "test_song.mp3", 5242880)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnqueueDownload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && gotID != tt.wantID {
				t.Errorf("EnqueueDownload() = %v, want %v", gotID, tt.wantID)
			}
		})
	}
}

func TestSlskdService_EnqueueDownload_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{ invalid json`))
	})))
	defer server.Close()

	cfg := &config.Config{SlskdURL: server.URL, SlskdAPIKey: testAPIKey}
	svc := NewSlskdService(cfg, nil)

	_, err := svc.EnqueueDownload("testuser", "test_song.mp3", 5242880)
	if err == nil {
		t.Error("Expected error for malformed JSON response, got nil")
	}
}

func TestSlskdService_EnqueueDownload_FailedItems(t *testing.T) {
	server := httptest.NewServer(withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enqueued": []interface{}{},
			"failed": []map[string]interface{}{
				{"filename": "test_song.mp3", "message": "user is offline"},
			},
		})
	})))
	defer server.Close()

	cfg := &config.Config{SlskdURL: server.URL, SlskdAPIKey: testAPIKey}
	svc := NewSlskdService(cfg, nil)

	_, err := svc.EnqueueDownload("testuser", "test_song.mp3", 5242880)
	if err == nil {
		t.Error("Expected error for failed download, got nil")
	}
}

func TestSlskdService_EnqueueDownload_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enqueued": []interface{}{},
			"failed":   []interface{}{},
		})
	})))
	defer server.Close()

	cfg := &config.Config{SlskdURL: server.URL, SlskdAPIKey: testAPIKey}
	svc := NewSlskdService(cfg, nil)

	_, err := svc.EnqueueDownload("testuser", "test_song.mp3", 5242880)
	if err == nil {
		t.Error("Expected error for empty enqueue response, got nil")
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

			_, err := svc.EnqueueDownload("username", "filename.mp3", 1024)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnqueueDownload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSlskdServiceGetDownload(t *testing.T) {
	testID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

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
				expectedPath := "/api/v0/transfers/downloads/testuser/" + testID
				if r.URL.Path != expectedPath {
					t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
					http.Error(w, "bad path", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"id":               testID,
					"state":            "Completed, Succeeded",
					"filename":         "Music/test song.mp3",
					"bytesTransferred": 5242880,
				})
			})),
			wantState: "Completed, Succeeded",
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
				SlskdURL:            server.URL,
				SlskdAPIKey:         tt.apiKey,
				DownloadStagingPath: "/downloads",
			}
			svc := NewSlskdService(cfg, nil)

			dl, err := svc.GetDownload("testuser", testID)
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
			if dl.LocalPath == "" {
				t.Error("Expected LocalPath to be computed")
			}
		})
	}
}

func TestDownloadState_IsSucceeded(t *testing.T) {
	tests := []struct {
		state DownloadState
		want  bool
	}{
		{"Completed, Succeeded", true},
		{"Completed, Errored", false},
		{"Completed, Cancelled", false},
		{"InProgress", false},
		{"Queued", false},
	}
	for _, tt := range tests {
		if got := tt.state.IsSucceeded(); got != tt.want {
			t.Errorf("DownloadState(%q).IsSucceeded() = %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestDownloadState_IsFailed(t *testing.T) {
	tests := []struct {
		state DownloadState
		want  bool
	}{
		{"Completed, Errored", true},
		{"Completed, Cancelled", true},
		{"Completed, TimedOut", true},
		{"Completed, Rejected", true},
		{"Completed, Succeeded", false},
		{"InProgress", false},
	}
	for _, tt := range tests {
		if got := tt.state.IsFailed(); got != tt.want {
			t.Errorf("DownloadState(%q).IsFailed() = %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestResolveDownloadPath(t *testing.T) {
	staging := filepath.Join("/app", "downloads")
	cfg := &config.Config{
		SlskdURL:            "http://localhost:5030",
		SlskdAPIKey:         "key",
		DownloadStagingPath: staging,
	}
	svc := NewSlskdService(cfg, nil)

	tests := []struct {
		username string
		filename string
		want     string
	}{
		// slskd keeps only the last directory segment + filename
		{"john", "Music/Artist/Song.mp3", filepath.Join(staging, "Artist", "Song.mp3")},
		{"john", "Music\\Artist\\Song.mp3", filepath.Join(staging, "Artist", "Song.mp3")},
		{"john", "@@john\\Music\\Song.mp3", filepath.Join(staging, "Music", "Song.mp3")},
		// single segment (no parent dir) keeps just the filename
		{"john", "Song.mp3", filepath.Join(staging, "Song.mp3")},
	}
	for _, tt := range tests {
		got := svc.resolveDownloadPath(tt.username, tt.filename)
		if got != tt.want {
			t.Errorf("resolveDownloadPath(%q, %q) = %q, want %q", tt.username, tt.filename, got, tt.want)
		}
	}
}

func TestResolveDownloadPath_Traversal(t *testing.T) {
	staging := filepath.Join("/app", "downloads")
	cfg := &config.Config{
		SlskdURL:            "http://localhost:5030",
		SlskdAPIKey:         "key",
		DownloadStagingPath: staging,
	}
	svc := NewSlskdService(cfg, nil)

	result := svc.resolveDownloadPath("john", "../../etc/passwd")
	parent := filepath.Clean(staging) + string(filepath.Separator)
	if !hasPrefix(result, parent) {
		t.Errorf("Path traversal not blocked: resolveDownloadPath returned %q, expected prefix %q", result, parent)
	}
}

func hasPrefix(path, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}
