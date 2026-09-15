package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedCaseLibrary writes tracks to disk under root and records a Track row for
// each, returning the library. Unlike the credit-variant fixture this creates
// real files, because the merge tests move them.
func seedCaseLibrary(t *testing.T, db *gorm.DB, root string, files map[string]string) {
	t.Helper()
	lib := &database.Library{Name: "Music", Path: root}
	require.NoError(t, db.Create(lib).Error)

	for rel, content := range files {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		require.NoError(t, db.Create(&database.Track{
			LibraryID: lib.ID, Title: "T", Path: p,
		}).Error)
	}
}

// The reachability rule, proved without depending on the filesystem: the source
// match folds case so a variant folder is reachable, while "already at the
// destination" stays exact so the canonical folder's own tracks are skipped.
// The pre-fix version compared the album segment exactly, which skipped the
// variant entirely — the merge reported "nothing to do" while the split stayed.
func TestAlbumDestSelector_ReachesCaseVariant(t *testing.T) {
	canonicalDir := filepath.Join("/lib", "PUP", "The Unraveling of Puptheband")
	sel := albumDestSelector(canonicalDir, "The Unraveling of Puptheband", "PUP")

	dest, ok := sel([]string{"PUP", "The Unraveling Of Puptheband", "02 - Totally Fine.mp3"})
	require.True(t, ok, "a case-variant album folder must be reachable")
	assert.Equal(t, filepath.Join(canonicalDir, "02 - Totally Fine.mp3"), dest)

	dest, ok = sel([]string{"Pup", "The Unraveling of Puptheband", "03 - x.mp3"})
	require.True(t, ok, "a case-variant artist folder must be reachable")
	assert.Equal(t, filepath.Join(canonicalDir, "03 - x.mp3"), dest)

	_, ok = sel([]string{"PUP", "The Unraveling of Puptheband", "01 - y.mp3"})
	assert.False(t, ok, "a track already exactly at the destination is left alone")

	_, ok = sel([]string{"PUP", "Morbid Stuff", "01 - z.mp3"})
	assert.False(t, ok, "a different album is not touched")

	_, ok = sel([]string{"Some Other Band", "The Unraveling Of Puptheband", "01.mp3"})
	assert.True(t, ok, "another artist folder holding this album is still a credit fragment")
}

func TestArtistDestSelector_ReachesCaseVariantArtist(t *testing.T) {
	sel := artistDestSelector("/lib", "PUP")

	dest, ok := sel([]string{"Pup", "Who Will Look After The Dogs", "01.mp3"})
	require.True(t, ok, "a case-variant artist folder must be reachable")
	assert.Equal(t, filepath.Join("/lib", "PUP", "Who Will Look After The Dogs", "01.mp3"), dest,
		"the album keeps its own folder name under the canonical artist")

	_, ok = sel([]string{"PUP", "Morbid Stuff", "01.mp3"})
	assert.False(t, ok, "the canonical artist folder is left alone")

	_, ok = sel([]string{"Pent Up Pup", "FURGAG", "01.mp3"})
	assert.False(t, ok, "a genuinely different artist is left alone")
}

// caseSensitiveFS reports whether this filesystem distinguishes "A" from "a".
//
// The beta library lives in a Linux container, where case-variant folders are
// genuinely distinct directories and the split is real. On a default Windows or
// macOS checkout they resolve to one directory, so the split cannot physically
// exist and no merge is observable — those assertions skip rather than pretend.
// The detection tests work off Track.Path strings, so they run everywhere.
func caseSensitiveFS(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "Case"), 0o755))
	_, err := os.Stat(filepath.Join(dir, "case"))
	return os.IsNotExist(err)
}

