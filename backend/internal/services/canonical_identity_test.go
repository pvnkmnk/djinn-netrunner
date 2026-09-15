package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newCanonicalIdentityHandler builds a handler over a file-backed SQLite DB and
// an empty library root. File-backed (not ":memory:") because the acquisition
// path may use pooled connections, and each pooled connection would otherwise
// open its own private in-memory database.
func newCanonicalIdentityHandler(t *testing.T) (*AcquisitionHandler, *gorm.DB, string) {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "canonical_identity.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&database.Acquisition{}))

	// Windows will not delete the TempDir while the DB file is still open.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	root := t.TempDir()
	h := &AcquisitionHandler{
		// db is not a promoted-field literal: naming the embedded struct is what
		// the go1.25 language version in go.mod allows.
		BaseHandler: BaseHandler{db: db},
		cfg:         &config.Config{MusicLibraryPath: root},
		ext:         NewMetadataExtractor(),
	}

	return h, db, root
}

// seedAcquisition inserts a row; the acquisition history is how the library
// records the artist/album casing it committed to.
func seedAcquisition(t *testing.T, db *gorm.DB, artist, album, finalPath string) *database.Acquisition {
	t.Helper()

	acq := &database.Acquisition{
		JobID:        1,
		JobItemID:    1,
		Artist:       artist,
		Album:        album,
		TrackTitle:   "Track",
		OriginalPath: "/staging/track.mp3",
		FinalPath:    finalPath,
	}
	require.NoError(t, db.Create(acq).Error)

	return acq
}

func mkLibraryDirs(t *testing.T, parts ...string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(parts...), 0o755))
}

func TestCanonicalKey(t *testing.T) {
	tests := map[string]string{
		"The Unraveling Of Puptheband": "the unraveling of puptheband",
		"PUP":                          "pup",
		"  PUP  ":                      "pup",
		"":                             "",
	}

	for in, want := range tests {
		require.Equal(t, want, CanonicalKey(in), "CanonicalKey(%q)", in)
	}
}

func TestLibraryRoot_FallsBackToDefault(t *testing.T) {
	require.Equal(t, "./music_library", (&AcquisitionHandler{}).libraryRoot())
	require.Equal(t, "/tmp/lib", (&AcquisitionHandler{cfg: &config.Config{MusicLibraryPath: "/tmp/lib"}}).libraryRoot())
}

// resolveIdentity calls the resolver and fails the test on a lookup error.
func resolveIdentity(t *testing.T, h *AcquisitionHandler, artist, album string) (string, string) {
	t.Helper()
	gotArtist, gotAlbum, err := h.resolveCanonicalIdentity(artist, album)
	require.NoError(t, err)
	return gotArtist, gotAlbum
}

// DJI-489: the beta library holds "The Unraveling of Puptheband" (imported
// earlier) beside "The Unraveling Of Puptheband". Either casing in a fresh
// re-acquire must resolve to the earlier one, so imports converge on a single
// folder instead of creating another case variant.
func TestResolveCanonicalIdentity_EarliestCasingWins(t *testing.T) {
	h, db, root := newCanonicalIdentityHandler(t)
	seedAcquisition(t, db, "PUP", "The Unraveling of Puptheband",
		filepath.Join(root, "PUP", "The Unraveling of Puptheband", "03 - Robot Writes a Love Song.mp3"))
	seedAcquisition(t, db, "PUP", "The Unraveling Of Puptheband",
		filepath.Join(root, "PUP", "The Unraveling Of Puptheband", "12 - PUPTHEBAND Inc. Is Filing For Bankruptcy.mp3"))

	artist, album := resolveIdentity(t, h, "PUP", "The Unraveling Of Puptheband")
	require.Equal(t, "PUP", artist)
	require.Equal(t, "The Unraveling of Puptheband", album)

	// Casing anywhere in the pair folds the same way.
	artist, album = resolveIdentity(t, h, "pup", "the unraveling OF puptheband")
	require.Equal(t, "PUP", artist)
	require.Equal(t, "The Unraveling of Puptheband", album)
}

