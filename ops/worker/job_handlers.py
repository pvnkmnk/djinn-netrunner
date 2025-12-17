"""
Job type handlers
Implements specific logic for each job type: sync, acquisition, import, index_refresh
"""
import asyncio
import json
import os
from pathlib import Path
from typing import Optional, Dict, Any, List
from datetime import datetime

import asyncpg
from slskd_client import SlskdClient, DownloadState
from import_pipeline import ImportPipeline, MetadataEnricher
from metadata_extractor import MetadataExtractor
from gonic_client import GonicClient
try:
    import spotipy
    from spotipy.oauth2 import SpotifyClientCredentials
except Exception:  # pragma: no cover - optional at import time
    spotipy = None
    SpotifyClientCredentials = None


class BaseJobHandler:
    """Base class for job handlers"""

    def __init__(
        self,
        db_pool: asyncpg.Pool,
        slskd_client: SlskdClient,
        gonic_client: GonicClient,
        import_pipeline: ImportPipeline,
        metadata_enricher: MetadataEnricher
    ):
        self.db = db_pool
        self.slskd = slskd_client
        self.gonic = gonic_client
        self.import_pipeline = import_pipeline
        self.enricher = metadata_enricher

    async def log(self, job_id: int, level: str, message: str, item_id: Optional[int] = None):
        """Append log to job"""
        await self.db.fetchval(
            "SELECT append_job_log($1, $2, $3, $4)",
            job_id, level, message, item_id
        )


