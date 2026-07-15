package services

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type AcquisitionHandler struct {
	BaseHandler
	cfg        *config.Config
	slskd      SlskdClient
	mb         *MusicBrainzService
	aid        *AcoustIDService
	ext        *MetadataExtractor
	gonic      GonicClientInterface
	navidrome  NavidromeClientInterface
	discogs    *DiscogsService
	cache      *CacheService
	lyrics     *LyricsService
	transcoder *TranscoderService
	ytdlp      YtdlpClientInterface
}

func NewAcquisitionHandler(db *gorm.DB, cfg *config.Config, slskd SlskdClient, mb *MusicBrainzService, aid *AcoustIDService, ext *MetadataExtractor, gonic GonicClientInterface, navidrome NavidromeClientInterface, discogs *DiscogsService, cache *CacheService, lyrics *LyricsService, transcoder *TranscoderService, ytdlp YtdlpClientInterface) *AcquisitionHandler {
	return &AcquisitionHandler{BaseHandler: BaseHandler{db: db}, cfg: cfg, slskd: slskd, mb: mb, aid: aid, ext: ext, gonic: gonic, navidrome: navidrome, discogs: discogs, cache: cache, lyrics: lyrics, transcoder: transcoder, ytdlp: ytdlp}
}

func (h *AcquisitionHandler) Execute(ctx context.Context, jobID uint64, job database.Job) error {
	h.Log(jobID, "INFO", "Monitoring acquisition progress", nil)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			// Bolt Optimization: Consolidate three count queries into one to reduce polling overhead.
			// Using COUNT(*) FILTER is supported by both PostgreSQL and modern SQLite.
			var stats struct {
				Total     int64
				Completed int64
				Failed    int64
			}
			h.db.Model(&database.JobItem{}).Where("job_id = ?", jobID).
				Select("COUNT(*) as total, " +
					"COUNT(*) FILTER (WHERE status LIKE 'completed%' OR status = 'imported') as completed, " +
					"COUNT(*) FILTER (WHERE status = 'failed') as failed").
				Scan(&stats)

			total, completed, failed := stats.Total, stats.Completed, stats.Failed

			summary := fmt.Sprintf("Progress: %d/%d (Success: %d, Failed: %d)", completed+failed, total, completed, failed)
			h.db.Model(&database.Job{}).Where("id = ?", jobID).Update("summary", summary)

			if completed+failed >= total {
				h.Log(jobID, "OK", fmt.Sprintf("Acquisition finished. %s", summary), nil)

				// Final State
				finalState := "succeeded"
				if failed == total {
					finalState = "failed"
				}
				h.db.Model(&database.Job{}).Where("id = ?", jobID).Update("state", finalState)

				// 2.3 Library Sync Hook (Gonic with Navidrome fallback)
				scanDone := false
				if h.gonic != nil {
					h.Log(jobID, "INFO", "Triggering Gonic scan...", nil)
					if ok, err := h.gonic.TriggerScan(); err != nil || !ok {
						h.Log(jobID, "WARN", fmt.Sprintf("Gonic scan trigger failed: %v", err), nil)
					} else {
						h.Log(jobID, "OK", "Gonic scan triggered", nil)
						scanDone = true
					}
				}
				if !scanDone && h.navidrome != nil {
					if h.gonic != nil {
						h.Log(jobID, "INFO", "Falling back to Navidrome scan...", nil)
					} else {
						h.Log(jobID, "INFO", "Triggering Navidrome scan...", nil)
					}
					if ok, err := h.navidrome.TriggerScan(); err != nil || !ok {
						h.Log(jobID, "WARN", fmt.Sprintf("Navidrome scan trigger failed: %v", err), nil)
					} else {
						h.Log(jobID, "OK", "Navidrome scan triggered", nil)
					}
				}
				return nil
			}
		}
	}
}

// acquisitionPipeline carries state between pipeline stages.
type acquisitionPipeline struct {
	ctx        context.Context
	item       database.JobItem
	job        database.Job
	profile    *database.QualityProfile
	results    []SearchResult
	best       SearchResult
	download   string     // path after download completes
	albumFiles []PeerFile // files found during album-mode browse
}

