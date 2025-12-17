"""
Djinn NETRUNNER ops-worker service (v2 with full job handlers)
Async job orchestration with fairness scheduling, heartbeats, and reaper
"""
import asyncio
import asyncpg
import json
from pathlib import Path
from typing import Optional, List, Dict
from pydantic_settings import BaseSettings
from datetime import datetime, timezone
from croniter import croniter

from slskd_client import SlskdClient
from gonic_client import GonicClient
from import_pipeline import ImportPipeline, MetadataEnricher
from job_handlers import (
    SyncJobHandler,
    AcquisitionJobHandler,
    IndexRefreshJobHandler,
    MetadataEnrichmentJobHandler
)


class Settings(BaseSettings):
    database_url: str
    slskd_url: str
    slskd_api_key: str
    download_staging: str
    music_library: str
    worker_id: str = "worker-1"
    gonic_url: str = "http://gonic:4747"
    gonic_username: str = "admin"
    gonic_password: str = "admin"

    class Config:
        env_file = ".env"


settings = Settings()


class WorkerOrchestrator:
    """Main worker orchestrator with round-robin fairness scheduling"""

    def __init__(self):
        self.worker_id = settings.worker_id
        self.db_pool: Optional[asyncpg.Pool] = None
        self.notify_conn: Optional[asyncpg.Connection] = None
        self.lock_conn: Optional[asyncpg.Connection] = None
        self.active_jobs: Dict[int, dict] = {}
        self.running = False
        self.scheduler_task: Optional[asyncio.Task] = None

        # External service clients
        self.slskd_client: Optional[SlskdClient] = None
        self.gonic_client: Optional[GonicClient] = None
        self.import_pipeline: Optional[ImportPipeline] = None
        self.metadata_enricher: Optional[MetadataEnricher] = None

        # Job handlers
        self.handlers: Dict[str, any] = {}

    async def start(self):
        """Initialize connections and start worker loop"""
        print(f"[WORKER] Starting worker {self.worker_id}")

        # Create database pool for short transactions
        self.db_pool = await asyncpg.create_pool(
            settings.database_url,
            min_size=2,
            max_size=10
        )

        # Create dedicated notify connection (autocommit)
        self.notify_conn = await asyncpg.connect(settings.database_url)
        await self.notify_conn.add_listener('opswakeup', self.handle_wakeup)

        # Create dedicated lock connection (autocommit, holds advisory locks)
        self.lock_conn = await asyncpg.connect(settings.database_url)

        # Initialize external service clients
        self.slskd_client = SlskdClient(settings.slskd_url, settings.slskd_api_key)
        self.gonic_client = GonicClient(
            settings.gonic_url,
            settings.gonic_username,
            settings.gonic_password
        )
        self.import_pipeline = ImportPipeline(
            Path(settings.download_staging),
            Path(settings.music_library)
        )
        self.metadata_enricher = MetadataEnricher()

        # Initialize job handlers
        self.handlers = {
            'sync': SyncJobHandler(
                self.db_pool,
                self.slskd_client,
                self.gonic_client,
                self.import_pipeline,
                self.metadata_enricher
            ),
            'acquisition': AcquisitionJobHandler(
                self.db_pool,
                self.slskd_client,
                self.gonic_client,
                self.import_pipeline,
                self.metadata_enricher
            ),
            'index_refresh': IndexRefreshJobHandler(
                self.db_pool,
                self.slskd_client,
                self.gonic_client,
                self.import_pipeline,
                self.metadata_enricher
            ),
            'import': MetadataEnrichmentJobHandler(
                self.db_pool,
                self.slskd_client,
                self.gonic_client,
                self.import_pipeline,
                self.metadata_enricher
            )
        }

        # Start background scheduler
        self.scheduler_task = asyncio.create_task(self.scheduler_loop())

        # Health checks
        print("[WORKER] Running health checks...")
        slskd_ok = await self.slskd_client.health_check()
        gonic_ok = await self.gonic_client.health_check()
        print(f"[WORKER] slskd: {'✓' if slskd_ok else '✗'}")
        print(f"[WORKER] Gonic: {'✓' if gonic_ok else '✗'}")

        self.running = True

        # Start background tasks
        await asyncio.gather(
            self.job_loop(),
            self.heartbeat_loop(),
            self.reaper_loop()
        )

    async def stop(self):
        """Cleanup connections"""
        self.running = False

        if self.slskd_client:
            await self.slskd_client.close()

        if self.gonic_client:
            await self.gonic_client.close()

        if self.notify_conn:
            await self.notify_conn.close()

        if self.lock_conn:
            await self.lock_conn.close()

        if self.db_pool:
            await self.db_pool.close()

    async def handle_wakeup(self, conn, pid, channel, payload):
        """Handle NOTIFY wakeup events"""
        try:
            event = json.loads(payload)
            print(f"[WORKER] Wakeup: {event.get('event')}")
        except Exception as e:
            print(f"[WORKER] Error handling wakeup: {e}")

    async def job_loop(self):
        """Main job processing loop with round-robin fairness"""
        print("[WORKER] Job loop started")

        while self.running:
            try:
                # Claim next available job if we have capacity
                if len(self.active_jobs) < 5:  # Max 5 concurrent jobs
                    await self.claim_next_job()

                # Process one item from each active job (round-robin)
                if self.active_jobs:
                    for job_id in list(self.active_jobs.keys()):
                        await self.process_next_item(job_id)

                # Sleep briefly between rounds
                await asyncio.sleep(0.5)

            except Exception as e:
                print(f"[WORKER] Error in job loop: {e}")
                await asyncio.sleep(5)

    async def claim_next_job(self):
        """Claim next available job using SKIP LOCKED pattern"""
        async with self.db_pool.acquire() as conn:
            job_id = await conn.fetchval(
                "SELECT claim_next_job($1)",
                self.worker_id
            )

            if job_id:
                # Get job details
                job = await conn.fetchrow(
                    "SELECT * FROM jobs WHERE id = $1",
                    job_id
                )

                if job:
                    # Acquire advisory lock for scope
                    lock_key = await self.get_scope_lock_key(
                        job['scope_type'],
                        job['scope_id']
                    )

                    # Try to acquire lock on lock_conn
                    acquired = await self.lock_conn.fetchval(
                        "SELECT pg_try_advisory_lock($1)",
                        lock_key
                    )

                    if acquired:
                        self.active_jobs[job_id] = {
                            'job': dict(job),
                            'lock_key': lock_key
                        }

                        await self.log_job(job_id, "OK", f"Job claimed by {self.worker_id}")
                        print(f"[WORKER] Claimed job {job_id} ({job['jobtype']})")
                    else:
                        # Could not acquire lock, requeue job
                        await conn.execute("""
                            UPDATE jobs
                            SET state = 'queued', worker_id = NULL, started_at = NULL
                            WHERE id = $1
                        """, job_id)
                        await self.log_job(job_id, "INFO", "Scope locked, requeueing")

    async def process_next_item(self, job_id: int):
        """Process next item for a job"""
        try:
            job_data = self.active_jobs[job_id]['job']
            job_type = job_data['jobtype']

            # Get handler for job type
            handler = self.handlers.get(job_type)

            if not handler:
                await self.log_job(job_id, "ERR", f"No handler for job type: {job_type}")
                await self.finish_job(job_id, failed=True)
                return

            # For sync, index_refresh, import jobs: execute once
            if job_type in ['sync', 'index_refresh', 'import']:
                await handler.execute(job_id, job_data)
                await self.finish_job(job_id)
                return

            # For acquisition jobs: process items one by one
            if job_type == 'acquisition':
                async with self.db_pool.acquire() as conn:
                    # Claim next item
                    item_id = await conn.fetchval(
                        "SELECT claim_next_jobitem($1)",
                        job_id
                    )

                    if item_id:
                        item = await conn.fetchrow(
                            "SELECT * FROM jobitems WHERE id = $1",
                            item_id
                        )

                        if item:
                            await handler.execute_item(job_id, dict(item))
                    else:
                        # No more items, finish job
                        await self.finish_job(job_id)

        except Exception as e:
            await self.log_job(job_id, "ERR", f"Error processing job: {e}")
            print(f"[WORKER] Error processing job {job_id}: {e}")
            import traceback
            traceback.print_exc()

    async def scheduler_loop(self):
        """Background loop that enqueues sync jobs based on schedules table."""
        await asyncio.sleep(1.0)  # give connections time to initialize
        print("[WORKER] Scheduler loop started")
        while True:
            try:
                now = datetime.now(timezone.utc)
                async with self.db_pool.acquire() as conn:
                    # Initialize next_run_at if NULL
                    rows = await conn.fetch(
                        """
                        UPDATE schedules s
                        SET next_run_at = CASE
                            WHEN next_run_at IS NULL THEN NOW()
                            ELSE next_run_at
                        END,
                            updated_at = NOW()
                        WHERE enabled = TRUE AND (next_run_at IS NULL)
                        RETURNING id
                        """
                    )
                    if rows:
                        print(f"[WORKER] Initialized next_run_at for {len(rows)} schedules")

                    # Find due schedules (use SKIP LOCKED pattern)
                    due = await conn.fetch(
                        """
                        SELECT id, source_id, cron_expr, timezone, next_run_at
                        FROM schedules
                        WHERE enabled = TRUE AND next_run_at <= NOW()
                        ORDER BY next_run_at ASC
                        LIMIT 50
                        """
                    )

                    for s in due:
                        sid = s['id']
                        tz = s['timezone'] or 'UTC'
                        expr = s['cron_expr']
                        # Compute next occurrence safely
                        try:
                            base = now
                            itr = croniter(expr, base)
                            nxt = itr.get_next(datetime)
                        except Exception:
                            # Disable invalid schedule to avoid tight loop
                            await conn.execute("UPDATE schedules SET enabled = FALSE, updated_at = NOW() WHERE id = $1", sid)
                            await self._notify(f"schedule_invalid:{sid}")
                            continue

                        # Enqueue sync job for the source
                        job_id = await conn.fetchval(
                            """
                            INSERT INTO jobs(jobtype, state, scope_type, scope_id, requested_at, owner_user_id)
                            VALUES ('sync', 'queued', 'source', $1, NOW(), (SELECT owner_user_id FROM sources WHERE id = $1))
                            RETURNING id
                            """,
                            str(s['source_id'])
                        )

                        # Update schedule times
                        await conn.execute(
                            "UPDATE schedules SET last_run_at = NOW(), next_run_at = $2, updated_at = NOW() WHERE id = $1",
                            sid, nxt
                        )

                        # Wake worker(s)
                        await self._notify("opswakeup")
                        print(f"[WORKER] Scheduled sync job {job_id} for source {s['source_id']} (next at {nxt})")

            except Exception as e:
                print(f"[WORKER] Scheduler error: {e}")

            # Sleep between ticks
            await asyncio.sleep(30)

    async def _notify(self, channel: str):
        try:
            if self.notify_conn:
                await self.notify_conn.execute(f"NOTIFY {channel}")
        except Exception:
            pass

    async def finish_job(self, job_id: int, failed: bool = False):
        """Finish a job and release locks"""
        try:
            async with self.db_pool.acquire() as conn:
                job_data = self.active_jobs[job_id]['job']
                job_type = job_data['jobtype']

                # For jobs with items, check completion
                if job_type == 'acquisition':
                    # Check if all items are done
                    pending = await conn.fetchval("""
                        SELECT COUNT(*)
                        FROM jobitems
                        WHERE job_id = $1 AND status NOT IN ('imported', 'skipped', 'failed')
                    """, job_id)

                    if pending > 0:
                        # Still has pending items, don't finish yet
                        return

                    # Get counts
                    stats = await conn.fetchrow("""
                        SELECT
                            COUNT(*) FILTER (WHERE status = 'imported') as imported,
                            COUNT(*) FILTER (WHERE status = 'skipped') as skipped,
                            COUNT(*) FILTER (WHERE status = 'failed') as failed
                        FROM jobitems
                        WHERE job_id = $1
                    """, job_id)

                    summary = f"Imported: {stats['imported']}, Skipped: {stats['skipped']}, Failed: {stats['failed']}"
                    final_state = 'succeeded' if stats['failed'] == 0 else 'failed'

                else:
                    # Jobs without items
                    summary = "Completed"
                    final_state = 'failed' if failed else 'succeeded'

                await conn.execute("""
                    UPDATE jobs
                    SET state = $2,
                        finished_at = NOW(),
                        summary = $3
                    WHERE id = $1
                """, job_id, final_state, summary)

                await self.log_job(job_id, "OK", f"Job finished: {summary}")

                # Release advisory lock
                if job_id in self.active_jobs:
                    lock_key = self.active_jobs[job_id]['lock_key']
                    await self.lock_conn.fetchval(
                        "SELECT pg_advisory_unlock($1)",
                        lock_key
                    )
                    del self.active_jobs[job_id]

                print(f"[WORKER] Finished job {job_id}: {final_state}")

                # For acquisition jobs, trigger index refresh
                if job_type == 'acquisition' and final_state == 'succeeded':
                    await self._queue_index_refresh(conn)

        except Exception as e:
            print(f"[WORKER] Error finishing job {job_id}: {e}")

    async def _queue_index_refresh(self, conn):
        """Queue an index refresh job after successful acquisition"""
        await conn.execute("""
            INSERT INTO jobs(jobtype, scope_type, scope_id)
            VALUES ('index_refresh', 'library', 'main')
        """)

    async def heartbeat_loop(self):
        """Periodically update heartbeat for running jobs"""
        print("[WORKER] Heartbeat loop started")

        while self.running:
            try:
                if self.active_jobs:
                    job_ids = list(self.active_jobs.keys())
                    async with self.db_pool.acquire() as conn:
                        await conn.execute("""
                            UPDATE jobs
                            SET heartbeat_at = NOW()
                            WHERE id = ANY($1)
                        """, job_ids)

                await asyncio.sleep(5)  # Heartbeat every 5 seconds

            except Exception as e:
                print(f"[WORKER] Error in heartbeat loop: {e}")
                await asyncio.sleep(5)

    async def reaper_loop(self):
        """Detect and requeue stale jobs (short-lived connection)"""
        print("[WORKER] Reaper loop started")

        while self.running:
            try:
                await asyncio.sleep(60)  # Run every 60 seconds

                # Use short-lived maintenance connection
                maint_conn = await asyncpg.connect(settings.database_url)

                try:
                    # Find stale jobs
                    stale_jobs = await maint_conn.fetch("""
                        SELECT id, scope_type, scope_id, attempt, max_attempts
                        FROM jobs
                        WHERE state = 'running'
                          AND heartbeat_at < NOW() - INTERVAL '10 minutes'
                    """)

                    for job in stale_jobs:
                        job_id = job['id']

                        # Check if scope lock is still held
                        lock_key = await self.get_scope_lock_key_direct(
                            maint_conn,
                            job['scope_type'],
                            job['scope_id']
                        )

                        # Try to acquire lock to verify it's not held
                        acquired = await maint_conn.fetchval(
                            "SELECT pg_try_advisory_lock($1)",
                            lock_key
                        )

                        if acquired:
                            # Lock not held, safe to requeue
                            await maint_conn.fetchval(
                                "SELECT pg_advisory_unlock($1)",
                                lock_key
                            )

                            if job['attempt'] < job['max_attempts']:
                                # Requeue
                                await maint_conn.execute("""
                                    UPDATE jobs
                                    SET state = 'queued',
                                        worker_id = NULL,
                                        started_at = NULL,
                                        heartbeat_at = NULL
                                    WHERE id = $1
                                """, job_id)

                                # Reset in-flight items
                                await maint_conn.execute("""
                                    UPDATE jobitems
                                    SET status = 'queued', started_at = NULL
                                    WHERE job_id = $1
                                      AND status IN ('searching', 'downloading')
                                """, job_id)

                                print(f"[REAPER] Requeued stale job {job_id}")
                            else:
                                # Max attempts reached, fail
                                await maint_conn.execute("""
                                    UPDATE jobs
                                    SET state = 'failed',
                                        finished_at = NOW(),
                                        summary = 'Max retries exceeded'
                                    WHERE id = $1
                                """, job_id)

                                print(f"[REAPER] Failed stale job {job_id} (max attempts)")

                finally:
                    await maint_conn.close()

            except Exception as e:
                print(f"[REAPER] Error in reaper loop: {e}")
                await asyncio.sleep(60)

    async def get_scope_lock_key(self, scope_type: str, scope_id: str) -> int:
        """Compute advisory lock key for scope"""
        async with self.db_pool.acquire() as conn:
            return await conn.fetchval(
                "SELECT scope_lock_key($1, $2)",
                scope_type, scope_id
            )

    async def get_scope_lock_key_direct(self, conn, scope_type: str, scope_id: str) -> int:
        """Compute advisory lock key for scope using provided connection"""
        return await conn.fetchval(
            "SELECT scope_lock_key($1, $2)",
            scope_type, scope_id
        )

    async def log_job(self, job_id: int, level: str, message: str, item_id: Optional[int] = None):
        """Append log to job"""
        async with self.db_pool.acquire() as conn:
            await conn.fetchval(
                "SELECT append_job_log($1, $2, $3, $4)",
                job_id, level, message, item_id
            )


async def main():
    worker = WorkerOrchestrator()

    try:
        await worker.start()
    except KeyboardInterrupt:
        print("[WORKER] Shutting down...")
    finally:
        await worker.stop()


if __name__ == "__main__":
    asyncio.run(main())
