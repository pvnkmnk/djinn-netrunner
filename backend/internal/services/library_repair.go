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
// There are two fragment classes, and detection must see both:
//
//   - credit variants — one album under "X" and "X & Y". Grouping by exact
//     folder name finds these.
//   - case variants — one album (or one artist) whose folders differ *only* by
//     capitalisation, e.g. "The Unraveling Of Puptheband" beside
//     "The Unraveling of Puptheband". Grouping by exact folder name provably
//     cannot see these: the two names are different map keys, so neither group
//     ever reaches the two-folder threshold and the library is reported clean
//     while every case split stays on disk. Detection therefore groups on the
//     folded key (CanonicalKey) and keeps the exact names for reporting.
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

// FragmentKind names the fragment class detected. It drives which repair
// command the CLI suggests, because the two classes need different operations:
// an album merge relocates one album's files, an artist merge relocates every
// album under the wrong-cased artist folder.
type FragmentKind string

const (
	// FragmentCreditVariants is the original shape: "X" plus "X & Y".
	FragmentCreditVariants FragmentKind = "credit_variants"
	// FragmentCaseAlbum is one album whose folder differs only by case.
	FragmentCaseAlbum FragmentKind = "case_album"
	// FragmentCaseArtist is one artist whose folder differs only by case.
	FragmentCaseArtist FragmentKind = "case_artist"
)

// AlbumFolder is one artist-folder holding tracks of a fragmented album.
type AlbumFolder struct {
	// ArtistFolder is the first path segment under the library root,
	// e.g. "Every Time I Die & Daryl Palumbo".
	ArtistFolder string
	// AlbumFolder is the second path segment, e.g. "Gutter Phenomenon".
	AlbumFolder string
	TrackCount  int
	// IsCanonical marks the suggested merge target: the folder with the
	// most tracks (ties broken lexicographically), or the folder matching
	// the canonical casing when the library already has one.
	IsCanonical bool
}

// FragmentedAlbum is one album split across multiple artist folders.
type FragmentedAlbum struct {
	// Album is the canonical album folder name for the merge target.
	Album string
	// CanonicalAlbum is the album folder spelling the library already
	// committed to — pass this (not the fragment's own spelling) to
	// MergeAlbumFolders, or the merge recreates the case variant.
	CanonicalAlbum string
	// Kind distinguishes credit variants from a case-only album split.
	Kind FragmentKind
	// TrackCount is the total number of library tracks in all fragments.
	TrackCount int
	// CanonicalFolder is the suggested merge target artist folder.
	CanonicalFolder string
	Folders         []AlbumFolder
}

// ArtistFragment is one artist folder holding tracks of an artist that exists
// under more than one case variant.
type ArtistFragment struct {
	Folder     string
	TrackCount int
	// IsCanonical marks the suggested merge target.
	IsCanonical bool
}

// FragmentedArtist is one artist whose folder name exists in more than one
// casing. Merging it relocates every album under the losing folders, so it is
// the broader repair: run it first, then re-detect to catch any album-level
// case variants it did not subsume.
type FragmentedArtist struct {
	// CanonicalFolder is the casing the library already uses (earliest
	// acquisition record, else an existing folder, else the first name).
	CanonicalFolder string
	TrackCount      int
	Fragments       []ArtistFragment
}

// LibraryFragments bundles both fragment classes.
type LibraryFragments struct {
	Albums  []FragmentedAlbum
	Artists []FragmentedArtist
}

// layout is one distinct artist/album folder pair found under the library root,
// with the number of tracks it holds.
type layout struct {
	artist string
	album  string
	count  int
}