// ExecuteItem runs the acquisition pipeline for a single job item.
// Stages are named and independently testable:
//  1. loadItemContext  — load job, item, profile from DB
//  2. checkGonicIndex  — skip if already in library
//  3. searchSoulseek   — execute search with profile awareness
//  4. selectBestResult — score and validate best match
//  5. downloadFile     — queue and wait for download
//  6. importAndEnrich  — import to library, enrich metadata
func (h *AcquisitionHandler) ExecuteItem(ctx context.Context, jobID uint64, itemID uint64) error {
	p := &acquisitionPipeline{ctx: ctx}

	if skip, err := h.stageLoadItemContext(p, itemID); err != nil {
		return err
	} else if skip {
		return nil
	}

	h.Log(jobID, "INFO", fmt.Sprintf("Processing: %s", p.item.NormalizedQuery), &itemID)

	if skip, err := h.stageCheckGonicIndex(p); err != nil {
		return err
	} else if skip {
		return nil
	}

	if skip, err := h.stageSearchSoulseek(p); err != nil {
		return err
	} else if skip {
		// Soulseek found nothing — try yt-dlp fallback if source URL exists
		if downloaded, ok := h.stageYtdlpFallback(ctx, p); ok {
			p.download = downloaded
			return h.stageImportAndEnrich(ctx, p)
		}
		return nil
	}

	if skip, err := h.stageSelectBestResult(p); err != nil {
		return err
	} else if skip {
		return nil
	}

	// Album mode: browse peer for full album if track is part of one
	h.stageAlbumBrowse(p)

	if skip, err := h.stageDownloadFile(p); err != nil {
		return err
	} else if skip {
		return nil
	}

	return h.stageImportAndEnrich(ctx, p)
}

// stageLoadItemContext loads the job item, parent job, and quality profile.
func (h *AcquisitionHandler) stageLoadItemContext(p *acquisitionPipeline, itemID uint64) (skip bool, err error) {
	if err := h.db.First(&p.item, itemID).Error; err != nil {
		return false, err
	}
	if err := h.db.First(&p.job, p.item.JobID).Error; err != nil {
		return false, err
	}

	// Load Quality Profile if specified in params
	if p.job.Params != nil {
		var params struct {
			ProfileID string `json:"quality_profile_id"`
		}
		if err := json.Unmarshal(p.job.Params, &params); err == nil && params.ProfileID != "" {
			id, _ := uuid.Parse(params.ProfileID)
			var profile database.QualityProfile
			if err := h.db.First(&profile, "id = ?", id).Error; err == nil {
				p.profile = &profile
			}
		}
	}
	return false, nil
}

// stageCheckGonicIndex checks if the track is already in the library server
// (Gonic or Navidrome). Returns skip=true if found (item already indexed).
func (h *AcquisitionHandler) stageCheckGonicIndex(p *acquisitionPipeline) (skip bool, err error) {
	if h.gonic == nil && h.navidrome == nil {
		return false, nil
	}

	h.Log(p.item.JobID, "INFO", "Checking library index...", &p.item.ID)

	// Try Gonic first
	if h.gonic != nil {
		songs, err := h.gonic.Search3(p.item.NormalizedQuery)
		if err == nil {
			for _, s := range songs {
				if (strings.EqualFold(s.Artist, p.item.Artist) || p.item.Artist == "") &&
					strings.EqualFold(s.Title, p.item.TrackTitle) {
					h.Log(p.item.JobID, "OK", fmt.Sprintf("Found in Gonic (ID: %s). Skipping.", s.ID), &p.item.ID)
					h.db.Model(&p.item).Updates(map[string]interface{}{
						"status":      "completed (already indexed)",
						"finished_at": time.Now(),
					})
					return true, nil
				}
			}
		}
	}

	// Fallback to Navidrome
	if h.navidrome != nil {
		songs, err := h.navidrome.Search3(p.item.NormalizedQuery)
		if err == nil {
			for _, s := range songs {
				if (strings.EqualFold(s.Artist, p.item.Artist) || p.item.Artist == "") &&
					strings.EqualFold(s.Title, p.item.TrackTitle) {
					h.Log(p.item.JobID, "OK", fmt.Sprintf("Found in Navidrome (ID: %s). Skipping.", s.ID), &p.item.ID)
					h.db.Model(&p.item).Updates(map[string]interface{}{
						"status":      "completed (already indexed)",
						"finished_at": time.Now(),
					})
					return true, nil
				}
			}
		}
	}

	return false, nil
}

