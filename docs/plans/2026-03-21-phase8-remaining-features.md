# Phase 8 Implementation Plan — Integration Tests, Cover Art Quality, UI Polish, MCP Expansion, Disk Quota, AcoustID

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement 6 remaining items from TODO.md: integration tests for slskd/providers/webhook, cover art quality improvements, watchlist preview UI enhancements, 4 missing MCP tools, disk quota monitoring, and AcoustID fingerprint storage.

**Architecture:**
- Integration tests use `httptest.NewServer` with `http.HandlerFunc` closures, following the established pattern from `slskd_service_test.go`
- Cover art quality: add source priority config, image size validation, cache cover art images, improve Discogs source quality
- Watchlist preview UI: return total count from handler, add source-type color badges, update CSS
- MCP tools: register existing handlers as MCP tools, create 2 new handlers for cancel/retry
- Disk quota: calculate per-library usage from Track.FileSize, configurable limits, webhook alerts
- AcoustID: store fingerprints in DB, add to Track model, add `Fingerprint` field

**Tech Stack:** Go 1.25+, httptest, GORM, Fiber, gcottom/audiometa, bogem/id3v2, go-flac

---

## Phase 1: Integration Tests

**Context:** 42 test files exist with strong httptest patterns. Gaps: `DiscogsService`, `NotificationService`, `SpotifyProvider`. Existing provider tests (LastFM, ListenBrainz, RSS, Discogs, FileWatchlist) already use `httptest.NewServer`.

### Task 1: Write NotificationService integration test

**Files:**
- Create: `backend/internal/services/notification_service_test.go`

**Step 1: Read existing notification service**

`backend/internal/services/notification_service.go`

**Step 2: Write the test file**

```go
package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotificationService_NotifyJobCompletion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var receivedPayload JobCompletionPayload
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			}
			if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
				t.Errorf("failed to decode payload: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		svc := NewNotificationService(server.URL, true)
		svc.NotifyJobCompletion(42, "sync", "succeeded", "Completed", "worker-1")

		if receivedPayload.JobID != 42 {
			t.Errorf("expected JobID 42, got %d", receivedPayload.JobID)
		}
		if receivedPayload.Type != "sync" {
			t.Errorf("expected Type sync, got %s", receivedPayload.Type)
		}
		if receivedPayload.State != "succeeded" {
			t.Errorf("expected State succeeded, got %s", receivedPayload.State)
		}
		if receivedPayload.WorkerID != "worker-1" {
			t.Errorf("expected WorkerID worker-1, got %s", receivedPayload.WorkerID)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// Should not panic, just log error
		svc := NewNotificationService(server.URL, true)
		svc.NotifyJobCompletion(1, "sync", "failed", "error", "worker-1")
	})

	t.Run("disabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("server should not be called when disabled")
		}))
		defer server.Close()

		svc := NewNotificationService(server.URL, false)
		svc.NotifyJobCompletion(1, "sync", "succeeded", "", "worker-1")
	})

	t.Run("empty url", func(t *testing.T) {
		svc := NewNotificationService("", true)
		// Should not panic
		svc.NotifyJobCompletion(1, "sync", "succeeded", "", "worker-1")
	})
}
```

**Step 3: Run test to verify it passes**

Run: `cd backend && go test ./internal/services/ -run TestNotificationService -v`
Expected: PASS

**Step 4: Commit**

```bash
cd backend && git add internal/services/notification_service_test.go && git commit -m "test: add NotificationService integration tests with httptest"
```

---

### Task 2: Write DiscogsService integration test

**Files:**
- Create: `backend/internal/services/discogs_service_test.go`

**Step 1: Read existing discogs service**

`backend/internal/services/discogs_service.go` — find `GetCoverArt`, `SearchRelease`, `GetRelease` methods

**Step 2: Write the test file**