// libraryLayout returns every artist/album folder pair (depth 2 under root)
// holding at least one Track row, in deterministic order. Tracks outside the
// root, or not following the artist/album/file layout, are ignored.
func libraryLayout(db *gorm.DB, root string) ([]layout, error) {
	var tracks []database.Track
	if err := db.Select("id", "path").Find(&tracks).Error; err != nil {
		return nil, fmt.Errorf("failed to load tracks: %w", err)
	}

	counts := map[string]int{} // "artist\x00album" -> count
	for _, t := range tracks {
		rel, err := filepath.Rel(root, t.Path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 || parts[2] == "" {
			continue // artist/album/file layout only
		}
		counts[parts[0]+"\x00"+parts[1]]++
	}

	out := make([]layout, 0, len(counts))
	for k, n := range counts {
		artist, album, _ := strings.Cut(k, "\x00")
		out = append(out, layout{artist: artist, album: album, count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].artist != out[j].artist {
			return out[i].artist < out[j].artist
		}
		return out[i].album < out[j].album
	})
	return out, nil
}

// DetectFragmentedAlbums groups a library's tracks by *folded* album name and
// reports every album that spans more than one distinct artist/album folder.
// Detection never mutates anything.
func DetectFragmentedAlbums(db *gorm.DB, libraryRoot string) ([]FragmentedAlbum, error) {
	frags, err := DetectLibraryFragments(db, libraryRoot)
	if err != nil {
		return nil, err
	}
	return frags.Albums, nil
}

// DetectLibraryFragments reports both fragment classes in one read.
func DetectLibraryFragments(db *gorm.DB, libraryRoot string) (*LibraryFragments, error) {
	root, err := filepath.Abs(filepath.Clean(libraryRoot))
	if err != nil {
		return nil, fmt.Errorf("invalid library root: %w", err)
	}
	pairs, err := libraryLayout(db, root)
	if err != nil {
		return nil, err
	}
	ext := &MetadataExtractor{}

	return &LibraryFragments{
		Albums:  detectAlbumFragments(db, ext, root, pairs),
		Artists: detectArtistFragments(db, ext, root, pairs),
	}, nil
}

// detectAlbumFragments is the album axis: group by folded album name, then
// classify the group. The folded key is what makes case variants visible.
func detectAlbumFragments(db *gorm.DB, ext *MetadataExtractor, root string, pairs []layout) []FragmentedAlbum {
	byFoldedAlbum := map[string][]layout{}
	for _, p := range pairs {
		key := CanonicalKey(p.album)
		byFoldedAlbum[key] = append(byFoldedAlbum[key], p)
	}

	var out []FragmentedAlbum
	for _, key := range sortedKeys(byFoldedAlbum) {
		group := byFoldedAlbum[key]
		if len(group) < 2 {
			continue
		}

		// Deterministic order: most tracks first (that is the whole existing
		// suggestion rule), ties broken lexicographically by folder path.
		sort.Slice(group, func(i, j int) bool {
			if group[i].count != group[j].count {
				return group[i].count > group[j].count
			}
			if group[i].artist != group[j].artist {
				return group[i].artist < group[j].artist
			}
			return group[i].album < group[j].album
		})

		artists := distinctStrings(group, func(l layout) string { return l.artist })
		albums := distinctStrings(group, func(l layout) string { return l.album })

		// An album-folder name shared by unrelated artists is legitimate
		// ("Band A/Greatest Hits" + "Band B/Greatest Hits"). The two observed
		// fragmentation shapes are credit variants of one primary artist
		// ("Every Time I Die", "Every Time I Die & Daryl Palumbo", …) and
		// case-only drift. Anything else is left alone.
		credit := isCreditVariantSet(artists)
		caseAlbum := allCaseEqual(albums)
		if !credit && !caseAlbum {
			continue
		}
		kind := FragmentCaseAlbum
		if credit {
			kind = FragmentCreditVariants
		}

		// Resolve the casing the library already committed to. Without this a
		// case repair would pick whichever spelling happened to sort first and
		// could fight the next import.
		canonicalArtist := group[0].artist
		canonicalAlbum := group[0].album
		if name, ok := resolveCanonicalArtistCasing(db, ext, root, group[0].artist); ok {
			canonicalArtist = name
		}
		if name, ok := resolveCanonicalAlbumCasing(db, ext, root, canonicalArtist, group[0].album); ok {
			canonicalAlbum = name
		}

		folders := make([]AlbumFolder, 0, len(group))
		total := 0
		for _, l := range group {
			total += l.count
			folders = append(folders, AlbumFolder{
				ArtistFolder: l.artist,
				AlbumFolder:  l.album,
				TrackCount:   l.count,
			})
		}
		markCanonicalFolder(folders, canonicalArtist, canonicalAlbum)

		out = append(out, FragmentedAlbum{
			Album:           canonicalAlbum,
			CanonicalAlbum:  canonicalAlbum,
			Kind:            kind,
			TrackCount:      total,
			CanonicalFolder: canonicalArtist,
			Folders:         folders,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Album < out[j].Album })
	return out
}

// detectArtistFragments is the artist axis: group by folded artist name, and
// report any artist whose folder exists under more than one casing. This is the
// only way to see an artist split whose albums do not coincide — the album axis
// groups by album, so "PUP/Morbid Stuff" beside "Pup/Who Will Look After The
// Dogs" appears in two different groups and each looks whole.
//
// Only case-only sets qualify. Credit variants ("X" beside "X & Y") are the
// album axis's business, and flagging them here would double-report.
func detectArtistFragments(db *gorm.DB, ext *MetadataExtractor, root string, pairs []layout) []FragmentedArtist {
	byFoldedArtist := map[string][]layout{}
	for _, p := range pairs {
		key := CanonicalKey(p.artist)
		byFoldedArtist[key] = append(byFoldedArtist[key], p)
	}

	var out []FragmentedArtist
	for _, key := range sortedKeys(byFoldedArtist) {
		group := byFoldedArtist[key]
		names := distinctStrings(group, func(l layout) string { return l.artist })
		if len(names) < 2 || !allCaseEqual(names) {
			continue
		}

		sort.Strings(names)
		canonical := names[0]
		if resolved, ok := resolveCanonicalArtistCasing(db, ext, root, names[0]); ok {
			canonical = resolved
		}

		totals := map[string]int{}
		for _, l := range group {
			totals[l.artist] += l.count
		}
		fragments := make([]ArtistFragment, 0, len(names))
		total := 0
		for _, name := range names {
			total += totals[name]
			fragments = append(fragments, ArtistFragment{
				Folder:     name,
				TrackCount: totals[name],
			})
		}
		markCanonicalArtistFragment(fragments, canonical)
		out = append(out, FragmentedArtist{
			CanonicalFolder: canonical,
			TrackCount:      total,
			Fragments:       fragments,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CanonicalFolder < out[j].CanonicalFolder })
	return out
}

// markCanonicalArtistFragment flags the artist folder matching the canonical
// casing, falling back to the first (lexicographically smallest) fragment when
// the canonical name is not among them.
func markCanonicalArtistFragment(fragments []ArtistFragment, canonical string) {
	if len(fragments) == 0 {
		return
	}
	for i := range fragments {
		if fragments[i].Folder == canonical {
			fragments[i].IsCanonical = true
			return
		}
	}
	fragments[0].IsCanonical = true
}

// markCanonicalFolder flags the folder that matches the canonical casing, so the
// reported target and the actual merge destination agree. When nothing matches
// (the canonical casing came from a folder or record that no longer exists), the
// first folder — the most-tracks suggestion — is flagged instead.
func markCanonicalFolder(folders []AlbumFolder, artist, album string) {
	if len(folders) == 0 {
		return
	}
	for i := range folders {
		if folders[i].ArtistFolder == artist && folders[i].AlbumFolder == album {
			folders[i].IsCanonical = true
			return
		}
	}
	folders[0].IsCanonical = true
}

// isCreditVariantSet reports whether one of the names is a prefix of every
// other, always followed by " & " (the credit-fragment shape this tooling
// repairs). Comparison is case-folded, so "PUP" beside "pup & Guest" is still
// recognised. Guards against false positives like "DJ" / "DJ Shadow" or two
// unrelated bands sharing an album name.
func isCreditVariantSet(names []string) bool {
	if len(names) < 2 {
		return false
	}
	for _, base := range names {
		allMatch := true
		for _, other := range names {
			if strings.EqualFold(other, base) {
				continue
			}
			if !strings.HasPrefix(CanonicalKey(other), CanonicalKey(base)+" & ") {
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

// allCaseEqual reports whether every name in the list is the same string once
// case-folded, and there is more than one distinct spelling. One spelling means
// nothing to fold; two spellings that differ by more than case means this is not
// a case drift (e.g. "DJ" beside "DJ Shadow").
func allCaseEqual(names []string) bool {
	if len(names) < 2 {
		return false
	}
	for _, n := range names[1:] {
		if CanonicalKey(n) != CanonicalKey(names[0]) {
			return false
		}
	}
	return true
}

// distinctStrings collects the distinct values of a projection, in the order
// the (already sorted) layouts supply them.
func distinctStrings(pairs []layout, project func(layout) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range pairs {
		v := project(p)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// sortedKeys returns a map's keys in deterministic order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MergeMove is one planned file relocation.
type MergeMove struct {
	From string
	To   string
}

// rowMove pairs a Track row with its planned file operation (move or
// content-duplicate removal, marked by To == "DELETE").
//
// A sidecar file (lyrics, artwork) has no Track row of its own, so sidecar
// moves carry a zero Track and are applied without touching the database.
type rowMove struct {
	track   database.Track
	move    MergeMove
	sidecar bool
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
// `albumFolder` and `canonicalArtist` must be the *canonical* spellings — pass
// FragmentedAlbum.CanonicalAlbum / .CanonicalFolder. The source match is
// case-folded, so a variant folder is reachable, and the destination is built
// from these arguments, so the merge converges the casing rather than
// recreating the variant.
//
// The intended caller flow is DetectFragmentedAlbums → (operator picks the
// canonical folder, possibly overriding the suggestion) → MergeAlbumFolders.
// A media-server rescan should follow a successful apply so the server
// re-indexes.
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

	report := &MergeReport{Album: albumFolder, CanonicalDir: canonicalDir, DryRun: dryRun}
	return mergeFolders(db, root, report, dryRun, albumDestSelector(canonicalDir, albumFolder, canonicalArtist))
}

// albumDestSelector is the pure matching rule for an album merge: which tracks
// belong to this album, and where each goes.
//
// It is extracted rather than inlined because the rule it encodes — the source
// match is case-folded, while "already at the destination" is exact — is exactly
// the part that used to be broken, and on Windows/macOS the two spellings are
// one directory, so no filesystem test there can observe the difference.
func albumDestSelector(canonicalDir, albumFolder, canonicalArtist string) func(parts []string) (string, bool) {
	albumKey := CanonicalKey(albumFolder)
	return func(parts []string) (string, bool) {
		if CanonicalKey(parts[1]) != albumKey {
			return "", false // a different album
		}
		if parts[0] == canonicalArtist && parts[1] == albumFolder {
			return "", false // already exactly at the destination
		}
		return filepath.Join(canonicalDir, parts[2]), true
	}
}

// MergeArtistFolders moves every library track held under an artist folder whose
// name differs only by case from `canonicalArtist` into canonicalArtist/,
// preserving each album's folder name, and rewrites the Track rows. Dry-run by
// default, like MergeAlbumFolders.
//
// This is the broader repair for a case-only artist split, and the only one that
// can reach albums that exist under a single casing. Run it first, then
// re-detect: any album-level case variants it did not subsume (an album present
// under both the winning and losing artist folders with different album casing)
// still show up on the album axis.
func MergeArtistFolders(db *gorm.DB, libraryRoot, canonicalArtist string, dryRun bool) (*MergeReport, error) {
	if canonicalArtist == "" {
		return nil, fmt.Errorf("canonical artist folder is required")
	}
	root, err := filepath.Abs(filepath.Clean(libraryRoot))
	if err != nil {
		return nil, fmt.Errorf("invalid library root: %w", err)
	}
	if !strings.HasPrefix(filepath.Join(root, canonicalArtist), root+string(filepath.Separator)) {
		return nil, fmt.Errorf("canonical folder escapes the library root")
	}

	report := &MergeReport{CanonicalDir: filepath.Join(root, canonicalArtist), DryRun: dryRun}
	return mergeFolders(db, root, report, dryRun, artistDestSelector(root, canonicalArtist))
}

// artistDestSelector is the pure matching rule for an artist merge: every track
// under a folder that differs from the canonical artist only by case moves into
// the canonical folder, keeping its album folder name. Extracted for the same
// reason as albumDestSelector.
func artistDestSelector(root, canonicalArtist string) func(parts []string) (string, bool) {
	artistKey := CanonicalKey(canonicalArtist)
	return func(parts []string) (string, bool) {
		if CanonicalKey(parts[0]) != artistKey {
			return "", false // a different artist
		}
		if parts[0] == canonicalArtist {
			return "", false // already exactly at the destination
		}
		return filepath.Join(root, canonicalArtist, parts[1], parts[2]), true
	}
}

// mergeFolders is the shared plan-then-apply engine. selectDest receives the
// artist/album/file path segments of a candidate track relative to root and
// returns its destination, or ok=false to leave it alone. Both merge entry
// points differ only in that predicate.
func mergeFolders(db *gorm.DB, root string, report *MergeReport, dryRun bool, selectDest func(parts []string) (string, bool)) (*MergeReport, error) {
	var tracks []database.Track
	if err := db.Where("path LIKE ?", root+string(filepath.Separator)+"%").Find(&tracks).Error; err != nil {
		return nil, fmt.Errorf("failed to load tracks: %w", err)
	}

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
		if len(parts) != 3 {
			continue
		}
		dest, ok := selectDest(parts)
		if !ok {
			continue
		}

		if destInfo, statErr := os.Stat(dest); statErr == nil {
			// On a case-insensitive filesystem (default Windows, macOS) a
			// case-variant folder IS the canonical folder, so source and
			// destination resolve to the very same file. Comparing it to
			// itself would report "same content" and delete a track that was
			// never a duplicate. Nothing to do — it is already in place.
			if srcInfo, err := os.Stat(t.Path); err == nil && os.SameFile(srcInfo, destInfo) {
				continue
			}
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
				rowMoves = planSidecarMoves(t.Path, filepath.Dir(dest), report, rowMoves)
			} else {
				report.Conflicts = append(report.Conflicts,
					fmt.Sprintf("%s collides with %s (different content); left in place", t.Path, dest))
			}
			continue
		} else if !os.IsNotExist(statErr) {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: stat failed: %v", dest, statErr))
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
		rowMoves = planSidecarMoves(t.Path, filepath.Dir(dest), report, rowMoves)
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
			if rm.sidecar {
				continue // a sidecar has no Track row to delete
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
		if rm.sidecar {
			continue // a sidecar has no Track row to rewrite
		}
		if err := db.Model(&database.Track{}).Where("id = ?", rm.track.ID).Update("path", rm.move.To).Error; err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: track row update failed: %v", rm.move.To, err))
		}
	}

	applyRemoveSourceDirs(report, rowMoves, root)
	return report, nil
}

// siblingSidecars returns the non-audio files sitting next to a track that
// belong to it. Lyrics and artwork sidecars share the track's base name and
// differ only by extension ("12 - Title.mp3" beside "12 - Title.lrc").
//
// Requiring a dot immediately after the stem deliberately excludes other tracks
// that merely start with the same characters ("12 - Title (live).mp3"), which
// are separate items with their own rows.
func siblingSidecars(trackPath string) []string {
	dir := filepath.Dir(trackPath)
	base := filepath.Base(trackPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		candidate := filepath.Join(dir, e.Name())
		// The track's own extension also starts with a dot, so without this the
		// track matches itself and the merge plans to move it onto itself.
		if candidate == trackPath {
			continue
		}
		if !strings.HasPrefix(e.Name(), stem) {
			continue
		}
		if rest := strings.TrimPrefix(e.Name(), stem); !strings.HasPrefix(rest, ".") || rest == "." {
			continue
		}
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

// planSidecarMoves appends the moves for a track's sidecar files so they follow
// it into the canonical folder.
//
// Without this the audio is relocated but its sidecar is not, so the source
// directory is never emptied and the repair leaves a skeleton folder behind —
// observed live: PUP/The Unraveling Of Puptheband kept an orphaned .lrc after
// its only track moved, and the merge reported "dirs removed: 0" while the
// split it had just repaired was still half present on disk.
//
// A sidecar whose destination already exists is a duplicate (the canonical
// folder was imported with the same lyrics), so the source copy is removed
// instead of moved.
func planSidecarMoves(srcPath, destDir string, report *MergeReport, rowMoves []rowMove) []rowMove {
	for _, src := range siblingSidecars(srcPath) {
		dest := filepath.Join(destDir, filepath.Base(src))
		if _, err := os.Stat(dest); err == nil {
			report.RemovedFiles = append(report.RemovedFiles, src)
			rowMoves = append(rowMoves, rowMove{move: MergeMove{From: src, To: "DELETE"}, sidecar: true})
			continue
		}
		rowMoves = append(rowMoves, rowMove{move: MergeMove{From: src, To: dest}, sidecar: true})
		report.Moved = append(report.Moved, MergeMove{From: src, To: dest})
	}
	return rowMoves
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
