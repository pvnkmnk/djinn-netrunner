package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

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

	if job.ScopeType != "watchlist" {
		h.Log(jobID, "ERR", fmt.Sprintf("Unsupported scope type: %s", job.ScopeType), nil)
		return fmt.Errorf("unsupported scope type: %s", job.ScopeType)
	}

	// Syncing a specific watchlist
	id, err := uuid.Parse(job.ScopeID)
	if err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Invalid watchlist ID: %v", err), nil)
		return err
	}

	watchlist, err := h.watchlist.GetWatchlist(id)
	if err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Watchlist not found: %v", err), nil)
		return err
	}
	ownerUserID = watchlist.OwnerUserID
	profileID = &watchlist.QualityProfileID

	h.Log(jobID, "INFO", fmt.Sprintf("Syncing watchlist: %s", watchlist.Name), nil)

	// Load sp_dc cookie from DB if the watchlist uses a Spotify source type.
	// SetActiveUser scopes all subsequent sp_dc operations to this user,
	// isolating per-user tokens from concurrent syncs.
	if isSpotifySourceType(watchlist.SourceType) && watchlist.OwnerUserID != nil {
		if spdc := h.watchlist.GetSpDcAuth(); spdc != nil {
			spdc.SetActiveUser(*watchlist.OwnerUserID)
			var spotifyToken database.SpotifyToken
			if err := h.db.Where("user_id = ?", *watchlist.OwnerUserID).First(&spotifyToken).Error; err == nil && spotifyToken.SpDcCookie != "" {
				spdc.SetSpDcCookie(spotifyToken.SpDcCookie)
			}
		}
	}

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

	if err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Source parsing failed: %v", err), nil)
		return err
	}

	if len(tracks) == 0 {
		h.Log(jobID, "OK", "No new tracks found", nil)
		if err := h.watchlist.UpdateLastSynced(id, snapshotID); err != nil {
			h.Log(jobID, "WARN", fmt.Sprintf("Failed to update last synced marker: %v", err), nil)
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
		ScopeType:   "watchlist",
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
	// Bolt Optimization: Batch create job items to reduce database roundtrips
	var jobItems []database.JobItem
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
		jobItems = append(jobItems, item)
	}

	if len(jobItems) > 0 {
		if err := h.db.CreateInBatches(jobItems, 100).Error; err != nil {
			h.Log(jobID, "ERR", fmt.Sprintf("Failed to batch create job items: %v", err), nil)
			return err
		}
	}

	// Update sync status
	if err := h.watchlist.UpdateLastSynced(id, snapshotID); err != nil {
		h.Log(jobID, "WARN", fmt.Sprintf("Failed to update last synced marker: %v", err), nil)
	}

	return nil
}

func isSpotifySourceType(sourceType string) bool {
	return sourceType == "spotify_playlist" ||
		sourceType == "spotify_liked" ||
		sourceType == "spotify_discover"
}