```go
package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscogsService_GetCoverArt(t *testing.T) {
	t.Run("success with cover image", func(t *testing.T) {
		var receivedQuery string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				t.Error("expected Authorization header")
			}
			receivedQuery = r.URL.Query().Get("q")

			resp := DiscogsSearchResponse{
				Results: []DiscogsRelease{
					{CoverImage: "https://api.discogs.com/images/release-123.jpg"},
					{CoverImage: ""},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		svc := NewDiscogsService(&config.Config{DiscogsToken: "test-token"})
		// Note: svc.baseURL needs to be settable or use a testable constructor
		// If constructor doesn't allow URL injection, use a simpler approach with
		// a package-level test server URL override pattern
	})

	t.Run("no results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := DiscogsSearchResponse{Results: []DiscogsRelease{}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()
		// Same constructor injection note applies
	})
}
```

**Note:** If `NewDiscogsService` hard-codes the base URL, refactor to accept a `baseURL` parameter or use an environment variable override pattern similar to other services. Check `discogs_service.go:18` — if it hardcodes `"https://api.discogs.com"`, extract it as a field that can be overridden for tests.

**Step 3: Run test to verify it passes**

**Step 4: Commit**

---

### Task 3: Write SpotifyProvider httptest integration

**Files:**
- Create: `backend/internal/services/spotify_provider_test.go`

**Context:** `SpotifyProvider` is used by `WatchlistService` but has no tests because it needs OAuth token mocking. The existing `SpotifyService` uses client credentials (M2M). The `SpotifyProvider` uses the `SpotifyClientProvider` interface for track fetching.

**Step 1: Read the SpotifyProvider and its interface**

`backend/internal/services/spotify_provider.go` and `backend/internal/services/spotify_service.go` (for the interface)

**Step 2: Write a test with a mock SpotifyClientProvider**

The test should mock the `SpotifyClientProvider` interface to return fake tracks, verifying the provider correctly transforms them into the `[]map[string]string` format.

**Step 3: Run and commit**

---

### Task 4: Write slskd integration test for EnqueueDownload error path

**Files:**
- Modify: `backend/internal/services/slskd_service_test.go`

**Context:** Phase 7 added httptest coverage but the "unauthorized" test for `EnqueueDownload` was noted as needing more coverage. Add a table-driven test for all HTTP error status codes (400, 403, 404, 429, 500) to verify graceful error handling.

**Step 1: Read current slskd_service_test.go**

**Step 2: Add error status code table test**

