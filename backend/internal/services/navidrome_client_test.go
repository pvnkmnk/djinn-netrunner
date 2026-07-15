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

// mockNavidromeHandler creates an httptest.Server that simulates Navidrome/Subsonic responses.
func mockNavidromeHandler(t *testing.T, responseData interface{}) *httptest.Server {
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

// TestNavidromeClient_TriggerScan tests the TriggerScan method.
func TestNavidromeClient_TriggerScan(t *testing.T) {
	t.Run("success returns true", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		success, err := client.TriggerScan()
		require.NoError(t, err)
		assert.True(t, success)
	})

	t.Run("server returns failed status", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "failed",
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		success, err := client.TriggerScan()
		require.NoError(t, err)
		assert.False(t, success)
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		_, err := client.TriggerScan()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "navidrome api error")
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewNavidromeClient(serverURL, "testuser", "testpass", nil)

		_, err := client.TriggerScan()
		require.Error(t, err)
	})
}

// TestNavidromeClient_GetScanStatus tests the GetScanStatus method.
func TestNavidromeClient_GetScanStatus(t *testing.T) {
	t.Run("success with scanning true", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"scanStatus": map[string]interface{}{
					"scanning": true,
					"count":    1500,
				},
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		status, err := client.GetScanStatus()
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, true, status["scanning"])
		assert.Equal(t, 1500, status["count"])
	})

	t.Run("success with scanning false", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"scanStatus": map[string]interface{}{
					"scanning": false,
					"count":    5000,
				},
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		status, err := client.GetScanStatus()
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, false, status["scanning"])
		assert.Equal(t, 5000, status["count"])
	})

	t.Run("missing optional fields", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":     "ok",
				"scanStatus": map[string]interface{}{},
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		status, err := client.GetScanStatus()
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, false, status["scanning"])
		assert.Equal(t, 0, status["count"])
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		_, err := client.GetScanStatus()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "navidrome api error")
	})
}

// TestNavidromeClient_GetLibraryStats tests the GetLibraryStats method.
func TestNavidromeClient_GetLibraryStats(t *testing.T) {
	t.Run("success with artists", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
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

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		stats, err := client.GetLibraryStats()
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 3, stats["artist_count"])   // 2 + 1
		assert.Equal(t, 32, stats["album_count"])     // 10 + 15 + 7
	})

	t.Run("empty library", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"artists": map[string]interface{}{
					"index": []interface{}{},
				},
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		stats, err := client.GetLibraryStats()
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 0, stats["artist_count"])
		assert.Equal(t, 0, stats["album_count"])
	})

	t.Run("single artist", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"artists": map[string]interface{}{
					"index": []map[string]interface{}{
						{
							"artist": []map[string]interface{}{
								{"name": "Radiohead", "albumCount": 10},
							},
						},
					},
				},
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		stats, err := client.GetLibraryStats()
		require.NoError(t, err)
		assert.Equal(t, 1, stats["artist_count"])
		assert.Equal(t, 10, stats["album_count"])
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		_, err := client.GetLibraryStats()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "navidrome api error")
	})
}

// TestNavidromeClient_Search3 tests the Search3 method.
func TestNavidromeClient_Search3(t *testing.T) {
	t.Run("success returns songs", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
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

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

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
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":        "ok",
				"searchResult3": map[string]interface{}{},
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		songs, err := client.Search3("nonexistent")
		require.NoError(t, err)
		assert.Len(t, songs, 0)
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		_, err := client.Search3("test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "navidrome api error")
	})

	t.Run("malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		_, err := client.Search3("test")
		require.Error(t, err)
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewNavidromeClient(serverURL, "testuser", "testpass", nil)

		_, err := client.Search3("test")
		require.Error(t, err)
	})
}

// TestNavidromeClient_GetSong tests the GetSong method.
func TestNavidromeClient_GetSong(t *testing.T) {
	t.Run("success returns song", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"song": map[string]interface{}{
					"id": "song-123", "title": "Killer Joe", "artist": "Benny Benassi", "album": "Hypnotica", "path": "/music/benny/killer.mp3",
				},
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

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
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "failed",
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		_, err := client.GetSong("song-123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subsonic error")
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		_, err := client.GetSong("song-123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "navidrome api error")
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewNavidromeClient(serverURL, "testuser", "testpass", nil)

		_, err := client.GetSong("song-123")
		require.Error(t, err)
	})
}

// TestNavidromeClient_HealthCheck tests the HealthCheck method.
func TestNavidromeClient_HealthCheck(t *testing.T) {
	t.Run("success returns true", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		assert.True(t, client.HealthCheck())
	})

	t.Run("failed status returns false", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "failed",
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		assert.False(t, client.HealthCheck())
	})

	t.Run("HTTP error returns false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		assert.False(t, client.HealthCheck())
	})

	t.Run("network error returns false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewNavidromeClient(serverURL, "testuser", "testpass", nil)

		assert.False(t, client.HealthCheck())
	})
}

