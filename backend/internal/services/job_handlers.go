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
	"github.com/pvnkmnk/netrunner/backend/internal/config"
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
		if itemID != nil {
			log.Printf("[HANDLER] Failed to append log | job_id=%d | item_id=%d | error=%v", jobID, *itemID, err)
		} else {
			log.Printf("[HANDLER] Failed to append log | job_id=%d | error=%v", jobID, err)
		}
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
		h.watchlist.UpdateLastSynced(id, snapshotID)
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
	h.watchlist.UpdateLastSynced(id, snapshotID)

	return nil
}

type AcquisitionHandler struct {
	BaseHandler
	cfg     *config.Config
	slskd   *SlskdService
	mb      *MusicBrainzService
	aid     *AcoustIDService
	ext     *MetadataExtractor
	gonic   *GonicClient
	discogs *DiscogsService
	cache   *CacheService
}

func NewAcquisitionHandler(db *gorm.DB, cfg *config.Config, slskd *SlskdService, mb *MusicBrainzService, aid *AcoustIDService, ext *MetadataExtractor, gonic *GonicClient, discogs *DiscogsService, cache *CacheService) *AcquisitionHandler {
	return &AcquisitionHandler{BaseHandler: BaseHandler{db: db}, cfg: cfg, slskd: slskd, mb: mb, aid: aid, ext: ext, gonic: gonic, discogs: discogs, cache: cache}
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

				// 2.3 Gonic Sync Hook
				if h.gonic != nil {
					h.Log(jobID, "INFO", "Triggering Gonic scan...", nil)
					if ok, err := h.gonic.TriggerScan(); err != nil || !ok {
						h.Log(jobID, "WARN", fmt.Sprintf("Gonic scan trigger failed: %v", err), nil)
					} else {
						h.Log(jobID, "OK", "Gonic scan triggered", nil)
					}
				}
				return nil
			}
		}
	}
}

// acquisitionPipeline carries state between pipeline stages.
type acquisitionPipeline struct {
	item     database.JobItem
	job      database.Job
	profile  *database.QualityProfile
	results  []SearchResult
	best     SearchResult
	download string // path after download completes
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
	p := &acquisitionPipeline{}

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
		return nil
	}

	if skip, err := h.stageSelectBestResult(p); err != nil {
		return err
	} else if skip {
		return nil
	}

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

