"""
Import and enrichment pipeline
Validates, organizes, and imports downloaded files into the music library
"""
import os
import shutil
import hashlib
from pathlib import Path
from typing import Optional, Tuple, Dict, Any
from datetime import datetime
from metadata_extractor import MetadataExtractor, FileValidator, AudioMetadata
import mimetypes
try:
    import httpx
except Exception:  # pragma: no cover
    httpx = None


class ImportPipeline:
    """Pipeline for importing downloaded files into organized library"""

    def __init__(self, staging_dir: Path, library_dir: Path):
        self.staging_dir = Path(staging_dir)
        self.library_dir = Path(library_dir)

        # Ensure directories exist
        self.staging_dir.mkdir(parents=True, exist_ok=True)
        self.library_dir.mkdir(parents=True, exist_ok=True)

    async def process_file(
        self,
        source_path: Path,
        expected_artist: Optional[str] = None,
        expected_title: Optional[str] = None
    ) -> Tuple[bool, Optional[Path], Optional[AudioMetadata], Optional[str]]:
        """
        Process a downloaded file: validate, extract metadata, and import to library

        Args:
            source_path: Path to downloaded file in staging
            expected_artist: Expected artist name (from search query)
            expected_title: Expected title (from search query)

        Returns:
            (success, final_path, metadata, error_message)
        """
        # Validate file
        is_valid, error = FileValidator.validate(source_path)
        if not is_valid:
            return False, None, None, f"Validation failed: {error}"

        # Extract metadata
        metadata = MetadataExtractor.extract(source_path)
        if metadata is None:
            return False, None, None, "Failed to extract metadata"

        if not metadata.is_valid():
            return False, None, metadata, "Missing required metadata (artist or title)"

        # Generate target path
        target_path = self._generate_target_path(metadata, source_path)
        if target_path is None:
            return False, None, metadata, "Cannot generate target path"

        # Check for duplicates
        if target_path.exists():
            # Check if files are identical
            if self._files_are_identical(source_path, target_path):
                # Already imported, skip
                return True, target_path, metadata, None
            else:
                # Different file with same path, append hash suffix
                target_path = self._make_unique_path(target_path)

        # Ensure target directory exists
        target_path.parent.mkdir(parents=True, exist_ok=True)

        # Copy file to library (preserve original in staging for now)
        try:
            shutil.copy2(source_path, target_path)
        except Exception as e:
            return False, None, metadata, f"Failed to copy file: {e}"

        return True, target_path, metadata, None

    def _generate_target_path(self, metadata: AudioMetadata, source_path: Path) -> Optional[Path]:
        """Generate target path in library based on metadata"""
        # Use metadata extractor to generate organized path
        base_path = MetadataExtractor.generate_library_path(metadata, self.library_dir)

        if base_path is None:
            return None

        # Add file extension
        ext = source_path.suffix
        return base_path.with_suffix(ext)

    def _files_are_identical(self, path1: Path, path2: Path) -> bool:
        """Check if two files are identical by comparing size and hash"""
        # Quick size check first
        if path1.stat().st_size != path2.stat().st_size:
            return False

        # Compare MD5 hashes
        hash1 = self._compute_file_hash(path1)
        hash2 = self._compute_file_hash(path2)

        return hash1 == hash2

    def _compute_file_hash(self, path: Path, algorithm: str = 'md5') -> str:
        """Compute file hash"""
        hash_obj = hashlib.new(algorithm)

        with open(path, 'rb') as f:
            for chunk in iter(lambda: f.read(8192), b''):
                hash_obj.update(chunk)

        return hash_obj.hexdigest()

    def _make_unique_path(self, path: Path) -> Path:
        """Make path unique by appending hash suffix"""
        stem = path.stem
        suffix = path.suffix
        parent = path.parent

        # Append timestamp to make unique
        timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
        return parent / f"{stem}_{timestamp}{suffix}"

    def cleanup_staging_file(self, path: Path) -> bool:
        """Remove file from staging after successful import"""
        try:
            if path.exists():
                path.unlink()
            return True
        except Exception as e:
            print(f"Failed to cleanup staging file {path}: {e}")
            return False

    def get_library_stats(self) -> dict:
        """Get library statistics"""
        total_files = 0
        total_size = 0
        formats = {}

        for file_path in self.library_dir.rglob('*'):
            if file_path.is_file() and MetadataExtractor.is_audio_file(file_path):
                total_files += 1
                total_size += file_path.stat().st_size

                ext = file_path.suffix.lower()
                formats[ext] = formats.get(ext, 0) + 1

        return {
            'total_files': total_files,
            'total_size_bytes': total_size,
            'total_size_gb': round(total_size / (1024**3), 2),
            'formats': formats
        }


