package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// Library repair tooling for albums fragmented across per-credit artist
// folders.
//
// Background: acquisitions processed before the album-fragmentation fix
// (#219) used the per-track tag artist (e.g. "Every Time I Die & Daryl
// Palumbo") for the artist folder, so one album could land in several
// sibling folders — one per track credit. Navidrome indexes by folder, so
// each fragment shows up as a separate album. The import path now stamps a
// canonical album artist and dedups on it; this module repairs the legacy
// damage already on disk.
//
// Safety model (mirrors the runbook in ops/docs/library-dedup-runbook.md):
//   - Detection is a pure read over Track rows; it never touches disk.
//   - Merging is plan-then-apply: every operation is computed up front,
//     dry-run by default, and --apply must be passed explicitly.
//   - Files are never overwritten: name collisions become either a
//     content-duplicate removal (same hash) or a reported conflict
//     (different content, source left in place).
//   - Only emptied source directories are removed, and only inside the
//     library root.

// AlbumFolder is one artist-folder holding tracks of a fragmented album.
type AlbumFolder struct {
	// ArtistFolder is the first path segment under the library root,
	// e.g. "Every Time I Die & Daryl Palumbo".
	ArtistFolder string
	// AlbumFolder is the second path segment, e.g. "Gutter Phenomenon".
	AlbumFolder string
	TrackCount  int
	// IsCanonical marks the suggested merge target: the folder with the
	// most tracks (ties broken lexicographically).
	IsCanonical bool
}

// FragmentedAlbum is one album split across multiple artist folders.
type FragmentedAlbum struct {
	// Album is the shared second path segment (the album folder name).
	Album string
	// TrackCount is the total number of library tracks in all fragments.
	TrackCount int
	// CanonicalFolder is the suggested merge target artist folder.
	CanonicalFolder string
	Folders         []AlbumFolder
}

