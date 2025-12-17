"""
Metadata extraction and file validation
Uses mutagen for audio file metadata
"""
import os
from pathlib import Path
from typing import Optional, Dict, Any
from dataclasses import dataclass
from mutagen import File as MutagenFile
from mutagen.easyid3 import EasyID3
from mutagen.mp3 import MP3
from mutagen.flac import FLAC
from mutagen.mp4 import MP4
from mutagen.oggvorbis import OggVorbis


@dataclass
class AudioMetadata:
    """Extracted audio metadata"""
    artist: Optional[str] = None
    album: Optional[str] = None
    title: Optional[str] = None
    track_number: Optional[int] = None
    year: Optional[int] = None
    genre: Optional[str] = None
    duration: Optional[float] = None  # seconds
    bitrate: Optional[int] = None  # kbps
    sample_rate: Optional[int] = None  # Hz
    channels: Optional[int] = None
    codec: Optional[str] = None
    file_size: int = 0

    def is_valid(self) -> bool:
        """Check if metadata contains minimum required fields"""
        return bool(self.artist and self.title)

    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary"""
        return {
            "artist": self.artist,
            "album": self.album,
            "title": self.title,
            "track_number": self.track_number,
            "year": self.year,
            "genre": self.genre,
            "duration": self.duration,
            "bitrate": self.bitrate,
            "sample_rate": self.sample_rate,
            "channels": self.channels,
            "codec": self.codec,
            "file_size": self.file_size
        }


class MetadataExtractor:
    """Extract metadata from audio files"""

    SUPPORTED_EXTENSIONS = {'.mp3', '.flac', '.m4a', '.ogg', '.opus', '.wma'}

    @staticmethod
    def is_audio_file(file_path: Path) -> bool:
        """Check if file is a supported audio format"""
        return file_path.suffix.lower() in MetadataExtractor.SUPPORTED_EXTENSIONS

    @staticmethod
    def extract(file_path: Path) -> Optional[AudioMetadata]:
        """
        Extract metadata from audio file

        Args:
            file_path: Path to audio file

        Returns:
            AudioMetadata object or None if extraction fails
        """
        if not file_path.exists():
            return None

        if not MetadataExtractor.is_audio_file(file_path):
            return None

        try:
            audio = MutagenFile(str(file_path), easy=True)
            if audio is None:
                return None

            metadata = AudioMetadata()
            metadata.file_size = file_path.stat().st_size

            # Extract common tags
            metadata.artist = MetadataExtractor._get_tag(audio, 'artist')
            metadata.album = MetadataExtractor._get_tag(audio, 'album')
            metadata.title = MetadataExtractor._get_tag(audio, 'title')
            metadata.genre = MetadataExtractor._get_tag(audio, 'genre')

            # Track number
            track_str = MetadataExtractor._get_tag(audio, 'tracknumber')
            if track_str:
                try:
                    # Handle "1/12" format
                    metadata.track_number = int(track_str.split('/')[0])
                except (ValueError, IndexError):
                    pass

            # Year
            year_str = MetadataExtractor._get_tag(audio, 'date')
            if year_str:
                try:
                    metadata.year = int(year_str[:4])
                except (ValueError, IndexError):
                    pass

            # Audio properties
            if hasattr(audio.info, 'length'):
                metadata.duration = audio.info.length

            if hasattr(audio.info, 'bitrate'):
                metadata.bitrate = audio.info.bitrate // 1000  # Convert to kbps

            if hasattr(audio.info, 'sample_rate'):
                metadata.sample_rate = audio.info.sample_rate

            if hasattr(audio.info, 'channels'):
                metadata.channels = audio.info.channels

            # Codec detection
            metadata.codec = MetadataExtractor._detect_codec(file_path, audio)

            return metadata

        except Exception as e:
            print(f"Error extracting metadata from {file_path}: {e}")
            return None

    @staticmethod
    def _get_tag(audio, key: str) -> Optional[str]:
        """Get tag value from audio file"""
        if key in audio:
            value = audio[key]
            if isinstance(value, list) and len(value) > 0:
                return str(value[0])
            elif isinstance(value, str):
                return value
        return None

    @staticmethod
    def _detect_codec(file_path: Path, audio) -> Optional[str]:
        """Detect audio codec"""
        ext = file_path.suffix.lower()

        if ext == '.mp3':
            return 'MP3'
        elif ext == '.flac':
            return 'FLAC'
        elif ext in ['.m4a', '.mp4']:
            return 'AAC'
        elif ext in ['.ogg', '.opus']:
            return 'OGG/Opus'
        elif ext == '.wma':
            return 'WMA'

        return None

    @staticmethod
    def normalize_filename(metadata: AudioMetadata, original_path: Path) -> str:
        """
        Generate normalized filename from metadata

        Format: Artist - Title.ext
        Falls back to original name if metadata is insufficient
        """
        if not metadata.is_valid():
            return original_path.name

        artist = MetadataExtractor._sanitize_filename(metadata.artist)
        title = MetadataExtractor._sanitize_filename(metadata.title)
        ext = original_path.suffix

        return f"{artist} - {title}{ext}"

    @staticmethod
    def generate_library_path(metadata: AudioMetadata, library_root: Path) -> Optional[Path]:
        """
        Generate organized library path from metadata

        Structure: library_root/Artist/Album/Track - Title.ext
        Returns None if metadata is insufficient
        """
        if not metadata.is_valid():
            return None

        artist = MetadataExtractor._sanitize_filename(metadata.artist)
        album = MetadataExtractor._sanitize_filename(metadata.album or "Unknown Album")
        title = MetadataExtractor._sanitize_filename(metadata.title)

        # Build filename with track number if available
        if metadata.track_number:
            filename = f"{metadata.track_number:02d} - {title}"
        else:
            filename = title

        return library_root / artist / album / filename

    @staticmethod
    def _sanitize_filename(text: str) -> str:
        """Remove/replace characters not safe for filenames"""
        if not text:
            return "Unknown"

        # Replace problematic characters
        replacements = {
            '/': '-',
            '\\': '-',
            ':': '-',
            '*': '',
            '?': '',
            '"': "'",
            '<': '',
            '>': '',
            '|': '-',
            '\0': ''
        }

        for old, new in replacements.items():
            text = text.replace(old, new)

        # Remove leading/trailing whitespace and dots
        text = text.strip('. ')

        return text or "Unknown"


class FileValidator:
    """Validate audio files"""

    MIN_FILE_SIZE = 100 * 1024  # 100 KB
    MAX_FILE_SIZE = 500 * 1024 * 1024  # 500 MB

    @staticmethod
    def validate(file_path: Path) -> tuple[bool, Optional[str]]:
        """
        Validate audio file

        Args:
            file_path: Path to file

        Returns:
            (is_valid, error_message)
        """
        if not file_path.exists():
            return False, "File does not exist"

        if not file_path.is_file():
            return False, "Not a file"

        # Check file size
        file_size = file_path.stat().st_size
        if file_size < FileValidator.MIN_FILE_SIZE:
            return False, f"File too small ({file_size} bytes)"

        if file_size > FileValidator.MAX_FILE_SIZE:
            return False, f"File too large ({file_size} bytes)"

        # Check if it's an audio file
        if not MetadataExtractor.is_audio_file(file_path):
            return False, f"Unsupported format: {file_path.suffix}"

        # Try to extract metadata
        metadata = MetadataExtractor.extract(file_path)
        if metadata is None:
            return False, "Cannot read audio metadata"

        if not metadata.is_valid():
            return False, "Missing required metadata (artist or title)"

        return True, None
