package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// requireFFmpeg skips tag tests when the container tooling is unavailable
// (minimal CI runners); the Docker integration suite always has it.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
}

// seedDriftedLibrary builds the live shape this repair exists for: one album
// folder holding a correctly tagged file and a second whose tags carry the
// peer's casing.
func seedDriftedLibrary(t *testing.T, root string) (canonicalPath, driftedPath string) {
	t.Helper()
	albumDir := filepath.Join(root, "PUP", "PUP")
	require.NoError(t, os.MkdirAll(albumDir, 0o755))

	canonicalPath = generateTestAudio(t, albumDir, "01 - Lionheart.mp3",
		"-metadata", "album_artist=PUP", "-metadata", "album=PUP",
		"-metadata", "artist=PUP", "-metadata", "title=Lionheart")
	driftedPath = generateTestAudio(t, albumDir, "02 - Morbid Stuff.mp3",
		"-metadata", "album_artist=Pup", "-metadata", "album=pup",
		"-metadata", "artist=pup", "-metadata", "title=Morbid Stuff")
	return canonicalPath, driftedPath
}

// seedCanonicalHistory commits the casing the library already uses, which is
// what a live library has after an earlier import.
func seedCanonicalHistory(t *testing.T, db *gorm.DB, artist, album string) {
	t.Helper()
	require.NoError(t, db.Create(&database.Acquisition{Artist: artist, Album: album}).Error)
}

func TestPlanIdentityTagRepair_FindsCaseDrift(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	db := repairTestDB(t)
	seedCanonicalHistory(t, db, "PUP", "PUP")
	root := t.TempDir()
	_, drifted := seedDriftedLibrary(t, root)

	plan, err := PlanIdentityTagRepair(db, NewMetadataExtractor(), root)
	require.NoError(t, err)

	require.Equal(t, 2, plan.Scanned)
	require.Len(t, plan.Fixes, 1, "only the peer-cased file needs a rewrite")

	fix := plan.Fixes[0]
	require.Equal(t, filepath.Join("PUP", "PUP", filepath.Base(drifted)), fix.File)
	require.Equal(t, "Pup", fix.AlbumArtistFrom)
	require.Equal(t, "PUP", fix.AlbumArtistTo)
	require.Equal(t, "pup", fix.AlbumFrom)
	require.Equal(t, "PUP", fix.AlbumTo)
	require.Equal(t, "pup", fix.TrackArtistFrom)
	require.Equal(t, "PUP", fix.TrackArtistTo)

	// The count a client shows: "Pup" and "pup" stop being artists of their own.
	require.Equal(t, 3, plan.DistinctArtistsBefore)
	require.Equal(t, 1, plan.DistinctArtistsAfter)
	require.Equal(t, []string{"Pup", "pup"}, plan.ArtistsRemoved)
}

func TestApplyIdentityTagRepair_BacksUpAndRewrites(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	db := repairTestDB(t)
	seedCanonicalHistory(t, db, "PUP", "PUP")
	root := t.TempDir()
	canonical, drifted := seedDriftedLibrary(t, root)

	ext := NewMetadataExtractor()
	plan, err := PlanIdentityTagRepair(db, ext, root)
	require.NoError(t, err)
	require.Len(t, plan.Fixes, 1)

	driftedBefore := fileHash(t, drifted)
	canonicalBefore := fileHash(t, canonical)

	backup := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, ApplyIdentityTagRepair(context.Background(), ext, root, backup, plan))
	require.Empty(t, plan.Failures)
	require.Equal(t, 1, plan.BackedUp)
	require.Equal(t, 1, plan.Rewritten)

	// The backup holds the original bytes of the file that changed.
	backedUp := filepath.Join(plan.BackupDir, plan.Fixes[0].File)
	require.Equal(t, driftedBefore, fileHash(t, backedUp), "backup must be the pre-repair bytes")

	// The file a client reads now agrees with the library's casing.
	m := readTagFile(drifted)
	require.NotNil(t, m)
	require.Equal(t, "PUP", m.AlbumArtist())
	require.Equal(t, "PUP", m.Album())
	require.Equal(t, "PUP", m.Artist())
	require.Equal(t, "Morbid Stuff", m.Title(), "unrelated tags survive")

	// A file that already agreed is neither backed up nor rewritten.
	require.Equal(t, canonicalBefore, fileHash(t, canonical))

	// And the repair is idempotent: nothing left to do, no drift invented.
	again, err := PlanIdentityTagRepair(db, ext, root)
	require.NoError(t, err)
	require.Empty(t, again.Fixes)
	require.Equal(t, again.DistinctArtistsBefore, again.DistinctArtistsAfter)
	require.Empty(t, again.ArtistsRemoved)
}

