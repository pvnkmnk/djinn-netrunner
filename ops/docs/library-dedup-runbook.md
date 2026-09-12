# Runbook — Repairing Albums Fragmented Across Per-Credit Artist Folders

**Audience:** operators of a djinn-netrunner (NetRunner) deployment whose Navidrome library was
built before the album-fragmentation fix (PR #219).

## The problem

Imports processed before #219 used the **per-track tag artist** for the artist folder. Albums
whose tracks carry varied credits were split across several sibling folders — for example one
download of *Gutter Phenomenon* landed as:

```
music/
├── Every Time I Die/Gutter Phenomenon/...                    ← most tracks (canonical)
├── Every Time I Die & Daryl Palumbo/Gutter Phenomenon/...    ← guest-vocal tracks
└── Every Time I Die & Gerard Way/Gutter Phenomenon/...       ← one guest track
```

Navidrome indexes by folder, so each fragment shows up as a **separate album** in the UI, with
separate cover art, play counts, and starred state. New imports no longer do this (#219 stamps a
canonical album artist and dedups on it), but the legacy damage stays on disk until repaired.

## Tools

Two CLI subcommands (in `netrunner-cli`):

| Command | Purpose |
|---|---|
| `library detect-fragments [libraryID]` | List every album folder that spans multiple per-credit artist folders. Read-only. Omit the ID to scan all libraries. |
| `library merge-album <libraryID> <albumFolder> <canonicalArtistFolder> [--apply]` | Merge one album's fragments into the canonical folder. **Dry run by default** — `--apply` executes. |

Both commands support the global `--json` flag for machine-readable output.

### What the merge does (and does not do)

- **Moves** each unique track from the fragment folders into
  `<canonical>/<albumFolder>/`, keeping filenames.
- **Rewrites** the corresponding `Track` rows in the app database to the new paths.
- **Removes duplicate content**: if a fragment file has identical bytes to an existing canonical
  file (same size + MD5), the fragment copy is deleted and its `Track` row removed.
- **Sweeps emptied directories**: album and artist folders left empty by the merge are removed,
  deepest first. Populated directories are never touched.
- **Never overwrites or deletes differing content**: if a fragment file's name collides with an
  existing canonical file *and the bytes differ*, the file is reported as a `CONFLICT` and left
  in place for manual resolution. Same-name collisions among two *moved* files are conflicts too.
- **Refuses to escape the library root** (path traversal guard on the canonical folder).

## Procedure

### 1. Back up first (strongly recommended)

The merge rewrites files and DB rows in one pass. Take a filesystem snapshot of the library
volume and a database backup before applying anything:

```bash
# DB backup (see ops/docs/backup.md for your setup)
pg_dump "$DATABASE_URL" > ~/netrunner-pre-dedup-$(date +%F).sql

# Library snapshot
docker run --rm -v netrunner_music:/data -v "$PWD/backups:/backup" alpine \
  tar czf /backup/music-pre-dedup-$(date +%F).tgz /data
```

### 2. Detect the fragmentation

```bash
docker exec netrunner-worker netrunner-cli library detect-fragments
```

Example output:

```
Found 1 fragmented album(s):

Gutter Phenomenon — 10 track(s) across 3 folder(s)
 ==> Every Time I Die & Daryl Palumbo          (6 track(s))
     Every Time I Die                          (3 track(s))
     Every Time I Die & Gerard Way             (1 track(s))
    merge: netrunner-cli library merge-album <libraryID> "Gutter Phenomenon" "Every Time I Die & Daryl Palumbo" [--apply]
```

The `==>` marker is the **suggested** canonical folder (most tracks; lexicographic tie-break).
You may prefer a different target — e.g. the folder matching the real album artist — the merge
accepts any folder that already holds tracks of that album.

> **Which folder should be canonical?** For grouping, Navidrome mainly reads the `ALBUMARTIST`
> tag / folder name. Pick the fragment whose name matches the album's actual primary artist —
> usually the one holding the most tracks. If fragments split the *same filename* differently
> (a conflict at merge time), resolve manually (step 5) rather than guessing.

### 3. Always dry-run first

```bash
netrunner-cli library merge-album <libraryID> "Gutter Phenomenon" "Every Time I Die" --json | jq
```

Read the plan: every `MOVE`, `DEL`, `RMDIR` line is exactly what `--apply` will do. Check the
`conflicts` count — resolve or accept them (conflicts are skipped automatically, the rest of the
merge proceeds). Nothing has touched disk yet.

### 4. Apply

```bash
netrunner-cli library merge-album <libraryID> "Gutter Phenomenon" "Every Time I Die" --apply
```

If errors are reported (a file vanished mid-merge, a dir wouldn't remove), they are listed under
`ERROR` — the merge is otherwise atomic per-file; re-running the merge after fixing the cause is
safe (moved files no longer match, so they are skipped).

### 5. Resolve conflicts (rare)

A `CONFLICT` means two files share a filename but differ in content — e.g. an MP3 and a FLAC
renamed identically, or a different master. Decide manually:

```bash
# Compare
ffprobe -i "<canonical>/Album X/01.mp3" 2>&1 | grep -E "Duration|Stream"
ffprobe -i "<fragment>/Album X/01.mp3" 2>&1 | grep -E "Duration|Stream"

# Keep the better copy: either
mv "<fragment>/Album X/01.mp3" "<fragment>/Album X/01 - fragment.mp3"   # then re-run merge
# or delete the worse copy yourself, then re-run merge
```

Re-running `merge-album` after manual resolution is safe and cheap.

### 6. Re-index both servers

The app database rows are already rewritten by the merge; trigger scans so both indexing layers
pick up the new layout:

```bash
# App-side scan job (re-scans the library folder, updates Track rows' metadata)
netrunner-cli library scan <libraryID>

# Navidrome re-index (Subsonic API)
curl -s -u "admin:PASS" "http://navidrome:4533/rest/startScan.view?u=admin&p=PASS&v=1.16.1&c=netrunner&f=json" | jq '.["subsonic-response"].status'
```

Navidrome's folder watcher usually picks up the changes on its own; `startScan` just makes it
immediate. Verify in the Navidrome UI: the album should now show a single entry with the full
track count and one cover image.

### 7. Verify no fragmentation remains

```bash
netrunner-cli library detect-fragments   # expect: No fragmented albums found.
```

## Edge cases & notes

- **"Greatest Hits" by two different bands is *not* flagged.** Detection requires the observed
  credit-variant shape: one folder name being a strict `X & …` prefix-extension of the other.
  Unrelated artists sharing an album folder name are ignored.
- **SLskd staging is unrelated**: staging cleanup (`DOWNLOAD_STAGING`) was fixed separately
  (#219's sweep + #220's UID alignment). This runbook repairs the *library*, not staging.
- **Incomplete fragments**: if some tracks were never downloaded, the merged album simply has
  the tracks that exist. The monitor loop may later re-enqueue missing releases; thanks to #219
  they will import into the same canonical folder.
- **Different formats of the same track** (FLAC + MP3) are *not* duplicates to this tool unless
  both are named identically — different extensions mean different destination paths, so both
  merge in cleanly. Content-identical same-name copies are removed (better-quality canonical
  copy wins by virtue of already being there).
- **Volumes/permissions**: merges run inside the worker container as UID 1000 (#220), the same
  UID slskd and the library volume use — no permission surprises expected. If your deployment
  predates #220, ensure the worker can write to the library volume first.

## Related

- PR #219 — album-fragmentation fix (canonical album-artist folders, album dedup, staging sweep)
- PR #220 — staging permission fix (slskd at UID 1000, volume-init bootstrap)
- PR #223 — tag writes via ffmpeg (replaces audiometa, whose MP4 parser crashed on real-world cover atoms)
- `netrunner-cli library duplicates` — MB-recording-level duplicates (different concern,
  quality-aware replacement is tracked as DJI-366)
