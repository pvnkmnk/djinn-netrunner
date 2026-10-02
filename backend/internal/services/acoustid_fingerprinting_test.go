package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Making fingerprinting deterministic
//
// MetadataExtractor.Fingerprint shells out to `fpcalc`, so the outcome of every
// claim below depends on whether that binary exists on the machine running the
// test. That is precisely the variable this ticket is about: the image shipped
// without it for the project's whole history, and the one test that touched it
// skipped - passing whether or not fingerprinting worked. Both sides are pinned
// here instead.
//
//	installFakeFpcalc    puts a working fpcalc on PATH
//	removeFpcalcFromPath takes PATH away entirely, reproducing the shipped image
//
// Neither leg skips, so neither can be an inert convention.
//
// Both are called only after the audio fixtures are generated. The fixtures
// come from the machine's own ffmpeg, which has nothing to do with
// fingerprinting, and taking PATH away first would have been a test that fails
// for a reason unrelated to the thing it claims.
// ---------------------------------------------------------------------------

// init doubles as the fake fpcalc binary. A copy of this test binary placed on
// PATH under the name fpcalc re-enters this package and answers with the
// fingerprint the test asked for. The env var is unset on an ordinary test run,
// so outside the child process this is a no-op.
func init() {
	spec := os.Getenv("NETRUNNER_FAKE_FPCALC")
	if spec == "" {
		return
	}
	var fp struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err := json.Unmarshal([]byte(spec), &fp); err != nil {
		fmt.Fprintln(os.Stderr, "fake fpcalc: bad spec:", err)
		os.Exit(2)
	}
	out, err := json.Marshal(fp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake fpcalc:", err)
		os.Exit(2)
	}
	fmt.Println(string(out))
	os.Exit(0)
}

// fakeFingerprint is a real Chromaprint fingerprint's length, not a tidy one.
// Captured from fpcalc 1.5.1 against a real seeded MP3: 3740 characters. The
// first version of this fake used 128, and its `duration` was an integer - so
// every guard passed against a contract the real binary never emits, and the
// live proof then failed on every single track. A fixture encoding a convenient
// assumption is the same defect as a test asserting one.
const realFingerprintLength = 3740 // measured from fpcalc 1.5.1 on a real seeded MP3

var fakeFingerprint = strings.Repeat("AQADtEmqSImp4d8yAQADtEmqSImp4d8y",
	realFingerprintLength/32) + "AQADtEmqSImp4d8yAQADtEmqSImp4d8y"[:realFingerprintLength%32]

// installFakeFpcalc puts a working fpcalc at the front of PATH. PATH is
// prepended rather than replaced so ffmpeg keeps working and the test stays
// about fingerprinting.
func installFakeFpcalc(t *testing.T, duration float64, fingerprint string) {
	t.Helper()

	self, err := os.Executable()
	require.NoError(t, err)
	raw, err := os.ReadFile(self)
	require.NoError(t, err)

	dir := t.TempDir()
	name := "fpcalc"
	if runtime.GOOS == "windows" {
		name = "fpcalc.exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), raw, 0o755))

	spec, err := json.Marshal(map[string]any{"duration": duration, "fingerprint": fingerprint})
	require.NoError(t, err)
	t.Setenv("NETRUNNER_FAKE_FPCALC", string(spec))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// removeFpcalcFromPath reproduces the shipped-image condition: no fpcalc
// reachable, while everything else the pipeline shells out to still works.
//
// Only the PATH entries that actually carry an fpcalc are dropped. Emptying
// PATH outright looks equivalent and is not - the extractor probes each file
// with ffprobe, so the scan then failed for a reason that had nothing to do
// with fingerprinting, which is a harness bug that reads exactly like a code
// defect.
func removeFpcalcFromPath(t *testing.T) {
	t.Helper()

	name := "fpcalc"
	if runtime.GOOS == "windows" {
		name = "fpcalc.exe"
	}
	kept := make([]string, 0, 8)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			continue
		}
		kept = append(kept, dir)
	}
	require.NotEmpty(t, kept, "cannot build a PATH without fpcalc: nothing was left")
	t.Setenv("PATH", strings.Join(kept, string(os.PathListSeparator)))
}

