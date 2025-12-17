"""
Djinn NETRUNNER ops-web service
FastAPI application with HTMX templates and WebSocket console streaming
"""
from contextlib import asynccontextmanager
from fastapi import FastAPI, Request, WebSocket, WebSocketDisconnect, HTTPException
from fastapi.responses import HTMLResponse, JSONResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
import asyncpg
import asyncio
import json
from typing import Optional, List
from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    database_url: str
    slskd_url: str
    slskd_api_key: str
    gonic_url: str

    class Config:
        env_file = ".env"


settings = Settings()

# WebSocket connection manager
class ConnectionManager:
    def __init__(self):
        self.active_connections: dict[int, List[WebSocket]] = {}

    async def connect(self, job_id: int, websocket: WebSocket):
        await websocket.accept()
        if job_id not in self.active_connections:
            self.active_connections[job_id] = []
        self.active_connections[job_id].append(websocket)

    def disconnect(self, job_id: int, websocket: WebSocket):
        if job_id in self.active_connections:
            self.active_connections[job_id].remove(websocket)
            if not self.active_connections[job_id]:
                del self.active_connections[job_id]

    async def broadcast_to_job(self, job_id: int, message: str):
        """Broadcast message to all WebSocket clients attached to a job"""
        if job_id in self.active_connections:
            dead_connections = []
            for connection in self.active_connections[job_id]:
                try:
                    await connection.send_text(message)
                except:
                    dead_connections.append(connection)

            # Clean up dead connections
            for conn in dead_connections:
                self.disconnect(job_id, conn)


manager = ConnectionManager()

# Database connection pool and NOTIFY listener
db_pool: Optional[asyncpg.Pool] = None
notify_task: Optional[asyncio.Task] = None


