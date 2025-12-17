#!/usr/bin/env python3
"""
CLI tool to add a source to NETRUNNER
"""
import sys
import asyncio
import asyncpg
from pathlib import Path


async def add_source(
    db_url: str,
    source_type: str,
    source_uri: str,
    display_name: str
):
    """Add a source to the database"""
    conn = await asyncpg.connect(db_url)

    try:
        # Check if exists
        existing = await conn.fetchval(
            "SELECT id FROM sources WHERE source_uri = $1",
            source_uri
        )

        if existing:
            print(f"Source already exists with ID: {existing}")
            return existing

        # Create source
        source_id = await conn.fetchval("""
            INSERT INTO sources(source_type, source_uri, display_name, sync_enabled)
            VALUES ($1, $2, $3, true)
            RETURNING id
        """, source_type, source_uri, display_name)

        print(f"✓ Created source #{source_id}: {display_name}")
        print(f"  Type: {source_type}")
        print(f"  URI: {source_uri}")

        return source_id

    finally:
        await conn.close()


def main():
    if len(sys.argv) < 5:
        print("Usage: python add_source.py <db_url> <type> <uri> <display_name>")
        print()
        print("Example:")
        print("  python add_source.py postgresql://user:pass@localhost:5432/musicops \\")
        print("    file_list /data/playlists/favorites.txt 'My Favorites'")
        sys.exit(1)

    db_url = sys.argv[1]
    source_type = sys.argv[2]
    source_uri = sys.argv[3]
    display_name = sys.argv[4]

    asyncio.run(add_source(db_url, source_type, source_uri, display_name))


if __name__ == "__main__":
    main()