// stageSearchSoulseek executes a Soulseek search with profile awareness.
func (h *AcquisitionHandler) stageSearchSoulseek(p *acquisitionPipeline) (skip bool, err error) {
	h.Log(p.item.JobID, "INFO", "Searching Soulseek...", &p.item.ID)

	results, err := h.slskd.Search(p.item.NormalizedQuery, 30, p.profile)
	if err != nil || len(results) == 0 {
		h.failItem(p.item.JobID, p.item.ID, "No results found")
		return true, nil
	}

	h.Log(p.item.JobID, "OK", fmt.Sprintf("Found %d results", len(results)), &p.item.ID)
	p.results = results
	return false, nil
}

// stageYtdlpFallback attempts to download via yt-dlp when Soulseek finds nothing.
// Returns (downloadPath, true) on success or ("", false) if not applicable/failed.
func (h *AcquisitionHandler) stageYtdlpFallback(ctx context.Context, p *acquisitionPipeline) (string, bool) {
	if h.ytdlp == nil || p.item.SourceURL == "" {
		return "", false
	}

	if !h.ytdlp.IsYtdlpAvailable() {
		h.Log(p.item.JobID, "DEBUG", "yt-dlp not installed, skipping fallback", &p.item.ID)
		return "", false
	}

	h.Log(p.item.JobID, "INFO", fmt.Sprintf("Trying yt-dlp fallback: %s", p.item.SourceURL), &p.item.ID)

	outputDir := h.cfg.DownloadStagingPath
	if outputDir == "" {
		outputDir = "./downloads"
	}

	audioFormat := "flac"
	if p.profile != nil && p.profile.AllowedFormats != "" {
		first := strings.Split(p.profile.AllowedFormats, ",")[0]
		if f := strings.TrimSpace(first); f != "" {
			audioFormat = strings.ToLower(f)
		}
	}

	downloaded, err := h.ytdlp.DownloadAudio(p.item.SourceURL, outputDir, audioFormat)
	if err != nil {
		h.Log(p.item.JobID, "WARN", fmt.Sprintf("yt-dlp fallback failed: %v", err), &p.item.ID)
		return "", false
	}

	h.Log(p.item.JobID, "OK", fmt.Sprintf("yt-dlp downloaded: %s", filepath.Base(downloaded)), &p.item.ID)

	// Reset the item from failed state since yt-dlp succeeded
	h.db.Model(&p.item).Updates(map[string]interface{}{
		"status":         "downloading",
		"failure_reason": "",
	})

	return downloaded, true
}

// stageSelectBestResult picks the top-scored result and validates it against the profile.
func (h *AcquisitionHandler) stageSelectBestResult(p *acquisitionPipeline) (skip bool, err error) {
	p.best = p.results[0]

	// Check if the best result matches the profile requirements
	if p.profile != nil {
		format := ""
		if dotIndex := strings.LastIndex(p.best.Filename, "."); dotIndex != -1 {
			format = strings.ToLower(p.best.Filename[dotIndex+1:])
		}
		bitrate := 0
		if p.best.Bitrate != nil {
			bitrate = *p.best.Bitrate
		}

		if !p.profile.IsMatch(format, bitrate) {
			h.Log(p.item.JobID, "WARN", fmt.Sprintf("Best result doesn't match profile requirements: %s", p.best.Filename), &p.item.ID)
		}
	}

	h.Log(p.item.JobID, "INFO", fmt.Sprintf("Selected: %s (score: %.1f)", p.best.Filename, p.best.Score), &p.item.ID)
	return false, nil
}

