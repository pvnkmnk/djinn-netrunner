package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// This file owns how an artist+album pair is *identified* in the library.
// Library folders are derived from tag text, and tag text for the same album
// disagrees on case between releases ("The Unraveling Of Puptheband" vs
// "The Unraveling of Puptheband"), which used to split one album across
// sibling folders and defeat the album dedup (DJI-489). Anything that decides
// "is this the same artist/album?" goes through here.
//
// The rule is: fold case for *comparison*, and reuse the casing the library
// already committed to for *display and paths*. Nothing here renames an
// existing folder — the earliest record wins, so a folder already on disk keeps
// its name and becomes the one all later imports converge on.
//
// The resolution functions are package-level (not handler methods) because the
// CLI repair tooling in library_repair.go needs the same answer: the canonical
// folder `library detect-fragments` suggests must be exactly the casing the
// importer would choose, or a repair fights the next import.

// CanonicalKey folds an artist or album name to the form used for identity
// comparisons. It mirrors the SQL fold `LOWER(TRIM(col))` used by the album
// dedup query — keep the two in step if either changes.
func CanonicalKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// libraryRoot is the single owner of the library-root fallback, so the import
// path and identity resolution can never disagree about where the library is.
func (h *AcquisitionHandler) libraryRoot() string {
	if h.cfg == nil || h.cfg.MusicLibraryPath == "" {
		return "./music_library"
	}
	return h.cfg.MusicLibraryPath
}

// ErrIdentityLookup reports that the library's committed casing could not be
// read. It exists so callers cannot confuse "the query failed" with "the
// artist/album is new": on a failed query the fallback below would pick
// filesystem or tag casing, and a repair acting on that answer could merge into
// the wrong destination folder. A failed lookup must abort the operation, not
// be silently read as a fresh identity.
var ErrIdentityLookup = errors.New("canonical identity lookup failed")

// ResolveCanonicalArtist returns the casing the library already uses for this
// artist, falling back to the input when the artist is genuinely new.
func ResolveCanonicalArtist(db *gorm.DB, ext *MetadataExtractor, libraryRoot, artist string) (string, error) {
	name, _, err := resolveCanonicalArtistCasing(db, ext, libraryRoot, artist)
	return name, err
}

// ResolveCanonicalAlbum returns the casing the library already uses for this
// album under the (already canonical) artist, falling back to the input.
func ResolveCanonicalAlbum(db *gorm.DB, ext *MetadataExtractor, libraryRoot, artist, album string) (string, error) {
	name, _, err := resolveCanonicalAlbumCasing(db, ext, libraryRoot, artist, album)
	return name, err
}

// ResolveCanonicalIdentity returns both casings. Artist is resolved before
// album so a new album lands in the artist's existing folder.
func ResolveCanonicalIdentity(db *gorm.DB, ext *MetadataExtractor, libraryRoot, artist, album string) (string, string, error) {
	if artist == "" || album == "" {
		return artist, album, nil
	}
	canonicalArtist, err := ResolveCanonicalArtist(db, ext, libraryRoot, artist)
	if err != nil {
		return artist, album, err
	}
	canonicalAlbum, err := ResolveCanonicalAlbum(db, ext, libraryRoot, canonicalArtist, album)
	if err != nil {
		return canonicalArtist, album, err
	}
	return canonicalArtist, canonicalAlbum, nil
}

// resolveCanonicalIdentity returns the artist and album casing the library
// already uses for this pair, so a re-acquire whose tags differ only by case
// lands in the existing folder instead of creating a case-variant sibling.
// Both fall back to the input casing when the pair is genuinely new.
func (h *AcquisitionHandler) resolveCanonicalIdentity(artist, album string) (string, string, error) {
	return ResolveCanonicalIdentity(h.db, h.ext, h.libraryRoot(), artist, album)
}

// resolveCanonicalArtist returns the casing already used for the artist: a new
// album by a known artist must not create a second, case-variant artist folder.
func (h *AcquisitionHandler) resolveCanonicalArtist(artist string) (string, error) {
	return ResolveCanonicalArtist(h.db, h.ext, h.libraryRoot(), artist)
}

// resolveCanonicalAlbum returns the casing already used for the album under
// the (already canonical) artist.
func (h *AcquisitionHandler) resolveCanonicalAlbum(artist, album string) (string, error) {
	return ResolveCanonicalAlbum(h.db, h.ext, h.libraryRoot(), artist, album)
}