// A new album by an artist the library already knows must not create a
// case-variant artist folder — artist casing resolves independently of album.
func TestResolveCanonicalIdentity_ArtistCasingAppliesToNewAlbum(t *testing.T) {
	h, _, root := newCanonicalIdentityHandler(t)
	mkLibraryDirs(t, root, "PUP", "Morbid Stuff")

	artist, album := resolveIdentity(t, h, "pup", "Brand New Album")
	require.Equal(t, "PUP", artist, "existing artist folder casing must win")
	require.Equal(t, "Brand New Album", album, "a genuinely new album keeps its tag casing")
}

// Libraries built before acquisitions were recorded have only folders to go on.
func TestResolveCanonicalIdentity_FallsBackToLibraryFolders(t *testing.T) {
	h, _, root := newCanonicalIdentityHandler(t)
	mkLibraryDirs(t, root, "PUP", "The Unraveling of Puptheband")

	artist, album := resolveIdentity(t, h, "pup", "the unraveling OF puptheband")
	require.Equal(t, "PUP", artist)
	require.Equal(t, "The Unraveling of Puptheband", album)
}

// Folder names are sanitised, so an album tagged with a "?" must still match the
// folder the library actually created for it.
func TestResolveCanonicalIdentity_MatchesSanitisedFolderName(t *testing.T) {
	h, _, root := newCanonicalIdentityHandler(t)
	mkLibraryDirs(t, root, "PUP", "Who Will Look After the Dogs")

	artist, album := resolveIdentity(t, h, "PUP", "Who Will Look After the Dogs?")
	require.Equal(t, "PUP", artist)
	require.Equal(t, "Who Will Look After the Dogs", album)
}

func TestResolveCanonicalIdentity_NewPairKeepsTagCasing(t *testing.T) {
	h, _, root := newCanonicalIdentityHandler(t)
	mkLibraryDirs(t, root, "Someone Else", "Other Album")

	artist, album := resolveIdentity(t, h, "Some Artist", "Some Album")
	require.Equal(t, "Some Artist", artist)
	require.Equal(t, "Some Album", album)
}

// The album dedup compared `artist = ? AND album = ?`, which Postgres and SQLite
// both compare case-sensitively — so a re-acquire tagged with different casing
// was imported a second time instead of being recognised as a duplicate.
func TestFindExistingAlbumAcquisition_FoldsCase(t *testing.T) {
	h, db, root := newCanonicalIdentityHandler(t)
	first := seedAcquisition(t, db, "PUP", "The Unraveling of Puptheband",
		filepath.Join(root, "PUP", "The Unraveling of Puptheband", "03 - Robot Writes a Love Song.mp3"))
	seedAcquisition(t, db, "PUP", "The Unraveling Of Puptheband",
		filepath.Join(root, "PUP", "The Unraveling Of Puptheband", "12 - PUPTHEBAND Inc. Is Filing For Bankruptcy.mp3"))

	found, err := h.findExistingAlbumAcquisition("PUP", "PUP", "The Unraveling Of Puptheband", "different-hash")
	require.NoError(t, err)
	require.Equal(t, first.ID, found.ID, "the earliest acquisition is the canonical one")

	// The pre-DJI-489 comparison is the bug: it does not see the case variant at
	// all, so the duplicate was invisible and a second copy was imported.
	var exact database.Acquisition
	err = db.Where("artist = ? AND album = ?", "PUP", "the unraveling of puptheband").First(&exact).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "case-sensitive lookup is exactly the bug being fixed")
}

func TestFindExistingAlbumAcquisition_SameFileIsNotAnAlbumDuplicate(t *testing.T) {
	h, db, root := newCanonicalIdentityHandler(t)
	acq := seedAcquisition(t, db, "PUP", "Morbid Stuff",
		filepath.Join(root, "PUP", "Morbid Stuff", "01 - Track.mp3"))
	require.NoError(t, db.Model(acq).Update("file_hash", "abc123").Error)

	// The exact file just installed is not a duplicate of itself. A missing row
	// comes back as (nil, nil) rather than gorm.ErrRecordNotFound, because the
	// import caller reads any non-nil error as "the lookup failed" — an error
	// here would make a real database failure look like a new album.
	found, err := h.findExistingAlbumAcquisition("PUP", "PUP", "Morbid Stuff", "abc123")
	require.NoError(t, err, "a missing row is not a failure")
	require.Nil(t, found)

	found, err = h.findExistingAlbumAcquisition("PUP", "PUP", "Morbid Stuff", "some-other-hash")
	require.NoError(t, err)
	require.Equal(t, acq.ID, found.ID)
}