// captureWorkerLog redirects the default logger for the duration of the test.
// AC5 is a claim about what the worker log says, so asserting it means reading
// the worker log.
func captureWorkerLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// ---------------------------------------------------------------------------
// The scanner
// ---------------------------------------------------------------------------

// AC3 and AC1 through the surface an operator checks the work with: a scan of a
// real audio file, with the binary present, must leave a real fingerprint.
func TestScanner_StoresRealFingerprintWhenFingerprintingWorks(t *testing.T) {
	requireProbeTools(t)
	db := newScannerTestDB(t)
	library := database.Library{Name: "Overcast", Path: t.TempDir()}
	require.NoError(t, db.Create(&library).Error)
	audio := generateTestAudio(t, library.Path, "01 - Faceless.mp3")

	installFakeFpcalc(t, 200.83, fakeFingerprint)
	logs := captureWorkerLog(t)

	s := NewScannerService(db)
	require.NoError(t, s.ScanLibrary(context.Background(), library.ID, library.Path))

	var track database.Track
	require.NoError(t, db.Where("path = ?", audio).First(&track).Error)
	require.Equal(t, fakeFingerprint, track.Fingerprint,
		"a fingerprinting image must leave a real fingerprint on the track")

	out := logs.String()
	require.Contains(t, out, "fingerprinted=1 fingerprint_failed=0",
		"the scan summary must count what it fingerprinted, in the summary line itself")
	require.NotContains(t, out, "Fingerprinting did not run",
		"nothing failed, so the warning must not fire")
}

// The defect itself, reproduced deterministically. With no fpcalc reachable the
// old code dropped fpErr on the floor at both call sites: the file was indexed,
// the scan reported success, and the empty fingerprint column was
// indistinguishable from a track nobody ever tried. The outcome must be visible.
func TestScanner_ReportsFingerprintFailureInsteadOfSwallowingIt(t *testing.T) {
	requireProbeTools(t)
	db := newScannerTestDB(t)
	library := database.Library{Name: "Overcast", Path: t.TempDir()}
	require.NoError(t, db.Create(&library).Error)
	audio := generateTestAudio(t, library.Path, "01 - Faceless.mp3")

	removeFpcalcFromPath(t)
	logs := captureWorkerLog(t)

	s := NewScannerService(db)
	require.NoError(t, s.ScanLibrary(context.Background(), library.ID, library.Path),
		"a missing fpcalc must not fail the scan: the file is still worth indexing for its tags")

	var track database.Track
	require.NoError(t, db.Where("path = ?", audio).First(&track).Error,
		"the track must still be indexed")
	require.Empty(t, track.Fingerprint)

	out := logs.String()
	require.Contains(t, out, "Fingerprinting failed while indexing",
		"the per-file failure must be logged, not dropped")
	require.Contains(t, out, "fingerprinted=0 fingerprint_failed=1",
		"the scan summary must count fingerprint failures separately from indexing failures")
	require.Contains(t, out, "Fingerprinting did not run for every indexed file")
	require.Contains(t, out, "missing fpcalc is a deployment fault",
		"the log has to name the cause, not just the count")
}

// A rescan of a track that already has a fingerprint must neither recompute it
// nor report a failure: the backfill branch is neither of the two outcomes.
func TestScanner_KeepsAnExistingFingerprintAndCountsNeitherOutcome(t *testing.T) {
	requireProbeTools(t)
	db := newScannerTestDB(t)
	library := database.Library{Name: "Overcast", Path: t.TempDir()}
	require.NoError(t, db.Create(&library).Error)
	audio := generateTestAudio(t, library.Path, "01 - Faceless.mp3")

	removeFpcalcFromPath(t)
	logs := captureWorkerLog(t)

	require.NoError(t, db.Create(&database.Track{
		LibraryID: library.ID, Title: "Faceless", Path: audio, Fingerprint: "preexisting",
	}).Error)

	s := NewScannerService(db)
	require.NoError(t, s.ScanLibrary(context.Background(), library.ID, library.Path))

	var track database.Track
	require.NoError(t, db.Where("path = ?", audio).First(&track).Error)
	require.Equal(t, "preexisting", track.Fingerprint,
		"a rescan must not blank a fingerprint it already had")

	out := logs.String()
	require.Contains(t, out, "fingerprinted=0")
	require.NotContains(t, out, "fingerprint_failed=1",
		"keeping an existing fingerprint is not a failure")
}

