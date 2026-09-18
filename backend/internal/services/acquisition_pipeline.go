package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	library    SubsonicClientInterface
	discogs    *DiscogsService
	cache      *CacheService
	lyrics     *LyricsService
	transcoder *TranscoderService
	ytdlp      YtdlpClientInterface
	// locker serialises the claim and the reclaim of one staged path across
	// worker processes. Nil means no other process can be acting on this
	// host's staging root; see WithLocker.
	locker database.LockManager
}

func NewAcquisitionHandler(db *gorm.DB, cfg *config.Config, slskd SlskdClient, mb *MusicBrainzService, aid *AcoustIDService, ext *MetadataExtractor, library SubsonicClientInterface, discogs *DiscogsService, cache *CacheService, lyrics *LyricsService, transcoder *TranscoderService, ytdlp YtdlpClientInterface) *AcquisitionHandler {
	return &AcquisitionHandler{BaseHandler: BaseHandler{db: db}, cfg: cfg, slskd: slskd, mb: mb, aid: aid, ext: ext, library: library, discogs: discogs, cache: cache, lyrics: lyrics, transcoder: transcoder, ytdlp: ytdlp}
}

// WithLocker supplies the cross-worker lock manager used to serialise claiming a
// staged path (recording download_path) against reclaiming it
// (discardStagedDownload). It is a setter rather than a constructor argument
// because the constructor already takes twelve positional dependencies and only
// the worker has a lock manager.
//
// A worker process that runs more than one instance MUST call this: two workers
// writing and reclaiming the same staged path without it can delete a file the
// other is waiting on. A single-process deployment is unaffected — there is
// nobody to serialise with — and lockStagingPath reports the lock as available.
func (h *AcquisitionHandler) WithLocker(lm database.LockManager) *AcquisitionHandler {
	h.locker = lm
	return h
}