class SyncJobHandler(BaseJobHandler):
    """Handler for sync job type - syncs playlists/sources to create acquisition jobs"""

    async def execute(self, job_id: int, job: dict):
        """Execute sync job"""
        await self.log(job_id, "INFO", "Starting sync job")

        # Get source
        source_id = int(job['scope_id'])
        async with self.db.acquire() as conn:
            source = await conn.fetchrow(
                "SELECT * FROM sources WHERE id = $1",
                source_id
            )

            if not source:
                await self.log(job_id, "ERR", f"Source {source_id} not found")
                return

        source_type = source['source_type']
        source_uri = source['source_uri']

        await self.log(job_id, "INFO", f"Syncing {source_type}: {source_uri}")

        # Parse source and get track list
        tracks = await self._parse_source(source_type, source_uri, job_id)

        if not tracks:
            await self.log(job_id, "ERR", "No tracks found in source")
            return

        await self.log(job_id, "OK", f"Found {len(tracks)} tracks")

        # Create acquisition job
        acquisition_job_id = await self._create_acquisition_job(
            source_id,
            tracks,
            job_id
        )

        await self.log(job_id, "OK", f"Created acquisition job #{acquisition_job_id}")

        # Update source last_synced_at
        async with self.db.acquire() as conn:
            await conn.execute(
                "UPDATE sources SET last_synced_at = NOW() WHERE id = $1",
                source_id
            )

    async def _parse_source(
        self,
        source_type: str,
        source_uri: str,
        job_id: int
    ) -> List[Dict[str, str]]:
        """
        Parse source and extract track list

        Returns:
            List of track dictionaries with 'artist', 'title', 'album' keys
        """
        if source_type == 'file_list':
            return await self._parse_file_list(source_uri, job_id)
        elif source_type == 'spotify_playlist':
            return await self._parse_spotify_playlist(source_uri, job_id)
        else:
            await self.log(job_id, "ERR", f"Unsupported source type: {source_type}")
            return []

    async def _parse_file_list(self, file_path: str, job_id: int) -> List[Dict[str, str]]:
        """Parse a simple text file with track listings"""
        tracks = []
        try:
            path = Path(file_path)
            if not path.exists():
                await self.log(job_id, "ERR", f"File not found: {file_path}")
                return []

            with open(path, 'r', encoding='utf-8') as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith('#'):
                        continue

                    # Expected format: Artist - Title
                    # or: Artist - Album - Title
                    parts = [p.strip() for p in line.split('-')]

                    if len(parts) >= 2:
                        track = {
                            'artist': parts[0],
                            'title': parts[-1],
                            'album': parts[1] if len(parts) == 3 else None
                        }
                        tracks.append(track)

            await self.log(job_id, "OK", f"Parsed {len(tracks)} tracks from file")

        except Exception as e:
            await self.log(job_id, "ERR", f"Error parsing file: {e}")

        return tracks

    async def _parse_spotify_playlist(self, playlist_uri: str, job_id: int) -> List[Dict[str, str]]:
        """Parse Spotify playlist via Spotify Web API.

        Supports URIs like:
          - spotify:playlist:PLAYLIST_ID
          - https://open.spotify.com/playlist/PLAYLIST_ID?si=...
        Requires SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET in environment.
        """
        tracks: List[Dict[str, str]] = []

        if spotipy is None or SpotifyClientCredentials is None:
            await self.log(job_id, "ERR", "spotipy not available. Install dependency in worker image.")
            return tracks

        client_id = os.getenv("SPOTIFY_CLIENT_ID")
        client_secret = os.getenv("SPOTIFY_CLIENT_SECRET")
        if not client_id or not client_secret:
            await self.log(job_id, "ERR", "Missing SPOTIFY_CLIENT_ID/SECRET environment variables")
            return tracks

        # Extract playlist ID
        playlist_id = self._extract_spotify_playlist_id(playlist_uri)
        if not playlist_id:
            await self.log(job_id, "ERR", f"Invalid Spotify playlist URI: {playlist_uri}")
            return tracks

        await self.log(job_id, "INFO", f"Fetching Spotify playlist: {playlist_id}")

        try:
            auth_mgr = SpotifyClientCredentials(client_id=client_id, client_secret=client_secret)
            sp = spotipy.Spotify(auth_manager=auth_mgr)

            # Fetch playlist metadata (for logging + snapshot)
            playlist_meta = sp.playlist(playlist_id=playlist_id, fields="name,tracks.total,snapshot_id,owner(display_name)")
            name = playlist_meta.get("name")
            total = playlist_meta.get("tracks", {}).get("total", 0)
            snapshot_id = playlist_meta.get("snapshot_id")
            owner = (playlist_meta.get("owner") or {}).get("display_name")

            await self.log(job_id, "OK", f"Playlist '{name}' by {owner or 'unknown'} — {total} tracks (snapshot {snapshot_id})")

            # Paginate items
            limit = 100
            offset = 0
            fetched = 0

            while True:
                page = sp.playlist_items(
                    playlist_id=playlist_id,
                    fields="items(track(name,album(name),artists(name))),next,total",
                    additional_types=("track",),
                    limit=limit,
                    offset=offset
                )

                items = page.get("items", [])
                for it in items:
                    t = it.get("track") or {}
                    if not t:
                        continue
                    artist_list = t.get("artists") or []
                    artist_name = artist_list[0]["name"] if artist_list else None
                    title = t.get("name")
                    album = (t.get("album") or {}).get("name")
                    if artist_name and title:
                        tracks.append({
                            "artist": artist_name,
                            "title": title,
                            "album": album
                        })
                fetched += len(items)
                await self.log(job_id, "INFO", f"Fetched {fetched}/{total} tracks...")

                if not page.get("next") or len(items) == 0:
                    break
                offset += limit

            await self.log(job_id, "OK", f"Parsed {len(tracks)} tracks from Spotify playlist")

        except Exception as e:
            await self.log(job_id, "ERR", f"Spotify API error: {e}")

        return tracks

    def _extract_spotify_playlist_id(self, uri: str) -> Optional[str]:
        """Extract playlist ID from a Spotify URI or URL."""
        try:
            if not uri:
                return None
            uri = uri.strip()
            if uri.startswith("spotify:playlist:"):
                return uri.split(":")[-1]
            if "open.spotify.com/playlist/" in uri:
                # e.g., https://open.spotify.com/playlist/{id}?si=...
                part = uri.split("/playlist/")[-1]
                part = part.split("?")[0]
                part = part.split("#")[0]
                return part
            # Accept raw ID (22 chars usually)
            if "/" not in uri and ":" not in uri and len(uri) >= 16:
                return uri
            return None
        except Exception:
            return None

    async def _create_acquisition_job(
        self,
        source_id: int,
        tracks: List[Dict[str, str]],
        parent_job_id: int
    ) -> int:
        """Create acquisition job with job items"""
        async with self.db.acquire() as conn:
            # Determine owner from parent job
            owner_user_id = await conn.fetchval("SELECT owner_user_id FROM jobs WHERE id = $1", parent_job_id)
            # Create acquisition job
            job_id = await conn.fetchval("""
                INSERT INTO jobs(jobtype, scope_type, scope_id, params, owner_user_id)
                VALUES ('acquisition', 'source', $1, $2, $3)
                RETURNING id
            """, str(source_id), json.dumps({"parent_job_id": parent_job_id}), owner_user_id)

            # Create job items
            for idx, track in enumerate(tracks):
                normalized_query = f"{track['artist']} {track['title']}"

                await conn.execute("""
                    INSERT INTO jobitems(
                        job_id, sequence, normalized_query,
                        artist, album, track_title, status,
                        owner_user_id
                    )
                    VALUES ($1, $2, $3, $4, $5, $6, 'queued', $7)
                """, job_id, idx, normalized_query,
                    track['artist'], track.get('album'), track['title'], owner_user_id)

            return job_id


