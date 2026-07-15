package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGonicHandler creates an httptest.Server that simulates Gonic/Subsonic responses.
// The server validates token-based auth parameters and returns the provided response data.
func mockGonicHandler(responseData interface{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for required Subsonic auth parameters
		query := r.URL.Query()
		if query.Get("u") == "" {
			http.Error(w, "Missing username", http.StatusUnauthorized)
			return
		}
		if query.Get("t") == "" || query.Get("s") == "" {
			http.Error(w, "Missing token or salt", http.StatusUnauthorized)
			return
		}
		if query.Get("v") != "1.16.1" {
			http.Error(w, "Unsupported protocol version", http.StatusBadRequest)
			return
		}
		if query.Get("f") != "json" {
			http.Error(w, "Only JSON format supported", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responseData)
	}))
}

// TestGonicClient_TriggerScan tests the TriggerScan method.
func TestGonicClient_TriggerScan(t *testing.T) {
	t.Run("success returns true", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		success, err := client.TriggerScan()
		require.NoError(t, err)
		assert.True(t, success)
	})

	t.Run("server error returns false", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "failed",
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		success, err := client.TriggerScan()
		require.NoError(t, err)
		assert.False(t, success)
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.TriggerScan()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gonic api error")
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewGonicClient(serverURL, "testuser", "testpass", nil)

		_, err := client.TriggerScan()
		require.Error(t, err)
	})
}

// TestGonicClient_GetScanStatus tests the GetScanStatus method.
func TestGonicClient_GetScanStatus(t *testing.T) {
	t.Run("success with scanning true", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"scanStatus": map[string]interface{}{
					"scanning": true,
					"count":    1500,
				},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		status, err := client.GetScanStatus()
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, true, status["scanning"])
		assert.Equal(t, 1500, status["count"])
	})

	t.Run("success with scanning false", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"scanStatus": map[string]interface{}{
					"scanning": false,
					"count":    5000,
				},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		status, err := client.GetScanStatus()
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, false, status["scanning"])
		assert.Equal(t, 5000, status["count"])
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.GetScanStatus()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gonic api error")
	})
}

// TestGonicClient_GetLibraryStats tests the GetLibraryStats method.
func TestGonicClient_GetLibraryStats(t *testing.T) {
	t.Run("success with artists", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"artists": map[string]interface{}{
					"index": []map[string]interface{}{
						{
							"artist": []map[string]interface{}{
								{"name": "Radiohead", "albumCount": 10},
								{"name": "Pink Floyd", "albumCount": 15},
							},
						},
						{
							"artist": []map[string]interface{}{
								{"name": "Queens of the Stone Age", "albumCount": 7},
							},
						},
					},
				},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		stats, err := client.GetLibraryStats()
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 3, stats["artist_count"])   // 2 + 1
		assert.Equal(t, 32, stats["album_count"])   // 10 + 15 + 7
	})

	t.Run("empty library", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"artists": map[string]interface{}{
					"index": []interface{}{},
				},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		stats, err := client.GetLibraryStats()
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 0, stats["artist_count"])
		assert.Equal(t, 0, stats["album_count"])
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.GetLibraryStats()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gonic api error")
	})
}

// TestGonicClient_Search3 tests the Search3 method.
func TestGonicClient_Search3_Success(t *testing.T) {
	t.Run("success returns songs", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"searchResult3": map[string]interface{}{
					"song": []map[string]interface{}{
						{"id": "song-1", "title": "Killer Joe", "artist": "Benny Benassi", "album": "Hypnotica", "path": "/music/benny.mp3"},
						{"id": "song-2", "title": "Crying", "artist": "Genesis", "album": "Invisible Touch", "path": "/music/genesis.mp3"},
					},
				},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		songs, err := client.Search3("Killer Joe")
		require.NoError(t, err)
		require.Len(t, songs, 2)
		assert.Equal(t, "song-1", songs[0].ID)
		assert.Equal(t, "Killer Joe", songs[0].Title)
		assert.Equal(t, "Benny Benassi", songs[0].Artist)
		assert.Equal(t, "Hypnotica", songs[0].Album)
		assert.Equal(t, "/music/benny.mp3", songs[0].Path)
	})

	t.Run("empty results", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":        "ok",
				"searchResult3": map[string]interface{}{},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		songs, err := client.Search3("nonexistent")
		require.NoError(t, err)
		assert.Len(t, songs, 0)
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.Search3("test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gonic api error")
	})

	t.Run("malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.Search3("test")
		require.Error(t, err)
	})
}