func TestPlanIdentityTagRepair_LeavesAnUnknownIdentityAlone(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	// No committed history and no matching folder: the library has no opinion
	// about this artist, so the peer's own casing is all there is to go on and
	// the plan must not rewrite it.
	db := repairTestDB(t)
	root := t.TempDir()
	dir := filepath.Join(root, "Some Band", "Some Album")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := generateTestAudio(t, dir, "01 - Track.mp3",
		"-metadata", "album_artist=Some Band", "-metadata", "album=Some Album",
		"-metadata", "artist=Some Band")

	plan, err := PlanIdentityTagRepair(db, NewMetadataExtractor(), root)
	require.NoError(t, err)
	require.Empty(t, plan.Fixes)
	require.Equal(t, 1, plan.DistinctArtistsBefore)
	require.Equal(t, 1, plan.DistinctArtistsAfter)

	before := fileHash(t, path)
	require.NoError(t, ApplyIdentityTagRepair(context.Background(), NewMetadataExtractor(), root,
		filepath.Join(t.TempDir(), "backup"), plan))
	require.Equal(t, before, fileHash(t, path))
}

func TestPlanIdentityTagRepair_ACreditForAnotherPerformerIsNotDrift(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	// The album artist is the library's; the track artist is a real
	// collaboration. Correcting the credit would destroy information.
	db := repairTestDB(t)
	seedCanonicalHistory(t, db, "Every Time I Die", "Gutter Phenomenon")
	root := t.TempDir()
	dir := filepath.Join(root, "Every Time I Die", "Gutter Phenomenon")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	generateTestAudio(t, dir, "01 - Kill the Music.mp3",
		"-metadata", "album_artist=Every Time I Die",
		"-metadata", "album=Gutter Phenomenon",
		"-metadata", "artist=Daryl Palumbo")

	plan, err := PlanIdentityTagRepair(db, NewMetadataExtractor(), root)
	require.NoError(t, err)
	require.Empty(t, plan.Fixes, "a genuine credit is not casing drift")
}

func TestApplyIdentityTagRepair_RefusesABackupInsideTheLibrary(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	db := repairTestDB(t)
	seedCanonicalHistory(t, db, "PUP", "PUP")
	root := t.TempDir()
	_, drifted := seedDriftedLibrary(t, root)

	ext := NewMetadataExtractor()
	plan, err := PlanIdentityTagRepair(db, ext, root)
	require.NoError(t, err)
	require.Len(t, plan.Fixes, 1)

	before := fileHash(t, drifted)
	inside := filepath.Join(root, "backup")
	err = ApplyIdentityTagRepair(context.Background(), ext, root, inside, plan)
	require.Error(t, err, "a backup inside the library would be indexed as a second copy")
	require.Contains(t, err.Error(), "inside the library root")
	require.Equal(t, before, fileHash(t, drifted), "a refused run must not have written anything")
	require.Equal(t, 0, plan.Rewritten)

	// A sibling that merely shares a name prefix is genuinely outside the
	// library, so it is allowed — the check is a path relationship, not a string
	// prefix (which is what would wrongly refuse "music-backup" beside
	// "music").
	sibling := filepath.Join(t.TempDir(), filepath.Base(root)+"-backup")
	require.NoError(t, ApplyIdentityTagRepair(context.Background(), ext, root, sibling, plan))
	require.Equal(t, 1, plan.Rewritten)
}

func TestApplyIdentityTagRepair_RequiresABackupDirectory(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	db := repairTestDB(t)
	seedCanonicalHistory(t, db, "PUP", "PUP")
	root := t.TempDir()
	_, drifted := seedDriftedLibrary(t, root)

	ext := NewMetadataExtractor()
	plan, err := PlanIdentityTagRepair(db, ext, root)
	require.NoError(t, err)
	require.Len(t, plan.Fixes, 1)

	before := fileHash(t, drifted)
	for _, backupDir := range []string{"", "   "} {
		err := ApplyIdentityTagRepair(context.Background(), ext, root, backupDir, plan)
		require.Error(t, err)
		// The message must be the guard's own: an unrelated failure (an
		// uncreatable path, say) would let the guard be deleted while this test
		// still passed.
		require.Contains(t, err.Error(), "a backup directory is required")
		require.Equal(t, before, fileHash(t, drifted), "an unbacked run must refuse rather than rewrite")
		require.Equal(t, 0, plan.Rewritten)
	}

	// A plan with nothing to do needs no backup at all.
	nothingToDo := t.TempDir()
	empty, err := PlanIdentityTagRepair(db, ext, nothingToDo)
	require.NoError(t, err)
	require.Empty(t, empty.Fixes)
	require.NoError(t, ApplyIdentityTagRepair(context.Background(), ext, root, "", empty))
}