class AcquisitionJobHandler(BaseJobHandler):
    """Handler for acquisition job type - searches and downloads tracks"""

    MAX_CONCURRENT_DOWNLOADS = 10
    SEARCH_TIMEOUT = 30

    async def execute_item(self, job_id: int, item: dict):
        """Execute single acquisition item"""
        item_id = item['id']
        query = item['normalized_query']

        await self.log(job_id, "INFO", f"Searching: {query}", item_id)

        # Search slskd
        try:
            results = await self.slskd.search(
                query,
                timeout=self.SEARCH_TIMEOUT,
                filter_responses=True
            )

            if not results:
                await self._fail_item(job_id, item_id, "No search results found")
                return

            await self.log(job_id, "OK", f"Found {len(results)} results", item_id)

            # Pick best result
            best_result = results[0]
            await self.log(
                job_id,
                "INFO",
                f"Selected: {best_result.filename} (score: {best_result.score:.1f})",
                item_id
            )

            # Update item with search metadata
            async with self.db.acquire() as conn:
                await conn.execute("""
                    UPDATE jobitems
                    SET status = 'downloading',
                        slskd_download_id = $2
                    WHERE id = $1
                """, item_id, f"{best_result.username}:{best_result.filename}")

            # Wait for download slot
            await self.slskd.wait_for_download_slot(self.MAX_CONCURRENT_DOWNLOADS)

            # Enqueue download
            download_id = await self.slskd.enqueue_download(
                best_result.username,
                best_result.filename
            )

            await self.log(job_id, "INFO", f"Download queued", item_id)

            # Wait for download completion
            download = await self.slskd.wait_for_download_completion(
                best_result.username,
                best_result.filename,
                check_interval=5.0,
                timeout=600.0  # 10 minute timeout per download
            )

            await self.log(job_id, "OK", f"Download completed", item_id)

            # Update item with download path
            async with self.db.acquire() as conn:
                await conn.execute("""
                    UPDATE jobitems
                    SET download_path = $2
                    WHERE id = $1
                """, item_id, download.path)

            # Trigger import
            await self._import_file(job_id, item_id, download.path, item)

        except asyncio.TimeoutError:
            await self._fail_item(job_id, item_id, "Download timeout")
        except Exception as e:
            await self._fail_item(job_id, item_id, f"Error: {e}")

    async def _import_file(self, job_id: int, item_id: int, download_path: str, item: dict):
        """Import downloaded file to library"""
        await self.log(job_id, "INFO", "Importing to library", item_id)

        source_path = Path(download_path)
        if not source_path.exists():
            await self._fail_item(job_id, item_id, f"Downloaded file not found: {download_path}")
            return

        # Process through import pipeline
        success, final_path, metadata, error = await self.import_pipeline.process_file(
            source_path,
            expected_artist=item.get('artist'),
            expected_title=item.get('track_title')
        )

        if not success:
            await self._fail_item(job_id, item_id, f"Import failed: {error}")
            return

        # Optional: metadata enrichment via MusicBrainz
        enrichment = {}
        try:
            enrichment = await self.enricher.enrich(metadata) if self.enricher else {}
            if enrichment:
                await self.log(job_id, "INFO", f"Enriched via MusicBrainz (confidence {enrichment.get('enrichment_confidence', 0.0):.2f})", item_id)
                # Update in-memory metadata for better library paths in future steps
                metadata.artist = enrichment.get('artist', metadata.artist)
                metadata.album = enrichment.get('album', metadata.album)
                metadata.title = enrichment.get('title', metadata.title)
                if enrichment.get('enriched_year'):
                    metadata.year = enrichment.get('enriched_year')
                if enrichment.get('enriched_genre'):
                    metadata.genre = enrichment.get('enriched_genre')
        except Exception as e:
            await self.log(job_id, "ERR", f"Enrichment failed: {e}", item_id)

        # Update item as imported
        async with self.db.acquire() as conn:
            await conn.execute("""
                UPDATE jobitems
                SET status = 'imported',
                    finished_at = NOW(),
                    final_path = $2
                WHERE id = $1
            """, item_id, str(final_path))

            # Create acquisition record
            # Look up job owner for ownership propagation
            owner_user_id = await conn.fetchval("SELECT owner_user_id FROM jobs WHERE id = $1", job_id)

            acq_id = await conn.fetchval("""
                INSERT INTO acquisitions(
                    job_id, jobitem_id, artist, album, track_title,
                    original_path, final_path, file_size, file_hash,
                    mb_recording_id, mb_release_id, mb_artist_id,
                    enrichment_confidence, enriched_year, enriched_genre,
                    owner_user_id
                )
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
                        $10, $11, $12, $13, $14, $15, $16)
                RETURNING id
            """, job_id, item_id,
                metadata.artist, metadata.album, metadata.title,
                str(source_path), str(final_path),
                metadata.file_size, None,
                enrichment.get('mb_recording_id'),
                enrichment.get('mb_release_id'),
                enrichment.get('mb_artist_id'),
                enrichment.get('enrichment_confidence'),
                enrichment.get('enriched_year'),
                enrichment.get('enriched_genre'),
                owner_user_id)

        await self.log(job_id, "OK", f"Imported: {final_path}", item_id)

        # Cleanup staging file
        self.import_pipeline.cleanup_staging_file(source_path)

        # Attempt cover art fetch (post-insert so we can update acquisition record)
        try:
            cover = await self.enricher.fetch_cover_art_by_musicbrainz(
                enrichment.get('mb_release_id'),
                metadata.artist,
                metadata.album,
                self.import_pipeline.library_dir
            ) if self.enricher else None

            if cover:
                async with self.db.acquire() as conn:
                    await conn.execute("""
                        UPDATE acquisitions
                        SET cover_art_url = $2,
                            cover_art_path = $3,
                            cover_art_etag = $4,
                            image_hash = $5,
                            cover_last_checked = NOW()
                        WHERE id = $1
                    """, acq_id,
                        cover.get('cover_art_url'),
                        cover.get('cover_art_path'),
                        cover.get('cover_art_etag'),
                        cover.get('image_hash'))
                await self.log(job_id, "OK", "Cover art saved", item_id)
            else:
                await self.log(job_id, "INFO", "No cover art available", item_id)
        except Exception as e:
            await self.log(job_id, "ERR", f"Cover art fetch failed: {e}", item_id)

    async def _fail_item(self, job_id: int, item_id: int, reason: str):
        """Mark item as failed"""
        await self.log(job_id, "ERR", reason, item_id)

        async with self.db.acquire() as conn:
            await conn.execute("""
                UPDATE jobitems
                SET status = 'failed',
                    finished_at = NOW(),
                    failure_reason = $2
                WHERE id = $1
            """, item_id, reason)


