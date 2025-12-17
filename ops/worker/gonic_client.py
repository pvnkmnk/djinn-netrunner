"""
Gonic API Client
Handles library scanning and index management
"""
import httpx
from typing import Optional, Dict, Any


class GonicClient:
    """Client for Gonic Subsonic-compatible server"""

    def __init__(self, base_url: str, username: str = "admin", password: str = "admin"):
        """
        Initialize Gonic client

        Args:
            base_url: Base URL of Gonic server (e.g., http://gonic:4747)
            username: Admin username (default from Gonic setup)
            password: Admin password (default from Gonic setup)
        """
        self.base_url = base_url.rstrip('/')
        self.username = username
        self.password = password
        self.client = httpx.AsyncClient(timeout=60.0)

    async def close(self):
        """Close HTTP client"""
        await self.client.aclose()

    async def trigger_scan(self) -> bool:
        """
        Trigger a full library scan

        Returns:
            True if scan was triggered successfully
        """
        try:
            # Gonic Subsonic API endpoint for scanning
            response = await self.client.get(
                f"{self.base_url}/rest/startScan",
                params={
                    "u": self.username,
                    "p": self.password,
                    "v": "1.16.1",
                    "c": "netrunner",
                    "f": "json"
                }
            )

            if response.status_code == 200:
                data = response.json()
                # Check for Subsonic API success
                if "subsonic-response" in data:
                    status = data["subsonic-response"].get("status")
                    return status == "ok"

            return False

        except Exception as e:
            print(f"Error triggering Gonic scan: {e}")
            return False

    async def get_scan_status(self) -> Dict[str, Any]:
        """
        Get current scan status

        Returns:
            Dictionary with scan status information
        """
        try:
            response = await self.client.get(
                f"{self.base_url}/rest/getScanStatus",
                params={
                    "u": self.username,
                    "p": self.password,
                    "v": "1.16.1",
                    "c": "netrunner",
                    "f": "json"
                }
            )

            if response.status_code == 200:
                data = response.json()
                if "subsonic-response" in data:
                    scan_status = data["subsonic-response"].get("scanStatus", {})
                    return {
                        "scanning": scan_status.get("scanning", False),
                        "count": scan_status.get("count", 0)
                    }

            return {"scanning": False, "count": 0}

        except Exception as e:
            print(f"Error getting Gonic scan status: {e}")
            return {"scanning": False, "count": 0}

    async def wait_for_scan_completion(self, check_interval: float = 5.0, timeout: Optional[float] = 300.0):
        """
        Wait for current scan to complete

        Args:
            check_interval: Seconds between status checks
            timeout: Maximum wait time in seconds (None for no timeout)

        Raises:
            TimeoutError: If timeout is reached while scan is still running
        """
        import asyncio

        start_time = asyncio.get_event_loop().time()

        while True:
            status = await self.get_scan_status()

            if not status["scanning"]:
                return

            if timeout is not None:
                elapsed = asyncio.get_event_loop().time() - start_time
                if elapsed >= timeout:
                    raise TimeoutError(f"Scan timeout after {timeout}s")

            await asyncio.sleep(check_interval)

    async def get_music_folders(self) -> list:
        """
        Get configured music folders

        Returns:
            List of music folder dictionaries
        """
        try:
            response = await self.client.get(
                f"{self.base_url}/rest/getMusicFolders",
                params={
                    "u": self.username,
                    "p": self.password,
                    "v": "1.16.1",
                    "c": "netrunner",
                    "f": "json"
                }
            )

            if response.status_code == 200:
                data = response.json()
                if "subsonic-response" in data:
                    folders = data["subsonic-response"].get("musicFolders", {}).get("musicFolder", [])
                    return folders if isinstance(folders, list) else [folders]

            return []

        except Exception as e:
            print(f"Error getting music folders: {e}")
            return []

    async def get_library_stats(self) -> Dict[str, Any]:
        """
        Get library statistics

        Returns:
            Dictionary with artist count, album count, song count
        """
        try:
            # Get artists
            artists_response = await self.client.get(
                f"{self.base_url}/rest/getArtists",
                params={
                    "u": self.username,
                    "p": self.password,
                    "v": "1.16.1",
                    "c": "netrunner",
                    "f": "json"
                }
            )

            artist_count = 0
            album_count = 0

            if artists_response.status_code == 200:
                data = artists_response.json()
                if "subsonic-response" in data:
                    artists = data["subsonic-response"].get("artists", {})
                    index_list = artists.get("index", [])
                    if not isinstance(index_list, list):
                        index_list = [index_list]

                    for index in index_list:
                        artist_list = index.get("artist", [])
                        if not isinstance(artist_list, list):
                            artist_list = [artist_list]
                        artist_count += len(artist_list)
                        # Sum album counts
                        for artist in artist_list:
                            album_count += artist.get("albumCount", 0)

            return {
                "artist_count": artist_count,
                "album_count": album_count
            }

        except Exception as e:
            print(f"Error getting library stats: {e}")
            return {
                "artist_count": 0,
                "album_count": 0
            }

    async def health_check(self) -> bool:
        """Check if Gonic is healthy and reachable"""
        try:
            response = await self.client.get(
                f"{self.base_url}/rest/ping",
                params={
                    "u": self.username,
                    "p": self.password,
                    "v": "1.16.1",
                    "c": "netrunner",
                    "f": "json"
                }
            )

            if response.status_code == 200:
                data = response.json()
                if "subsonic-response" in data:
                    return data["subsonic-response"].get("status") == "ok"

            return False

        except Exception as e:
            print(f"Gonic health check failed: {e}")
            return False
