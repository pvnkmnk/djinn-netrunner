"""
Source management API endpoints
"""
from fastapi import APIRouter, HTTPException, Depends, Request
from pydantic import BaseModel
from typing import Optional
from auth import get_current_user_optional, get_current_user


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


class ScheduleCreate(BaseModel):
    source_id: int
    cron_expr: str
    timezone: Optional[str] = "UTC"


@router.post("/schedules")
async def create_schedule(schedule: ScheduleCreate, request: Request):
    """Create or update a schedule for a source"""
    db_pool = request.app.state.db_pool

    async with db_pool.acquire() as conn:
        # Upsert by (source_id, cron_expr)
        existing = await conn.fetchrow(
            "SELECT id FROM schedules WHERE source_id = $1 AND cron_expr = $2",
            schedule.source_id, schedule.cron_expr
        )

        if existing:
            await conn.execute(
                """
                UPDATE schedules
                SET timezone = $3, enabled = TRUE, updated_at = NOW()
                WHERE id = $4
                """,
                schedule.timezone, schedule.source_id, schedule.cron_expr, existing['id']
            )
            sched_id = existing['id']
        else:
            sched_id = await conn.fetchval(
                """
                INSERT INTO schedules(source_id, cron_expr, timezone, enabled)
                VALUES ($1, $2, $3, TRUE)
                RETURNING id
                """,
                schedule.source_id, schedule.cron_expr, schedule.timezone
            )

    return {"id": sched_id}


@router.get("/{source_id}/schedules")
async def list_schedules(source_id: int, request: Request):
    db_pool = request.app.state.db_pool
    async with db_pool.acquire() as conn:
        rows = await conn.fetch(
            "SELECT * FROM schedules WHERE source_id = $1 ORDER BY id DESC",
            source_id
        )
    return [dict(r) for r in rows]


@router.delete("/schedules/{schedule_id}")
async def delete_schedule(schedule_id: int, request: Request):
    db_pool = request.app.state.db_pool
    async with db_pool.acquire() as conn:
        result = await conn.execute("DELETE FROM schedules WHERE id = $1", schedule_id)
        if result == "DELETE 0":
            raise HTTPException(status_code=404, detail="Schedule not found")
    return {"status": "deleted"}


@router.post("")
async def create_source(source: SourceCreate, request: Request, current_user: dict = Depends(get_current_user)):
    """Create a new source"""
    db_pool = request.app.state.db_pool

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
            INSERT INTO sources(source_type, source_uri, display_name, sync_enabled, config, owner_user_id)
            VALUES ($1, $2, $3, $4, $5, $6)
            RETURNING id
        """, source.source_type, source.source_uri, source.display_name,
            source.sync_enabled, source.config, current_user["id"])

    return {"id": source_id}


@router.get("/{source_id}")
async def get_source(source_id: int, request: Request, current_user: dict = Depends(get_current_user)):
    """Get source details"""
    db_pool = request.app.state.db_pool

    async with db_pool.acquire() as conn:
        if current_user.get("role") == "admin":
            source = await conn.fetchrow(
                "SELECT * FROM sources WHERE id = $1",
                source_id
            )
        else:
            source = await conn.fetchrow(
                "SELECT * FROM sources WHERE id = $1 AND owner_user_id = $2",
                source_id, current_user["id"]
            )

        if not source:
            raise HTTPException(status_code=404, detail="Source not found")

    return dict(source)


@router.get("")
async def list_sources(request: Request, current_user: dict = Depends(get_current_user)):
    """List all sources"""
    db_pool = request.app.state.db_pool

    async with db_pool.acquire() as conn:
        if current_user.get("role") == "admin":
            sources = await conn.fetch(
                "SELECT * FROM sources ORDER BY display_name"
            )
        else:
            sources = await conn.fetch(
                "SELECT * FROM sources WHERE owner_user_id = $1 ORDER BY display_name",
                current_user["id"]
            )

    return [dict(s) for s in sources]


@router.patch("/{source_id}")
async def update_source(source_id: int, update: SourceUpdate, request: Request, current_user: dict = Depends(get_current_user)):
    """Update source"""
    db_pool = request.app.state.db_pool

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

    # Scoping
    if current_user.get("role") == "admin":
        where_clause = f"id = ${param_idx}"
        values.append(source_id)
    else:
        where_clause = f"id = ${param_idx} AND owner_user_id = ${param_idx+1}"
        values.extend([source_id, current_user["id"]])
    query = f"UPDATE sources SET {', '.join(updates)} WHERE {where_clause}"

    async with db_pool.acquire() as conn:
        result = await conn.execute(query, *values)

        if result == "UPDATE 0":
            raise HTTPException(status_code=404, detail="Source not found")

    return {"status": "updated"}


@router.delete("/{source_id}")
async def delete_source(source_id: int, request: Request, current_user: dict = Depends(get_current_user)):
    """Delete source"""
    db_pool = request.app.state.db_pool

    async with db_pool.acquire() as conn:
        if current_user.get("role") == "admin":
            result = await conn.execute(
                "DELETE FROM sources WHERE id = $1",
                source_id
            )
        else:
            result = await conn.execute(
                "DELETE FROM sources WHERE id = $1 AND owner_user_id = $2",
                source_id, current_user["id"]
            )

        if result == "DELETE 0":
            raise HTTPException(status_code=404, detail="Source not found")

    return {"status": "deleted"}
