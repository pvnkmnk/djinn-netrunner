package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The janitor exists for the exits the pipeline cannot see. Only the paths it
// reclaims are asserted here; staging_cleanup_test.go owns the owner's own
// contract (root refusal, sibling safety, deferral to a live item).

func TestStagingReclaim_ReclaimsAFinishedItemsStagedFile(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, db := newStagingReclaim(t, staging, nil)

	staged := filepath.Join(staging, "Some Artist", "Some Album", "01 - track.m4a")
	writeAgedFile(t, staged, 2*time.Hour)
	// The item is finished with the file: this is a dead worker's download, or a
	// candidate that was abandoned after the bytes already landed.
	stagedItem(t, db, "imported", staged)

	report := reclaim.Reclaim(context.Background(), "test-worker")

	assert.Equal(t, 1, report.Removed, "the finished item's staged file must be reclaimed")
	assert.Positive(t, report.BytesRemoved, "the report must account for the bytes")
	_, err := os.Stat(staged)
	assert.True(t, os.IsNotExist(err), "the file must be gone")
	_, err = os.Stat(filepath.Join(staging, "Some Artist"))
	assert.True(t, os.IsNotExist(err), "the emptied directories must be swept too")
	_, err = os.Stat(staging)
	require.NoError(t, err, "the staging root itself must survive")
}

func TestStagingReclaim_KeepsTheFileOfALiveItem(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, db := newStagingReclaim(t, staging, nil)

	staged := filepath.Join(staging, "Some Artist", "01 - track.m4a")
	writeAgedFile(t, staged, 2*time.Hour)
	stagedItem(t, db, "downloading", staged)

	report := reclaim.Reclaim(context.Background(), "test-worker")

	assert.Zero(t, report.Removed, "a live item's file must not be reclaimed")
	assert.Positive(t, report.Deferred, "the deferral must be reported")
	_, err := os.Stat(staged)
	require.NoError(t, err, "the file of a live item must survive")
}

// A fallback that fails partway leaves a file no item has ever heard of. That is
// what the orphan pass is for.
func TestStagingReclaim_ReclaimsAnOrphanOlderThanTheGrace(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, _ := newStagingReclaim(t, staging, nil)

	orphan := filepath.Join(staging, "Unknown Artist", "Unknown Album", "partial.webm.part")
	writeAgedFile(t, orphan, 2*time.Hour)

	report := reclaim.Reclaim(context.Background(), "test-worker")

	assert.Equal(t, 1, report.Orphans, "the unreferenced file must be noticed")
	assert.Equal(t, 1, report.Removed, "and reclaimed once it is past the grace")
	_, err := os.Stat(orphan)
	assert.True(t, os.IsNotExist(err), "the orphan must be gone")
}

// The grace is what keeps the janitor from racing a transfer that is merely
// slow, so a fresh file — referenced or not — has to survive.
func TestStagingReclaim_KeepsAFileYoungerThanTheGrace(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, _ := newStagingReclaim(t, staging, nil)

	fresh := filepath.Join(staging, "Unknown Artist", "in-flight.mp3.part")
	writeAgedFile(t, fresh, time.Minute)

	report := reclaim.Reclaim(context.Background(), "test-worker")

	// Not even counted: "orphans" means unreferenced *and* past the grace, which
	// is the number an operator would act on.
	assert.Zero(t, report.Orphans, "a file inside the grace is not an orphan yet")
	assert.Zero(t, report.Removed, "and it is certainly not removed")
	_, err := os.Stat(fresh)
	require.NoError(t, err, "a file inside the grace must survive")
}

// A live item expecting a file in a directory means the album is still in
// flight, so the whole directory is left alone — the same rule the owner applies
// to siblings it can see.
func TestStagingReclaim_KeepsADirectoryALiveItemIsStillFilling(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, db := newStagingReclaim(t, staging, nil)

	albumDir := filepath.Join(staging, "Some Artist", "Some Album")
	// Aged residue in the same album...
	residue := filepath.Join(albumDir, "leftover.flac")
	writeAgedFile(t, residue, 2*time.Hour)
	// ...and a live item that still expects a file in that directory.
	stagedItem(t, db, "downloading", filepath.Join(albumDir, "02 - pending.flac"))

	report := reclaim.Reclaim(context.Background(), "test-worker")

	assert.Equal(t, 1, report.SiblingsKept, "the sibling directory must be recognised as live")
	assert.Zero(t, report.Removed, "nothing in a live item's directory may be reclaimed")
	_, err := os.Stat(residue)
	require.NoError(t, err, "the residue must survive while the album is in flight")
}