// stageCheckGonicIndex checks if the track is already in the Gonic library.
// Returns skip=true if found (item already indexed).
func (h *AcquisitionHandler) stageCheckGonicIndex(p *acquisitionPipeline) (skip bool, err error) {
	if h.gonic == nil {
		return false, nil
	}

	h.Log(p.item.JobID, "INFO", "Checking Gonic index...", &p.item.ID)
	songs, err := h.gonic.Search3(p.item.NormalizedQuery)
	if err != nil || len(songs) == 0 {
		return false, nil // not found, continue pipeline
	}

	for _, s := range songs {
		if (strings.EqualFold(s.Artist, p.item.Artist) || p.item.Artist == "") &&
			strings.EqualFold(s.Title, p.item.TrackTitle) {
			h.Log(p.item.JobID, "OK", fmt.Sprintf("Found in Gonic (ID: %s). Skipping.", s.ID), &p.item.ID)
			h.db.Model(&p.item).Updates(map[string]interface{}{
				"status":      "completed (already indexed)",
				"finished_at": time.Now(),
			})
			return true, nil // skip remaining stages
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

// stageDownloadFile queues the download and waits for completion.
func (h *AcquisitionHandler) stageDownloadFile(p *acquisitionPipeline) (skip bool, err error) {
	h.db.Model(&p.item).Updates(map[string]interface{}{
		"status":            "downloading",
		"slskd_search_id":   "completed",
		"slskd_download_id": fmt.Sprintf("%s:%s", p.best.Username, p.best.Filename),
	})

	_, err = h.slskd.EnqueueDownload(p.best.Username, p.best.Filename)
	if err != nil {
		h.failItem(p.item.JobID, p.item.ID, fmt.Sprintf("Download enqueue failed: %v", err))
		return true, nil
	}

	h.Log(p.item.JobID, "INFO", "Download queued", &p.item.ID)

	download, err := h.slskd.WaitForDownload(p.best.Username, p.best.Filename, 10*time.Minute)
	if err != nil {
		h.failItem(p.item.JobID, p.item.ID, fmt.Sprintf("Download failed or timed out: %v", err))
		return true, nil
	}

	h.Log(p.item.JobID, "OK", "Download completed", &p.item.ID)
	p.download = download.Path
	return false, nil
}

// stageImportAndEnrich imports the downloaded file and enriches metadata.
func (h *AcquisitionHandler) stageImportAndEnrich(ctx context.Context, p *acquisitionPipeline) error {
	var coverArtSources []string
	if p.profile != nil {
		coverArtSources = parseCoverArtSources(p.profile.CoverArtSources)
	}
	return h.importFile(ctx, p.item.JobID, p.item.ID, p.download, p.item, coverArtSources)
}

// Default cover art source priority order
var defaultCoverArtSources = []string{"source", "musicbrainz", "discogs"}

// parseCoverArtSources splits a comma-separated CoverArtSources string into a slice.
// Returns nil (uses default) if the input is empty or invalid.
func parseCoverArtSources(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	sources := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			sources = append(sources, p)
		}
	}
	if len(sources) == 0 {
		return nil
	}
	return sources
}

const coverArtCacheTTL = 168 * time.Hour // 1 week, same as MusicBrainz cache TTL

// coverArtCacheKey generates a cache key for cover art lookups.
func coverArtCacheKey(artist, album, source string) string {
	return fmt.Sprintf("%s:%s:%s", strings.ToLower(artist), strings.ToLower(album), source)
}

// getCoverArtWithFallback attempts to fetch cover art from multiple sources in priority order
func (h *AcquisitionHandler) getCoverArtWithFallback(ctx context.Context, item *database.JobItem, artist, title, album string, sources []string) ([]byte, error) {
	if sources == nil {
		sources = defaultCoverArtSources
	}

	for _, source := range sources {
		// Check cache first
		if h.cache != nil {
			key := coverArtCacheKey(artist, album, source)
			if data, found, err := h.cache.GetBytes("coverart", key); err == nil && found {
				h.Log(item.JobID, "DEBUG", fmt.Sprintf("Cover art cache hit for %s", key), &item.ID)
				return data, nil
			}
		}

		var artData []byte
		var err error

		switch source {
		case "source":
			artData, err = h.fetchCoverFromSourceURL(ctx, item)
		case "musicbrainz":
			artData, err = h.fetchCoverFromMusicBrainz(ctx, item, artist, album)
		case "discogs":
			artData, err = h.fetchCoverFromDiscogs(ctx, item, artist, title)
		}

		if err == nil && len(artData) > 0 {
			// Cache the successful result
			if h.cache != nil {
				key := coverArtCacheKey(artist, album, source)
				_ = h.cache.SetBytes("coverart", key, artData, coverArtCacheTTL)
			}
			return artData, nil
		}
		// Try next source
	}

	return nil, fmt.Errorf("no cover art found from any source")
}

// fetchCoverFromSourceURL extracts the source URL logic from the original function
func (h *AcquisitionHandler) fetchCoverFromSourceURL(ctx context.Context, item *database.JobItem) ([]byte, error) {
	if item.CoverArtURL == "" {
		return nil, fmt.Errorf("no source URL")
	}
	resp, err := SafeGet(item.CoverArtURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source URL returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// fetchCoverFromMusicBrainz extracts the MusicBrainz logic
func (h *AcquisitionHandler) fetchCoverFromMusicBrainz(ctx context.Context, item *database.JobItem, artist, album string) ([]byte, error) {
	if h.mb == nil || (artist == "" && album == "") {
		return nil, fmt.Errorf("no artist/album for MB lookup")
	}
	queryArtist := artist
	if queryArtist == "" && item.Artist != "" {
		queryArtist = item.Artist
	}
	queryAlbum := album
	if queryAlbum == "" && item.Album != "" {
		queryAlbum = item.Album
	}

	release, err := h.mb.GetReleaseByArtistTitle(queryArtist, queryAlbum)
	if err != nil || release == nil {
		return nil, err
	}
	for _, img := range release.Images {
		if img.Front {
			resp, err := SafeGet(img.Image)
			if err != nil {
				continue
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				data, err := io.ReadAll(resp.Body)
				if err == nil && len(data) > 0 {
					h.Log(item.JobID, "INFO", fmt.Sprintf("Cover art from MusicBrainz: %s", img.Image), &item.ID)
					return data, nil
				}
			}
		}
	}
	// Fall back to first image
	if len(release.Images) > 0 {
		resp, err := SafeGet(release.Images[0].Image)
		if err != nil {
			return nil, fmt.Errorf("no front cover from MB")
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(resp.Body)
			if err == nil && len(data) > 0 {
				h.Log(item.JobID, "INFO", fmt.Sprintf("Cover art from MusicBrainz: %s", release.Images[0].Image), &item.ID)
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("no front cover from MB")
}

// fetchCoverFromDiscogs extracts the Discogs logic
func (h *AcquisitionHandler) fetchCoverFromDiscogs(ctx context.Context, item *database.JobItem, artist, title string) ([]byte, error) {
	if h.discogs == nil || artist == "" {
		return nil, fmt.Errorf("no artist for Discogs lookup")
	}
	coverURL, err := h.discogs.GetCoverArt(artist, title)
	if err != nil || coverURL == "" {
		return nil, err
	}
	resp, err := SafeGet(coverURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Discogs returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	h.Log(item.JobID, "INFO", fmt.Sprintf("Cover art from Discogs: %s", coverURL), &item.ID)
	return data, nil
}

func (h *AcquisitionHandler) importFile(ctx context.Context, jobID uint64, itemID uint64, downloadPath string, item database.JobItem, coverArtSources []string) error {
	h.Log(jobID, "INFO", "Importing to library", &itemID)

	if _, err := os.Stat(downloadPath); os.IsNotExist(err) {
		h.failItem(jobID, itemID, fmt.Sprintf("Downloaded file not found: %s", downloadPath))
		return nil
	}

	// 1. Compute Hash for deduplication
	hash, err := h.ext.HashFile(downloadPath)
	if err != nil {
		h.Log(jobID, "WARN", fmt.Sprintf("Failed to compute hash: %v", err), &itemID)
	}

	if hash != "" {
		var existing database.Acquisition
		if err := h.db.Where("file_hash = ?", hash).First(&existing).Error; err == nil {
			h.Log(jobID, "OK", fmt.Sprintf("File already acquired (ID: %d). Skipping.", existing.ID), &itemID)
			h.db.Model(&item).Updates(map[string]interface{}{
				"status":      "completed (duplicate hash)",
				"finished_at": time.Now(),
				"final_path":  existing.FinalPath,
			})
			return nil
		}
	}

	// 2. Extract basic tags
	metadata, err := h.ext.Extract(downloadPath)
	if err != nil {
		h.Log(jobID, "ERR", fmt.Sprintf("Metadata extraction failed: %v", err), &itemID)
		metadata = &AudioMetadata{
			Artist: item.Artist,
			Title:  item.TrackTitle,
			Album:  item.Album,
		}
	}

	// 2.5 Generate Fingerprint
	fingerprint, duration, err := h.ext.Fingerprint(downloadPath)
	if err != nil {
		h.Log(jobID, "WARN", fmt.Sprintf("Fingerprinting failed: %v", err), &itemID)
	}

	// 3. MusicBrainz & AcoustID Enrichment
	var mbIDs struct {
		RecordingID string
		ReleaseID   string
		ArtistID    string
	}
	var acoustidScore int

	if h.aid != nil && fingerprint != "" {
		h.Log(jobID, "INFO", "Looking up AcoustID...", &itemID)
		results, err := h.aid.Lookup(fingerprint, duration)
		if err == nil && len(results) > 0 {
			acoustidScore = int(results[0].Score * 100) // Convert 0-1 float to 0-100 int
			h.Log(jobID, "OK", fmt.Sprintf("AcoustID match found (score: %d%%)", acoustidScore), &itemID)
			if len(results[0].Recordings) > 0 {
				mbIDs.RecordingID = results[0].Recordings[0].ID
				// Try to get artist/release IDs if available in future AcoustID meta enhancements
			}
		}
	}

	if h.mb != nil && (mbIDs.RecordingID != "" || metadata.IsValid()) {
		h.Log(jobID, "INFO", "Enriching with MusicBrainz...", &itemID)

		if mbIDs.RecordingID != "" {
			// Real MBID from AcoustID!
			h.Log(jobID, "OK", fmt.Sprintf("Using canonical Recording ID: %s", mbIDs.RecordingID), &itemID)
		} else {
			// Fallback to search
			query := fmt.Sprintf("recording:%s AND artist:%s", metadata.Title, metadata.Artist)
			recordings, err := h.mb.SearchRecording(query)
			if err == nil && len(recordings) > 0 {
				mbIDs.RecordingID = recordings[0].ID
				mbIDs.ReleaseID = recordings[0].ReleaseID
				h.Log(jobID, "OK", fmt.Sprintf("Found recording via search: %s", mbIDs.RecordingID), &itemID)
			} else if err != nil {
				h.Log(jobID, "WARN", fmt.Sprintf("Recording search failed: %v", err), &itemID)
			}
		}
	}

	// Determine library path
	libraryRoot := h.cfg.MusicLibraryPath
	if libraryRoot == "" {
		libraryRoot = "./music_library"
	}
	os.MkdirAll(libraryRoot, 0755)

	finalPath := h.ext.GenerateLibraryPath(metadata, libraryRoot)

	// Ensure unique path
	if _, err := os.Stat(finalPath); err == nil {
		ext := filepath.Ext(finalPath)
		base := strings.TrimSuffix(finalPath, ext)
		finalPath = fmt.Sprintf("%s_%s%s", base, time.Now().Format("20060102_150405"), ext)
	}
	os.MkdirAll(filepath.Dir(finalPath), 0755)

	// Move file
	if err := h.moveFile(downloadPath, finalPath); err != nil {
		h.failItem(jobID, itemID, fmt.Sprintf("Failed to move file: %v", err))
		return nil
	}

	// Attempt to fetch and embed cover art with fallback chain
	h.Log(jobID, "INFO", "Fetching cover art...", &itemID)
	artData, err := h.getCoverArtWithFallback(ctx, &item, metadata.Artist, metadata.Title, metadata.Album, coverArtSources)
	if err == nil && len(artData) > 0 {
		h.Log(jobID, "INFO", "Embedding cover art...", &itemID)
		if err := h.ext.EmbedCoverArt(finalPath, artData); err != nil {
			h.Log(jobID, "WARN", fmt.Sprintf("Failed to embed cover art: %v", err), &itemID)
		} else {
			h.Log(jobID, "OK", "Cover art embedded successfully", &itemID)
		}
	} else {
		h.Log(jobID, "INFO", "No cover art available", &itemID)
	}

	// Update DB
	h.db.Model(&item).Updates(map[string]interface{}{
		"status":      "imported",
		"finished_at": time.Now(),
		"final_path":  finalPath,
	})

	// Create acquisition record
	acq := database.Acquisition{
		JobID:         jobID,
		JobItemID:     itemID,
		Artist:        metadata.Artist,
		Album:         metadata.Album,
		TrackTitle:    metadata.Title,
		OriginalPath:  downloadPath,
		FinalPath:     finalPath,
		FileSize:      metadata.FileSize,
		FileHash:      hash,
		OwnerUserID:   item.OwnerUserID,
		MBRecordingID: mbIDs.RecordingID,
		MBReleaseID:   mbIDs.ReleaseID,
		MBArtistID:    mbIDs.ArtistID,
		AcoustIDScore: acoustidScore,
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
		log.Printf("[HANDLER] Failed to find item for failure update | job_id=%d | item_id=%d | error=%v", jobID, itemID, err)
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
