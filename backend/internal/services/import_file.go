package services

import (
	"context"
	"fmt"
	"io"
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
			// The status write is checked, like the other two dedup branches. If
			// it fails the item is never marked terminal, so nothing re-claims it
			// and no retry is scheduled: the worker would report success while
			// the row sat in `running`. The staged file itself is the import
			// stage's business — see stageImportAndEnrich.
			if err := h.db.Model(&item).Updates(map[string]interface{}{
				"status":      "completed (duplicate hash)",
				"finished_at": time.Now(),
				"final_path":  existing.FinalPath,
			}).Error; err != nil {
				return fmt.Errorf("hash dedup: marking item completed: %w", err)
			}
			return nil
		}
	}

	// 2. Extract basic tags
	metadata, err := h.ext.Extract(downloadPath)
	if err != nil {
		h.Log(jobID, "WARN", fmt.Sprintf("Metadata extraction failed: %v", err), &itemID)
		metadata = &AudioMetadata{
			Artist: item.Artist,
			Title:  item.TrackTitle,
			Album:  item.Album,
		}
	}

	// Preserve the container type when tag parsing failed or yielded no
	// format: an extensionless import is invisible to the media server's
	// scanner (Navidrome ignores files without recognized extensions).
	if metadata.Format == "" {
		if ext := strings.TrimPrefix(filepath.Ext(downloadPath), "."); ext != "" {
			metadata.Format = ext
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
			// The status write is checked before cleanup, and deliberately so. If it
			// fails the item is never marked terminal, so discarding the staged file
			// and returning nil would have the worker report success while the row
			// sat in `running` — no retry, and no download left to retry with.
			if err := h.db.Model(&item).Updates(map[string]interface{}{
				"status":      "completed (duplicate recording)",
				"finished_at": time.Now(),
				"final_path":  existing.FinalPath,
			}).Error; err != nil {
				return fmt.Errorf("recording dedup: marking item completed: %w", err)
			}
			// The staged file is redundant — the library already holds this
			// recording. It is discarded by the import stage's boundary, which
			// every non-import exit passes through (see stageImportAndEnrich).
			// This branch once returned with no cleanup at all, so its downloads
			// sat in staging forever while the album branch's did not (DJI-492).
			return nil
		}
	}

	// The monitored artist (item.Artist) is the canonical album artist. Resolve
	// it BEFORE the album-level dedup check so the dedup key uses the canonical
	// album artist rather than per-track credits — cross-credit tracks of one
	// album then share a single key and all land in one folder.
	if metadata.AlbumArtist == "" && item.Artist != "" {
		metadata.AlbumArtist = item.Artist
	}

	// 3.6 Album-level dedup: acquisitions are keyed on the monitored artist,
	// but one album download imports many tracks — re-running a release (or a
	// re-enqueued backlog item) must not re-import the whole album. If another
	// acquisition already imported this artist+album, treat this file as a
	// duplicate unless it is the exact file hash we just checked. The key is
	// the canonical (album) artist, so per-track credit variants dedup too, and
	// the comparison folds case (DJI-489) so a case-only difference is the same
	// album rather than a second folder.
	//
	// Adopt the casing the library already uses for this artist+album before the
	// dedup key and the library path are derived from it. The lookup is keyed on
	// the acquisition target — the monitored artist, and the album the item names
	// — not on the peer's tags: a per-credit album artist ("Every Time I Die &
	// Daryl Palumbo") keys a lookup that matches nothing, so the credit became
	// the library's canonical identity for that album and every tag written from
	// it (DJI-494). The file's tags remain the input for an item that carries no
	// artist of its own.
	identityArtist, identityAlbum := metadata.AlbumArtist, metadata.Album
	if item.Artist != "" {
		identityArtist = item.Artist
	}
	if item.Album != "" {
		identityAlbum = item.Album
	}
	if identityArtist != "" && identityAlbum != "" {
		canonicalArtist, canonicalAlbum, err := h.resolveCanonicalIdentity(identityArtist, identityAlbum)
		if err != nil {
			// The library's committed casing could not be read. Importing anyway
			// would derive the path from whatever these tags say and risk creating
			// the case-variant sibling this resolution exists to prevent, so fail
			// the item instead — failItem schedules the retry.
			h.failItem(jobID, itemID, fmt.Sprintf("Canonical identity lookup failed: %v", err))
			return nil
		}
		metadata.AlbumArtist, metadata.Album = canonicalArtist, canonicalAlbum
	}

	albumArtist := metadata.AlbumArtist
	if albumArtist == "" {
		albumArtist = metadata.Artist
	}
	if albumArtist != "" && metadata.Album != "" {
		// The lookup folds case on both sides and returns the earliest match, so a
		// case-only difference is recognised as the album the library already
		// holds and the row reported to the caller is the canonical one (DJI-489).
		// A failed lookup must not read as "not a duplicate": that would import a
		// second copy of an album the library already holds. findExistingAlbumAcquisition
		// reports a missing row as (nil, nil), so any error here is real.
		existing, err := h.findExistingAlbumAcquisition(albumArtist, metadata.Artist, metadata.Album, hash)
		if err != nil {
			return fmt.Errorf("album dedup lookup: %w", err)
		}
		if existing != nil {
			metrics.AcquisitionDedupTotal.WithLabelValues("artist_album").Inc()
			h.Log(jobID, "OK", fmt.Sprintf("Album already acquired (existing acquisition #%d at %s). Skipping track.",
				existing.ID, existing.FinalPath), &itemID)
			if err := h.db.Model(&item).Updates(map[string]interface{}{
				"status":      "completed (duplicate album)",
				"finished_at": time.Now(),
				"final_path":  existing.FinalPath,
			}).Error; err != nil {
				return fmt.Errorf("album dedup: marking item completed: %w", err)
			}
			// The staged file is now redundant: the import stage's boundary
			// discards it and sweeps the album folder it emptied, so staging does
			// not grow unbounded (see stageImportAndEnrich).
			return nil
		}
	}

	// Determine library path
	libraryRoot := h.libraryRoot()
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
	} else {
		// The move succeeded; sweep empty album directories the download left
		// behind so staging does not accumulate skeletons.
		h.cleanupEmptyStagingDirs(filepath.Dir(downloadPath), jobID, &itemID)
	}

	// Stamp the canonical identity into the file's tags, not just the folder:
	// a client groups by tag, so a canonical folder holding a peer-cased tag
	// still lists one artist twice (DJI-494). The library's committed casing
	// wins when resolution found it; otherwise the monitored artist the item
	// was enqueued for is the canonical one.
	canonicalArtist := metadata.AlbumArtist
	if canonicalArtist == "" {
		canonicalArtist = item.Artist
	}
	if err := h.ext.NormalizeAlbumTags(ctx, finalPath, AlbumTagIdentity{
		AlbumArtist: canonicalArtist,
		Album:       metadata.Album,
		TrackArtist: canonicalArtist,
	}); err != nil {
		h.Log(jobID, "WARN", fmt.Sprintf("Identity tag normalization failed: %v", err), &itemID)
	}

	// Attempt to fetch and embed cover art with fallback chain
	h.Log(jobID, "INFO", "Fetching cover art...", &itemID)
	artData, err := h.getCoverArtWithFallback(ctx, &item, metadata.Artist, metadata.Title, metadata.Album, coverArtSources)
	if err == nil && len(artData) > 0 {
		h.Log(jobID, "INFO", "Embedding cover art...", &itemID)
		if err := h.ext.EmbedCoverArt(ctx, finalPath, artData); err != nil {
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
				if md, mdErr := h.ext.Extract(finalPath); mdErr == nil && md != nil {
					metadata = md
				}
				if newHash, hashErr := h.ext.HashFile(finalPath); hashErr == nil {
					hash = newHash
				}
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

	out, err := os.Create(dst)
	if err != nil {
		in.Close()
		return nil, err
	}

	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		in.Close()
		return nil, err
	}

	// Best-effort staging cleanup; may fail when slskd writes as a
	// different UID than the worker.
	// Explicitly close handles before removal - defers run on function
	// return which is too late for os.Remove on Windows.
	out.Close()
	in.Close()
	return os.Remove(src), nil
}