// stageAlbumBrowse attempts to discover the full album from the best result's peer.
// When album tracks are found, it creates additional job items for tracks not already
// queued in this job — enabling "search then browse" album-mode acquisition.
func (h *AcquisitionHandler) stageAlbumBrowse(p *acquisitionPipeline) {
	dir := filepath.Dir(p.best.Filename)
	if dir == "." || dir == "" {
		return
	}

	h.Log(p.item.JobID, "INFO", fmt.Sprintf("Album mode: browsing %s for album contents...", p.best.Username), &p.item.ID)

	files, err := h.slskd.Browse(p.best.Username)
	if err != nil {
		h.Log(p.item.JobID, "DEBUG", fmt.Sprintf("Album browse failed: %v", err), &p.item.ID)
		return
	}

	// Find audio files in the same directory
	audioExts := map[string]bool{".mp3": true, ".flac": true, ".ogg": true, ".m4a": true, ".opus": true, ".wav": true, ".aac": true, ".wma": true}
	var albumTracks []PeerFile
	for _, f := range files {
		if filepath.Dir(f.Filename) == dir {
			ext := strings.ToLower(filepath.Ext(f.Filename))
			if audioExts[ext] {
				albumTracks = append(albumTracks, f)
			}
		}
	}

	if len(albumTracks) <= 1 {
		return
	}

	h.Log(p.item.JobID, "OK", fmt.Sprintf("Album mode: found %d tracks in %s", len(albumTracks), dir), &p.item.ID)
	p.albumFiles = albumTracks

	// Check which album tracks are already queued as job items
	var existingQueries []string
	h.db.Model(&database.JobItem{}).Where("job_id = ?", p.item.JobID).Pluck("normalized_query", &existingQueries)
	existing := make(map[string]bool)
	for _, q := range existingQueries {
		existing[strings.ToLower(q)] = true
	}

	// Get max sequence for new items
	var maxSeq int
	h.db.Model(&database.JobItem{}).Where("job_id = ?", p.item.JobID).Select("COALESCE(MAX(sequence), 0)").Scan(&maxSeq)

	created := 0
	for _, track := range albumTracks {
		base := filepath.Base(track.Filename)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		query := strings.ToLower(name)
		if existing[query] {
			continue
		}

		maxSeq++
		newItem := database.JobItem{
			JobID:           p.item.JobID,
			Status:          "queued",
			NormalizedQuery: name,
			Artist:          p.item.Artist,
			Album:           p.item.Album,
			Sequence:        maxSeq,
			OwnerUserID:     p.item.OwnerUserID,
		}
		if err := h.db.Create(&newItem).Error; err != nil {
			h.Log(p.item.JobID, "WARN", fmt.Sprintf("Failed to create album item: %v", err), &p.item.ID)
			continue
		}
		created++
	}

	if created > 0 {
		h.Log(p.item.JobID, "OK", fmt.Sprintf("Album mode: queued %d additional tracks", created), &p.item.ID)
	}
}

// stageDownloadFile queues the download and waits for completion.
func (h *AcquisitionHandler) stageDownloadFile(p *acquisitionPipeline) (skip bool, err error) {
	downloadID, err := h.slskd.EnqueueDownload(p.best.Username, p.best.Filename, p.best.Size)
	if err != nil {
		h.failItem(p.item.JobID, p.item.ID, fmt.Sprintf("Download enqueue failed: %v", err))
		return true, nil
	}

	h.db.Model(&p.item).Updates(map[string]interface{}{
		"status":            "downloading",
		"slskd_search_id":   "completed",
		"slskd_download_id": downloadID,
	})

	h.Log(p.item.JobID, "INFO", fmt.Sprintf("Download queued (id: %s)", downloadID), &p.item.ID)

	download, err := h.slskd.WaitForDownload(p.ctx, p.best.Username, downloadID, 10*time.Minute)
	if err != nil {
		h.failItem(p.item.JobID, p.item.ID, fmt.Sprintf("Download failed or timed out: %v", err))
		return true, nil
	}

	h.Log(p.item.JobID, "OK", "Download completed", &p.item.ID)
	p.download = download.LocalPath
	return false, nil
}

// stageImportAndEnrich imports the downloaded file and enriches metadata.
func (h *AcquisitionHandler) stageImportAndEnrich(ctx context.Context, p *acquisitionPipeline) error {
	var coverArtSources []string
	if p.profile != nil {
		coverArtSources = parseCoverArtSources(p.profile.CoverArtSources)
	}
	return h.importFile(ctx, p.item.JobID, p.item.ID, p.download, p.item, coverArtSources, p.profile)
}
