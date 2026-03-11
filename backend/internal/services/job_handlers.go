package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type JobHandler interface {
	Execute(ctx context.Context, jobID uint64, data database.Job) error
}

type BaseHandler struct {
	db *gorm.DB
}

func (h *BaseHandler) Log(jobID uint64, level, message string, itemID *uint64) {
	err := database.AppendJobLog(h.db, jobID, level, message, itemID)
	if err != nil {
		log.Printf("[HANDLER] Failed to append log: %v", err)
	}
}

type SyncHandler struct {
	BaseHandler
	spotify   *SpotifyService
	watchlist *WatchlistService
}

func NewSyncHandler(db *gorm.DB, spotify *SpotifyService, watchlist *WatchlistService) *SyncHandler {
	return &SyncHandler{
		BaseHandler: BaseHandler{db: db},
		spotify:     spotify,
		watchlist:   watchlist,
	}
}

func (h *SyncHandler) Execute(ctx context.Context, jobID uint64, job database.Job) error {
	h.Log(jobID, "INFO", "Starting sync job", nil)

	var tracks []map[string]string
	var err error
	var snapshotID string
	var ownerUserID *uint64
	var profileID *uuid.UUID

	if job.ScopeType == "watchlist" {
		// Syncing a specific watchlist
		id, _ := uuid.Parse(job.ScopeID)
		watchlist, err := h.watchlist.GetWatchlist(id)
		if err != nil {
			h.Log(jobID, "ERR", fmt.Sprintf("Watchlist not found: %v", err), nil)
			return err
		}
		ownerUserID = watchlist.OwnerUserID
		profileID = &watchlist.QualityProfileID

		h.Log(jobID, "INFO", fmt.Sprintf("Syncing watchlist: %s", watchlist.Name), nil)
		currentTracks, snap, err := h.watchlist.FetchWatchlistTracks(ctx, watchlist)
		if err != nil {
			h.Log(jobID, "ERR", fmt.Sprintf("Watchlist fetch failed: %v", err), nil)
			return err
		}
		snapshotID = snap

		// Discovery logic: find new tracks
		tracks = h.watchlist.GetNewTracks(ctx, watchlist, currentTracks)
		
		// Deduplication logic: check library and active queue
		tracks = h.watchlist.FilterExistingTracks(ctx, tracks)

	} else if job.ScopeType == "source" {
		// Legacy Source sync
		var source database.Source
		if err := h.db.First(&source, job.ScopeID).Error; err != nil {
			h.Log(jobID, "ERR", fmt.Sprintf("Source not found: %v", err), nil)
			return fmt.Errorf("source not found: %w", err)
		}
		ownerUserID = source.OwnerUserID

		h.Log(jobID, "INFO", fmt.Sprintf("Syncing %s: %s", source.SourceType, source.SourceURI), nil)

		switch source.SourceType {
		case "spotify_playlist":
			id := h.spotify.ExtractPlaylistID(source.SourceURI)
			tracks, err = h.spotify.GetPlaylistTracks(ctx, id)
		default:
			h.Log(jobID, "ERR", fmt.Sprintf("Unsupported source type: %s", source.SourceType), nil)
			return fmt.Errorf("unsupported source type: %s", source.SourceType)
		}
	}

	if err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Source parsing failed: %v", err), nil)
		return err
	}

	if len(tracks) == 0 {
		h.Log(jobID, "OK", "No new tracks found", nil)
		if job.ScopeType == "watchlist" {
			id, _ := uuid.Parse(job.ScopeID)
			h.watchlist.UpdateLastSynced(id, snapshotID)
		}
		return nil
	}

	h.Log(jobID, "OK", fmt.Sprintf("Found %d tracks to acquire", len(tracks)), nil)

	// Create acquisition job
	paramsMap := map[string]interface{}{"parent_job_id": jobID}
	if profileID != nil {
		paramsMap["quality_profile_id"] = profileID.String()
	}
	params, _ := json.Marshal(paramsMap)

	acqJob := database.Job{
		Type:        "acquisition",
		State:       "queued",
		ScopeType:   job.ScopeType,
		ScopeID:     job.ScopeID,
		RequestedAt: time.Now(),
		OwnerUserID: ownerUserID,
		Params:      params,
		CreatedBy:   "sync_handler",
	}

	if err := h.db.Create(&acqJob).Error; err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Failed to create acquisition job: %v", err), nil)
		return err
	}

	h.Log(jobID, "OK", fmt.Sprintf("Created acquisition job #%d", acqJob.ID), nil)

	// Create job items
	for i, t := range tracks {
		item := database.JobItem{
			JobID:           acqJob.ID,
			Sequence:        i,
			NormalizedQuery: fmt.Sprintf("%s %s", t["artist"], t["title"]),
			Artist:          t["artist"],
			Album:           t["album"],
			TrackTitle:      t["title"],
			CoverArtURL:     t["cover_art_url"],
			Status:          "queued",
			OwnerUserID:     ownerUserID,
		}
		h.db.Create(&item)
	}

	// Update sync status
	if job.ScopeType == "watchlist" {
		id, _ := uuid.Parse(job.ScopeID)
		h.watchlist.UpdateLastSynced(id, snapshotID)
	} else if job.ScopeType == "source" {
		now := time.Now()
		h.db.Model(&database.Source{}).Where("id = ?", job.ScopeID).Update("last_synced_at", &now)
	}

	return nil
}