// ---------------------------------------------------------------------------
// Ingestion
// ---------------------------------------------------------------------------

// newImportFixture wires an acquisition handler against a shared in-memory
// database, so the AcoustID service and the handler observe the same state. The
// staging directory is passed in because the audio is generated before the
// fingerprint environment is arranged.
func newImportFixture(t *testing.T, db *gorm.DB, aid *AcoustIDService, staging string) (*AcquisitionHandler, database.Job, database.JobItem) {
	t.Helper()
	requireProbeTools(t)

	libraryRoot := t.TempDir()
	h := NewAcquisitionHandler(db, &config.Config{
		DownloadStagingPath: staging,
		MusicLibraryPath:    libraryRoot,
		// db, cfg, slskd, musicbrainz, acoustid, extractor, library, discogs,
		// cache, lyrics, transcoder, yt-dlp.
	}, nil, nil, aid, nil, nil, nil, nil, nil, nil, nil)
	h.ext = NewMetadataExtractor()

	job := database.Job{Type: "acquisition", State: "running", MaxAttempts: 3}
	require.NoError(t, db.Create(&job).Error)
	item := database.JobItem{
		JobID: job.ID, Status: "running", Sequence: 1,
		NormalizedQuery: "Overcast Twin Terror",
		Artist:          "Overcast", Album: "Twin Terror", TrackTitle: "Faceless",
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.First(&item, item.ID).Error)

	return h, job, item
}

// facelessDownload is the staged file the import tests import: real audio,
// tagged, in the staging directory the handler is configured with.
func facelessDownload(t *testing.T, staging string) string {
	return generateTestAudio(t, staging, "01 - Faceless.mp3",
		"-metadata", "artist=Overcast", "-metadata", "album=Twin Terror", "-metadata", "title=Faceless")
}

// seedAcoustIDCache primes the shadow cache so Lookup answers without a network
// call. Building the key here rather than inside the service pins the cache-key
// contract as well as the scoring.
func seedAcoustIDCache(t *testing.T, aid *AcoustIDService, duration int, fingerprint string, results []AcoustIDResult) {
	t.Helper()
	prefix := fingerprint
	if len(prefix) > 32 {
		prefix = prefix[:32]
	}
	require.NoError(t, aid.cache.Set("acoustid", fmt.Sprintf("lookup:%d:%s", duration, prefix), results, time.Hour))
}

// AC3: a successful lookup stores a real, non-zero score. Driven end to end
// through importFile with the binary present and the lookup answered from the
// cache, so the assertion is about what lands in the database.
func TestImportFile_StoresNonZeroScoreWhenTheLookupSucceeds(t *testing.T) {
	requireProbeTools(t)
	staging := t.TempDir()
	download := facelessDownload(t, staging)

	installFakeFpcalc(t, 200.83, fakeFingerprint)
	db := setupPipelineTestDB(t)
	aid := NewAcoustIDService(&config.Config{AcoustIDApiKey: "test-key"})
	aid.SetCache(NewCacheService(db))
	seedAcoustIDCache(t, aid, 200, fakeFingerprint, []AcoustIDResult{{Score: 0.93}})

	h, job, item := newImportFixture(t, db, aid, staging)
	logs := captureWorkerLog(t)

	require.NoError(t, h.importFile(context.Background(), job.ID, item.ID, download, item, nil, nil))

	var acq database.Acquisition
	require.NoError(t, db.Where("track_title = ?", "Faceless").First(&acq).Error)
	require.NotNil(t, acq.AcoustIDScore, "a successful lookup must leave a score behind")
	require.Equal(t, 93, *acq.AcoustIDScore)
	require.Contains(t, logs.String(), "AcoustID match found")
}