// The library the beta stack actually held: one album under two spellings that
// differ only in the capitalisation of "of". Grouping by the *exact* folder name
// puts these in two different map keys, so neither group ever reached the
// two-folder threshold and detect-fragments reported a clean library while the
// split sat on disk. The acquisition record is what the real library had, and
// earliest-wins makes its spelling canonical.
func TestDetectFragmentedAlbums_FlagsCaseOnlyAlbumSplit(t *testing.T) {
	db := repairTestDB(t)
	root := t.TempDir()
	seedCaseLibrary(t, db, root, map[string]string{
		"PUP/The Unraveling of Puptheband/01 - Robot Writes a Love Song.m4a": "a",
		"PUP/The Unraveling Of Puptheband/02 - Totally Fine.mp3":             "b",
	})
	require.NoError(t, db.Create(&database.Acquisition{
		Artist: "PUP", Album: "The Unraveling of Puptheband",
	}).Error)

	found, err := DetectFragmentedAlbums(db, root)
	require.NoError(t, err)
	require.Len(t, found, 1, "a case-only album split must be detected")

	g := found[0]
	assert.Equal(t, FragmentCaseAlbum, g.Kind)
	assert.Equal(t, 2, g.TrackCount)
	assert.Equal(t, "PUP", g.CanonicalFolder)
	// The acquisition record's spelling wins, not whichever fragment sorted
	// first — otherwise the repair picks a casing the importer will not reuse.
	assert.Equal(t, "The Unraveling of Puptheband", g.CanonicalAlbum)
	require.Len(t, g.Folders, 2)
	for _, f := range g.Folders {
		assert.True(t, strings.EqualFold(f.AlbumFolder, g.CanonicalAlbum),
			"every fragment is the same album up to case: %q", f.AlbumFolder)
	}
}

// An artist split whose albums do not coincide is invisible to the album axis:
// "PUP/Morbid Stuff" and "Pup/Who Will Look After The Dogs" group by different
// album keys, so each group looks whole. Only grouping by artist sees it.
func TestDetectLibraryFragments_FlagsCaseOnlyArtistSplit(t *testing.T) {
	db := repairTestDB(t)
	root := t.TempDir()
	seedCaseLibrary(t, db, root, map[string]string{
		"PUP/Morbid Stuff/01 - Kids.m4a":                     "a",
		"Pup/Who Will Look After The Dogs/01 - Pup Song.mp3": "b",
	})

	frags, err := DetectLibraryFragments(db, root)
	require.NoError(t, err)

	assert.Empty(t, frags.Albums, "the album axis cannot see this split")
	require.Len(t, frags.Artists, 1)
	require.Len(t, frags.Artists[0].Fragments, 2)
	assert.Equal(t, "PUP", frags.Artists[0].CanonicalFolder)
	assert.Equal(t, 2, frags.Artists[0].TrackCount)
	require.True(t, frags.Artists[0].Fragments[0].IsCanonical,
		"the canonical fragment must be flagged as the merge target")
}

// The false-positive control must survive the folding: two unrelated bands
// sharing an album name are not fragmentation. Folding case is exactly what
// could have made these look like one album.
func TestDetectFragmentedAlbums_StillIgnoresUnrelatedSameNameAlbums(t *testing.T) {
	db := repairTestDB(t)
	root := t.TempDir()
	seedCaseLibrary(t, db, root, map[string]string{
		"Band A/Greatest Hits/01.mp3": "a",
		"Band B/Greatest Hits/01.mp3": "b",
	})

	frags, err := DetectLibraryFragments(db, root)
	require.NoError(t, err)
	assert.Empty(t, frags.Albums, "unrelated artists sharing an album name is not fragmentation")
	assert.Empty(t, frags.Artists, "neither artist name is a case variant of the other")
}

// A credit variant must still be classified as such, not swallowed by the new
// case branch.
func TestDetectFragmentedAlbums_StillClassifiesCreditVariants(t *testing.T) {
	db := repairTestDB(t)
	root, _, _ := seedFragmentedLibrary(t, db)

	found, err := DetectFragmentedAlbums(db, root)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, FragmentCreditVariants, found[0].Kind)
}

// The merge must be able to *reach* the variant. The source match used to be an
// exact `parts[1] != albumFolder`, so the "Of" folder was skipped entirely and
// the merge reported nothing to do while the split remained.
func TestMergeAlbumFolders_ReachesCaseVariantAlbum(t *testing.T) {
	if !caseSensitiveFS(t) {
		t.Skip("a case-variant folder is the same directory here; the repair is only observable where the split exists")
	}
	db := repairTestDB(t)
	root := t.TempDir()
	seedCaseLibrary(t, db, root, map[string]string{
		"PUP/The Unraveling of Puptheband/01 - Robot Writes a Love Song.m4a": "a",
		"PUP/The Unraveling Of Puptheband/02 - Totally Fine.mp3":             "b",
	})

	report, err := MergeAlbumFolders(db, root, "The Unraveling of Puptheband", "PUP", false)
	require.NoError(t, err)
	assert.Empty(t, report.Errors)
	assert.Len(t, report.Moved, 1, "the case-variant track must be relocated")

	canonical := filepath.Join(root, "PUP", "The Unraveling of Puptheband")
	for _, name := range []string{"01 - Robot Writes a Love Song.m4a", "02 - Totally Fine.mp3"} {
		_, err := os.Stat(filepath.Join(canonical, name))
		require.NoError(t, err, "canonical album should hold %s", name)
	}
	_, err = os.Stat(filepath.Join(root, "PUP", "The Unraveling Of Puptheband"))
	assert.True(t, os.IsNotExist(err), "the case-variant album folder should be swept")

	var paths []string
	require.NoError(t, db.Model(&database.Track{}).Pluck("path", &paths).Error)
	require.Len(t, paths, 2)
	for _, p := range paths {
		assert.True(t, strings.HasPrefix(p, canonical),
			"every remaining row should point at the canonical folder: %s", p)
	}

	// Convergence: nothing left to repair.
	found, err := DetectFragmentedAlbums(db, root)
	require.NoError(t, err)
	assert.Empty(t, found, "the repaired library should be clean")
}