// DetectFragmentedAlbums groups a library's tracks by album folder name and
// reports every album folder that spans more than one distinct artist folder
// directly under libraryRoot. Tracks outside libraryRoot (or not following
// the artist/album/file layout) are ignored. Detection never mutates
// anything.
func DetectFragmentedAlbums(db *gorm.DB, libraryRoot string) ([]FragmentedAlbum, error) {
	var tracks []database.Track
	if err := db.Select("id", "path").Find(&tracks).Error; err != nil {
		return nil, fmt.Errorf("failed to load tracks: %w", err)
	}

	root, err := filepath.Abs(filepath.Clean(libraryRoot))
	if err != nil {
		return nil, fmt.Errorf("invalid library root: %w", err)
	}

	type fragmentKey struct{ artist, album string }
	counts := map[fragmentKey]int{}
	albumTotals := map[string]int{}

	for _, t := range tracks {
		rel, err := filepath.Rel(root, t.Path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 || parts[2] == "" {
			continue // artist/album/file layout only
		}
		counts[fragmentKey{parts[0], parts[1]}]++
		albumTotals[parts[1]]++
	}

	byAlbum := map[string][]AlbumFolder{}
	for k, n := range counts {
		byAlbum[k.album] = append(byAlbum[k.album], AlbumFolder{
			ArtistFolder: k.artist,
			AlbumFolder:  k.album,
			TrackCount:   n,
		})
	}

	var out []FragmentedAlbum
	for album, folders := range byAlbum {
		if len(folders) < 2 {
			continue
		}
		// An album-folder name shared by unrelated artists is legitimate
		// ("Band A/Greatest Hits" + "Band B/Greatest Hits"). The observed
		// fragmentation pattern is credit variants of one primary artist:
		// "Every Time I Die", "Every Time I Die & Daryl Palumbo", … So flag
		// the group only when every artist folder is the shortest one plus
		// a " & <credit>" suffix (or equals it).
		names := make([]string, 0, len(folders))
		for _, f := range folders {
			names = append(names, f.ArtistFolder)
		}
		if !isCreditVariantSet(names) {
			continue
		}
		sort.Slice(folders, func(i, j int) bool {
			if folders[i].TrackCount != folders[j].TrackCount {
				return folders[i].TrackCount > folders[j].TrackCount
			}
			return folders[i].ArtistFolder < folders[j].ArtistFolder
		})
		folders[0].IsCanonical = true
		out = append(out, FragmentedAlbum{
			Album:           album,
			TrackCount:      albumTotals[album],
			CanonicalFolder: folders[0].ArtistFolder,
			Folders:         folders,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Album < out[j].Album })
	return out, nil
}

// isCreditVariantSet reports whether one of the names is a prefix of every
// other, always followed by " & " (the credit-fragment shape this tooling
// repairs). Guards against false positives like "DJ" / "DJ Shadow" or two
// unrelated bands sharing an album name.
func isCreditVariantSet(names []string) bool {
	for _, base := range names {
		allMatch := true
		for _, other := range names {
			if other == base {
				continue
			}
			if !strings.HasPrefix(other, base+" & ") {
				allMatch = false
				break
			}
		}
		if allMatch {
			return true
		}
	}
	return false
}

// MergeMove is one planned file relocation.
type MergeMove struct {
	From string
	To   string
}

// rowMove pairs a Track row with its planned file operation (move or
// content-duplicate removal, marked by To == "DELETE").
type rowMove struct {
	track database.Track
	move  MergeMove
}

// MergeReport describes what a merge did (or would do, when DryRun).
type MergeReport struct {
	Album        string
	CanonicalDir string
	DryRun       bool
	Moved        []MergeMove
	// RemovedFiles are duplicate-content files deleted instead of moved.
	RemovedFiles []string
	// RemovedDirs are source directories emptied by the merge.
	RemovedDirs []string
	// Conflicts are files that could not be merged safely: same filename
	// as a different file already in the canonical folder. The source is
	// left untouched and must be resolved manually.
	Conflicts []string
	// Errors are non-fatal failures (e.g. a file vanished mid-merge).
	Errors []string
}

// MergeAlbumFolders moves every library track of `albumFolder` (the second
// path segment) that lives in an artist folder other than
// `canonicalArtist` into canonicalArtist/<albumFolder>/, rewrites the Track
// rows, and removes source directories left empty. When dryRun is true the
// plan is computed and returned but nothing is touched.
//
// The intended caller flow is DetectFragmentedAlbums → (operator picks the
// canonical folder, possibly overriding the suggestion) → MergeAlbumFolders.
// A Navidrome rescan should follow a successful apply so the server re-indexes.
func MergeAlbumFolders(db *gorm.DB, libraryRoot, albumFolder, canonicalArtist string, dryRun bool) (*MergeReport, error) {
	if albumFolder == "" || canonicalArtist == "" {
		return nil, fmt.Errorf("album folder and canonical artist folder are required")
	}
	root, err := filepath.Abs(filepath.Clean(libraryRoot))
	if err != nil {
		return nil, fmt.Errorf("invalid library root: %w", err)
	}
	canonicalDir := filepath.Join(root, canonicalArtist, albumFolder)
	if !strings.HasPrefix(canonicalDir, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("canonical folder escapes the library root")
	}

	var tracks []database.Track
	if err := db.Where("path LIKE ?", root+string(filepath.Separator)+"%").Find(&tracks).Error; err != nil {
		return nil, fmt.Errorf("failed to load tracks: %w", err)
	}

	report := &MergeReport{Album: albumFolder, CanonicalDir: canonicalDir, DryRun: dryRun}
	ext := &MetadataExtractor{}

	var rowMoves []rowMove
	destRows := map[string]bool{} // Track paths that will occupy destinations
	for _, t := range tracks {
		destRows[t.Path] = true
	}

	// ---- Plan everything before touching anything. ----
	for _, t := range tracks {
		rel, err := filepath.Rel(root, t.Path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 || parts[1] != albumFolder || parts[0] == canonicalArtist {
			continue
		}
		dest := filepath.Join(canonicalDir, parts[2])

		if _, err := os.Stat(dest); err == nil {
			// Destination exists: same content → the source is a true
			// duplicate (remove it); different content → conflict.
			same, hashErr := sameFileContent(t.Path, dest, ext)
			if hashErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: hash failed: %v", t.Path, hashErr))
				continue
			}
			if same {
				report.RemovedFiles = append(report.RemovedFiles, t.Path)
				rowMoves = append(rowMoves, rowMove{track: t, move: MergeMove{From: t.Path, To: "DELETE"}})
			} else {
				report.Conflicts = append(report.Conflicts,
					fmt.Sprintf("%s collides with %s (different content); left in place", t.Path, dest))
			}
			continue
		} else if !os.IsNotExist(err) {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: stat failed: %v", dest, err))
			continue
		}
		if destRows[dest] {
			report.Conflicts = append(report.Conflicts,
				fmt.Sprintf("%s: another moved track already targets %s; left in place", t.Path, dest))
			continue
		}
		destRows[dest] = true
		rowMoves = append(rowMoves, rowMove{track: t, move: MergeMove{From: t.Path, To: dest}})
		report.Moved = append(report.Moved, MergeMove{From: t.Path, To: dest})
	}

	if dryRun {
		planRemovedDirs(report, rowMoves, root)
		return report, nil
	}

	// ---- Apply. ----
	for _, rm := range rowMoves {
		if rm.move.To == "DELETE" {
			if err := os.Remove(rm.move.From); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: remove failed: %v", rm.move.From, err))
				continue
			}
			if err := db.Delete(&database.Track{}, "id = ?", rm.track.ID).Error; err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: track row delete failed: %v", rm.move.From, err))
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(rm.move.To), 0o755); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: mkdir failed: %v", rm.move.To, err))
			continue
		}
		if err := os.Rename(rm.move.From, rm.move.To); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s → %s: move failed: %v", rm.move.From, rm.move.To, err))
			continue
		}
		if err := db.Model(&database.Track{}).Where("id = ?", rm.track.ID).Update("path", rm.move.To).Error; err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: track row update failed: %v", rm.move.To, err))
		}
	}

	applyRemoveSourceDirs(report, rowMoves, root)
	return report, nil
}

