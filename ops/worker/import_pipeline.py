"""
Import and enrichment pipeline
Validates, organizes, and imports downloaded files into the music library
"""
import os
import shutil
import hashlib
from pathlib import Path
from typing import Optional, Tuple
from datetime import datetime
from metadata_extractor import MetadataExtractor, FileValidator, AudioMetadata


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
    """Enrich metadata from external sources (future: MusicBrainz, LastFM, etc.)"""

    def __init__(self):
        pass

    async def enrich(self, metadata: AudioMetadata) -> AudioMetadata:
        """
        Enrich metadata with information from external sources

        Args:
            metadata: Existing metadata

        Returns:
            Enriched metadata
        """
        # TODO: Implement external metadata lookups
        # - MusicBrainz for canonical artist/album/track info
        # - LastFM for tags, similar artists
        # - Cover art downloads

        # For now, return metadata as-is
        return metadata

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
        # TODO: Implement cover art fetching
        # - Try MusicBrainz Cover Art Archive
        # - Fallback to LastFM
        # - Embed in audio files if desired

        return None
