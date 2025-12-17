"""
Source management API endpoints
"""
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import Optional
import asyncpg


router = APIRouter(prefix="/api/sources", tags=["sources"])


class SourceCreate(BaseModel):
    source_type: str  # 'file_list', 'spotify_playlist', etc.
    source_uri: str
    display_name: str
    sync_enabled: bool = True
    config: Optional[dict] = None


class SourceUpdate(BaseModel):
    display_name: Optional[str] = None
    sync_enabled: Optional[bool] = None
    config: Optional[dict] = None


@router.post("")
async def create_source(source: SourceCreate, db_pool: asyncpg.Pool):
    """Create a new source"""
    async with db_pool.acquire() as conn:
        # Check if source_uri already exists
        existing = await conn.fetchval(
            "SELECT id FROM sources WHERE source_uri = $1",
            source.source_uri
        )

        if existing:
            raise HTTPException(status_code=400, detail="Source URI already exists")

        # Create source
        source_id = await conn.fetchval("""
            INSERT INTO sources(source_type, source_uri, display_name, sync_enabled, config)
            VALUES ($1, $2, $3, $4, $5)
            RETURNING id
        """, source.source_type, source.source_uri, source.display_name,
            source.sync_enabled, source.config)

    return {"id": source_id}


@router.get("/{source_id}")
async def get_source(source_id: int, db_pool: asyncpg.Pool):
    """Get source details"""
    async with db_pool.acquire() as conn:
        source = await conn.fetchrow(
            "SELECT * FROM sources WHERE id = $1",
            source_id
        )

        if not source:
            raise HTTPException(status_code=404, detail="Source not found")

    return dict(source)


@router.get("")
async def list_sources(db_pool: asyncpg.Pool):
    """List all sources"""
    async with db_pool.acquire() as conn:
        sources = await conn.fetch(
            "SELECT * FROM sources ORDER BY display_name"
        )

    return [dict(s) for s in sources]


@router.patch("/{source_id}")
async def update_source(source_id: int, update: SourceUpdate, db_pool: asyncpg.Pool):
    """Update source"""
    updates = []
    values = []
    param_idx = 1

    if update.display_name is not None:
        updates.append(f"display_name = ${param_idx}")
        values.append(update.display_name)
        param_idx += 1

    if update.sync_enabled is not None:
        updates.append(f"sync_enabled = ${param_idx}")
        values.append(update.sync_enabled)
        param_idx += 1

    if update.config is not None:
        updates.append(f"config = ${param_idx}")
        values.append(update.config)
        param_idx += 1

    if not updates:
        raise HTTPException(status_code=400, detail="No fields to update")

    values.append(source_id)
    query = f"UPDATE sources SET {', '.join(updates)} WHERE id = ${param_idx}"

    async with db_pool.acquire() as conn:
        result = await conn.execute(query, *values)

        if result == "UPDATE 0":
            raise HTTPException(status_code=404, detail="Source not found")

    return {"status": "updated"}


@router.delete("/{source_id}")
async def delete_source(source_id: int, db_pool: asyncpg.Pool):
    """Delete source"""
    async with db_pool.acquire() as conn:
        result = await conn.execute(
            "DELETE FROM sources WHERE id = $1",
            source_id
        )

        if result == "DELETE 0":
            raise HTTPException(status_code=404, detail="Source not found")

    return {"status": "deleted"}