// sourceDirsOf returns the distinct album directories (artist/album, depth 2
// under root) holding planned moves, plus their artist directories (depth 1).
// Returned deepest-first: album dirs before artist dirs.
func sourceDirsOf(rowMoves []rowMove, root string) (albumDirs, artistDirs []string) {
	albums := map[string]bool{}
	artists := map[string]bool{}
	for _, rm := range rowMoves {
		albumDir := filepath.Dir(rm.move.From)
		rel, err := filepath.Rel(root, albumDir)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		if len(strings.Split(rel, string(filepath.Separator))) != 2 {
			continue // not an artist/album dir under root
		}
		albums[albumDir] = true
		artists[filepath.Dir(albumDir)] = true
	}
	for d := range albums {
		albumDirs = append(albumDirs, d)
	}
	for d := range artists {
		artistDirs = append(artistDirs, d)
	}
	sort.Strings(albumDirs)
	sort.Strings(artistDirs)
	return albumDirs, artistDirs
}

// planRemovedDirs predicts which source directories the planned moves will
// empty out, for dry-run reporting. A directory counts as emptied when every
// entry in it is a file that the plan moves or removes.
func planRemovedDirs(report *MergeReport, rowMoves []rowMove, root string) {
	albumDirs, artistDirs := sourceDirsOf(rowMoves, root)

	opsIn := map[string]int{}
	for _, rm := range rowMoves {
		opsIn[filepath.Dir(rm.move.From)]++
	}

	empiedAlbum := map[string]bool{}
	for _, dir := range albumDirs {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != opsIn[dir] {
			continue // unreadable, or something will remain
		}
		empiedAlbum[dir] = true
		report.RemovedDirs = append(report.RemovedDirs, dir)
	}
	for _, dir := range artistDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		willEmpty := true
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if !empiedAlbum[full] {
					willEmpty = false
					break
				}
			} else if opsIn[dir] == 0 {
				// a stray file directly under the artist dir never moves
				willEmpty = false
				break
			}
		}
		if willEmpty {
			report.RemovedDirs = append(report.RemovedDirs, dir)
		}
	}
}

// applyRemoveSourceDirs removes source directories actually emptied by the
// applied moves, deepest-first (album dirs, then artist dirs). Populated
// directories are silently kept — os.Remove would fail on them anyway, and
// an artist folder keeping other albums is the normal case, not an error.
func applyRemoveSourceDirs(report *MergeReport, rowMoves []rowMove, root string) {
	albumDirs, artistDirs := sourceDirsOf(rowMoves, root)
	for _, dir := range append(albumDirs, artistDirs...) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: dir cleanup skipped: %v", dir, err))
			continue
		}
		if len(entries) > 0 {
			continue // still holds files or other albums; expected
		}
		if err := os.Remove(dir); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: remove dir failed: %v", dir, err))
			continue
		}
		report.RemovedDirs = append(report.RemovedDirs, dir)
	}
}

// sameFileContent reports whether two files have identical content, falling
// back to size-then-hash comparison.
func sameFileContent(a, b string, ext *MetadataExtractor) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	if ia.Size() != ib.Size() {
		return false, nil
	}
	ha, err := ext.HashFile(a)
	if err != nil {
		return false, err
	}
	hb, err := ext.HashFile(b)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}