// acquisitionPipeline carries state between pipeline stages.
type acquisitionPipeline struct {
	ctx        context.Context
	item       database.JobItem
	job        database.Job
	profile    *database.QualityProfile
	results    []SearchResult
	best       SearchResult
	candidates []SearchResult // results that passed the plausibility gate, best first
	download   string         // path after download completes
	albumFiles []PeerFile     // files found during album-mode browse
} // ExecuteItem runs the acquisition pipeline for a single job item.
// Stages are named and independently testable:
//  1. loadItemContext     — load job, item, profile from DB
//  2. checkLibraryIndex   — skip if already in library
//  3. searchSoulseek      — execute search with profile awareness
//  4. selectBestResult    — score and validate best match
//  5. downloadFile        — queue and wait for download
//  6. importAndEnrich     — import to library, enrich metadata
func (h *AcquisitionHandler) ExecuteItem(ctx context.Context, jobID uint64, itemID uint64) error {
	p := &acquisitionPipeline{ctx: ctx}

	if skip, err := h.stageLoadItemContext(p, itemID); err != nil {
		return err
	} else if skip {
		return nil
	}

	h.Log(jobID, "INFO", fmt.Sprintf("Processing: %s", p.item.NormalizedQuery), &itemID)

	if skip, err := h.stageCheckLibraryIndex(p); err != nil {
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

// stageCheckLibraryIndex checks if the track is already in the library server
// (Subsonic-compatible: Navidrome, Gonic, …). Returns skip=true if found.
func (h *AcquisitionHandler) stageCheckLibraryIndex(p *acquisitionPipeline) (skip bool, err error) {
	if h.library == nil {
		return false, nil
	}

	h.Log(p.item.JobID, "INFO", "Checking library index...", &p.item.ID)

	songs, err := h.library.Search3(p.item.NormalizedQuery)
	if err == nil {
		for _, s := range songs {
			if (strings.EqualFold(s.Artist, p.item.Artist) || p.item.Artist == "") &&
				strings.EqualFold(s.Title, p.item.TrackTitle) {
				h.Log(p.item.JobID, "OK", fmt.Sprintf("Found in library (ID: %s). Skipping.", s.ID), &p.item.ID)
				h.db.Model(&p.item).Updates(map[string]interface{}{
					"status":      "completed (already indexed)",
					"finished_at": time.Now(),
				})
				return true, nil
			}
		}
	}

	return false, nil
}

// stageSearchSoulseek executes a Soulseek search with profile awareness.
func (h *AcquisitionHandler) stageSearchSoulseek(p *acquisitionPipeline) (skip bool, err error) {
	h.Log(p.item.JobID, "INFO", "Searching Soulseek...", &p.item.ID)

	results, err := h.slskd.Search(p.item.NormalizedQuery, 30, p.profile)
	if err != nil {
		// A transport failure is retryable — use the standard failure path.
		h.failItem(p.item.JobID, p.item.ID, fmt.Sprintf("Soulseek search failed: %v", err))
		return true, nil
	}
	if len(results) == 0 {
		// Genuinely nothing found. Retrying in a few minutes will not conjure
		// results, so record a terminal no-results outcome instead of cycling
		// the item through the retry machinery.
		h.noResultsItem(p.item.JobID, p.item.ID, "No results found")
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

	outputDir := stagingRoot(h.cfg)

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

	// Reset the item from failed state since yt-dlp succeeded. The output path is
	// recorded here too, so a fallback download whose import never happens is
	// still reclaimable by path rather than only by the orphan scan. The claim is
	// taken under the path lock for the same reason as the slskd claim above.
	unlock, _ := h.lockStagingPath(ctx, downloaded)
	updateErr := h.db.Model(&p.item).Updates(map[string]interface{}{
		"status":         "downloading",
		"failure_reason": "",
		"download_path":  downloaded,
	}).Error
	unlock()

	if updateErr != nil {
		// DownloadAudio verified the file exists, so it is on disk and belongs to
		// no item. Discard it now rather than leaving it for the orphan scan, and
		// stop the fallback: importing a file with no recorded owner is exactly
		// what the ownership record exists to prevent.
		h.Log(p.item.JobID, "WARN", fmt.Sprintf("Could not record the fallback download on the item: %v", updateErr), &p.item.ID)
		h.discardStagedDownload(ctx, downloaded, p.item.JobID, &p.item.ID)
		return "", false
	}

	return downloaded, true
}

// stageSelectBestResult picks the top-scored result and validates it against the profile.
func (h *AcquisitionHandler) stageSelectBestResult(p *acquisitionPipeline) (skip bool, err error) {
	if len(p.results) == 0 {
		// Defensive: the search stage classifies empty results itself, so this
		// only triggers if stages are reordered. Guarding here keeps an index
		// panic from killing the item without a terminal status.
		h.noResultsItem(p.item.JobID, p.item.ID, "No results found")
		return true, nil
	}

	// Reject results that cannot be real audio before spending a download on
	// them. Peers list truncated rips, placeholders and files renamed to match
	// the query, and a bad pick lands in the library as a first-class track.
	var rejected int
	var firstReason string
	for _, r := range p.results {
		reason := candidateRejectionReason(r)
		if reason == "" {
			p.candidates = append(p.candidates, r)
			continue
		}
		if rejected == 0 {
			firstReason = reason
		}
		rejected++
	}

	if len(p.candidates) == 0 {
		h.failItem(p.item.JobID, p.item.ID, fmt.Sprintf("no usable candidate among %d results (e.g. %s)", len(p.results), firstReason))
		return true, nil
	}

	if rejected > 0 {
		h.Log(p.item.JobID, "WARN", fmt.Sprintf("Skipped %d of %d results that do not look like playable audio (e.g. %s)", rejected, len(p.results), firstReason), &p.item.ID)
	}

	p.best = p.candidates[0]

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

// minPlausibleSize returns the smallest size, in bytes, that a real audio file
// of this format can plausibly have. When the peer reports both a bitrate and a
// length, half the expected size is used as the floor so implausible files are
// caught even for formats with no sensible default.
func minPlausibleSize(bitrateKbps, lengthSec int) int64 {
	// With both figures present the metadata is authoritative: a short,
	// low-bitrate track is legitimate and must not be rejected for being
	// smaller than a fixed floor. Only when there is nothing to derive from does
	// a conservative stand-in apply — 64 KiB, comfortably below any real track
	// while still catching the few-kilobyte placeholders peers share.
	const fallbackFloor = int64(64 << 10)
	if lengthSec > 0 && lengthSec <= 6*60*60 && bitrateKbps > 0 {
		if expected := int64(lengthSec) * int64(bitrateKbps) * 1000 / 8; expected > 0 {
			return expected / 2
		}
	}
	return fallbackFloor
}

// candidateRejectionReason explains why a search result should not be downloaded,
// or returns "" when the result looks like playable audio. Kept deliberately
// blunt: it filters out the obviously fake, not the merely unpreferred.
func candidateRejectionReason(r SearchResult) string {
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(r.Filename, "\\", "/")))
	if name == "" || name == "." || name == "/" {
		return "no filename"
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	switch ext {
	case "mp3", "flac", "m4a", "mp4", "aac", "ogg", "opus", "wav", "aiff", "wma":
	default:
		return fmt.Sprintf("unsupported audio format %q", ext)
	}

	bitrate, length := 0, 0
	if r.Bitrate != nil {
		bitrate = *r.Bitrate
	}
	if r.Length != nil {
		length = *r.Length
	}

	min := minPlausibleSize(bitrate, length)
	if r.Size < min {
		return fmt.Sprintf("implausibly small %s file (%s, expected at least %s)", ext, humanBytes(r.Size), humanBytes(min))
	}
	return ""
}

// humanBytes renders a byte count for job logs.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f TiB", value/unit)
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

// downloadTimeout bounds a single candidate's transfer; downloadAttempts bounds
// how many candidates one item tries before it is failed.
const (
	downloadTimeout  = 10 * time.Minute
	downloadAttempts = 3
)

// stageDownloadFile queues a download and waits for it, moving on to the next
// best candidate when a peer cannot deliver. A peer that accepts the transfer
// but never starts sending used to consume the entire timeout, and because the
// worker runs one job at a time, that also stalled every other queued job.
func (h *AcquisitionHandler) stageDownloadFile(p *acquisitionPipeline) (skip bool, err error) {
	candidates := p.candidates
	if len(candidates) == 0 {
		// Defensive: selection either populates candidates or fails the item.
		candidates = []SearchResult{p.best}
	}
	if len(candidates) > downloadAttempts {
		candidates = candidates[:downloadAttempts]
	}

	var failures []string
	for i, candidate := range candidates {
		downloadID, err := h.slskd.EnqueueDownload(candidate.Username, candidate.Filename, candidate.Size)
		if err != nil {
			h.Log(p.item.JobID, "WARN", fmt.Sprintf("Could not queue %s: %v", candidate.Filename, err), &p.item.ID)
			failures = append(failures, fmt.Sprintf("%s: enqueue failed: %v", candidate.Username, err))
			continue
		}

		// Record where slskd will put this transfer *before* waiting for it. The
		// path is knowable now — it is the same model WaitForDownload uses for
		// LocalPath — and recording it is what lets an abandoned or late-sending
		// candidate's file be reclaimed later, when nothing else knows its path.
		//
		// The claim is taken under the path lock, the same lock a reclaim of this
		// path takes, so a file can never be reclaimed in the window between being
		// written and being claimed.
		localPath := h.slskd.LocalPathFor(candidate.Username, candidate.Filename)
		unlock, _ := h.lockStagingPath(p.ctx, localPath)
		updateErr := h.db.Model(&p.item).Updates(map[string]interface{}{
			"status":            "downloading",
			"slskd_search_id":   "completed",
			"slskd_download_id": downloadID,
			"download_path":     localPath,
		}).Error
		unlock()

		if updateErr != nil {
			// Nothing can reclaim what no row points at, so letting this transfer
			// run would produce a file that only the orphan scan can ever find —
			// and the item would look idle in the UI, hiding which transfer to
			// cancel. Cancel it and fail the item so the retry re-claims the path.
			h.Log(p.item.JobID, "WARN", fmt.Sprintf("Could not record the download on the item: %v", updateErr), &p.item.ID)
			if cancelErr := h.slskd.CancelDownload(candidate.Username, downloadID); cancelErr != nil {
				h.Log(p.item.JobID, "WARN", fmt.Sprintf("Could not cancel the untracked transfer %s: %v", downloadID, cancelErr), &p.item.ID)
			}
			return false, fmt.Errorf("recording the staged download failed: %w", updateErr)
		}

		h.Log(p.item.JobID, "INFO", fmt.Sprintf("Download queued from %s (id: %s)", candidate.Username, downloadID), &p.item.ID)

		// A peer may still deliver later, so one that queues the transfer and goes
		// silent can be abandoned early. The last candidate is allowed to wait the
		// transfer out, since abandoning it saves nothing.
		waitOpts := DownloadWaitOptions{
			Timeout:         downloadTimeout,
			HasAlternatives: i < len(candidates)-1,
		}
		download, err := h.slskd.WaitForDownload(p.ctx, candidate.Username, downloadID, waitOpts)
		if err == nil {
			reason, fatalErr := h.rejectUnusableDownload(p, candidate, download)
			if fatalErr != nil {
				return true, fatalErr
			}
			if reason != "" {
				failures = append(failures, reason)
				continue
			}
			h.Log(p.item.JobID, "OK", "Download completed", &p.item.ID)
			p.download = download.LocalPath
			return false, nil
		}

		// The transfer is still queued on this peer. Abandoning it here only
		// helps if it can no longer start: left in place it may begin sending
		// later, downloading a file nothing will import.
		if cancelErr := h.slskd.CancelDownload(candidate.Username, downloadID); cancelErr != nil {
			h.Log(p.item.JobID, "DEBUG", fmt.Sprintf("Could not cancel the transfer from %s: %v", candidate.Username, cancelErr), &p.item.ID)
		}

		if errors.Is(err, ErrRemoteQueueStalled) {
			h.Log(p.item.JobID, "WARN", fmt.Sprintf("%s queued the transfer but never started sending — trying another candidate", candidate.Username), &p.item.ID)
			failures = append(failures, fmt.Sprintf("%s: never started sending", candidate.Username))
		} else {
			h.Log(p.item.JobID, "WARN", fmt.Sprintf("Download from %s failed: %v", candidate.Username, err), &p.item.ID)
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.Username, err))
		}
	}

	h.failItem(p.item.JobID, p.item.ID, fmt.Sprintf("Download failed for all %d candidate(s): %s", len(candidates), strings.Join(failures, "; ")))
	return true, nil
}

// rejectUnplayableDownload runs ffprobe over a completed transfer and removes
// the file when it is not playable audio, returning the reason to record on the
// item (empty when the file is good). Peers serve truncated rips and renamed
// non-audio; the pre-download gate only sees advertised metadata, so this is the
// check against the actual bytes. A rejection is treated as a candidate failure
// rather than an item failure, so a better peer can still satisfy the item.
//
// When ffprobe is unavailable the file is accepted and the gap is logged: an
// unconfigured probe must not reject every download. A cancellation is returned
// as a fatal error instead of a rejection, so shutting the worker down cannot
// delete a good download.
func (h *AcquisitionHandler) rejectUnplayableDownload(p *acquisitionPipeline, candidate SearchResult, download *Download) (string, error) {
	if h.ext == nil || download == nil || download.LocalPath == "" {
		return "", nil
	}

	result, err := h.ext.ProbeAudio(p.ctx, download.LocalPath)
	switch {
	case err == nil:
		h.Log(p.item.JobID, "DEBUG", fmt.Sprintf("Validated %s (%s, %s, %s)", filepath.Base(download.LocalPath), result.FormatName, humanBytes(result.SizeBytes), result.Duration), &p.item.ID)
		return "", nil
	case errors.Is(err, ErrProbeUnavailable):
		h.Log(p.item.JobID, "WARN", fmt.Sprintf("Cannot validate %s — importing without an ffprobe check (%v)", filepath.Base(download.LocalPath), err), &p.item.ID)
		return "", nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The job is going away, not the file. Keep the download and let the
		// caller unwind so a shutdown cannot delete valid audio.
		return "", fmt.Errorf("probing %s: %w", filepath.Base(download.LocalPath), err)
	}

	// Drop it so a bad file cannot be imported later or linger in staging. The
	// discard also sweeps the directory the file emptied: rejecting a
	// single-file download used to delete the bytes and leave its album and
	// artist folders behind forever, because the sweep only reclaims
	// directories that are *already* empty and nothing else removed them
	// (DJI-490).
	h.discardStagedDownload(p.ctx, download.LocalPath, p.item.JobID, &p.item.ID)
	h.Log(p.item.JobID, "WARN", fmt.Sprintf("%s delivered an unplayable file — rejected: %v", candidate.Username, err), &p.item.ID)
	return fmt.Sprintf("%s: unplayable file: %v", candidate.Username, err), nil
}

// rejectUnusableDownload is the post-download gate: every check that reads the
// downloaded bytes runs here, and the reason to record on the item is returned
// (empty when the file is good). A reason is a candidate failure rather than an
// item failure, so the next peer still gets a chance to satisfy the item; a
// fatal error is reserved for the job itself going away, so a shutdown cannot
// delete a good download.
//
// The checks are ordered what-the-bytes-*are* before what-they-*are-of*: a file
// that is not audio at all gets the clearer reason, and the identity check runs
// on bytes ffprobe has already accepted.
func (h *AcquisitionHandler) rejectUnusableDownload(p *acquisitionPipeline, candidate SearchResult, download *Download) (string, error) {
	if h.ext == nil || download == nil || download.LocalPath == "" {
		return "", nil
	}

	reason, err := h.rejectUnplayableDownload(p, candidate, download)
	if reason != "" || err != nil {
		return reason, err
	}
	return h.rejectMismatchedDownload(p, candidate, download)
}

// rejectMismatchedDownload checks that a completed transfer is *the recording
// the item asked for*, not merely playable audio, and removes it when it is
// confidently something else.
//
// Both gates that preceded this one only establish that a file *is* audio: the
// pre-download plausibility check reads advertised metadata, and ffprobe reads
// the bytes. Neither establishes that the file *is the audio that was asked
// for*, so a peer serving an unrelated track whose filename matches the query
// reached the library, organised under its own embedded tags (DJI-495).
//
// The judgement itself lives in canonical_identity.go, the owner of "is this the
// same artist/album?". Tags this build cannot read are a check that could not
// run, not evidence against the file: the download is accepted and the gap is
// logged, mirroring how an absent ffprobe is handled.
func (h *AcquisitionHandler) rejectMismatchedDownload(p *acquisitionPipeline, candidate SearchResult, download *Download) (string, error) {
	meta, err := h.ext.Extract(download.LocalPath)
	if err != nil {
		h.Log(p.item.JobID, "WARN", fmt.Sprintf("Cannot check %s against the request — importing without an identity check (%v)", filepath.Base(download.LocalPath), err), &p.item.ID)
		return "", nil
	}

	mismatch := identityMismatch(&p.item, meta)
	if mismatch == "" {
		return "", nil
	}

	// Same owner as every other rejection, so the file is removed and the
	// directories it emptied are swept with it rather than left behind (DJI-490).
	h.discardStagedDownload(p.ctx, download.LocalPath, p.item.JobID, &p.item.ID)
	h.Log(p.item.JobID, "WARN", fmt.Sprintf("%s delivered a file that does not match the request — rejected: %s", candidate.Username, mismatch), &p.item.ID)
	return fmt.Sprintf("%s: does not match the request: %s", candidate.Username, mismatch), nil
}

// stageImportAndEnrich imports the downloaded file and enriches metadata.
//
// It is also the single boundary that guarantees a staged download does not
// survive an import that did not happen. importFile reports most of its terminal
// outcomes by returning nil — a duplicate by hash, a duplicate album, an item it
// failed — and a successful import is the only one that *moves* the file out of
// staging. So the staged path still existing after the call is proof that
// nothing imported it, and leaving it there is how one discography run
// accumulated whole directories of downloads no item would ever claim (DJI-490)
// while a retry re-downloads from scratch rather than reusing them.
//
// The decision lives here rather than in each branch because this is the only
// place that knows both the staged path and the outcome — importFile has exactly
// one caller and is unexported. It routes through discardStagedDownload, the
// single owner of "this staged file will never be imported": that owner removes
// only this item's own file and sweeps only directories it emptied, so a sibling
// track another item still has to import is never taken with it.
func (h *AcquisitionHandler) stageImportAndEnrich(ctx context.Context, p *acquisitionPipeline) error {
	var coverArtSources []string
	if p.profile != nil {
		coverArtSources = parseCoverArtSources(p.profile.CoverArtSources)
	}

	err := h.importFile(ctx, p.item.JobID, p.item.ID, p.download, p.item, coverArtSources, p.profile)

	// Still present means it was not imported. A download that was moved into the
	// library is not at risk: the stat fails against the old path and nothing is
	// removed. An empty path also stats as missing, so it is skipped.
	if _, statErr := os.Stat(p.download); statErr == nil {
		h.discardStagedDownload(ctx, p.download, p.item.JobID, &p.item.ID)
	}

	return err
}