// A failed query must never be reported as "no duplicate": the import would then
// install a second copy of an album the library already holds.
func TestFindExistingAlbumAcquisition_PropagatesQueryFailure(t *testing.T) {
	h, db, _ := newCanonicalIdentityHandler(t)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close(), "closing the pool makes the next query fail")

	found, err := h.findExistingAlbumAcquisition("PUP", "PUP", "Morbid Stuff", "hash")
	require.Error(t, err, "a failed lookup must reach the caller")
	require.NotErrorIs(t, err, gorm.ErrRecordNotFound)
	require.Nil(t, found)
}

// A row records metadata.Artist (the track artist) while the import keys the
// lookup on the canonical album artist, so a multi-credit track would never
// match its own album unless the lookup folds both identities together.
func TestFindExistingAlbumAcquisition_MatchesTrackArtistAgainstAlbumArtist(t *testing.T) {
	h, db, root := newCanonicalIdentityHandler(t)
	credit := "Every Time I Die & Daryl Palumbo"
	row := seedAcquisition(t, db, credit, "Gutter Phenomenon",
		filepath.Join(root, "Every Time I Die", "Gutter Phenomenon", "02 - Choke-Son.mp3"))

	found, err := h.findExistingAlbumAcquisition("Every Time I Die", credit, "Gutter Phenomenon", "different-hash")
	require.NoError(t, err, "the album artist lookup must see the row stored under the track credit")
	require.Equal(t, row.ID, found.ID)

	// And the same row is found when the row holds the album artist instead.
	h2, db2, root2 := newCanonicalIdentityHandler(t)
	row2 := seedAcquisition(t, db2, "Every Time I Die", "Gutter Phenomenon",
		filepath.Join(root2, "Every Time I Die", "Gutter Phenomenon", "02 - Choke-Son.mp3"))

	found, err = h2.findExistingAlbumAcquisition("Every Time I Die", credit, "Gutter Phenomenon", "other-hash")
	require.NoError(t, err)
	require.Equal(t, row2.ID, found.ID)
}

// Folder names are sanitised, so an artist tagged "AC/DC" lives in "AC-DC" and
// the filesystem lookup has to compare the sanitised form.
func TestResolveCanonicalArtist_MatchesSanitisedFolderName(t *testing.T) {
	h, _, root := newCanonicalIdentityHandler(t)
	mkLibraryDirs(t, root, "AC-DC", "Back In Black")

	gotArtist, err := h.resolveCanonicalArtist("AC/DC")
	require.NoError(t, err)
	require.Equal(t, "AC-DC", gotArtist,
		"the existing sanitised folder casing must win over the raw tag")

	gotAlbum, err := h.resolveCanonicalAlbum("AC-DC", "Back In Black")
	require.NoError(t, err)
	require.Equal(t, "Back In Black", gotAlbum)
}

// Resolution and path building must compose into the canonical folder — the
// unit-level counterpart of the live re-acquire proof.
func TestResolveCanonicalIdentity_ProducesCanonicalFolder(t *testing.T) {
	h, db, root := newCanonicalIdentityHandler(t)
	seedAcquisition(t, db, "PUP", "The Unraveling of Puptheband",
		filepath.Join(root, "PUP", "The Unraveling of Puptheband", "03 - Robot Writes a Love Song.mp3"))
	seedAcquisition(t, db, "PUP", "The Unraveling Of Puptheband",
		filepath.Join(root, "PUP", "The Unraveling Of Puptheband", "12 - PUPTHEBAND Inc. Is Filing For Bankruptcy.mp3"))

	metadata := &AudioMetadata{
		Artist:      "PUP",
		AlbumArtist: "PUP",
		Album:       "The Unraveling Of Puptheband",
		Title:       "Robot Writes a Love Song",
		TrackNumber: 3,
		Format:      "MP3",
	}
	metadata.AlbumArtist, metadata.Album = resolveIdentity(t, h, metadata.AlbumArtist, metadata.Album)

	want := filepath.Join(root, "PUP", "The Unraveling of Puptheband", "03 - Robot Writes a Love Song.mp3")
	require.Equal(t, want, h.ext.GenerateLibraryPath(metadata, root))
}
