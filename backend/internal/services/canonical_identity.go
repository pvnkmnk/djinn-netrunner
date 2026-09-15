package services

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
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

// resolveCanonicalIdentity returns the artist and album casing the library
// already uses for this pair, so a re-acquire whose tags differ only by case
// lands in the existing folder instead of creating a case-variant sibling.
// Both fall back to the input casing when the pair is genuinely new.
func (h *AcquisitionHandler) resolveCanonicalIdentity(artist, album string) (string, string) {
	canonicalArtist := h.resolveCanonicalArtist(artist)
	return canonicalArtist, h.resolveCanonicalAlbum(canonicalArtist, album)
}

// resolveCanonicalArtist returns the casing already used for the artist. It is
// resolved independently of the album: a new album by a known artist must not
// create a second, case-variant artist folder. Acquisition history wins over
// the filesystem (earliest record first), and the input is the last resort.
//
// Trade-off, deliberate: two genuinely distinct artists whose names differ only
// by case are treated as one. The alternative is the fragmentation this fixes,
// and a case-only artist collision is far rarer than case-only tag drift.
func (h *AcquisitionHandler) resolveCanonicalArtist(artist string) string {
	key := CanonicalKey(artist)
	if key == "" {
		return artist
	}

	if h.db != nil {
		var existing database.Acquisition
		err := h.db.Select("artist").
			Where("LOWER(TRIM(artist)) = ?", key).
			Order("id ASC").First(&existing).Error
		if err == nil && existing.Artist != "" {
			return existing.Artist
		}
	}

	// Library folder names are sanitised, so "AC/DC" is stored as "AC-DC" and the
	// filesystem comparison has to use the sanitised form. The database keeps the
	// raw artist name, so the query above must NOT use this key.
	folderKey := key
	if h.ext != nil {
		folderKey = CanonicalKey(h.ext.SanitizeFilename(artist))
	}

	for _, name := range sortedSubdirs(h.libraryRoot()) {
		if CanonicalKey(name) == folderKey {
			return name
		}
	}

	return artist
}

// resolveCanonicalAlbum returns the casing already used for the album under the
// (already canonical) artist. Folder names on disk are sanitised, so the
// filesystem comparison uses the sanitised album — an album tagged
// "Who Will Look After the Dogs?" lives in a folder without the "?" and must
// still be recognised as the same album.
func (h *AcquisitionHandler) resolveCanonicalAlbum(artist, album string) string {
	albumKey := CanonicalKey(album)
	if albumKey == "" {
		return album
	}

	if h.db != nil {
		var existing database.Acquisition
		err := h.db.Select("album").
			Where("LOWER(TRIM(artist)) = ? AND LOWER(TRIM(album)) = ?", CanonicalKey(artist), albumKey).
			Order("id ASC").First(&existing).Error
		if err == nil && existing.Album != "" {
			return existing.Album
		}
	}

	if h.ext == nil {
		return album
	}

	root := h.libraryRoot()
	artistKey := CanonicalKey(h.ext.SanitizeFilename(artist))
	folderKey := CanonicalKey(h.ext.SanitizeFilename(album))
	for _, artistDir := range sortedSubdirs(root) {
		if CanonicalKey(artistDir) != artistKey {
			continue
		}
		if name, ok := matchSubdir(filepath.Join(root, artistDir), folderKey); ok {
			return name
		}
	}

	return album
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