// AC4 and AC5 on the ingestion path: the lookup failed, so nothing may claim a
// score - and the failure must be said out loud. The old code read
// `if err == nil && len(results) > 0`, discarding the error with nothing logged
// on either side, so a missing API key, an upstream outage and a genuine
// no-match were one indistinguishable silence.
func TestImportFile_LookupFailureIsLoggedAndRecordedAsUnscored(t *testing.T) {
	requireProbeTools(t)
	staging := t.TempDir()
	download := facelessDownload(t, staging)

	installFakeFpcalc(t, 200.83, fakeFingerprint)
	db := setupPipelineTestDB(t)
	// No API key: the service refuses before it reaches the cache or the
	// network, which is the configuration every deployment actually runs.
	aid := NewAcoustIDService(&config.Config{})

	h, job, item := newImportFixture(t, db, aid, staging)
	logs := captureWorkerLog(t)

	require.NoError(t, h.importFile(context.Background(), job.ID, item.ID, download, item, nil, nil))

	var acq database.Acquisition
	require.NoError(t, db.Where("track_title = ?", "Faceless").First(&acq).Error)
	require.Nil(t, acq.AcoustIDScore,
		"a lookup that never completed must not be recorded as a score of zero")

	out := logs.String()
	require.Contains(t, out, "AcoustID lookup failed",
		"AC5: the worker must log the failure rather than swallow it")
	require.NotContains(t, out, "AcoustID match found")

	// And on the operator-facing surface, the job's own log.
	var entry database.JobLog
	require.NoError(t, db.Where("job_id = ? AND message LIKE ?", job.ID, "%AcoustID lookup failed%").First(&entry).Error)
	require.Equal(t, "WARN", entry.Level)
}

// A lookup that completes and finds nothing is unscored, not a zero.
func TestImportFile_NoAcoustIDMatchIsUnscoredAndSaidSo(t *testing.T) {
	requireProbeTools(t)
	staging := t.TempDir()
	download := facelessDownload(t, staging)

	installFakeFpcalc(t, 200.83, fakeFingerprint)
	db := setupPipelineTestDB(t)
	aid := NewAcoustIDService(&config.Config{AcoustIDApiKey: "test-key"})
	aid.SetCache(NewCacheService(db))
	seedAcoustIDCache(t, aid, 200, fakeFingerprint, []AcoustIDResult{})

	h, job, item := newImportFixture(t, db, aid, staging)
	logs := captureWorkerLog(t)

	require.NoError(t, h.importFile(context.Background(), job.ID, item.ID, download, item, nil, nil))

	var acq database.Acquisition
	require.NoError(t, db.Where("track_title = ?", "Faceless").First(&acq).Error)
	require.Nil(t, acq.AcoustIDScore,
		"AcoustID returning nothing is a fact about the lookup, not a score of zero")

	require.Contains(t, logs.String(), "AcoustID returned no match",
		"a no-match was previously indistinguishable from a failure")

	// The sentence for the operator goes to the job log, which is where they
	// read it; the worker log line is for whoever is reading the container.
	var entry database.JobLog
	require.NoError(t, db.Where("job_id = ? AND message LIKE ?", job.ID, "%no match - recording as unscored%").First(&entry).Error)
}