class IndexRefreshJobHandler(BaseJobHandler):
    """Handler for index_refresh job type - triggers Gonic library scan"""

    async def execute(self, job_id: int, job: dict):
        """Execute index refresh job"""
        await self.log(job_id, "INFO", "Starting library index refresh")

        # Check Gonic health
        if not await self.gonic.health_check():
            await self.log(job_id, "ERR", "Gonic is not reachable")
            return

        # Get pre-scan stats
        pre_stats = await self.gonic.get_library_stats()
        await self.log(
            job_id,
            "INFO",
            f"Current library: {pre_stats['artist_count']} artists, {pre_stats['album_count']} albums"
        )

        # Trigger scan
        await self.log(job_id, "INFO", "Triggering Gonic scan...")
        success = await self.gonic.trigger_scan()

        if not success:
            await self.log(job_id, "ERR", "Failed to trigger scan")
            return

        await self.log(job_id, "OK", "Scan triggered")

        # Wait for scan completion
        try:
            await self.log(job_id, "INFO", "Waiting for scan to complete...")
            await self.gonic.wait_for_scan_completion(
                check_interval=5.0,
                timeout=300.0
            )

            await self.log(job_id, "OK", "Scan completed")

            # Get post-scan stats
            post_stats = await self.gonic.get_library_stats()
            await self.log(
                job_id,
                "OK",
                f"Updated library: {post_stats['artist_count']} artists, {post_stats['album_count']} albums"
            )

            # Calculate changes
            artist_diff = post_stats['artist_count'] - pre_stats['artist_count']
            album_diff = post_stats['album_count'] - pre_stats['album_count']

            await self.log(
                job_id,
                "OK",
                f"Changes: {artist_diff:+d} artists, {album_diff:+d} albums"
            )

        except TimeoutError:
            await self.log(job_id, "ERR", "Scan timeout (still running in background)")