async def notify_listener():
    """Background task that listens for PostgreSQL NOTIFY events and fans out to WebSocket clients"""
    conn = await asyncpg.connect(settings.database_url)

    async def handle_notification(connection, pid, channel, payload):
        try:
            event = json.loads(payload)
            if event.get('event') == 'job_log':
                job_id = event['job_id']
                log_id = event['log_id']

                # Fetch the log entry
                log = await db_pool.fetchrow(
                    "SELECT id, ts, level, message FROM joblogs WHERE id = $1",
                    log_id
                )

                if log:
                    # Format log line for console
                    log_html = f'<div class="log-line log-{log["level"].lower()}" data-log-id="{log["id"]}">' \
                               f'<span class="log-ts">{log["ts"].isoformat()}</span> ' \
                               f'<span class="log-level">[{log["level"]}]</span> ' \
                               f'<span class="log-msg">{log["message"]}</span>' \
                               f'</div>'

                    await manager.broadcast_to_job(job_id, log_html)
        except Exception as e:
            print(f"Error handling notification: {e}")

    await conn.add_listener('opsevents', handle_notification)

    try:
        # Keep connection alive
        while True:
            await asyncio.sleep(1)
    finally:
        await conn.close()


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Startup and shutdown lifecycle"""
    global db_pool, notify_task

    # Create database pool
    db_pool = await asyncpg.create_pool(settings.database_url, min_size=2, max_size=10)

    # Store in app state for dependency injection
    app.state.db_pool = db_pool

    # Start NOTIFY listener
    notify_task = asyncio.create_task(notify_listener())

    yield

    # Cleanup
    if notify_task:
        notify_task.cancel()
        try:
            await notify_task
        except asyncio.CancelledError:
            pass

    if db_pool:
        await db_pool.close()


app = FastAPI(lifespan=lifespan)

# Include source management routes
from source_manager import router as source_router
app.include_router(source_router)

# Templates and static files
templates = Jinja2Templates(directory="templates")
app.mount("/static", StaticFiles(directory="static"), name="static")


@app.get("/", response_class=HTMLResponse)
async def index(request: Request):
    """Main dashboard page"""
    async with db_pool.acquire() as conn:
        # Get job stats
        stats = await conn.fetchrow("""
            SELECT
                COUNT(*) FILTER (WHERE state = 'queued') as queued_count,
                COUNT(*) FILTER (WHERE state = 'running') as running_count,
                COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded_count,
                COUNT(*) FILTER (WHERE state = 'failed') as failed_count
            FROM jobs
            WHERE requested_at > NOW() - INTERVAL '24 hours'
        """)

        # Get recent jobs
        jobs = await conn.fetch("""
            SELECT id, jobtype, state, requested_at, started_at, finished_at, summary
            FROM jobs
            ORDER BY requested_at DESC
            LIMIT 20
        """)

        # Get sources
        sources = await conn.fetch("""
            SELECT id, source_type, display_name, last_synced_at, sync_enabled
            FROM sources
            ORDER BY display_name
        """)

    return templates.TemplateResponse("index.html", {
        "request": request,
        "stats": dict(stats),
        "jobs": [dict(j) for j in jobs],
        "sources": [dict(s) for s in sources]
    })


@app.get("/jobs/{job_id}", response_class=HTMLResponse)
async def job_detail(request: Request, job_id: int):
    """Job detail page"""
    async with db_pool.acquire() as conn:
        job = await conn.fetchrow("SELECT * FROM jobs WHERE id = $1", job_id)
        if not job:
            raise HTTPException(status_code=404, detail="Job not found")

        items = await conn.fetch("""
            SELECT * FROM jobitems WHERE job_id = $1 ORDER BY sequence
        """, job_id)

    return templates.TemplateResponse("job_detail.html", {
        "request": request,
        "job": dict(job),
        "items": [dict(i) for i in items]
    })


@app.websocket("/ws/jobs/{job_id}")
async def websocket_console(websocket: WebSocket, job_id: int, tail: Optional[int] = None, since_id: Optional[int] = None):
    """WebSocket endpoint for console log streaming"""
    await manager.connect(job_id, websocket)

    try:
        async with db_pool.acquire() as conn:
            # Send initial backlog based on attach mode
            if tail is not None:
                # STARTED mode: send last N lines
                logs = await conn.fetch("""
                    SELECT id, ts, level, message
                    FROM joblogs
                    WHERE job_id = $1
                    ORDER BY id DESC
                    LIMIT $2
                """, job_id, tail)
                logs = reversed(logs)
            elif since_id is not None:
                # ATTACHED mode: send lines after since_id
                logs = await conn.fetch("""
                    SELECT id, ts, level, message
                    FROM joblogs
                    WHERE job_id = $1 AND id > $2
                    ORDER BY id ASC
                """, job_id, since_id)
            else:
                # Default: small backlog
                logs = await conn.fetch("""
                    SELECT id, ts, level, message
                    FROM joblogs
                    WHERE job_id = $1
                    ORDER BY id DESC
                    LIMIT 50
                """, job_id)
                logs = reversed(logs)

            # Send initial logs
            for log in logs:
                log_html = f'<div class="log-line log-{log["level"].lower()}" data-log-id="{log["id"]}">' \
                           f'<span class="log-ts">{log["ts"].isoformat()}</span> ' \
                           f'<span class="log-level">[{log["level"]}]</span> ' \
                           f'<span class="log-msg">{log["message"]}</span>' \
                           f'</div>'
                await websocket.send_text(log_html)

        # Keep connection alive (new logs sent via NOTIFY fanout)
        while True:
            # Receive any client messages (e.g., ping)
            await websocket.receive_text()

    except WebSocketDisconnect:
        manager.disconnect(job_id, websocket)


@app.post("/api/jobs/sync")
async def create_sync_job(source_id: int):
    """Create a new sync job for a source"""
    async with db_pool.acquire() as conn:
        # Get source
        source = await conn.fetchrow("SELECT * FROM sources WHERE id = $1", source_id)
        if not source:
            raise HTTPException(status_code=404, detail="Source not found")

        # Create job
        job_id = await conn.fetchval("""
            INSERT INTO jobs(jobtype, scope_type, scope_id, params)
            VALUES ('sync', $1, $2, $3)
            RETURNING id
        """, source['source_type'], str(source_id), json.dumps({"source_uri": source['source_uri']}))

    return {"job_id": job_id}


@app.post("/api/jobs/{job_id}/cancel")
async def cancel_job(job_id: int):
    """Cancel a running or queued job"""
    async with db_pool.acquire() as conn:
        updated = await conn.execute("""
            UPDATE jobs
            SET state = 'cancelled', finished_at = NOW()
            WHERE id = $1 AND state IN ('queued', 'running')
        """, job_id)

        if updated == "UPDATE 0":
            raise HTTPException(status_code=404, detail="Job not found or cannot be cancelled")

    return {"status": "cancelled"}


@app.get("/health")
async def health():
    """Health check endpoint"""
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
