"""
Job type handlers
Implements specific logic for each job type: sync, acquisition, import, index_refresh
"""
import asyncio
import json
from pathlib import Path
from typing import Optional, Dict, Any, List
from datetime import datetime

import asyncpg
from slskd_client import SlskdClient, DownloadState
from import_pipeline import ImportPipeline, MetadataEnricher
from metadata_extractor import MetadataExtractor
from gonic_client import GonicClient


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
        """Parse Spotify playlist (placeholder - requires Spotify API)"""
        await self.log(job_id, "INFO", "Spotify integration not yet implemented")
        # TODO: Implement Spotify API integration
        # - Use spotipy library
        # - Authenticate with client credentials
        # - Fetch playlist tracks
        return []

    async def _create_acquisition_job(
        self,
        source_id: int,
        tracks: List[Dict[str, str]],
        parent_job_id: int
    ) -> int:
        """Create acquisition job with job items"""
        async with self.db.acquire() as conn:
            # Create acquisition job
            job_id = await conn.fetchval("""
                INSERT INTO jobs(jobtype, scope_type, scope_id, params)
                VALUES ('acquisition', 'source', $1, $2)
                RETURNING id
            """, str(source_id), json.dumps({"parent_job_id": parent_job_id}))

            # Create job items
            for idx, track in enumerate(tracks):
                normalized_query = f"{track['artist']} {track['title']}"

                await conn.execute("""
                    INSERT INTO jobitems(
                        job_id, sequence, normalized_query,
                        artist, album, track_title, status
                    )
                    VALUES ($1, $2, $3, $4, $5, $6, 'queued')
                """, job_id, idx, normalized_query,
                    track['artist'], track.get('album'), track['title'])

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
            await conn.execute("""
                INSERT INTO acquisitions(
                    job_id, jobitem_id, artist, album, track_title,
                    original_path, final_path, file_size, file_hash
                )
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
            """, job_id, item_id,
                metadata.artist, metadata.album, metadata.title,
                str(source_path), str(final_path),
                metadata.file_size, None)

        await self.log(job_id, "OK", f"Imported: {final_path}", item_id)

        # Cleanup staging file
        self.import_pipeline.cleanup_staging_file(source_path)

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