class MetadataEnrichmentJobHandler(BaseJobHandler):
    """Handler for metadata enrichment - enriches library metadata from external sources"""

    async def execute(self, job_id: int, job: dict):
        """Execute metadata enrichment job"""
        await self.log(job_id, "INFO", "Starting metadata enrichment")

        # Get list of files to enrich
        params = job.get('params', {})
        if isinstance(params, str):
            params = json.loads(params)

        # If specific path provided, enrich that subtree
        target_path = params.get('path')
        if target_path:
            library_path = Path(self.import_pipeline.library_dir) / target_path
        else:
            library_path = Path(self.import_pipeline.library_dir)

        await self.log(job_id, "INFO", f"Scanning: {library_path}")

        # Find all audio files
        audio_files = []
        for file_path in library_path.rglob('*'):
            if file_path.is_file() and MetadataExtractor.is_audio_file(file_path):
                audio_files.append(file_path)

        await self.log(job_id, "OK", f"Found {len(audio_files)} audio files")

        enriched_count = 0
        skipped_count = 0

        # Process each file
        for file_path in audio_files:
            # Extract current metadata
            metadata = MetadataExtractor.extract(file_path)
            if metadata is None:
                skipped_count += 1
                continue

            # Enrich metadata
            enriched = await self.enricher.enrich(metadata)

            # TODO: Write enriched metadata back to file
            # For now, just count as enriched
            enriched_count += 1

        await self.log(
            job_id,
            "OK",
            f"Enrichment complete: {enriched_count} enriched, {skipped_count} skipped"
        )