func TestStagingReclaim_OrphanRemovalCanBeDisabled(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, _ := newStagingReclaim(t, staging, func(cfg *StagingReclaimConfig) {
		cfg.RemoveOrphans = false
	})

	orphan := filepath.Join(staging, "Unknown Artist", "partial.webm.part")
	writeAgedFile(t, orphan, 2*time.Hour)

	report := reclaim.Reclaim(context.Background(), "test-worker")

	assert.Equal(t, 1, report.Orphans, "the orphan is still reported")
	assert.Zero(t, report.Removed, "but nothing is removed when the pass is off")
	_, err := os.Stat(orphan)
	require.NoError(t, err, "the orphan must survive with the pass disabled")
}

// A recorded path that is not under the staging root is refused by the owner,
// and the janitor must not route around that refusal.
func TestStagingReclaim_DoesNotReachOutsideTheStagingRoot(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, db := newStagingReclaim(t, staging, nil)

	outside := filepath.Join(t.TempDir(), "Elsewhere", "01 - track.m4a")
	writeAgedFile(t, outside, 2*time.Hour)
	stagedItem(t, db, "imported", outside)

	report := reclaim.Reclaim(context.Background(), "test-worker")

	assert.Zero(t, report.Removed, "the root guard must hold for the janitor too")
	_, err := os.Stat(outside)
	require.NoError(t, err, "a file outside the staging root must not be touched")
}

// A file pass one deferred is owned, not orphaned. What protects it is the
// sibling rule: the live item sharing the path keeps the whole directory out of
// the orphan pass, so pass two never gets to second-guess pass one.
func TestStagingReclaim_DoesNotOrphanAFileItAlreadyDeferred(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, db := newStagingReclaim(t, staging, nil)

	shared := filepath.Join(staging, "Some Artist", "shared.m4a")
	writeAgedFile(t, shared, 2*time.Hour)
	stagedItem(t, db, "completed (duplicate album)", shared)
	stagedItem(t, db, "queued", shared)

	report := reclaim.Reclaim(context.Background(), "test-worker")

	assert.Zero(t, report.Orphans, "a file pass one deferred is not an orphan")
	assert.Equal(t, 1, report.SiblingsKept, "its live directory keeps it out of pass two")
	assert.Zero(t, report.Removed, "and it is certainly not removed")
	_, err := os.Stat(shared)
	require.NoError(t, err, "the shared file must survive")
}

func TestStagingReclaim_DisabledJanitorReturnsImmediately(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, _ := newStagingReclaim(t, staging, func(cfg *StagingReclaimConfig) {
		cfg.Enabled = false
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		reclaim.Run(context.Background(), "test-worker")
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a disabled janitor must return rather than block on its ticker")
	}
}

// A staging root that does not exist is a host where nothing was ever staged;
// the janitor must not treat that as a reason to stop working.
func TestStagingReclaim_MissingStagingRootIsNotAnError(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "never-created")
	reclaim, _, _ := newStagingReclaim(t, staging, nil)

	require.Empty(t, reclaim.Reclaim(context.Background(), "test-worker"),
		"a missing staging root must produce an empty report, not a panic")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newStagingReclaim wires a janitor over a file-backed DB and a staging root.
func newStagingReclaim(t *testing.T, staging string, tweak func(*StagingReclaimConfig)) (*StagingReclaim, *AcquisitionHandler, *gorm.DB) {
	t.Helper()

	db := stagingImportTestDB(t)
	handler := NewAcquisitionHandler(db, cfgWithStaging(t, staging),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cfg := DefaultStagingReclaimConfig()
	if tweak != nil {
		tweak(&cfg)
	}

	return NewStagingReclaim(db, handler, cfg), handler, db
}

// writeAgedFile writes a file and backdates it, because the janitor's only clock
// is the file's own mtime (jobitems has no updated_at to consult).
func writeAgedFile(t *testing.T, path string, age time.Duration) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("staged audio bytes"), 0o644))

	stamp := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, stamp, stamp))
}

var _ = database.JobItem{}

// Pass one acts on a snapshot, and that snapshot goes stale in the one direction
// that hurts: RetryJob resets a terminal item to queued and the claimant picks it
// up again. The janitor therefore passes no item ID, so the owner's live-owner
// check — which re-reads the row — is authoritative instead of the snapshot.
func TestStagingReclaim_KeepsTheFileOfAnItemRetriedAfterTheSnapshot(t *testing.T) {
	staging := t.TempDir()
	reclaim, _, db := newStagingReclaim(t, staging, nil)

	staged := filepath.Join(staging, "Some Artist", "01 - track.m4a")
	writeAgedFile(t, staged, 2*time.Hour)
	item := stagedItem(t, db, "imported", staged)

	// The snapshot this pass is holding still says "imported"; the row does not.
	require.NoError(t, db.Model(&database.JobItem{}).Where("id = ?", item.ID).
		Update("status", "queued").Error)

	assert.False(t, reclaim.reclaimTerminalFile(context.Background(), item),
		"an item retried into the queue must not have its staged file reclaimed")
	_, err := os.Stat(staged)
	require.NoError(t, err, "the file the retried item will download into must survive")
}
