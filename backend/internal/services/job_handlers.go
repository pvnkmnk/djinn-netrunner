package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

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
	spotify *SpotifyService
}

func NewSyncHandler(db *gorm.DB, spotify *SpotifyService) *SyncHandler {
	return &SyncHandler{BaseHandler: BaseHandler{db: db}, spotify: spotify}
}

func (h *SyncHandler) Execute(ctx context.Context, jobID uint64, job database.Job) error {
	h.Log(jobID, "INFO", "Starting sync job", nil)
	
	var source database.Source
	if err := h.db.First(&source, job.ScopeID).Error; err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Source not found: %v", err), nil)
		return fmt.Errorf("source not found: %w", err)
	}

	h.Log(jobID, "INFO", fmt.Sprintf("Syncing %s: %s", source.SourceType, source.SourceURI), nil)

	var tracks []map[string]string
	var err error

	switch source.SourceType {
	case "spotify_playlist":
		id := h.spotify.ExtractPlaylistID(source.SourceURI)
		tracks, err = h.spotify.GetPlaylistTracks(ctx, id)
	default:
		h.Log(jobID, "ERR", fmt.Sprintf("Unsupported source type: %s", source.SourceType), nil)
		return fmt.Errorf("unsupported source type: %s", source.SourceType)
	}

	if err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Source parsing failed: %v", err), nil)
		return err
	}

	h.Log(jobID, "OK", fmt.Sprintf("Found %d tracks", len(tracks)), nil)

	// Create acquisition job
	params, _ := json.Marshal(map[string]interface{}{"parent_job_id": jobID})
	acqJob := database.Job{
		Type:        "acquisition",
		State:       "queued",
		ScopeType:   "source",
		ScopeID:     fmt.Sprintf("%d", source.ID),
		RequestedAt: time.Now(),
		OwnerUserID: source.OwnerUserID,
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
			Status:          "queued",
			OwnerUserID:     source.OwnerUserID,
		}
		h.db.Create(&item)
	}

	// Update source last_synced_at
	now := time.Now()
	h.db.Model(&source).Update("last_synced_at", &now)

	return nil
}

type AcquisitionHandler struct {
	BaseHandler
	slskd *SlskdService
	ext   *MetadataExtractor
}

func NewAcquisitionHandler(db *gorm.DB, slskd *SlskdService, ext *MetadataExtractor) *AcquisitionHandler {
	return &AcquisitionHandler{BaseHandler: BaseHandler{db: db}, slskd: slskd, ext: ext}
}

func (h *AcquisitionHandler) Execute(ctx context.Context, jobID uint64, job database.Job) error {
	return nil
}

func (h *AcquisitionHandler) ExecuteItem(ctx context.Context, jobID uint64, itemID uint64) error {
	var item database.JobItem
	if err := h.db.First(&item, itemID).Error; err != nil {
		return err
	}

	h.Log(jobID, "INFO", fmt.Sprintf("Searching: %s", item.NormalizedQuery), &itemID)

	// 1. Search
	results, err := h.slskd.Search(item.NormalizedQuery, 30)
	if err != nil || len(results) == 0 {
		h.failItem(jobID, itemID, "No results found")
		return nil
	}

	h.Log(jobID, "OK", fmt.Sprintf("Found %d results", len(results)), &itemID)
	best := results[0]
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
	h.db.Model(&database.JobItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
		"status":         "failed",
		"failure_reason": reason,
		"finished_at":    time.Now(),
	})
}