// AC4 on the ingestion path, other direction: no binary means no fingerprint,
// so the lookup is never attempted, so the result is unscored. This is the
// exact state all 144 legacy rows were in while reporting a zero.
func TestImportFile_WithoutFingerprintingTheScoreIsUnscoredNotZero(t *testing.T) {
	requireProbeTools(t)
	staging := t.TempDir()
	download := facelessDownload(t, staging)

	db := setupPipelineTestDB(t)
	aid := NewAcoustIDService(&config.Config{AcoustIDApiKey: "test-key"})
	aid.SetCache(NewCacheService(db))
	h, job, item := newImportFixture(t, db, aid, staging)

	// Only now, with the audio in place, is fpcalc taken off PATH.
	removeFpcalcFromPath(t)
	logs := captureWorkerLog(t)

	require.NoError(t, h.importFile(context.Background(), job.ID, item.ID, download, item, nil, nil))

	var acq database.Acquisition
	require.NoError(t, db.Where("track_title = ?", "Faceless").First(&acq).Error)
	require.Nil(t, acq.AcoustIDScore,
		"no binary means no fingerprint means no score - and unscored, not zero")
	require.Contains(t, logs.String(), "Audio fingerprinting failed")
}

// The contract, pinned to what fpcalc actually prints.
//
// Captured from `fpcalc -json` (Chromaprint 1.5.1) against a real seeded MP3:
//
//	{"duration": 200.83, "fingerprint": "AQADtEuyJImiaIoS9NxRG0n2MHDVITyPK8mJ..."}
//
// Two things in that line had the implementation wrong. `duration` is a float,
// so the struct declared `int` and every real fingerprint died in json.Unmarshal -
// which shipped-green because the only test skipped when the binary was absent.
// And the fingerprint is 3740 characters, not the tidy 128 the fake first used.
// Neither is guessable, so both are asserted against the captured bytes rather
// than against what the code happens to produce.
func TestFingerprint_AcceptsTheShapeFpcalcActuallyEmits(t *testing.T) {
	requireProbeTools(t)
	probe := generateTestAudio(t, t.TempDir(), "01 - Faceless.mp3")

	installFakeFpcalc(t, 200.83, fakeFingerprint)
	e := NewMetadataExtractor()
	require.Equal(t, realFingerprintLength, len(fakeFingerprint),
		"the fake must speak the real fingerprint's length, not a convenient one")

	fingerprint, duration, err := e.Fingerprint(probe)
	require.NoError(t, err, "a fractional duration is the normal case, not a parse error")
	require.Equal(t, fakeFingerprint, fingerprint)
	require.Equal(t, 200, duration,
		"AcoustID takes whole seconds, so the fraction is truncated - not rounded away")
}

// ---------------------------------------------------------------------------
// The service seam
// ---------------------------------------------------------------------------

// The cache key truncated a fixed 32 characters off the fingerprint.
// Chromaprint fingerprints are long but are shorter for short audio, and this
// slice is what first made the service reachable - so the first short
// fingerprint it was handed would have panicked the worker mid-import.
func TestAcoustIDService_LookupAcceptsAShortFingerprint(t *testing.T) {
	db := setupPipelineTestDB(t)
	aid := NewAcoustIDService(&config.Config{AcoustIDApiKey: "test-key"})
	aid.SetCache(NewCacheService(db))

	require.NotPanics(t, func() {
		_, err := aid.Lookup("short", 180)
		require.Error(t, err, "no cached entry and no reachable API, so an error is right")
	}, "a fingerprint shorter than the 32-character cache-key prefix must not panic")
}

// fpcalc exits non-zero for audio it cannot fingerprint, but a zero exit with
// an empty fingerprint is still no fingerprint. Reporting that as success would
// store an empty string, which reads exactly like a track nobody ever tried -
// the same shape as the defect, one layer down. The fake here answers the way a
// real fpcalc does for audio it cannot analyse.
func TestFingerprint_RejectsAnEmptyFingerprintFromATooShortOrUnreadableFile(t *testing.T) {
	requireProbeTools(t)
	probe := generateTestAudio(t, t.TempDir(), "01 - Faceless.mp3")

	installFakeFpcalc(t, 1.2, "")
	e := NewMetadataExtractor()

	_, _, err := e.Fingerprint(probe)
	require.Error(t, err,
		"an empty fingerprint is not a fingerprint: storing one is indistinguishable from never trying")
	require.Contains(t, err.Error(), "reported no fingerprint")
}