type AcquisitionHandler struct {
	BaseHandler
	slskd *SlskdService
	ext   *MetadataExtractor
	gonic *GonicClient
}

func NewAcquisitionHandler(db *gorm.DB, slskd *SlskdService, ext *MetadataExtractor, gonic *GonicClient) *AcquisitionHandler {
	return &AcquisitionHandler{BaseHandler: BaseHandler{db: db}, slskd: slskd, ext: ext, gonic: gonic}
}

func (h *AcquisitionHandler) Execute(ctx context.Context, jobID uint64, job database.Job) error {
	return nil
}

func (h *AcquisitionHandler) ExecuteItem(ctx context.Context, jobID uint64, itemID uint64) error {
	var item database.JobItem
	if err := h.db.First(&item, itemID).Error; err != nil {
		return err
	}

	var job database.Job
	if err := h.db.First(&job, item.JobID).Error; err != nil {
		return err
	}

	// 0. Load Quality Profile if specified in params
	var profile *database.QualityProfile
	if job.Params != nil {
		var params struct {
			ProfileID string `json:"quality_profile_id"`
		}
		if err := json.Unmarshal(job.Params, &params); err == nil && params.ProfileID != "" {
			id, _ := uuid.Parse(params.ProfileID)
			var p database.QualityProfile
			if err := h.db.First(&p, "id = ?", id).Error; err == nil {
				profile = &p
			}
		}
	}

	h.Log(jobID, "INFO", fmt.Sprintf("Processing: %s", item.NormalizedQuery), &itemID)

	// 0.5 Pre-flight check with Gonic
	if h.gonic != nil {
		h.Log(jobID, "INFO", "Checking Gonic index...", &itemID)
		songs, err := h.gonic.Search3(item.NormalizedQuery)
		if err == nil && len(songs) > 0 {
			for _, s := range songs {
				// Basic heuristic match
				if (strings.EqualFold(s.Artist, item.Artist) || item.Artist == "") &&
					strings.EqualFold(s.Title, item.TrackTitle) {
					h.Log(jobID, "OK", fmt.Sprintf("Found in Gonic (ID: %s). Skipping.", s.ID), &itemID)
					h.db.Model(&item).Updates(map[string]interface{}{
						"status":      "completed (already indexed)",
						"finished_at": time.Now(),
					})
					return nil
				}
			}
		}
	}

	h.Log(jobID, "INFO", "Searching Soulseek...", &itemID)

	// 1. Search with Profile awareness
	results, err := h.slskd.Search(item.NormalizedQuery, 30, profile)
	if err != nil || len(results) == 0 {
		h.failItem(jobID, itemID, "No results found")
		return nil
	}

	h.Log(jobID, "OK", fmt.Sprintf("Found %d results", len(results)), &itemID)
	best := results[0]
	
	// Check if the best result actually matches the profile (if strictly required)
	if profile != nil {
		format := ""
		if dotIndex := strings.LastIndex(best.Filename, "."); dotIndex != -1 {
			format = strings.ToLower(best.Filename[dotIndex+1:])
		}
		bitrate := 0
		if best.Bitrate != nil {
			bitrate = *best.Bitrate
		}
		
		if !profile.IsMatch(format, bitrate) {
			h.Log(jobID, "WARN", fmt.Sprintf("Best result doesn't match profile requirements: %s", best.Filename), &itemID)
			// For now, we continue but we could fail here if "strict mode" was enabled
		}
	}

	h.Log(jobID, "INFO", fmt.Sprintf("Selected: %s (score: %.1f)", best.Filename, best.Score), &itemID)

	// 2. Queue Download
	h.db.Model(&item).Updates(map[string]interface{}{
		"status":            "downloading",
		"slskd_search_id":   "completed",
		"slskd_download_id": fmt.Sprintf("%s:%s", best.Username, best.Filename),
	})

	_, err = h.slskd.EnqueueDownload(best.Username, best.Filename)
	if err != nil {
		h.failItem(jobID, itemID, fmt.Sprintf("Download enqueue failed: %v", err))
		return nil
	}

	h.Log(jobID, "INFO", "Download queued", &itemID)

	// 3. Wait for completion
	download, err := h.slskd.WaitForDownload(best.Username, best.Filename, 10*time.Minute)
	if err != nil {
		h.failItem(jobID, itemID, fmt.Sprintf("Download failed or timed out: %v", err))
		return nil
	}

	h.Log(jobID, "OK", "Download completed", &itemID)

	// 4. Import
	return h.importFile(jobID, itemID, download.Path, item)
}

