package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// repairTestDB returns an in-memory DB migrated with the tables the repair
// tooling touches.
func repairTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&database.Library{}, &database.Track{}, &database.Acquisition{}))
	return db
}

// seedFragmentedLibrary reproduces the observed beta fragmentation: one
// album ("Gutter Phenomenon") split across per-credit artist folders. The
// canonical folder already holds tracks 01+02; the "& Daryl Palumbo"
// fragment holds a duplicate copy of 02 (same bytes) plus unique track 03;
// the "& Gerard Way" fragment holds another duplicate copy of 02.
func seedFragmentedLibrary(t *testing.T, db *gorm.DB) (root, dup1Path, dup2Path string) {
	t.Helper()
	root = t.TempDir()

	lib := &database.Library{Name: "Music", Path: root}
	require.NoError(t, db.Create(lib).Error)

	mk := func(rel, content string) string {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		return p
	}
	// Canonical target content
	canon1 := mk(filepath.Join("Every Time I Die", "Gutter Phenomenon", "01 - Kill The Music.mp3"), "track1")
	canon2 := mk(filepath.Join("Every Time I Die", "Gutter Phenomenon", "02 - Choke-Son.mp3"), "track2")
	// Fragment: "& Daryl Palumbo" credit — dup of 02 + unique 03
	dup1Path = mk(filepath.Join("Every Time I Die & Daryl Palumbo", "Gutter Phenomenon", "02 - Choke-Son.mp3"), "track2")
	track3 := mk(filepath.Join("Every Time I Die & Daryl Palumbo", "Gutter Phenomenon", "03 - Bored Stiff.mp3"), "track3")
	// Fragment: "& Gerard Way" credit — another dup of 02
	dup2Path = mk(filepath.Join("Every Time I Die & Gerard Way", "Gutter Phenomenon", "02 - Choke-Son.mp3"), "track2")

	for _, p := range []string{canon1, canon2, dup1Path, track3, dup2Path} {
		require.NoError(t, db.Create(&database.Track{
			LibraryID: lib.ID, Title: "T", Artist: "Every Time I Die",
			Album: "Gutter Phenomenon", Path: p,
		}).Error)
	}
	return root, dup1Path, dup2Path
}

func TestDetectFragmentedAlbums_FindsCreditFragments(t *testing.T) {
	db := repairTestDB(t)
	root, _, _ := seedFragmentedLibrary(t, db)

	found, err := DetectFragmentedAlbums(db, root)
	require.NoError(t, err)
	require.Len(t, found, 1, "exactly one fragmented album expected")

	g := found[0]
	assert.Equal(t, "Gutter Phenomenon", g.Album)
	assert.Equal(t, 5, g.TrackCount)
	assert.Equal(t, "Every Time I Die", g.CanonicalFolder,
		"the fragment with most tracks (tie broken lexicographically) is canonical")
	require.Len(t, g.Folders, 3)
	assert.True(t, g.Folders[0].IsCanonical)

	// Detection is read-only: every fragment still on disk.
	for _, f := range g.Folders {
		_, err := os.Stat(filepath.Join(root, f.ArtistFolder, f.AlbumFolder))
		require.NoError(t, err)
	}
}

func TestDetectFragmentedAlbums_NoFalsePositives(t *testing.T) {
	db := repairTestDB(t)
	root := t.TempDir()
	lib := &database.Library{Name: "Music", Path: root}
	require.NoError(t, db.Create(lib).Error)

	// Same album folder name under DIFFERENT artists is legitimate
	// ("Greatest Hits" by two bands) — must not be flagged.
	mk := func(rel string) string {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
		return p
	}
	p1 := mk(filepath.Join("Band A", "Greatest Hits", "01.mp3"))
	p2 := mk(filepath.Join("Band B", "Greatest Hits", "01.mp3"))
	for _, p := range []string{p1, p2} {
		require.NoError(t, db.Create(&database.Track{LibraryID: lib.ID, Title: "T", Path: p}).Error)
	}

	found, err := DetectFragmentedAlbums(db, root)
	require.NoError(t, err)
	assert.Empty(t, found, "two different artists sharing an album name is not fragmentation")
}

func TestMergeAlbumFolders_DryRunTouchesNothing(t *testing.T) {
	db := repairTestDB(t)
	root, _, _ := seedFragmentedLibrary(t, db)

	report, err := MergeAlbumFolders(db, root, "Gutter Phenomenon", "Every Time I Die", true)
	require.NoError(t, err)
	assert.True(t, report.DryRun)
	assert.Len(t, report.Moved, 1, "only the unique fragment track should move")
	assert.Len(t, report.RemovedFiles, 2, "both same-content duplicates planned for removal")
	assert.Empty(t, report.Conflicts)
	assert.Len(t, report.RemovedDirs, 4, "two album fragments + two artist dirs predicted empty")

	// Nothing moved or deleted on disk.
	_, err = os.Stat(filepath.Join(root, "Every Time I Die & Daryl Palumbo", "Gutter Phenomenon", "03 - Bored Stiff.mp3"))
	require.NoError(t, err, "dry run must not move files")
	_, err = os.Stat(filepath.Join(root, "Every Time I Die & Gerard Way", "Gutter Phenomenon", "02 - Choke-Son.mp3"))
	require.NoError(t, err, "dry run must not delete duplicates")

	// Track rows untouched.
	var rows int64
	db.Model(&database.Track{}).Where("path LIKE ?", filepath.Join(root, "Every Time I Die & Daryl Palumbo", "%")).Count(&rows)
	assert.Equal(t, int64(2), rows, "dry run must not rewrite or delete track rows")
}

