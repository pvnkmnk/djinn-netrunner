package services

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Library identity-tag repair for files imported before the tag-write fix
// (DJI-494). The folder repair in library_repair.go relocates files; this
// rewrites the tags of files already in place, because a client groups by tag
// and shows the same artist twice when one file says "Pup" and another "PUP".
//
// Scope: only a disagreement the canonical identity can actually settle. The
// canonical values come from ResolveCanonicalIdentity — the same owner the
// import path uses — so a repair can never invent a name the library does not
// already use; a file whose tags the library has no opinion about is left
// alone. Detection reads tags and never writes; applying copies every file it
// is about to rewrite into a backup directory outside the library first.
//
// A volume-level snapshot (see ops/docs/library-dedup-runbook.md) is still the
// pre-flight for a whole-library operation: the backup here covers the files
// this run rewrites, which is the risk it introduces.

// TagFix is one file whose identity tags disagree with the canonical identity.
// The From/To pairs are per field so the report shows exactly what changes.
type TagFix struct {
	File            string `json:"file"`
	AlbumArtistFrom string `json:"album_artist_from,omitempty"`
	AlbumArtistTo   string `json:"album_artist_to,omitempty"`
	AlbumFrom       string `json:"album_from,omitempty"`
	AlbumTo         string `json:"album_to,omitempty"`
	TrackArtistFrom string `json:"track_artist_from,omitempty"`
	TrackArtistTo   string `json:"track_artist_to,omitempty"`

	// Source* is the identity the plan was derived from. Detection and apply
	// are separate commands, so the file is revalidated against these before
	// anything is copied or written: a file an import replaced in between must
	// not be restamped with canonical values resolved for the track that used
	// to be at that path.
	SourceAlbumArtist string `json:"source_album_artist,omitempty"`
	SourceAlbum       string `json:"source_album,omitempty"`
	SourceTrackArtist string `json:"source_track_artist,omitempty"`
}

// TagRepairReport is the plan a dry run prints and the record of an apply run.
// DistinctArtists counts the exact artist names a client would list (album
// artists and track artists, case-sensitively) — the number that drops when a
// case variant stops being a separate artist.
type TagRepairReport struct {
	LibraryRoot string   `json:"library_root"`
	Scanned     int      `json:"scanned"`
	Unreadable  int      `json:"unreadable"`
	Fixes       []TagFix `json:"fixes"`
	// LeftAlone lists files whose identity disagrees with the canonical one in
	// a way this repair deliberately does not touch: a substantive difference
	// (a different album name, a sanitised folder name) is a wrong-file or
	// provenance signal, not casing drift.
	LeftAlone             []string `json:"left_alone,omitempty"`
	DistinctArtistsBefore int      `json:"distinct_artists_before"`
	DistinctArtistsAfter  int      `json:"distinct_artists_after"`
	ArtistsRemoved        []string `json:"artists_removed,omitempty"`
	BackupDir             string   `json:"backup_dir,omitempty"`
	BackedUp              int      `json:"backed_up"`
	Rewritten             int      `json:"rewritten"`
	// Stale lists files whose identity changed between the plan and the apply.
	// They are left exactly as they are: this plan's targets were resolved for
	// a different track.
	Stale    []string `json:"stale,omitempty"`
	Failures []string `json:"failures,omitempty"`
}