func TestPlanIdentityTagRepair_ReportsASanitisedFolderNameWithoutRewritingIt(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	// Observed on the live beta library: the tag carries the real title, the
	// folder carries the sanitised name the resolver answers with. Rewriting the
	// tag would trade real punctuation for a folder-safe variant.
	db := repairTestDB(t)
	root := t.TempDir()
	dir := filepath.Join(root, "Various Artists", "Triple J- Like a Version, Volume 13")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := generateTestAudio(t, dir, "18 - You Don't Get Me High Anymore.mp3",
		"-metadata", "album_artist=Various Artists",
		"-metadata", "album=Triple J: Like a Version, Volume 13",
		"-metadata", "artist=Phantogram")

	plan, err := PlanIdentityTagRepair(db, NewMetadataExtractor(), root)
	require.NoError(t, err)
	require.Empty(t, plan.Fixes, "a sanitised folder name is not casing drift")
	require.Equal(t, []string{filepath.Join("Various Artists", "Triple J- Like a Version, Volume 13",
		"18 - You Don't Get Me High Anymore.mp3")}, plan.LeftAlone)

	// And applying an empty plan touches nothing.
	before := fileHash(t, path)
	require.NoError(t, ApplyIdentityTagRepair(context.Background(), NewMetadataExtractor(), root,
		filepath.Join(t.TempDir(), "backup"), plan))
	require.Equal(t, before, fileHash(t, path))
}

// TestApplyIdentityTagRepair_LeavesAFileThatChangedSinceThePlan pins the guard
// that makes the dry-run/apply split safe. The plan is written by one command and
// applied by another, so a file an import replaced in between carries a different
// identity — writing this plan's canonical values onto it would rename another
// track rather than correct casing.
func TestApplyIdentityTagRepair_LeavesAFileThatChangedSinceThePlan(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	db := repairTestDB(t)
	seedCanonicalHistory(t, db, "PUP", "PUP")
	root := t.TempDir()
	_, drifted := seedDriftedLibrary(t, root)

	ext := NewMetadataExtractor()
	plan, err := PlanIdentityTagRepair(db, ext, root)
	require.NoError(t, err)
	require.Len(t, plan.Fixes, 1)

	// An import lands a different track at the same path between the two commands.
	require.NoError(t, os.Remove(drifted))
	generateTestAudio(t, filepath.Dir(drifted), filepath.Base(drifted),
		"-metadata", "album_artist=Another Band", "-metadata", "album=Another Album",
		"-metadata", "artist=Another Band", "-metadata", "title=Another Track")
	replacedBefore := fileHash(t, drifted)

	require.NoError(t, ApplyIdentityTagRepair(context.Background(), ext, root, t.TempDir(), plan))

	require.Equal(t, []string{filepath.Join("PUP", "PUP", filepath.Base(drifted))}, plan.Stale,
		"a file whose identity changed must be reported, not silently rewritten")
	require.Zero(t, plan.Rewritten)
	require.Zero(t, plan.BackedUp, "nothing is copied for a file that is left untouched")
	require.Empty(t, plan.Failures)

	require.Equal(t, replacedBefore, fileHash(t, drifted), "the replacement's bytes must be untouched")
	m := readTagFile(drifted)
	require.NotNil(t, m)
	require.Equal(t, "Another Band", m.AlbumArtist(), "a stale plan must not rename another track")
	require.Equal(t, "Another Album", m.Album())
}

// TestPlanIdentityTagRepair_DoesNotCountUnnamedFiles pins the rule the
// before/after numbers rest on: a client lists named artists, so a file with
// no artist tags is not an artist of its own. Counting it inflates the
// "before" number and makes the headline disagree with what a client shows.
func TestPlanIdentityTagRepair_DoesNotCountUnnamedFiles(t *testing.T) {
	requireProbeTools(t)
	requireFFmpeg(t)

	db := repairTestDB(t)
	seedCanonicalHistory(t, db, "PUP", "PUP")
	root := t.TempDir()
	seedDriftedLibrary(t, root)

	generateTestAudio(t, filepath.Join(root, "PUP", "PUP"), "03 - Untitled.mp3",
		"-metadata", "title=Untitled")

	plan, err := PlanIdentityTagRepair(db, NewMetadataExtractor(), root)
	require.NoError(t, err)

	require.Equal(t, 3, plan.Scanned)
	require.Equal(t, 3, plan.DistinctArtistsBefore, "an untagged file is not an artist")
	require.Equal(t, 1, plan.DistinctArtistsAfter)
}