// resolveCanonicalArtistCasing is the shared implementation behind
// ResolveCanonicalArtist. It is resolved independently of the album: a new
// album by a known artist must not create a second, case-variant artist folder.
// Acquisition history wins over the filesystem (earliest record first), and the
// input is the last resort. The bool reports whether the library had an answer:
// false means the returned name is the input, unchanged.
//
// Trade-off, deliberate: two genuinely distinct artists whose names differ only
// by case are treated as one. The alternative is the fragmentation this fixes,
// and a case-only artist collision is far rarer than case-only tag drift.
func resolveCanonicalArtistCasing(db *gorm.DB, ext *MetadataExtractor, libraryRoot, artist string) (string, bool, error) {
	key := CanonicalKey(artist)
	if key == "" {
		return artist, false, nil
	}

	if db != nil {
		var existing database.Acquisition
		err := db.Select("artist").
			Where("LOWER(TRIM(artist)) = ?", key).
			Order("id ASC").First(&existing).Error
		switch {
		case err == nil:
			if existing.Artist != "" {
				return existing.Artist, true, nil
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			// Genuinely new artist; the filesystem is the next source of truth.
		default:
			return artist, false, fmt.Errorf("%w: artist %q: %v", ErrIdentityLookup, artist, err)
		}
	}

	// Library folder names are sanitised, so "AC/DC" is stored as "AC-DC" and the
	// filesystem comparison has to use the sanitised form. The database keeps the
	// raw artist name, so the query above must NOT use this key.
	folderKey := key
	if ext != nil {
		folderKey = CanonicalKey(ext.SanitizeFilename(artist))
	}

	for _, name := range sortedSubdirs(libraryRoot) {
		if CanonicalKey(name) == folderKey {
			return name, true, nil
		}
	}

	return artist, false, nil
}

// resolveCanonicalAlbumCasing is the shared implementation behind
// ResolveCanonicalAlbum. The bool reports whether the library had an answer.
//
// Folder names on disk are sanitised, so the filesystem comparison uses the
// sanitised album — an album tagged "Who Will Look After the Dogs?" lives in a
// folder without the "?" and must still be recognised as the same album.
func resolveCanonicalAlbumCasing(db *gorm.DB, ext *MetadataExtractor, libraryRoot, artist, album string) (string, bool, error) {
	albumKey := CanonicalKey(album)
	if albumKey == "" {
		return album, false, nil
	}

	if db != nil {
		var existing database.Acquisition
		err := db.Select("album").
			Where("LOWER(TRIM(artist)) = ? AND LOWER(TRIM(album)) = ?", CanonicalKey(artist), albumKey).
			Order("id ASC").First(&existing).Error
		switch {
		case err == nil:
			if existing.Album != "" {
				return existing.Album, true, nil
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			// Genuinely new album; the filesystem is the next source of truth.
		default:
			return album, false, fmt.Errorf("%w: album %q by %q: %v", ErrIdentityLookup, album, artist, err)
		}
	}

	if ext == nil {
		return album, false, nil
	}

	artistKey := CanonicalKey(ext.SanitizeFilename(artist))
	folderKey := CanonicalKey(ext.SanitizeFilename(album))
	for _, artistDir := range sortedSubdirs(libraryRoot) {
		if CanonicalKey(artistDir) != artistKey {
			continue
		}
		if name, ok := matchSubdir(filepath.Join(libraryRoot, artistDir), folderKey); ok {
			return name, true, nil
		}
	}

	return album, false, nil
}

// findExistingAlbumAcquisition returns the earliest acquisition that already
// holds this album, ignoring the exact file we just hashed. It is the single
// owner of the album (release-group) dedup lookup: case is folded on both
// sides, so a re-acquire whose tags differ only by case is recognised as the
// album the library already holds rather than importing a second copy.
//
// Both the canonical album artist and the per-track artist are matched. The
// import resolves the album artist to key the lookup, but a row records
// metadata.Artist - the track artist - so a multi-credit track ("X & Y" tagged
// under album artist "X") would never match its own album otherwise.
func (h *AcquisitionHandler) findExistingAlbumAcquisition(albumArtist, trackArtist, album, fileHash string) (*database.Acquisition, error) {
	if h.db == nil {
		return nil, errors.New("acquisition handler has no database")
	}

	primaryArtist := CanonicalKey(albumArtist)
	secondaryArtist := CanonicalKey(trackArtist)
	if secondaryArtist == "" {
		secondaryArtist = primaryArtist
	}

	var existing database.Acquisition
	err := h.db.
		Where("(LOWER(TRIM(artist)) = ? OR LOWER(TRIM(artist)) = ?) AND LOWER(TRIM(album)) = ? AND (file_hash = '' OR file_hash IS NULL OR file_hash != ?)",
			primaryArtist, secondaryArtist, CanonicalKey(album), fileHash).
		Order("id ASC").
		First(&existing).Error
	// A missing row is not a failure — it means the album is new. Everything else
	// must reach the caller: reading a failed query as "not a duplicate" would
	// import a second copy of an album the library already holds.
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &existing, nil
}

// matchSubdir returns the first (sorted) directory under parent whose folded
// name equals key.
func matchSubdir(parent, key string) (string, bool) {
	for _, name := range sortedSubdirs(parent) {
		if CanonicalKey(name) == key {
			return name, true
		}
	}
	return "", false
}

// sortedSubdirs lists a directory's subdirectories in sorted order, so the
// canonical choice is stable regardless of the filesystem's readdir order.
// A missing or unreadable directory yields no names.
func sortedSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	return names
}
