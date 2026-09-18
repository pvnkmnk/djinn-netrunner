package services

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// This file owns the post-download gate: the checks that read a downloaded
// file's bytes, and the decision to remove it.
//
// There is exactly one of it on purpose. The pipeline reaches the import stage
// from two places — the Soulseek candidate loop and the yt-dlp fallback — and a
// gate per entrance is how the second entrance came to have no gate at all: the
// fallback returned straight into the import stage, so a downloaded file that no
// check had ever seen could be imported (found by DJI-495's audit). Callers
// differ in what they do with a rejection; they must not differ in whether the
// check ran.

// rejectUnusableDownload runs every check that reads a completed download and
// removes the file when it cannot be imported. It returns the reason to record
// on the item, empty when the file may be imported, and separately a fatal error
// reserved for the job itself going away — a shutdown must never be mistaken for
// a bad file, or it would delete good audio.
//
// The checks run cheapest-and-clearest first: what the bytes *are* (playable
// audio), then what they *are of* (the recording the item asked for). Both read
// the actual file, because the pre-download gate only sees what a peer
// advertises.
//
// source names where the download came from — a peer's username, or "yt-dlp" —
// and appears in the reason and the job log, so an operator can tell which
// entrance produced a rejection.
//
// A rejection is the caller's to act on. The Soulseek loop records the reason
// against the candidate and tries the next peer; the yt-dlp entrance has no
// alternative source, so it fails the item with the same reason.
//
// When a check cannot run — no ffprobe binary, tags this build cannot read, a
// caller with no staged path — the file is accepted and the gap is logged, never
// rejected. That is a property of the check, not of the caller, which is why it
// lives here: a deployment without ffprobe must not reject every download it
// makes, and a peer that forgot a tag must not have its file thrown away.
func (h *AcquisitionHandler) rejectUnusableDownload(p *acquisitionPipeline, source, path string) (string, error) {
	if h.ext == nil {
		// Not reachable in a deployed worker: the extractor is wired at
		// construction. There is no runtime gap here to report.
		return "", nil
	}
	if path == "" {
		// A caller with no staged path has nothing for either check to read. It
		// does not import — importFile fails a path it cannot stat — but the
		// skipped checks are worth a diagnostic rather than an assumption, or a
		// fallback that reported success with no path would traverse both
		// entrances without leaving a trace.
		h.Log(p.item.JobID, "WARN", "No staged path to verify — no post-download check ran", &p.item.ID)
		return "", nil
	}

	// Is this playable audio at all?
	result, err := h.ext.ProbeAudio(p.ctx, path)
	switch {
	case err == nil:
		h.Log(p.item.JobID, "DEBUG", fmt.Sprintf("Validated %s (%s, %s, %s)", filepath.Base(path), result.FormatName, humanBytes(result.SizeBytes), result.Duration), &p.item.ID)
	case errors.Is(err, ErrProbeUnavailable):
		h.Log(p.item.JobID, "WARN", fmt.Sprintf("Cannot validate %s — importing without an ffprobe check (%v)", filepath.Base(path), err), &p.item.ID)
		return "", nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The job is going away, not the file. Keep the download and let the
		// caller unwind so a shutdown cannot delete valid audio.
		return "", fmt.Errorf("probing %s: %w", filepath.Base(path), err)
	default:
		// Drop it so a bad file cannot be imported later or linger in staging.
		// The discard also sweeps the directory the file emptied: rejecting a
		// single-file download used to delete the bytes and leave its album and
		// artist folders behind forever, because the sweep only reclaims
		// directories that are *already* empty and nothing else removed them
		// (DJI-490).
		h.discardStagedDownload(p.ctx, path, p.item.JobID, &p.item.ID)
		h.Log(p.item.JobID, "WARN", fmt.Sprintf("%s delivered an unplayable file — rejected: %v", source, err), &p.item.ID)
		return fmt.Sprintf("%s: unplayable file: %v", source, err), nil
	}

	// Is it the recording the item asked for? Being playable audio is not the
	// same as being the audio that was requested: Soulseek search is fuzzy, so a
	// peer can hand back an unrelated track whose filename matches the query, and
	// the importer would file it under its own embedded tags (DJI-495). The
	// judgement lives in canonical_identity.go, the owner of "is this the same
	// artist/album?".
	meta, err := h.ext.Extract(path)
	if err != nil {
		h.Log(p.item.JobID, "WARN", fmt.Sprintf("Cannot check %s against the request — importing without an identity check (%v)", filepath.Base(path), err), &p.item.ID)
		return "", nil
	}

	mismatch := identityMismatch(&p.item, meta)
	if mismatch == "" {
		return "", nil
	}

	h.discardStagedDownload(p.ctx, path, p.item.JobID, &p.item.ID)
	h.Log(p.item.JobID, "WARN", fmt.Sprintf("%s delivered a file that does not match the request — rejected: %s", source, mismatch), &p.item.ID)
	return fmt.Sprintf("%s: does not match the request: %s", source, mismatch), nil
}