// TestGonicClient_GetSong tests the GetSong method.
func TestGonicClient_GetSong_Success(t *testing.T) {
	t.Run("success returns song", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"song": map[string]interface{}{
					"id": "song-123", "title": "Killer Joe", "artist": "Benny Benassi", "album": "Hypnotica", "path": "/music/benny/killer.mp3",
				},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		song, err := client.GetSong("song-123")
		require.NoError(t, err)
		require.NotNil(t, song)
		assert.Equal(t, "song-123", song.ID)
		assert.Equal(t, "Killer Joe", song.Title)
		assert.Equal(t, "Benny Benassi", song.Artist)
		assert.Equal(t, "Hypnotica", song.Album)
		assert.Equal(t, "/music/benny/killer.mp3", song.Path)
	})

	t.Run("error status from server", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "failed",
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.GetSong("song-123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subsonic error")
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.GetSong("song-123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gonic api error")
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewGonicClient(serverURL, "testuser", "testpass", nil)

		_, err := client.GetSong("song-123")
		require.Error(t, err)
	})
}

// TestGonicClient_HealthCheck tests the HealthCheck method.
func TestGonicClient_HealthCheck(t *testing.T) {
	t.Run("success returns true", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		assert.True(t, client.HealthCheck())
	})

	t.Run("failed status returns false", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "failed",
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		assert.False(t, client.HealthCheck())
	})

	t.Run("HTTP error returns false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		assert.False(t, client.HealthCheck())
	})

	t.Run("network error returns false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewGonicClient(serverURL, "testuser", "testpass", nil)

		assert.False(t, client.HealthCheck())
	})
}

// TestGonicClient_NewGonicClient tests the constructor.
func TestGonicClient_NewGonicClient(t *testing.T) {
	t.Run("with default HTTP client", func(t *testing.T) {
		client := NewGonicClient("http://localhost:4747", "admin", "admin", nil)
		require.NotNil(t, client)
		assert.Equal(t, "http://localhost:4747/rest", client.baseURL)
		assert.Equal(t, "admin", client.username)
		assert.Equal(t, "admin", client.password)
		assert.NotNil(t, client.client)
	})

	t.Run("with custom HTTP client", func(t *testing.T) {
		customClient := &http.Client{Timeout: 30 * time.Second}
		client := NewGonicClient("http://localhost:4747", "admin", "admin", customClient)
		require.NotNil(t, client)
		assert.Equal(t, customClient, client.client)
	})

	t.Run("URL formatting", func(t *testing.T) {
		client := NewGonicClient("http://localhost:4747", "user", "pass", nil)
		// baseURL should include /rest
		assert.Equal(t, "http://localhost:4747/rest", client.baseURL)
	})

	t.Run("with https URL", func(t *testing.T) {
		client := NewGonicClient("https://gonic.example.com", "user", "pass", nil)
		assert.Equal(t, "https://gonic.example.com/rest", client.baseURL)
	})
}

// TestGonicClient_doRequest_authParams tests that auth parameters are correctly sent.
func TestGonicClient_doRequest_authParams(t *testing.T) {
	var receivedQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
			},
		})
	}))
	defer server.Close()

	client := NewGonicClient(server.URL, "testuser", "testpass", nil)

	// TriggerScan calls doRequest internally
	_, _ = client.TriggerScan()

	require.NotNil(t, receivedQuery)
	assert.Equal(t, "testuser", receivedQuery.Get("u"))
	assert.NotEmpty(t, receivedQuery["t"]) // token
	assert.NotEmpty(t, receivedQuery["s"])  // salt
	assert.Equal(t, "1.16.1", receivedQuery.Get("v"))
	assert.Equal(t, "netrunner", receivedQuery.Get("c"))
	assert.Equal(t, "json", receivedQuery.Get("f"))
}

// TestGonicClient_authFailure tests behavior when auth fails.
func TestGonicClient_doRequest_authFailure(t *testing.T) {
	t.Run("server returns unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always return unauthorized to simulate auth failure
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.TriggerScan()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Unauthorized")
	})

	t.Run("server returns unauthorized for invalid token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			// Return unauthorized if token doesn't match expected pattern
			token := query.Get("t")
			if token == "" || len(token) != 32 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"subsonic-response": map[string]interface{}{
					"status": "ok",
				},
			})
		}))
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		_, err := client.TriggerScan()
		require.NoError(t, err, "client should handle valid auth")
	})
}

