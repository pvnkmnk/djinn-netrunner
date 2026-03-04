package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type JobHandler interface {
	Execute(ctx context.Context, jobID uint64, data database.Job) error
}

type SyncHandler struct {
	db      *gorm.DB
	spotify *SpotifyService
}

func NewSyncHandler(db *gorm.DB, spotify *SpotifyService) *SyncHandler {
	return &SyncHandler{db: db, spotify: spotify}
}

func (h *SyncHandler) Execute(ctx context.Context, jobID uint64, job database.Job) error {
	log.Printf("[HANDLER] Executing sync job %d", jobID)
	
	var source database.Source
	if err := h.db.First(&source, job.ScopeID).Error; err != nil {
		return fmt.Errorf("source not found: %w", err)
	}

	var tracks []map[string]string
	var err error

	switch source.SourceType {
	case "spotify_playlist":
		id := h.spotify.ExtractPlaylistID(source.SourceURI)
		tracks, err = h.spotify.GetPlaylistTracks(ctx, id)
	default:
		return fmt.Errorf("unsupported source type: %s", source.SourceType)
	}

	if err != nil {
		return err
	}

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
		return err
	}

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

	return nil
}

type AcquisitionHandler struct {
	db    *gorm.DB
	slskd *SlskdService
	ext   *MetadataExtractor
}

func NewAcquisitionHandler(db *gorm.DB, slskd *SlskdService, ext *MetadataExtractor) *AcquisitionHandler {
	return &AcquisitionHandler{db: db, slskd: slskd, ext: ext}
}

func (h *AcquisitionHandler) Execute(ctx context.Context, jobID uint64, job database.Job) error {
	// Monolithic execution of acquisition is handled item-by-item in WorkerOrchestrator
	// This Execute method could be used for overall job orchestration if needed.
	return nil
}

func (h *AcquisitionHandler) ExecuteItem(ctx context.Context, jobID uint64, itemID uint64) error {
	var item database.JobItem
	if err := h.db.First(&item, itemID).Error; err != nil {
		return err
	}

	log.Printf("[HANDLER] Processing item %d: %s", itemID, item.NormalizedQuery)

	// 1. Search
	results, err := h.slskd.Search(item.NormalizedQuery, 30)
	if err != nil || len(results) == 0 {
		h.failItem(itemID, "No results found")
		return nil
	}

	best := results[0]
	
	// 2. Download
	h.db.Model(&item).Updates(map[string]interface{}{
		"status":            "downloading",
		"slskd_search_id":   "completed",
		"slskd_download_id": fmt.Sprintf("%s:%s", best.Username, best.Filename),
	})

	downloadID, err := h.slskd.EnqueueDownload(best.Username, best.Filename)
	if err != nil {
		h.failItem(itemID, fmt.Sprintf("Download failed: %v", err))
		return nil
	}

	log.Printf("[HANDLER] Download enqueued: %s", downloadID)

	// In a real worker, we'd wait for completion or poll.
	// For this stub, we'll mark as imported if download completes.
	
	return nil
}

func (h *AcquisitionHandler) failItem(itemID uint64, reason string) {
	h.db.Model(&database.JobItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
		"status":         "failed",
		"failure_reason": reason,
		"finished_at":    time.Now(),
	})
}