func TestMergeAlbumFolders_AppliesAndSweepsEmptyDirs(t *testing.T) {
	db := repairTestDB(t)
	root, dup1Path, dup2Path := seedFragmentedLibrary(t, db)

	report, err := MergeAlbumFolders(db, root, "Gutter Phenomenon", "Every Time I Die", false)
	require.NoError(t, err)
	assert.False(t, report.DryRun)
	assert.Len(t, report.Moved, 1)
	assert.Len(t, report.RemovedFiles, 2, "content duplicates removed")
	assert.Empty(t, report.Conflicts)
	assert.Empty(t, report.Errors)

	// Canonical album now holds all three unique tracks.
	for _, name := range []string{"01 - Kill The Music.mp3", "02 - Choke-Son.mp3", "03 - Bored Stiff.mp3"} {
		_, err := os.Stat(filepath.Join(root, "Every Time I Die", "Gutter Phenomenon", name))
		require.NoError(t, err, "canonical album should hold %s", name)
	}

	// Fragments gone: duplicate files removed, empty dirs swept.
	_, err = os.Stat(dup1Path)
	assert.True(t, os.IsNotExist(err), "content duplicate file should be deleted")
	_, err = os.Stat(dup2Path)
	assert.True(t, os.IsNotExist(err), "content duplicate file should be deleted")
	_, err = os.Stat(filepath.Join(root, "Every Time I Die & Daryl Palumbo"))
	assert.True(t, os.IsNotExist(err), "emptied artist folder should be removed")
	_, err = os.Stat(filepath.Join(root, "Every Time I Die & Gerard Way"))
	assert.True(t, os.IsNotExist(err), "emptied artist folder should be removed")
	// Canonical artist folder obviously still exists.
	_, err = os.Stat(filepath.Join(root, "Every Time I Die"))
	require.NoError(t, err)

	// Track rows: two dup rows deleted, remaining point at canonical paths.
	var paths []string
	require.NoError(t, db.Model(&database.Track{}).Order("path").Pluck("path", &paths).Error)
	require.Len(t, paths, 3, "duplicate track rows deleted")
	for _, p := range paths {
		assert.Contains(t, p, filepath.Join(root, "Every Time I Die", "Gutter Phenomenon"),
			"every remaining track row should point at the canonical folder: %s", p)
	}

	// After a successful merge, detection finds nothing left.
	found, err := DetectFragmentedAlbums(db, root)
	require.NoError(t, err)
	assert.Empty(t, found, "merged library should be clean")
}

func TestMergeAlbumFolders_ConflictingContentLeftInPlace(t *testing.T) {
	db := repairTestDB(t)
	root := t.TempDir()
	lib := &database.Library{Name: "Music", Path: root}
	require.NoError(t, db.Create(lib).Error)

	mk := func(rel, content string) string {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		return p
	}
	canon := mk(filepath.Join("Artist A", "Album X", "01.mp3"), "original")
	conflict := mk(filepath.Join("Artist B", "Album X", "01.mp3"), "DIFFERENT REMIX")
	for _, p := range []string{canon, conflict} {
		require.NoError(t, db.Create(&database.Track{LibraryID: lib.ID, Title: "T", Path: p}).Error)
	}

	report, err := MergeAlbumFolders(db, root, "Album X", "Artist A", false)
	require.NoError(t, err)
	require.Len(t, report.Conflicts, 1, "different content at same destination must conflict")
	assert.Empty(t, report.Moved)
	assert.Empty(t, report.RemovedFiles)
	assert.Empty(t, report.Errors)
	assert.Empty(t, report.RemovedDirs, "populated source dirs are kept")

	// Both files still exist; nothing deleted or moved.
	_, err = os.Stat(canon)
	require.NoError(t, err)
	_, err = os.Stat(conflict)
	require.NoError(t, err, "conflicting source must be left in place for manual resolution")
}

func TestMergeAlbumFolders_RejectsEscapingCanonicalFolder(t *testing.T) {
	db := repairTestDB(t)
	root, _, _ := seedFragmentedLibrary(t, db)

	_, err := MergeAlbumFolders(db, root, "Gutter Phenomenon", "..", false)
	require.Error(t, err, "a canonical folder escaping the library root must be rejected")
}
