"""
slskd API Client
Handles search, download queue management, and download monitoring
"""
import httpx
import asyncio
from typing import List, Dict, Optional, Any
from dataclasses import dataclass
from enum import Enum


class DownloadState(Enum):
    """slskd download states"""
    QUEUED = "Queued"
    INITIALIZING = "Initializing"
    INPROGRESS = "InProgress"
    COMPLETED = "Completed"
    CANCELLED = "Cancelled"
    ERRORED = "Errored"


@dataclass
class SearchResult:
    """Search result from slskd"""
    username: str
    filename: str
    size: int
    speed: int
    queue_length: int
    locked: bool
    bitrate: Optional[int] = None
    length: Optional[int] = None

    @property
    def score(self) -> float:
        """Calculate quality score for ranking results"""
        score = 0.0

        # Prefer higher bitrate
        if self.bitrate:
            if self.bitrate >= 320:
                score += 10
            elif self.bitrate >= 256:
                score += 7
            elif self.bitrate >= 192:
                score += 5

        # Prefer faster users
        if self.speed > 0:
            score += min(self.speed / 1000000, 5)  # Cap at 5 points

        # Penalize queue length
        score -= min(self.queue_length / 10, 3)  # Cap penalty at 3 points

        # Penalize locked files
        if self.locked:
            score -= 5

        return score


@dataclass
class Download:
    """Download status from slskd"""
    id: str
    username: str
    filename: str
    size: int
    state: DownloadState
    bytes_downloaded: int
    average_speed: float
    path: Optional[str] = None

    @property
    def progress_percent(self) -> float:
        """Calculate download progress percentage"""
        if self.size == 0:
            return 0.0
        return (self.bytes_downloaded / self.size) * 100