```go
func TestSlskdService_EnqueueDownload_ErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"bad request", 400, true},
		{"forbidden", 403, true},
		{"not found", 404, false}, // 404 is not an error for enqueue
		{"rate limited", 429, true},
		{"server error", 500, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			cfg := &config.Config{SlskdURL: server.URL, SlskdAPIKey: testAPIKey}
			svc := NewSlskdService(cfg)

			_, err := svc.EnqueueDownload(context.Background(), "username", "filename.mp3")
			if (err != nil) != tt.wantErr {
				t.Errorf("EnqueueDownload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

**Step 3: Run and commit**

---

### Task 5: Write webhook end-to-end integration test

**Files:**
- Create: `backend/internal/services/webhook_integration_test.go`

**Context:** Test the full webhook flow: job completes → `NotificationService.NotifyJobCompletion` → HTTP POST to server → server receives correct JSON payload.

```go
func TestWebhookEndToEnd(t *testing.T) {
	var received bool
	var payload JobCompletionPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	svc := NewNotificationService(server.URL, true)

	tests := []struct {
		name      string
		jobType   string
		state     string
		summary   string
		workerID  string
	}{
		{"sync success", "sync", "succeeded", "10 tracks acquired", "worker-1"},
		{"scan success", "scan", "succeeded", "500 tracks indexed", "worker-2"},
		{"sync failure", "sync", "failed", "slskd connection timeout", "worker-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			received = false
			jobID := uint64(100 + len(tests))

			svc.NotifyJobCompletion(jobID, tt.jobType, tt.state, tt.summary, tt.workerID)

			if !received {
				t.Error("webhook server was not called")
			}
			if payload.JobID != jobID {
				t.Errorf("expected JobID %d, got %d", jobID, payload.JobID)
			}
		})
	}
}
```

---

## Phase 2: Cover Art Quality Improvements

**Context:** Current fallback chain (provider URL → MusicBrainz → Discogs) has no quality control, no image caching, and no MIME type validation. Discogs returns low-res preview images.

### Task 6: Add cover art source priority to QualityProfile

**Files:**
- Modify: `backend/internal/database/models.go` — add `CoverArtSources` field to `QualityProfile`
- Modify: `backend/internal/api/profiles.go` — accept new field in CRUD
- Modify: `backend/internal/services/job_handlers.go` — use priority ordering in `getCoverArtWithFallback`
- Modify: `ops/web/templates/partials/profiles.html` — add UI for source priority

**Step 1: Read QualityProfile model**

`backend/internal/database/models.go:36-49`

**Step 2: Add CoverArtSources field**

```go
// CoverArtSources is a comma-separated priority list: "musicbrainz,discogs,source"
CoverArtSources string
```

**Step 3: Modify getCoverArtWithFallback to respect priority**

Read `job_handlers.go` — find `getCoverArtWithFallback` function. Replace the hardcoded 3-step chain with a configurable priority list derived from the job's watchlist quality profile.

**Step 4: Commit**

```bash
git add backend/internal/database/models.go backend/internal/api/profiles.go backend/internal/services/job_handlers.go && git commit -m "feat: add configurable cover art source priority per quality profile"
```

---

### Task 7: Add image size validation and format conversion

**Files:**
- Modify: `backend/internal/services/metadata_extractor.go` — validate image size before embedding

**Context:** Embedding a 5MB JPEG as cover art is wasteful. Add validation to skip images below a minimum size threshold (2KB) and log quality warnings.

**Step 1: Read metadata_extractor.go**

Find `EmbedCoverArt` function and the per-format embedding helpers.

**Step 2: Add size validation**

```go
// MinimumCoverArtSize is the minimum byte size for a valid cover art image.
const MinimumCoverArtSize = 2048 // 2KB