func (h *AcquisitionHandler) importFile(jobID uint64, itemID uint64, downloadPath string, item database.JobItem) error {
	h.Log(jobID, "INFO", "Importing to library", &itemID)

	if _, err := os.Stat(downloadPath); os.IsNotExist(err) {
		h.failItem(jobID, itemID, fmt.Sprintf("Downloaded file not found: %s", downloadPath))
		return nil
	}

	// Extract metadata
	metadata, err := h.ext.Extract(downloadPath)
	if err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Metadata extraction failed: %v", err), &itemID)
		// Continue anyway with basic info
		metadata = &AudioMetadata{
			Artist: item.Artist,
			Title:  item.TrackTitle,
			Album:  item.Album,
		}
	}

	// Determine library path (stub for library selection)
	libraryRoot := "./music_library" // Placeholder
	os.MkdirAll(libraryRoot, 0755)

	finalPath := h.ext.GenerateLibraryPath(metadata, libraryRoot)
	os.MkdirAll(filepath.Dir(finalPath), 0755)

	// Move file
	if err := h.moveFile(downloadPath, finalPath); err != nil {
		h.failItem(jobID, itemID, fmt.Sprintf("Failed to move file: %v", err))
		return nil
	}

	// Embed cover art if available
	if item.CoverArtURL != "" {
		h.Log(jobID, "INFO", "Fetching cover art...", &itemID)
		resp, err := http.Get(item.CoverArtURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			artData, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil {
				h.Log(jobID, "INFO", "Embedding cover art...", &itemID)
				if err := h.ext.EmbedCoverArt(finalPath, artData); err != nil {
					h.Log(jobID, "WARN", fmt.Sprintf("Failed to embed cover art: %v", err), &itemID)
				} else {
					h.Log(jobID, "OK", "Cover art embedded successfully", &itemID)
				}
			}
		} else if err != nil {
			h.Log(jobID, "WARN", fmt.Sprintf("Failed to fetch cover art: %v", err), &itemID)
		}
	}

	// Update DB
	h.db.Model(&item).Updates(map[string]interface{}{
		"status":      "imported",
		"finished_at": time.Now(),
		"final_path":  finalPath,
	})

	// Create acquisition record
	acq := database.Acquisition{
		JobID:        jobID,
		JobItemID:    itemID,
		Artist:       metadata.Artist,
		Album:        metadata.Album,
		TrackTitle:   metadata.Title,
		OriginalPath: downloadPath,
		FinalPath:    finalPath,
		FileSize:     metadata.FileSize,
		OwnerUserID:  item.OwnerUserID,
	}
	h.db.Create(&acq)

	h.Log(jobID, "OK", fmt.Sprintf("Imported: %s", finalPath), &itemID)
	return nil
}

func (h *AcquisitionHandler) moveFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return os.Remove(src)
}

func (h *AcquisitionHandler) failItem(jobID uint64, itemID uint64, reason string) {
	h.Log(jobID, "ERR", reason, &itemID)

	var item database.JobItem
	if err := h.db.First(&item, itemID).Error; err != nil {
		log.Printf("[HANDLER] Failed to find item %d for failure update: %v", itemID, err)
		return
	}

	backoff := database.CalculateBackoff(item.RetryCount)
	nextAttempt := time.Now().Add(backoff)

	h.db.Model(&database.JobItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
		"status":          "failed",
		"failure_reason":  reason,
		"retry_count":     item.RetryCount + 1,
		"next_attempt_at": &nextAttempt,
		"finished_at":     time.Now(),
	})
}