class SlskdClient:
    """Client for slskd API"""

    def __init__(self, base_url: str, api_key: str):
        self.base_url = base_url.rstrip('/')
        self.api_key = api_key
        self.client = httpx.AsyncClient(
            headers={"X-API-Key": api_key},
            timeout=30.0
        )

    async def close(self):
        """Close HTTP client"""
        await self.client.aclose()

    async def search(
        self,
        query: str,
        timeout: int = 30,
        filter_responses: bool = True
    ) -> List[SearchResult]:
        """
        Execute a search and wait for results

        Args:
            query: Search query string
            timeout: Search timeout in seconds
            filter_responses: Whether to filter out low-quality responses

        Returns:
            List of search results sorted by quality score
        """
        # Start search
        response = await self.client.post(
            f"{self.base_url}/api/v0/searches",
            json={
                "searchText": query,
                "searchTimeout": timeout * 1000,  # Convert to milliseconds
                "filterResponses": filter_responses
            }
        )
        response.raise_for_status()
        search_data = response.json()
        search_id = search_data["id"]

        # Wait for search to complete
        await asyncio.sleep(timeout)

        # Get search results
        response = await self.client.get(
            f"{self.base_url}/api/v0/searches/{search_id}"
        )
        response.raise_for_status()
        results_data = response.json()

        # Parse results
        results = []
        for response_item in results_data.get("responses", []):
            username = response_item["username"]
            for file_item in response_item.get("files", []):
                result = SearchResult(
                    username=username,
                    filename=file_item["filename"],
                    size=file_item["size"],
                    speed=response_item.get("uploadSpeed", 0),
                    queue_length=response_item.get("queueLength", 0),
                    locked=file_item.get("isLocked", False),
                    bitrate=file_item.get("bitRate"),
                    length=file_item.get("length")
                )
                results.append(result)

        # Sort by quality score (best first)
        results.sort(key=lambda r: r.score, reverse=True)

        return results

    async def enqueue_download(
        self,
        username: str,
        filename: str
    ) -> str:
        """
        Add a file to the download queue

        Args:
            username: Username of the file owner
            filename: Full path to the file

        Returns:
            Download ID
        """
        response = await self.client.post(
            f"{self.base_url}/api/v0/downloads",
            json={
                "username": username,
                "files": [filename]
            }
        )
        response.raise_for_status()
        data = response.json()

        # Extract download ID from response
        # slskd returns download info, we need to construct the ID
        return f"{username}:{filename}"

    async def get_download(self, username: str, filename: str) -> Optional[Download]:
        """
        Get download status

        Args:
            username: Username of the file owner
            filename: Full path to the file

        Returns:
            Download object or None if not found
        """
        response = await self.client.get(
            f"{self.base_url}/api/v0/downloads/{username}/{filename}"
        )

        if response.status_code == 404:
            return None

        response.raise_for_status()
        data = response.json()

        return Download(
            id=f"{username}:{filename}",
            username=data["username"],
            filename=data["filename"],
            size=data["size"],
            state=DownloadState(data["state"]),
            bytes_downloaded=data["bytesDownloaded"],
            average_speed=data.get("averageSpeed", 0.0),
            path=data.get("path")
        )

    async def get_all_downloads(self) -> List[Download]:
        """Get all downloads"""
        response = await self.client.get(
            f"{self.base_url}/api/v0/downloads"
        )
        response.raise_for_status()
        data = response.json()

        downloads = []
        for item in data:
            downloads.append(Download(
                id=f"{item['username']}:{item['filename']}",
                username=item["username"],
                filename=item["filename"],
                size=item["size"],
                state=DownloadState(item["state"]),
                bytes_downloaded=item["bytesDownloaded"],
                average_speed=item.get("averageSpeed", 0.0),
                path=item.get("path")
            ))

        return downloads

    async def cancel_download(self, username: str, filename: str) -> bool:
        """
        Cancel a download

        Args:
            username: Username of the file owner
            filename: Full path to the file

        Returns:
            True if cancelled successfully
        """
        response = await self.client.delete(
            f"{self.base_url}/api/v0/downloads/{username}/{filename}"
        )
        return response.status_code == 204

    async def get_active_download_count(self) -> int:
        """Get count of active (non-completed) downloads"""
        downloads = await self.get_all_downloads()
        return sum(1 for d in downloads if d.state in [
            DownloadState.QUEUED,
            DownloadState.INITIALIZING,
            DownloadState.INPROGRESS
        ])

    async def wait_for_download_slot(self, max_concurrent: int = 10, check_interval: float = 5.0):
        """
        Wait until there's an available download slot

        Args:
            max_concurrent: Maximum concurrent downloads
            check_interval: Seconds between checks
        """
        while True:
            active_count = await self.get_active_download_count()
            if active_count < max_concurrent:
                return
            await asyncio.sleep(check_interval)

    async def wait_for_download_completion(
        self,
        username: str,
        filename: str,
        check_interval: float = 5.0,
        timeout: Optional[float] = None
    ) -> Download:
        """
        Wait for a download to complete

        Args:
            username: Username of the file owner
            filename: Full path to the file
            check_interval: Seconds between status checks
            timeout: Optional timeout in seconds

        Returns:
            Final Download object

        Raises:
            TimeoutError: If timeout is reached
            RuntimeError: If download fails or is cancelled
        """
        start_time = asyncio.get_event_loop().time()

        while True:
            download = await self.get_download(username, filename)

            if download is None:
                raise RuntimeError("Download not found")

            if download.state == DownloadState.COMPLETED:
                return download

            if download.state in [DownloadState.CANCELLED, DownloadState.ERRORED]:
                raise RuntimeError(f"Download {download.state.value}")

            if timeout is not None:
                elapsed = asyncio.get_event_loop().time() - start_time
                if elapsed >= timeout:
                    raise TimeoutError(f"Download timeout after {timeout}s")

            await asyncio.sleep(check_interval)

    async def health_check(self) -> bool:
        """Check if slskd is healthy and reachable"""
        try:
            response = await self.client.get(f"{self.base_url}/api/v0/session")
            return response.status_code == 200
        except:
            return False
