"""
Djinn NETRUNNER ops-worker service
Async job orchestration with fairness scheduling, heartbeats, and reaper
"""
import asyncio
import asyncpg
import httpx
import json
import hashlib
from typing import Optional, List, Dict
from datetime import datetime, timedelta
from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    database_url: str
    slskd_url: str
    slskd_api_key: str
    download_staging: str
    music_library: str
    worker_id: str = "worker-1"

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
            print(f"[WORKER] Wakeup: {event}")
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
                await asyncio.sleep(0.1)

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
                        await self.process_item(job_id, dict(item))
                else:
                    # No more items, finish job
                    await self.finish_job(job_id)

        except Exception as e:
            await self.log_job(job_id, "ERR", f"Error processing item: {e}")
            print(f"[WORKER] Error processing job {job_id}: {e}")

    async def process_item(self, job_id: int, item: dict):
        """Process a single job item"""
        item_id = item['id']

        try:
            await self.log_job(
                job_id,
                "INFO",
                f"Processing: {item['normalized_query']}",
                item_id
            )

            # Simulate search/download workflow
            # In production, this would interact with slskd API

            # Update item status
            async with self.db_pool.acquire() as conn:
                await conn.execute("""
                    UPDATE jobitems
                    SET status = 'imported', finished_at = NOW()
                    WHERE id = $1
                """, item_id)

            await self.log_job(
                job_id,
                "OK",
                f"Completed: {item['normalized_query']}",
                item_id
            )

        except Exception as e:
            async with self.db_pool.acquire() as conn:
                await conn.execute("""
                    UPDATE jobitems
                    SET status = 'failed',
                        finished_at = NOW(),
                        failure_reason = $2
                    WHERE id = $1
                """, item_id, str(e))

            await self.log_job(job_id, "ERR", f"Failed: {str(e)}", item_id)

    async def finish_job(self, job_id: int):
        """Finish a job and release locks"""
        try:
            async with self.db_pool.acquire() as conn:
                # Check if all items are done
                pending = await conn.fetchval("""
                    SELECT COUNT(*)
                    FROM jobitems
                    WHERE job_id = $1 AND status NOT IN ('imported', 'skipped', 'failed')
                """, job_id)

                if pending == 0:
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

                    # Determine final state
                    final_state = 'succeeded' if stats['failed'] == 0 else 'failed'

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

        except Exception as e:
            print(f"[WORKER] Error finishing job {job_id}: {e}")

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
                        lock_key = await self.get_scope_lock_key(
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