// PlanIdentityTagRepair reports every library file whose identity tags disagree
// with the canonical identity, without writing anything.
func PlanIdentityTagRepair(db *gorm.DB, ext *MetadataExtractor, libraryRoot string) (*TagRepairReport, error) {
	if ext == nil {
		return nil, fmt.Errorf("identity tag repair: metadata extractor is required to read tags")
	}
	files, err := audioFilesUnder(libraryRoot)
	if err != nil {
		return nil, err
	}

	report := &TagRepairReport{LibraryRoot: libraryRoot}
	var leftAlone []string
	before := map[string]struct{}{}
	after := map[string]struct{}{}

	for _, path := range files {
		report.Scanned++
		rel, relErr := filepath.Rel(libraryRoot, path)
		if relErr != nil {
			rel = path
		}

		meta, err := ext.Extract(path)
		if err != nil {
			report.Unreadable++
			continue
		}
		cur := readTagFile(path)

		// The file's own tags are the lookup input: the resolver folds case and
		// prefers the casing the library already committed (history, then the
		// folders on disk), so it answers with the library's name for this
		// artist rather than with the peer's.
		canonicalArtist, canonicalAlbum := meta.AlbumArtist, meta.Album
		if canonicalArtist != "" && canonicalAlbum != "" {
			resolvedArtist, resolvedAlbum, resolveErr := ResolveCanonicalIdentity(db, ext, libraryRoot, canonicalArtist, canonicalAlbum)
			if resolveErr != nil {
				// A failed lookup must not read as "nothing to do": that would
				// report a clean library while the drift stays on disk.
				return nil, fmt.Errorf("canonical identity lookup for %s: %w", rel, resolveErr)
			}
			canonicalArtist, canonicalAlbum = resolvedArtist, resolvedAlbum
		}

		// Named artists only: an untagged file would otherwise be counted as
		// an artist of its own, and the before/after numbers would disagree
		// with what a client lists.
		if meta.AlbumArtist != "" {
			before[meta.AlbumArtist] = struct{}{}
		}
		if meta.Artist != "" {
			before[meta.Artist] = struct{}{}
		}
		if canonicalArtist != "" {
			after[canonicalArtist] = struct{}{}
		}
		if trackArtist := correctedTrackArtist(meta.Artist, canonicalArtist); trackArtist != "" {
			after[trackArtist] = struct{}{}
		}

		// A substantive album disagreement is reported, not corrected: the
		// canonical value may itself be a sanitised folder name.
		if meta.Album != "" && canonicalAlbum != "" &&
			!strings.EqualFold(meta.Album, canonicalAlbum) {
			leftAlone = append(leftAlone, rel)
		}

		want := AlbumTagIdentity{
			AlbumArtist: canonicalArtist,
			Album:       canonicalAlbum,
			TrackArtist: canonicalArtist,
		}
		// The write decision is the tagger's, not a second copy of the rule: a
		// file with nothing to write is exactly one whose correction arguments
		// are empty.
		if len(tagCorrectionArgs(cur, want, filepath.Ext(path))) == 0 {
			continue
		}
		report.Fixes = append(report.Fixes, TagFix{
			File:              rel,
			SourceAlbumArtist: meta.AlbumArtist,
			SourceAlbum:       meta.Album,
			SourceTrackArtist: meta.Artist,
			AlbumArtistFrom:   differing(meta.AlbumArtist, canonicalArtist),
			AlbumArtistTo:     differing(canonicalArtist, meta.AlbumArtist),
			AlbumFrom:         differing(meta.Album, canonicalAlbum),
			AlbumTo:           differing(canonicalAlbum, meta.Album),
			TrackArtistFrom:   differing(meta.Artist, correctedTrackArtist(meta.Artist, canonicalArtist)),
			TrackArtistTo:     differing(correctedTrackArtist(meta.Artist, canonicalArtist), meta.Artist),
		})
	}

	sort.Strings(leftAlone)
	report.LeftAlone = leftAlone
	report.DistinctArtistsBefore = len(before)
	report.DistinctArtistsAfter = len(after)
	report.ArtistsRemoved = namesRemoved(before, after)
	return report, nil
}

// ApplyIdentityTagRepair rewrites the files in a plan, copying each one into
// backupDir first. Detection and apply are separate commands, so every file is
// revalidated against the identity the plan recorded for it before it is copied
// or written: a file that changed in between is left untouched and reported as
// stale rather than restamped with canonical values resolved for another track.
// The tagger re-reads the file before writing too, which makes a second apply a
// no-op.
func ApplyIdentityTagRepair(ctx context.Context, ext *MetadataExtractor, libraryRoot, backupDir string, report *TagRepairReport) error {
	if ext == nil {
		return fmt.Errorf("identity tag repair: metadata extractor is required to write tags")
	}
	if len(report.Fixes) == 0 {
		return nil
	}
	if strings.TrimSpace(backupDir) == "" {
		return fmt.Errorf("identity tag repair: a backup directory is required before rewriting %d file(s)", len(report.Fixes))
	}
	absBackup, err := filepath.Abs(backupDir)
	if err != nil {
		return fmt.Errorf("identity tag repair: backup directory %q: %w", backupDir, err)
	}
	// Backing up inside the library would hand the media server a second copy
	// of every track to index, which is a worse outcome than no backup.
	if isInsidePath(libraryRoot, absBackup) {
		return fmt.Errorf("identity tag repair: backup directory %s is inside the library root %s; choose a path outside it", absBackup, libraryRoot)
	}
	if err := os.MkdirAll(absBackup, 0o755); err != nil {
		return fmt.Errorf("identity tag repair: create backup directory: %w", err)
	}
	report.BackupDir = absBackup

	for _, fix := range report.Fixes {
		src := filepath.Join(libraryRoot, fix.File)
		if fixSourceChanged(ext, src, fix) {
			report.Stale = append(report.Stale, fix.File)
			continue
		}
		dst := filepath.Join(absBackup, fix.File)
		if err := copyFileForBackup(src, dst); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: backup failed: %v", fix.File, err))
			continue
		}
		report.BackedUp++

		// Revalidate again immediately before the write: the copy above is a
		// second window in which an import can replace the file, and a tag write
		// is not something re-running the plan undoes.
		if fixSourceChanged(ext, src, fix) {
			report.Stale = append(report.Stale, fix.File)
			continue
		}

		if err := ext.NormalizeAlbumTags(ctx, src, AlbumTagIdentity{
			AlbumArtist: fix.AlbumArtistTo,
			Album:       fix.AlbumTo,
			TrackArtist: fix.TrackArtistTo,
		}); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: tag write failed: %v", fix.File, err))
			continue
		}
		report.Rewritten++
	}
	return nil
}