func (e *MetadataExtractor) EmbedCoverArt(filePath string, artData []byte) error {
	if len(artData) < MinimumCoverArtSize {
		return fmt.Errorf("cover art image too small (%d bytes), likely invalid", len(artData))
	}
	// ... existing switch on file extension
}
```

**Step 3: Add JPEG format normalization**

MP3 and FLAC embedding hardcode `image/jpeg` MIME type. Add format detection from image magic bytes before embedding.

```go
func detectImageMimeType(data []byte) string {
	if len(data) < 4 {
		return "image/jpeg" // default
	}
	switch {
	case data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47:
		return "image/png"
	case data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46:
		return "image/gif"
	case data[0] == 0x52 && data[1] == 0x49 && bytes.HasPrefix(data[2:], []byte("WEBP")):
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
```

Update `embedMP3` and `embedFLAC` to use detected MIME type instead of hardcoded `"image/jpeg"`.

**Step 4: Commit**

---

### Task 8: Cache cover art images in CacheService

**Files:**
- Modify: `backend/internal/services/job_handlers.go` — cache fetched cover art images

**Context:** The `CacheService` already exists and is used for API responses. Cache the actual cover art image bytes using a hash of `(artist+album+source)` as the cache key.

**Step 1: Read CacheService interface**

`backend/internal/services/cache_service.go` — find `Get`/`Set` signatures for byte data.

**Step 2: Modify getCoverArtWithFallback to use cache**

```go
// Cache cover art images for 168 hours (same as MusicBrainz cache TTL)
const coverArtCacheTTL = 168 * time.Hour
const coverArtCacheNamespace = "coverart"

// In getCoverArtWithFallback:
cacheKey := fmt.Sprintf("%s:%s:%s", artist, album, source)
if h.cache != nil {
	if found, _ := h.cache.GetBytes(coverArtCacheNamespace, cacheKey); found {
		return data, nil
	}
}
// ... after successful fetch:
h.cache.SetBytes(coverArtCacheNamespace, cacheKey, artData, coverArtCacheTTL)
```

**Step 3: Commit**

---

## Phase 3: Watchlist Preview UI Improvements

**Context:** Current preview shows 10 tracks with no count, no source badge, no total. The handler discards the total count from `FetchWatchlistTracks`.

### Task 9: Return total track count from preview handler

**Files:**
- Modify: `backend/internal/api/watchlist_preview.go` — return `TotalCount`

**Step 1: Read watchlist_preview.go**

**Step 2: Capture and return total count**

The `FetchWatchlistTracks` returns `(tracks, total, error)`. Currently `total` is discarded with `_`. Change the return to expose it:

```go
type PreviewData struct {
	Tracks      []PreviewTrack
	TotalCount  int
	WatchlistID uuid.UUID
	HasMore     bool
}

func (h *WatchlistPreviewHandler) GetPreview(c *fiber.Ctx) error {
	// ...
	tracks, total, err := h.watchlistService.FetchWatchlistTracks(c.Context(), watchlist)
	// ...
	return c.Render("partials/watchlist-preview", fiber.Map{
		"Tracks":      items,
		"TotalCount":  total,
		"WatchlistID": id,
		"HasMore":     len(items) >= previewLimit && total > previewLimit,
	})
}
```

**Step 3: Commit**

---

### Task 10: Add source badges and count display to preview template

**Files:**
- Modify: `ops/web/templates/partials/watchlist-preview.html`
- Modify: `ops/web/static/css/styles.css`

**Step 1: Read current watchlist-preview.html**

**Step 2: Update template with count and source badge**

```html
<div class="preview-header">
    <span class="preview-count">{{ len .Tracks }} of {{ .TotalCount }} tracks</span>
    <span class="badge badge-source badge-{{ .SourceType }}">{{ .SourceType }}</span>
</div>
{{ range .Tracks }}
<div class="preview-track">
    {{ if .CoverURL }}<span class="track-cover" style="background-image: url({{ .CoverURL }})"></span>{{ end }}
    <div class="track-info">
        <span class="track-title">{{ .Title }}</span>
        <span class="track-artist">{{ .Artist }}</span>
        {{ if .Album }}<span class="track-album">{{ .Album }}</span>{{ end }}
    </div>
</div>
{{ end }}
{{ if .HasMore }}
<div class="more-indicator">+ more tracks — sync to see full list</div>
{{ end }}
```

**Step 3: Add source badge CSS variants**

In `styles.css`, add:

```css
.badge-source {
    font-size: 0.65rem;
    padding: 2px 6px;
    border-radius: 4px;
    text-transform: capitalize;
}
.badge-source.badge-spotify_playlist,
.badge-source.badge-spotify_liked  { background: #1DB954; color: white; }
.badge-source.badge-lastfm_loved,
.badge-source.badge-lastfm_top     { background: #d51007; color: white; }
.badge-source.badge-rss_feed        { background: #f26522; color: white; }
.badge-source.badge-discogs_wantlist { background: #1A1A1A; color: white; }
.badge-source.badge-listenbrainz_listens { background: #6d4c7d; color: white; }
.badge-source.badge-local_file,
.badge-source.badge-local_directory { background: #555; color: white; }
```

**Step 4: Commit**

---

## Phase 4: MCP Tool Expansion

**Context:** `ScanLibrary` and `AddLibrary` handlers exist. `CancelJob` and `RetryJob` need to be created. `ListMonitoredArtists` needs a new handler.

### Task 11: Register ScanLibrary and AddLibrary as MCP tools

**Files:**
- Modify: `backend/cmd/agent/main.go` — add `scan_library` and `add_library` tool registrations

**Step 1: Read existing sync_watchlist registration for pattern**

`backend/cmd/agent/main.go:145-166`

**Step 2: Register scan_library**

```go
s.AddTool(mcp.NewTool("scan_library",
	mcp.WithDescription("Trigger a library scan job to index local music files"),
	mcp.WithString("library_id", mcp.Description("The UUID of the library to scan"), mcp.Required()),
), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	libIDStr := mcp.ParseString(request, "library_id", "")
	if libIDStr == "" {
		return mcp.NewToolResultError("Missing required 'library_id' argument"), nil
	}
	libID, err := uuid.Parse(libIDStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid library_id UUID: %v", err)), nil
	}
	job, err := agent.ScanLibrary(db, libID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to scan library: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Scan job #%d queued for library %s.", job.ID, libIDStr)), nil
})
```

**Step 3: Register add_library**

```go
s.AddTool(mcp.NewTool("add_library",
	mcp.WithDescription("Register a new music library directory for scanning"),
	mcp.WithString("name", mcp.Description("Display name for the library"), mcp.Required()),
	mcp.WithString("path", mcp.Description("Absolute filesystem path to the library directory"), mcp.Required()),
), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(request, "name", "")
	path := mcp.ParseString(request, "path", "")
	if name == "" || path == "" {
		return mcp.NewToolResultError("Missing required 'name' or 'path' argument"), nil
	}
	lib, err := agent.AddLibrary(db, name, path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to add library: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Library '%s' registered at %s (ID: %s).", name, path, lib.ID.String())), nil
})
```

**Step 4: Commit**

---

### Task 12: Create ListMonitoredArtists handler and MCP tool

**Files:**
- Create: handler function in `backend/internal/agent/handlers.go`
- Modify: `backend/cmd/agent/main.go` — register `list_monitored_artists` tool

**Step 1: Read ScannerService for GetMonitoredArtists**

`backend/internal/services/scanner_service.go:144`

**Step 2: Add handler in handlers.go**

```go
func ListMonitoredArtists(db *gorm.DB) ([]map[string]interface{}, error) {
	var artists []database.MonitoredArtist
	if err := db.Preload("AcquiredReleases").Find(&artists).Error; err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	for _, a := range artists {
		result = append(result, map[string]interface{}{
			"id":                 a.ID.String(),
			"name":               a.ArtistName,
			"mbid":               a.MBID.String(),
			"total_releases":     a.TotalReleases,
			"acquired_releases":  a.AcquiredReleases,
			"last_checked_at":    a.LastCheckedAt,
			"last_found_at":      a.LastFoundAt,
		})
	}
	return result, nil
}
```

**Step 3: Register MCP tool**

```go
s.AddTool(mcp.NewTool("list_monitored_artists",
	mcp.WithDescription("List all monitored artists with their release counts"),
), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	artists, err := agent.ListMonitoredArtists(db)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list artists: %v", err)), nil
	}
	if len(artists) == 0 {
		return mcp.NewToolResultText("No monitored artists."), nil
	}
	out := "Monitored Artists:\n"
	for _, a := range artists {
		out += fmt.Sprintf("- %s (MBID: %s): %d acquired / %d total\n",
			a["name"], a["mbid"], len(a["acquired_releases"].([]database.TrackedRelease)), a["total_releases"])
	}
	return mcp.NewToolResultText(out), nil
})
```

**Step 4: Commit**

---

### Task 13: Create CancelJob handler and MCP tool

**Files:**
- Create: `backend/internal/agent/cancel_job.go` — new handler file
- Modify: `backend/cmd/agent/main.go` — register `cancel_job` tool

**Step 1: Read Job model for state transitions**

`backend/internal/database/models.go` — Job struct, state field

**Step 2: Create cancel_job handler**

```go
func CancelJob(db *gorm.DB, jobID uint64) error {
	result := db.Model(&database.Job{}).
		Where("id = ? AND state IN ?", jobID, []string{"queued", "running"}).
		Update("state", "cancelled")
	if result.RowsAffected == 0 {
		return fmt.Errorf("job %d not found or cannot be cancelled (state must be queued or running)", jobID)
	}
	// Also cancel any pending job items
	db.Model(&database.JobItem{}).
		Where("job_id = ? AND state IN ?", jobID, []string{"queued", "running"}).
		Update("state", "cancelled")
	return nil
}
```

**Step 3: Register MCP tool**

```go
s.AddTool(mcp.NewTool("cancel_job",
	mcp.WithDescription("Cancel a queued or running job"),
	mcp.WithString("job_id", mcp.Description("The numeric ID of the job to cancel"), mcp.Required()),
), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	idStr := mcp.ParseString(request, "job_id", "")
	if idStr == "" {
		return mcp.NewToolResultError("Missing required 'job_id' argument"), nil
	}
	jobID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid job_id: %v", err)), nil
	}
	if err := agent.CancelJob(db, jobID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Job #%d cancelled.", jobID)), nil
})
```

**Step 4: Commit**

---

### Task 14: Create RetryJob handler and MCP tool

**Files:**
- Create: `backend/internal/agent/retry_job.go` — new handler file
- Modify: `backend/cmd/agent/main.go` — register `retry_job` tool

**Step 1: Read job_handlers.go for requeue patterns**

Find how failed job items are reset. The reaper logic in `cmd/worker/main.go:405-409` shows how to requeue.

**Step 2: Create retry_job handler**

```go
func RetryJob(db *gorm.DB, jobID uint64) error {
	var job database.Job
	if err := db.First(&job, "id = ?", jobID).Error; err != nil {
		return fmt.Errorf("job %d not found", jobID)
	}
	if job.State != "failed" {
		return fmt.Errorf("job %d is not in failed state (current: %s)", jobID, job.State)
	}

	// Reset failed items to queued
	db.Model(&database.JobItem{}).
		Where("job_id = ? AND state IN ?", jobID, []string{"failed", "completed (duplicate hash)", "completed (already indexed)"}).
		Update("state", "queued")

	// Reset job to queued
	db.Model(&job).Update("state", "queued")
	return nil
}
```

**Step 3: Register MCP tool**

```go
s.AddTool(mcp.NewTool("retry_job",
	mcp.WithDescription("Retry a failed job by resetting its items to queued"),
	mcp.WithString("job_id", mcp.Description("The numeric ID of the job to retry"), mcp.Required()),
), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	idStr := mcp.ParseString(request, "job_id", "")
	if idStr == "" {
		return mcp.NewToolResultError("Missing required 'job_id' argument"), nil
	}
	jobID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid job_id: %v", err)), nil
	}
	if err := agent.RetryJob(db, jobID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Job #%d requeued for retry.", jobID)), nil
})
```

**Step 4: Commit**

---

## Phase 5: Disk Quota Monitoring

**Context:** Zero disk usage calculation exists. Library paths are configurable but no quota fields exist. No alerting exists for disk space.

### Task 15: Add disk quota fields to Library model and config

**Files:**
- Modify: `backend/internal/database/models.go` — add quota fields to `Library`
- Modify: `backend/internal/config/config.go` — add global defaults
- Modify: `backend/internal/api/libraries.go` — accept quota fields in CRUD

**Step 1: Read Library model**

`backend/internal/database/models.go:130`

**Step 2: Add quota fields**

```go
type Library struct {
	// ... existing fields ...
	MaxSizeBytes *int64 `gorm:"default:null"` // nil = no limit
	QuotaAlertAt *int   `gorm:"default:80"`   // percentage threshold for alerts
}
```

**Step 3: Add config defaults**

In `config.go`:
```go
DefaultLibraryQuotaBytes int64  // default quota per library (0 = no default)
DefaultQuotaAlertAt      int    // default alert threshold percentage
```

**Step 4: Commit**

---

### Task 16: Implement disk usage calculation and quota checking

**Files:**
- Create: `backend/internal/services/disk_quota_service.go`

**Step 1: Create the service**

```go
package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

type DiskQuotaService struct {
	db *sql.DB
}

func NewDiskQuotaService(db *sql.DB) *DiskQuotaService {
	return &DiskQuotaService{db: db}
}

// CalculateLibraryUsage returns the total byte size of all tracks in a library.
func (s *DiskQuotaService) CalculateLibraryUsage(libraryID string) (int64, error) {
	var total int64
	err := s.db.Model(&Track{}).
		Where("library_id = ?", libraryID).
		Select("COALESCE(SUM(file_size), 0)").
		Scan(&total).Error
	return total, err
}

// GetLibraryUsageWithQuota returns usage bytes, limit bytes, and usage percentage.
func (s *DiskQuotaService) GetLibraryUsageWithQuota(libraryID string) (used, limit int64, pct int, err error) {
	used, err = s.CalculateLibraryUsage(libraryID)
	if err != nil {
		return
	}

	var lib Library
	if err = s.db.First(&lib, "id = ?", libraryID).Error; err != nil {
		return
	}

	if lib.MaxSizeBytes != nil {
		limit = *lib.MaxSizeBytes
	} else {
		// Fall back to filesystem-level quota
		limit, _, err = s.GetFilesystemUsage(lib.Path)
		if err != nil {
			return 0, 0, 0, err
		}
	}

	if limit > 0 {
		pct = int(float64(used) / float64(limit) * 100)
	}
	return
}

// GetFilesystemUsage returns total bytes and free bytes for the filesystem containing path.
func (s *DiskQuotaService) GetFilesystemUsage(path string) (total, free int64, err error) {
	// On Windows, use GetDiskFreeSpaceEx
	// On Unix, use statfs / syscall.Statfs
	// Use golang.org/x/sys/windows or golang.org/x/sys/unix
	return 0, 0, fmt.Errorf("implement cross-platform filesystem usage")
}
```

**Step 2: Add filesystem usage for Linux/macOS**

For Unix, use `golang.org/x/sys/unix.Statfs`:

```go
import "golang.org/x/sys/unix"

func (s *DiskQuotaService) GetFilesystemUsage(path string) (total, free int64, err error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	total = int64(stat.Bsize) * int64(stat.Blocks)
	free = int64(stat.Bsize) * int64(stat.Bfree)
	return total, free, nil
}
```

For Windows (if needed), use `golang.org/x/sys/windows.GetDiskFreeSpaceEx`.

**Step 3: Add quota check to scanner**

In `ScannerService.ScanLibrary`, after scanning completes, check quota:

```go
func (s *ScannerService) checkQuotaAlert(libID uuid.UUID) {
	quota, _ := s.diskQuota.GetLibraryUsageWithQuota(libID.String())
	if quota.pct >= quota.limit*quota.QuotaAlertAt/100 {
		log.Printf("[SCANNER] WARNING: Library %s at %d%% quota (%s / %s)",
			libID, quota.pct, formatBytes(quota.used), formatBytes(quota.limit))
	}
}
```

**Step 4: Commit**

---

### Task 17: Wire quota alerts into NotificationService

**Files:**
- Modify: `backend/internal/services/notification_service.go` — add `NotifyQuotaWarning`
- Modify: `backend/cmd/worker/main.go` — call quota check after scan completes

**Step 1: Add quota warning method**

```go
func (s *NotificationService) NotifyQuotaWarning(libraryID, libraryName string, usedPct int, usedBytes, limitBytes int64) {
	if !s.enabled || s.webhookURL == "" {
		return
	}
	payload := map[string]interface{}{
		"type":         "quota_warning",
		"library_id":   libraryID,
		"library_name": libraryName,
		"usage_pct":    usedPct,
		"used_bytes":  usedBytes,
		"limit_bytes": limitBytes,
	}
	// ... POST payload as JSON
}
```

**Step 2: Wire into worker after scan job completes**

In `cmd/worker/main.go`, after a scan job finishes, call the quota check.

**Step 3: Commit**

---

## Phase 6: AcoustID Fingerprint Storage

**Context:** `fpcalc` integration exists in `MetadataExtractor.Fingerprint()`. AcoustID service is wired in the pipeline. Gap: fingerprints are not stored in the database.

### Task 18: Add Fingerprint field to Track model

**Files:**
- Modify: `backend/internal/database/models.go` — add `Fingerprint` field to `Track`
- Modify: `backend/internal/services/scanner_service.go` — store fingerprint from MetadataExtractor

**Step 1: Read Track model**

`backend/internal/database/models.go` — find Track struct

**Step 2: Add Fingerprint field**

```go
Fingerprint string `gorm:"size:128;default:null"` // Chromaprint fingerprint from fpcalc
```

**Step 3: Store fingerprint in scanner**

In `scanner_service.go` → `processFile()`, after extracting metadata, generate fingerprint:

```go
fingerprint, _, err := s.metadata.Fingerprint(path)
if err == nil && fingerprint != "" {
	updates["fingerprint"] = fingerprint
}
```

**Step 4: Add AcoustID field to Acquisition model**

```go
AcoustIDScore float64 `gorm:"default:null"` // AcoustID match confidence (0.0-1.0)
```

**Step 5: Run migration**

The project uses GORM AutoMigrate. Ensure `db.AutoMigrate(&Track{}, &Acquisition{})` covers the new fields.

**Step 6: Commit**

---

### Task 19: Write test for fpcalc availability check

**Files:**
- Modify: `backend/internal/services/metadata_extractor_test.go`

**Context:** `MetadataExtractor.Fingerprint()` calls `fpcalc` via shell. If fpcalc is not installed, it returns an error. Add a test that verifies graceful degradation.

**Step 1: Write test**

```go
func TestMetadataExtractor_Fingerprint_NotInstalled(t *testing.T) {
	// Temporarily modify PATH to remove fpcalc
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	os.Setenv("PATH", "/nonexistent")

	ext := NewMetadataExtractor()
	_, _, err := ext.Fingerprint("testdata/test.mp3")
	if err == nil {
		t.Error("expected error when fpcalc not installed")
	}
}

func TestMetadataExtractor_Fingerprint_Success(t *testing.T) {
	// Skip if fpcalc is not available in CI
	if os.Getenv("CI") == "true" {
		t.Skip("skipping fpcalc test in CI (fpcalc not installed)")
	}

	ext := NewMetadataExtractor()
	fp, dur, err := ext.Fingerprint("testdata/test.mp3")
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if fp == "" {
		t.Error("expected non-empty fingerprint")
	}
	if dur <= 0 {
		t.Error("expected positive duration")
	}
}
```

**Step 2: Commit**

---

## Summary of All Tasks

| # | Task | Files | Type |
|---|------|-------|------|
| 1 | NotificationService httptest | `notification_service_test.go` (new) | Test |
| 2 | DiscogsService httptest | `discogs_service_test.go` (new) | Test |
| 3 | SpotifyProvider httptest | `spotify_provider_test.go` (new) | Test |
| 4 | slskd error status codes | `slskd_service_test.go` (modify) | Test |
| 5 | Webhook E2E integration test | `webhook_integration_test.go` (new) | Test |
| 6 | Cover art source priority | `models.go`, `profiles.go`, `job_handlers.go` | Feature |
| 7 | Image size validation + MIME detection | `metadata_extractor.go` | Feature |
| 8 | Cover art image caching | `job_handlers.go` | Feature |
| 9 | Return total count from preview | `watchlist_preview.go` | Feature |
| 10 | Source badges + count display | `watchlist-preview.html`, `styles.css` | UI |
| 11 | scan_library + add_library MCP tools | `agent/main.go` | Feature |
| 12 | list_monitored_artists handler + MCP | `handlers.go`, `agent/main.go` | Feature |
| 13 | cancel_job handler + MCP | `cancel_job.go`, `agent/main.go` | Feature |
| 14 | retry_job handler + MCP | `retry_job.go`, `agent/main.go` | Feature |
| 15 | Disk quota model + config | `models.go`, `config.go`, `libraries.go` | Feature |
| 16 | DiskQuotaService implementation | `disk_quota_service.go` (new) | Feature |
| 17 | Quota alerts wired to NotificationService | `notification_service.go`, `worker/main.go` | Feature |
| 18 | Fingerprint field on Track + Acquisition | `models.go`, `scanner_service.go` | Feature |
| 19 | fpcalc availability test | `metadata_extractor_test.go` | Test |

**Total: 19 tasks across 6 phases**

**Recommended execution order:** Run Phase 1 (integration tests) first since it validates existing code. Then Phases 2-6 can run in any order with reasonable independence. The UI work (Phase 3 Task 10) depends on Phase 3 Task 9.