func TestMergeArtistFolders_MergesCaseVariantArtist(t *testing.T) {
	if !caseSensitiveFS(t) {
		t.Skip("a case-variant folder is the same directory here; the repair is only observable where the split exists")
	}
	db := repairTestDB(t)
	root := t.TempDir()
	seedCaseLibrary(t, db, root, map[string]string{
		"PUP/Morbid Stuff/01 - Kids.m4a":                     "a",
		"Pup/Who Will Look After The Dogs/01 - Pup Song.mp3": "b",
	})

	report, err := MergeArtistFolders(db, root, "PUP", false)
	require.NoError(t, err)
	assert.Empty(t, report.Errors)
	assert.Len(t, report.Moved, 1, "the losing artist folder's track must be relocated")

	_, err = os.Stat(filepath.Join(root, "PUP", "Who Will Look After The Dogs", "01 - Pup Song.mp3"))
	require.NoError(t, err, "the album keeps its name under the canonical artist folder")
	_, err = os.Stat(filepath.Join(root, "Pup"))
	assert.True(t, os.IsNotExist(err), "the emptied case-variant artist folder should be swept")

	frags, err := DetectLibraryFragments(db, root)
	require.NoError(t, err)
	assert.Empty(t, frags.Artists, "the repaired artist should be clean")
}

// On a case-insensitive filesystem the variant folder IS the canonical folder,
// so source and destination are the same file. The merge must leave it alone:
// comparing it to itself reports "same content" and would delete a real track.
func TestMergeAlbumFolders_CaseInsensitiveFSNeverDeletesSelfDuplicate(t *testing.T) {
	if caseSensitiveFS(t) {
		t.Skip("only meaningful where a case-variant folder resolves to the same directory")
	}
	db := repairTestDB(t)
	root := t.TempDir()
	seedCaseLibrary(t, db, root, map[string]string{
		"PUP/The Unraveling of Puptheband/01 - Robot.m4a":        "a",
		"PUP/The Unraveling Of Puptheband/02 - Totally Fine.mp3": "b",
	})

	report, err := MergeAlbumFolders(db, root, "The Unraveling of Puptheband", "PUP", false)
	require.NoError(t, err)
	assert.Empty(t, report.Moved)
	assert.Empty(t, report.RemovedFiles, "no track may be removed as its own duplicate")
	assert.Empty(t, report.Errors)

	var rows int64
	db.Model(&database.Track{}).Count(&rows)
	assert.Equal(t, int64(2), rows, "no track row may be deleted")
}

func TestMergeArtistFolders_DryRunTouchesNothing(t *testing.T) {
	if !caseSensitiveFS(t) {
		t.Skip("a case-variant folder is the same directory here; the repair is only observable where the split exists")
	}
	db := repairTestDB(t)
	root := t.TempDir()
	seedCaseLibrary(t, db, root, map[string]string{
		"Pup/Who Will Look After The Dogs/01 - Pup Song.mp3": "b",
	})

	report, err := MergeArtistFolders(db, root, "PUP", true)
	require.NoError(t, err)
	assert.True(t, report.DryRun)
	assert.Len(t, report.Moved, 1)

	_, err = os.Stat(filepath.Join(root, "Pup", "Who Will Look After The Dogs", "01 - Pup Song.mp3"))
	require.NoError(t, err, "dry run must not move files")
}

func TestMergeArtistFolders_RejectsEscapingCanonicalFolder(t *testing.T) {
	db := repairTestDB(t)
	root := t.TempDir()
	seedCaseLibrary(t, db, root, map[string]string{"Pup/Album/01.mp3": "b"})

	_, err := MergeArtistFolders(db, root, "..", false)
	require.Error(t, err, "a canonical folder escaping the library root must be rejected")
}