// PersistentTagBackupRoot is the volume a deployment keeps backups on.
const PersistentTagBackupRoot = "/backups"

// DefaultTagBackupDir returns the backup location for a library: a timestamped
// directory on the persistent volume when one is mounted, otherwise a
// timestamped sibling of the library root. Outside the library either way, so
// the media server cannot index a second copy of every track.
//
// The volume is preferred because a sibling of the library root is still
// inside the container: in Compose it disappears when the container is
// recreated, which is the one moment the backup is needed.
func DefaultTagBackupDir(libraryRoot string, now time.Time) string {
	stamp := now.UTC().Format("20060102T150405Z")
	if info, err := os.Stat(PersistentTagBackupRoot); err == nil && info.IsDir() {
		return filepath.Join(PersistentTagBackupRoot, "identity-tags-"+stamp)
	}
	abs, err := filepath.Abs(libraryRoot)
	if err != nil {
		abs = libraryRoot
	}
	return filepath.Join(filepath.Dir(abs), fmt.Sprintf("%s-backup-%s", filepath.Base(abs), stamp))
}

// fixSourceChanged reports whether the file's identity still matches the one the
// plan recorded for it. A file that cannot be read counts as changed: the plan
// cannot be applied to something it no longer recognises.
func fixSourceChanged(ext *MetadataExtractor, path string, fix TagFix) bool {
	meta, err := ext.Extract(path)
	if err != nil {
		return true
	}
	return meta.AlbumArtist != fix.SourceAlbumArtist ||
		meta.Album != fix.SourceAlbum ||
		meta.Artist != fix.SourceTrackArtist
}

// correctedTrackArtist is the artist tag the repair would write: the canonical
// album artist when the file's track artist is that same name with different
// casing, otherwise the file's value unchanged. It mirrors the tagger's rule so
// a genuine credit is reported as untouched.
func correctedTrackArtist(trackArtist, canonicalArtist string) string {
	if canonicalArtist == "" || trackArtist == "" {
		return trackArtist
	}
	if strings.EqualFold(trackArtist, canonicalArtist) {
		return canonicalArtist
	}
	return trackArtist
}

// differing returns a when it differs from b, else "" — so a fix only carries
// the fields that actually change.
func differing(a, b string) string {
	if a == b {
		return ""
	}
	return a
}

func namesRemoved(before, after map[string]struct{}) []string {
	var removed []string
	for name := range before {
		if name == "" {
			continue
		}
		if _, still := after[name]; !still {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	return removed
}

// audioFilesUnder lists the library's audio files in deterministic order.
func audioFilesUnder(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := mapMuxer(filepath.Ext(path)); ok {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("identity tag repair: walking %s: %w", root, err)
	}
	sort.Strings(files)
	return files, nil
}

// copyFileForBackup copies one file, creating parent directories and keeping
// the source's permissions.
func copyFileForBackup(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// isInsidePath reports whether path is inside root, comparing absolute path
// relationships rather than string prefixes (the same trap that made a sibling
// like "./downloads-backup" read as inside "./downloads"). The root itself
// counts as inside.
func isInsidePath(root, path string) bool {
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
