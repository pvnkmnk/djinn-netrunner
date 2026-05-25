package services

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/metrics"
)

// Quality-Aware Replacement (DJI-366 — deferred to follow-up)
//
// When a content-level duplicate is detected (same MusicBrainz recording ID),
// the current behavior is to skip the new file. A future enhancement will
// compare quality before deciding:
//
//   1. Extract format/bitrate from both existing and new file metadata.
//   2. Score each using QualityProfile.CalculateScore() if a profile is set.
//   3. If the new file scores higher (e.g. FLAC vs MP3, or 320kbps vs 128kbps):
//      a. Move the new file into the library at the same path (or new path).
//      b. Update the Acquisition record with the new file details.
//      c. Mark the old file for cleanup.
//   4. If the new file scores equal or lower, skip as today.
//
// Until implemented, duplicates can be reviewed via `netrunner-cli library duplicates`
// and manually resolved.

func (h *AcquisitionHandler) importFile(ctx context.Context, jobID uint64, itemID uint64, downloadPath string, item database.JobItem, coverArtSources []string, profile *database.QualityProfile) error {
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
			metrics.AcquisitionDedupTotal.WithLabelValues("hash").Inc()
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

	// 3.5 Content-level dedup: check for existing acquisitions with matching recording ID (DJI-366)
	if mbIDs.RecordingID != "" {
		var existing database.Acquisition
		if err := h.db.Where("mb_recording_id = ?", mbIDs.RecordingID).First(&existing).Error; err == nil {
			metrics.AcquisitionDedupTotal.WithLabelValues("recording_id").Inc()
			h.Log(jobID, "OK", fmt.Sprintf("Duplicate recording detected (MB ID: %s, existing acquisition #%d at %s). "+
				"Quality-aware replacement deferred — see DJI-366 docs.", mbIDs.RecordingID, existing.ID, existing.FinalPath), &itemID)
			h.db.Model(&item).Updates(map[string]interface{}{
				"status":      "completed (duplicate recording)",
				"finished_at": time.Now(),
				"final_path":  existing.FinalPath,
			})
			return nil
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
	cleanupErr, copyErr := h.moveFile(downloadPath, finalPath)
	if copyErr != nil {
		h.failItem(jobID, itemID, fmt.Sprintf("Failed to move file: %v", copyErr))
		return nil
	}
	if cleanupErr != nil {
		h.Log(jobID, "WARN", fmt.Sprintf("Staging cleanup failed (file imported OK): %v", cleanupErr), &itemID)
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

	// Fetch and embed lyrics (best-effort enrichment)
	if h.lyrics != nil && metadata.Artist != "" && metadata.Title != "" {
		h.Log(jobID, "INFO", "Fetching lyrics...", &itemID)
		lyricsResult, lyricsErr := h.lyrics.FetchLyrics(ctx, metadata.Artist, metadata.Title, metadata.Album)
		if lyricsErr == nil && lyricsResult != nil {
			lrcContent := h.lyrics.GetSyncedLyrics(lyricsResult)
			if lrcContent != "" {
				lrcPath := strings.TrimSuffix(finalPath, filepath.Ext(finalPath)) + ".lrc"
				if writeErr := os.WriteFile(lrcPath, []byte(lrcContent), 0644); writeErr != nil {
					h.Log(jobID, "WARN", fmt.Sprintf("Failed to write lyrics file: %v", writeErr), &itemID)
				} else {
					h.Log(jobID, "OK", "Lyrics saved", &itemID)
				}
			}
		} else if lyricsErr != nil {
			h.Log(jobID, "DEBUG", fmt.Sprintf("No lyrics found: %v", lyricsErr), &itemID)
		}
	}

	// Transcode if quality profile specifies a preferred format different from current
	if h.transcoder != nil && profile != nil && profile.AllowedFormats != "" {
		currentExt := strings.TrimPrefix(filepath.Ext(finalPath), ".")
		targetFormat := strings.Split(profile.AllowedFormats, ",")[0]
		targetFormat = strings.TrimSpace(strings.ToLower(targetFormat))
		if targetFormat != "" && !strings.EqualFold(currentExt, targetFormat) {
			h.Log(jobID, "INFO", fmt.Sprintf("Transcoding %s → %s", currentExt, targetFormat), &itemID)
			transcodedPath, transcodeErr := h.transcoder.Transcode(finalPath, targetFormat)
			if transcodeErr != nil {
				h.Log(jobID, "WARN", fmt.Sprintf("Transcoding failed: %v", transcodeErr), &itemID)
			} else {
				os.Remove(finalPath)
				finalPath = transcodedPath
				h.Log(jobID, "OK", fmt.Sprintf("Transcoded to %s", targetFormat), &itemID)
			}
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

// moveFile copies src to dst and removes src on a best-effort basis.
// Returns (cleanupErr, copyErr) so the caller can warn on cleanup
// failures without aborting the import.
func (h *AcquisitionHandler) moveFile(src, dst string) (cleanupErr error, copyErr error) {
	in, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return nil, err
	}

	// Best-effort staging cleanup; may fail when slskd writes as a
	// different UID than the worker.
	return os.Remove(src), nil
}

func (h *AcquisitionHandler) failItem(jobID uint64, itemID uint64, reason string) {
	h.Log(jobID, "ERR", reason, &itemID)

	var item database.JobItem
	if err := h.db.First(&item, itemID).Error; err != nil {
		slog.Error("Failed to find item for failure update", "job_id", jobID, "item_id", itemID, "error", err)
		return
	}

	// Check job-level max attempts to determine if item should be abandoned
	var job database.Job
	abandoned := false
	if err := h.db.First(&job, jobID).Error; err == nil {
		maxAttempts := job.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 3 // safety default
		}
		if item.RetryCount+1 >= maxAttempts {
			abandoned = true
		}
	}

	if abandoned {
		slog.Warn("Item exceeded max retries, abandoning", "job_id", jobID, "item_id", itemID, "retries", item.RetryCount+1)
		h.db.Model(&database.JobItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
			"status":         "abandoned",
			"failure_reason": reason,
			"retry_count":    item.RetryCount + 1,
			"finished_at":    time.Now(),
		})
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