// TestGonicClient_responseFormat tests various response formats.
func TestGonicClient_responseFormat(t *testing.T) {
	t.Run("missing optional fields", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"scanStatus": map[string]interface{}{
					// missing scanning and count
				},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		status, err := client.GetScanStatus()
		require.NoError(t, err)
		// Should default to zero values
		assert.Equal(t, false, status["scanning"])
		assert.Equal(t, 0, status["count"])
	})

	t.Run("song with missing optional fields", func(t *testing.T) {
		server := mockGonicHandler(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"song": map[string]interface{}{
					"id": "song-1",
					// missing title, artist, album, path
				},
			},
		})
		defer server.Close()

		client := NewGonicClient(server.URL, "testuser", "testpass", nil)

		song, err := client.GetSong("song-1")
		require.NoError(t, err)
		require.NotNil(t, song)
		assert.Equal(t, "song-1", song.ID)
	})
}

// TestGonicClient_search3_queryParam tests that query param is sent correctly.
func TestGonicClient_search3_queryParam(t *testing.T) {
	var receivedQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"searchResult3": map[string]interface{}{
					"song": []interface{}{},
				},
			},
		})
	}))
	defer server.Close()

	client := NewGonicClient(server.URL, "testuser", "testpass", nil)

	_, _ = client.Search3("Radiohead")

	require.NotNil(t, receivedQuery)
	assert.Equal(t, "Radiohead", receivedQuery.Get("query"))
	assert.Equal(t, "20", receivedQuery.Get("songCount"))
}

// TestGonicClient_getSong_idParam tests that id param is sent correctly.
func TestGonicClient_getSong_idParam(t *testing.T) {
	var receivedQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"song":   map[string]interface{}{},
			},
		})
	}))
	defer server.Close()

	client := NewGonicClient(server.URL, "testuser", "testpass", nil)

	_, _ = client.GetSong("song-456")

	require.NotNil(t, receivedQuery)
	assert.Equal(t, "song-456", receivedQuery.Get("id"))
}

// Test tokenFromPassword generates correct tokens
func TestGonicClient_tokenFromPassword(t *testing.T) {
	// Known test case: password "test" with salt "abcd"
	// md5("test" + "abcd") - let's verify it produces consistent output
	token1 := tokenFromPassword("test", "abcd")
	assert.Len(t, token1, 32) // MD5 produces 32 hex chars

	token2 := tokenFromPassword("test", "abcd")
	assert.Equal(t, token1, token2) // Same input = same output

	// Different salt produces different token
	token3 := tokenFromPassword("test", "xxxx")
	assert.NotEqual(t, token1, token3)
}

// Test salt generates random values
func TestGonicClient_salt(t *testing.T) {
	s1, err := salt()
	require.NoError(t, err)
	assert.Len(t, s1, 8) // 4 bytes = 8 hex chars

	s2, err := salt()
	require.NoError(t, err)
	assert.Len(t, s2, 8)

	// Each salt should be unique (statistically)
	assert.NotEqual(t, s1, s2)
}

// Test that empty password doesn't panic
func TestGonicClient_emptyPassword(t *testing.T) {
	server := mockGonicHandler(map[string]interface{}{
		"subsonic-response": map[string]interface{}{
			"status": "ok",
		},
	})
	defer server.Close()

	client := NewGonicClient(server.URL, "testuser", "", nil)

	// Should not panic
	success, err := client.TriggerScan()
	require.NoError(t, err)
	assert.True(t, success)
}

// Test library stats with deeply nested structure
func TestGonicClient_GetLibraryStats_deepNesting(t *testing.T) {
	server := mockGonicHandler(map[string]interface{}{
		"subsonic-response": map[string]interface{}{
			"status": "ok",
			"artists": map[string]interface{}{
				"index": []map[string]interface{}{
					{
						"artist": []map[string]interface{}{
							{"name": "Artist1", "albumCount": 1},
						},
					},
					{
						"artist": []map[string]interface{}{
							{"name": "Artist2", "albumCount": 2},
							{"name": "Artist3", "albumCount": 3},
						},
					},
				},
			},
		},
	})
	defer server.Close()

	client := NewGonicClient(server.URL, "testuser", "testpass", nil)

	stats, err := client.GetLibraryStats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats["artist_count"])
	assert.Equal(t, 6, stats["album_count"]) // 1 + 2 + 3
}

// Test HTTP 503 Service Unavailable
func TestGonicClient_HTTP503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewGonicClient(server.URL, "testuser", "testpass", nil)

	_, err := client.TriggerScan()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gonic api error")
}

// Test connection refused
func TestGonicClient_ConnectionRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close immediately
	}))
	serverURL := server.URL
	server.Close()

	client := NewGonicClient(serverURL, "testuser", "testpass", nil)

	_, err := client.TriggerScan()
	require.Error(t, err)
	// Connection refused or similar network error
	assert.True(t, strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "ConnectException"))
}