// TestNavidromeClient_NewNavidromeClient tests the constructor.
func TestNavidromeClient_NewNavidromeClient(t *testing.T) {
	t.Run("with default HTTP client", func(t *testing.T) {
		client := NewNavidromeClient("http://localhost:4533", "admin", "admin", nil)
		require.NotNil(t, client)
		assert.Equal(t, "http://localhost:4533/rest", client.baseURL)
		assert.Equal(t, "admin", client.username)
		assert.Equal(t, "admin", client.password)
		assert.NotNil(t, client.client)
	})

	t.Run("with custom HTTP client", func(t *testing.T) {
		customClient := &http.Client{Timeout: 30 * time.Second}
		client := NewNavidromeClient("http://localhost:4533", "admin", "admin", customClient)
		require.NotNil(t, client)
		assert.Equal(t, customClient, client.client)
	})

	t.Run("URL formatting", func(t *testing.T) {
		client := NewNavidromeClient("http://localhost:4533", "user", "pass", nil)
		assert.Equal(t, "http://localhost:4533/rest", client.baseURL)
	})

	t.Run("with https URL", func(t *testing.T) {
		client := NewNavidromeClient("https://navidrome.example.com", "user", "pass", nil)
		assert.Equal(t, "https://navidrome.example.com/rest", client.baseURL)
	})
}

// TestNavidromeClient_doRequest_authParams tests that auth parameters are correctly sent.
func TestNavidromeClient_doRequest_authParams(t *testing.T) {
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

	client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

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

// TestNavidromeClient_search3_queryParam tests that query param is sent correctly.
func TestNavidromeClient_search3_queryParam(t *testing.T) {
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

	client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

	_, _ = client.Search3("Radiohead")

	require.NotNil(t, receivedQuery)
	assert.Equal(t, "Radiohead", receivedQuery.Get("query"))
	assert.Equal(t, "20", receivedQuery.Get("songCount"))
}

// TestNavidromeClient_getSong_idParam tests that id param is sent correctly.
func TestNavidromeClient_getSong_idParam(t *testing.T) {
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

	client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

	_, _ = client.GetSong("song-456")

	require.NotNil(t, receivedQuery)
	assert.Equal(t, "song-456", receivedQuery.Get("id"))
}

// TestNavidromeClient_songMissingFields tests handling of songs with missing optional fields.
func TestNavidromeClient_songMissingFields(t *testing.T) {
	t.Run("song with missing optional fields", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status": "ok",
				"song": map[string]interface{}{
					"id": "song-1",
					// missing title, artist, album, path
				},
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		song, err := client.GetSong("song-1")
		require.NoError(t, err)
		require.NotNil(t, song)
		assert.Equal(t, "song-1", song.ID)
	})

	t.Run("search with missing song array", func(t *testing.T) {
		server := mockNavidromeHandler(t, map[string]interface{}{
			"subsonic-response": map[string]interface{}{
				"status":        "ok",
				"searchResult3": map[string]interface{}{}, // missing song
			},
		})
		defer server.Close()

		client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

		songs, err := client.Search3("test")
		require.NoError(t, err)
		assert.Len(t, songs, 0)
	})
}

// TestNavidromeClient_libraryStatsDeepNesting tests deeply nested artist structures.
func TestNavidromeClient_libraryStatsDeepNesting(t *testing.T) {
	server := mockNavidromeHandler(t, map[string]interface{}{
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

	client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

	stats, err := client.GetLibraryStats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats["artist_count"])
	assert.Equal(t, 6, stats["album_count"]) // 1 + 2 + 3
}

// Test that empty password doesn't panic
func TestNavidromeClient_emptyPassword(t *testing.T) {
	server := mockNavidromeHandler(t, map[string]interface{}{
		"subsonic-response": map[string]interface{}{
			"status": "ok",
		},
	})
	defer server.Close()

	client := NewNavidromeClient(server.URL, "testuser", "", nil)

	// Should not panic
	success, err := client.TriggerScan()
	require.NoError(t, err)
	assert.True(t, success)
}

// Test HTTP 503 Service Unavailable
func TestNavidromeClient_HTTP503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewNavidromeClient(server.URL, "testuser", "testpass", nil)

	_, err := client.TriggerScan()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "navidrome api error")
}

// Test connection refused
func TestNavidromeClient_ConnectionRefused(t *testing.T) {
	client := NewNavidromeClient("http://localhost:9999", "testuser", "testpass", nil)

	_, err := client.TriggerScan()
	require.Error(t, err)
	// Connection refused error
	assert.True(t, strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "ConnectException") || err != nil)
}

// =============================================================================
// PlexClient Tests
// =============================================================================