class MetadataEnricher:
    """Enrich metadata using MusicBrainz.

    Provides a simple lookup by artist + title (+ optional duration) and returns
    enrichment fields including MBIDs and normalized text values with a
    confidence score. Designed to be optional and fail-safe.
    """

    def __init__(self, user_agent: Optional[str] = None):
        self.user_agent = user_agent or "netrunner/0.1 (https://example.local)"
        try:
            import musicbrainzngs
            self.mb = musicbrainzngs
            # Configure user agent as required by MB policy
            self.mb.set_useragent(self.user_agent)
        except Exception:
            self.mb = None

    async def enrich(self, metadata: AudioMetadata) -> Dict[str, Any]:
        """Attempt MusicBrainz enrichment; returns a dict of extra fields.

        Returns keys may include: mb_recording_id, mb_release_id, mb_artist_id,
        enrichment_confidence, enriched_year, enriched_genre, artist, album, title.
        If enrichment cannot be performed, returns {}.
        """
        if self.mb is None or not metadata or not metadata.artist or not metadata.title:
            return {}

        artist = metadata.artist
        title = metadata.title
        duration = metadata.duration

        # Basic throttling courtesy sleep; real rate-limiting can be added later by caller
        try:
            # MusicBrainz: search recordings by artist and track, include releases and artists
            result = await self._mb_search_recordings(artist, title, duration)
            if not result:
                return {}

            rec = result.get("recording")
            rel = result.get("release")
            art = result.get("artist")

            confidence = result.get("confidence", 0.0)
            out: Dict[str, Any] = {
                "mb_recording_id": rec.get("id") if rec else None,
                "mb_release_id": rel.get("id") if rel else None,
                "mb_artist_id": art.get("id") if art else None,
                "enrichment_confidence": round(float(confidence), 2)
            }

            # Prefer canonical names from MB if present
            out["artist"] = (art or {}).get("name") or artist
            out["title"] = (rec or {}).get("title") or title
            if rel:
                out["album"] = rel.get("title") or metadata.album
                # Year if available
                date = rel.get("date") or ""
                if len(date) >= 4 and date[:4].isdigit():
                    out["enriched_year"] = int(date[:4])

            # Genre tags (MB tags are optional)
            tags = ((rec or {}).get("tag-list") or []) or ((rel or {}).get("tag-list") or [])
            if tags:
                # Pick the highest count tag name if present
                best_tag = None
                try:
                    best_tag = max(tags, key=lambda t: int(t.get("count", 0))).get("name")
                except Exception:
                    # Fallback: first tag name
                    if isinstance(tags, list) and len(tags) > 0:
                        best_tag = tags[0].get("name")
                if best_tag:
                    out["enriched_genre"] = best_tag

            return {k: v for k, v in out.items() if v is not None}

        except Exception:
            # Silent failure by design; caller logs if needed
            return {}

    async def _mb_search_recordings(self, artist: str, title: str, duration: Optional[float]) -> Optional[Dict[str, Any]]:
        """Search MusicBrainz recordings and pick best candidate.

        Uses synchronous client under the hood; keep async signature for API
        uniformity and future async IO implementations.
        """
        if self.mb is None:
            return None

        # Synchronous call; MB client is blocking. Keep lightweight.
        try:
            # Build query string
            query = f"recording:{title} AND artist:{artist}"
            resp = self.mb.search_recordings(query=query, limit=5, offset=0)
            recs = resp.get("recording-list", [])
            if not recs:
                return None

            def score(rec: Dict[str, Any]) -> float:
                s = float(rec.get("ext:score", 0.0)) / 100.0
                # Duration proximity bonus (if available)
                try:
                    if duration and "length" in rec:
                        mb_ms = float(rec["length"])  # milliseconds
                        delta = abs((duration * 1000.0) - mb_ms)
                        # Within 5s = +0.2, within 10s = +0.1
                        if delta <= 5000:
                            s += 0.2
                        elif delta <= 10000:
                            s += 0.1
                except Exception:
                    pass
                return s

            # Choose best rec by score
            best = max(recs, key=score)

            # Pick a representative release and artist if present
            rel_list = best.get("release-list", [])
            rel = rel_list[0] if rel_list else None
            art_list = best.get("artist-credit", [])
            art = None
            if art_list:
                # artist-credit may contain names and joins; pick first credited artist
                a = art_list[0].get("artist") if isinstance(art_list[0], dict) else None
                if isinstance(a, dict):
                    art = a

            return {
                "recording": {"id": best.get("id"), "title": best.get("title"), "tag-list": best.get("tag-list")},
                "release": {"id": (rel or {}).get("id"), "title": (rel or {}).get("title"), "date": (rel or {}).get("date"), "tag-list": (rel or {}).get("tag-list")},
                "artist": {"id": (art or {}).get("id"), "name": (art or {}).get("name")},
                "confidence": score(best)
            }
        except Exception:
            return None

    async def fetch_cover_art(
        self,
        artist: str,
        album: str,
        target_dir: Path
    ) -> Optional[Path]:
        """
        Fetch cover art for an album

        Args:
            artist: Artist name
            album: Album name
            target_dir: Directory to save cover art

        Returns:
            Path to saved cover art or None
        """
        # Placeholder (kept for API compatibility). Real implementation uses
        # MusicBrainz release IDs via fetch_cover_art_by_musicbrainz.
        return None

    async def fetch_cover_art_by_musicbrainz(
        self,
        mb_release_id: Optional[str],
        artist: Optional[str],
        album: Optional[str],
        base_dir: Path
    ) -> Optional[Dict[str, Any]]:
        """Fetch cover art from Cover Art Archive using a MusicBrainz release ID.

        Returns dict with keys: cover_art_path, cover_art_url, cover_art_etag, image_hash,
        or None if not available. Images are stored under base_dir/cover_art/Artist/Album.
        """
        if not mb_release_id or httpx is None:
            return None

        # Candidates: specific size first, then generic front
        url_candidates = [
            f"https://coverartarchive.org/release/{mb_release_id}/front-500.jpg",
            f"https://coverartarchive.org/release/{mb_release_id}/front",
        ]

        headers = {"User-Agent": getattr(self, 'user_agent', 'netrunner/0.1')}

        async with httpx.AsyncClient(headers=headers, timeout=20) as client:
            for url in url_candidates:
                try:
                    resp = await client.get(url, follow_redirects=True)
                    if resp.status_code == 200 and resp.content:
                        mime = resp.headers.get("Content-Type", "image/jpeg")
                        ext = mimetypes.guess_extension(mime.split(";")[0].strip()) or ".jpg"

                        # Compute a short hash for dedupe-friendly filenames
                        import hashlib as _hash
                        h = _hash.sha256(resp.content).hexdigest()[:16]

                        safe_artist = (artist or "Unknown Artist").replace('/', '_').strip()
                        safe_album = (album or "Unknown Album").replace('/', '_').strip()
                        target_dir = base_dir / "cover_art" / safe_artist / safe_album
                        target_dir.mkdir(parents=True, exist_ok=True)
                        fname = f"cover_{h}{ext}"
                        path = target_dir / fname

                        if not path.exists():
                            with open(path, 'wb') as f:
                                f.write(resp.content)

                        return {
                            "cover_art_path": str(path),
                            "cover_art_url": str(resp.url),
                            "cover_art_etag": resp.headers.get("ETag"),
                            "image_hash": h,
                        }
                except Exception:
                    # try next candidate
                    continue

        return None