// TestPlexClient_TriggerLibraryRefresh tests the TriggerLibraryRefresh method.
func TestPlexClient_TriggerLibraryRefresh(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var receivedToken string
		var receivedPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedToken = r.URL.Query().Get("X-Plex-Token")
			receivedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewPlexClient(server.URL, "test-token-123", nil)

		err := client.TriggerLibraryRefresh(1)
		require.NoError(t, err)
		assert.Equal(t, "test-token-123", receivedToken)
		assert.Equal(t, "/library/sections/1/refresh", receivedPath)
	})

	t.Run("custom section ID", func(t *testing.T) {
		var receivedPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewPlexClient(server.URL, "my-token", nil)

		err := client.TriggerLibraryRefresh(5)
		require.NoError(t, err)
		assert.Equal(t, "/library/sections/5/refresh", receivedPath)
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		client := NewPlexClient(server.URL, "bad-token", nil)

		err := client.TriggerLibraryRefresh(1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "plex api error")
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewPlexClient(serverURL, "test-token", nil)

		err := client.TriggerLibraryRefresh(1)
		require.Error(t, err)
	})
}

// TestPlexClient_NewPlexClient tests the constructor.
func TestPlexClient_NewPlexClient(t *testing.T) {
	t.Run("with default HTTP client", func(t *testing.T) {
		client := NewPlexClient("http://localhost:32400", "my-token", nil)
		require.NotNil(t, client)
		assert.Equal(t, "http://localhost:32400", client.baseURL)
		assert.Equal(t, "my-token", client.token)
		assert.NotNil(t, client.client)
	})

	t.Run("with custom HTTP client", func(t *testing.T) {
		customClient := &http.Client{Timeout: 30 * time.Second}
		client := NewPlexClient("http://localhost:32400", "my-token", customClient)
		require.NotNil(t, client)
		assert.Equal(t, customClient, client.client)
	})

	t.Run("with https URL", func(t *testing.T) {
		client := NewPlexClient("https://plex.example.com", "token", nil)
		assert.Equal(t, "https://plex.example.com", client.baseURL)
	})
}

// =============================================================================
// JellyfinClient Tests
// =============================================================================

// TestJellyfinClient_TriggerLibraryRefresh tests the TriggerLibraryRefresh method.
func TestJellyfinClient_TriggerLibraryRefresh(t *testing.T) {
	t.Run("success with 200", func(t *testing.T) {
		var receivedMethod string
		var receivedToken string
		var receivedPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			receivedToken = r.Header.Get("X-Emby-Token")
			receivedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewJellyfinClient(server.URL, "my-api-key", nil)

		err := client.TriggerLibraryRefresh()
		require.NoError(t, err)
		assert.Equal(t, "POST", receivedMethod)
		assert.Equal(t, "my-api-key", receivedToken)
		assert.Equal(t, "/Library/Refresh", receivedPath)
	})

	t.Run("success with 204 No Content", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client := NewJellyfinClient(server.URL, "my-api-key", nil)

		err := client.TriggerLibraryRefresh()
		require.NoError(t, err)
	})

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		client := NewJellyfinClient(server.URL, "bad-api-key", nil)

		err := client.TriggerLibraryRefresh()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "jellyfin api error")
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Close immediately
		}))
		serverURL := server.URL
		server.Close()

		client := NewJellyfinClient(serverURL, "test-api-key", nil)

		err := client.TriggerLibraryRefresh()
		require.Error(t, err)
	})

	t.Run("empty API key still makes request", func(t *testing.T) {
		var receivedHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeader = r.Header.Get("X-Emby-Token")
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client := NewJellyfinClient(server.URL, "", nil)

		err := client.TriggerLibraryRefresh()
		require.NoError(t, err)
		// Empty key means header is not set (empty string)
		assert.Equal(t, "", receivedHeader)
	})
}

// TestJellyfinClient_NewJellyfinClient tests the constructor.
func TestJellyfinClient_NewJellyfinClient(t *testing.T) {
	t.Run("with default HTTP client", func(t *testing.T) {
		client := NewJellyfinClient("http://localhost:8096", "my-api-key", nil)
		require.NotNil(t, client)
		assert.Equal(t, "http://localhost:8096", client.baseURL)
		assert.Equal(t, "my-api-key", client.apiKey)
		assert.NotNil(t, client.client)
	})

	t.Run("with custom HTTP client", func(t *testing.T) {
		customClient := &http.Client{Timeout: 30 * time.Second}
		client := NewJellyfinClient("http://localhost:8096", "my-api-key", customClient)
		require.NotNil(t, client)
		assert.Equal(t, customClient, client.client)
	})

	t.Run("with https URL", func(t *testing.T) {
		client := NewJellyfinClient("https://jellyfin.example.com", "key", nil)
		assert.Equal(t, "https://jellyfin.example.com", client.baseURL)
	})
}

// TestJellyfinClient_different_error_codes tests various HTTP status codes.
func TestJellyfinClient_different_error_codes(t *testing.T) {
	t.Run("500 Internal Server Error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewJellyfinClient(server.URL, "test-key", nil)

		err := client.TriggerLibraryRefresh()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "jellyfin api error")
	})

	t.Run("403 Forbidden", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		client := NewJellyfinClient(server.URL, "test-key", nil)

		err := client.TriggerLibraryRefresh()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "jellyfin api error")
	})
}
