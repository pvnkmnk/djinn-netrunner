"""
Authentication utilities for ops-web
Session-based auth with cookies backed by the database.
"""
import uuid
from datetime import datetime, timedelta, timezone
from typing import Optional

from fastapi import Request, HTTPException


SESSION_COOKIE = "session_id"
SESSION_TTL_DAYS = 7


async def get_current_user(request: Request) -> dict:
    """Require an authenticated user; raise 401 if missing/expired."""
    user = await _resolve_user_from_cookie(request)
    if not user:
        raise HTTPException(status_code=401, detail="Not authenticated")
    return user


async def get_current_user_optional(request: Request) -> Optional[dict]:
    """Return user dict if authenticated, else None."""
    return await _resolve_user_from_cookie(request)


async def _resolve_user_from_cookie(request: Request) -> Optional[dict]:
    session_id = request.cookies.get(SESSION_COOKIE)
    if not session_id:
        return None

    db_pool = request.app.state.db_pool
    async with db_pool.acquire() as conn:
        row = await conn.fetchrow(
            """
            SELECT u.* FROM sessions s
            JOIN users u ON u.id = s.user_id
            WHERE s.session_id = $1 AND s.expires_at > NOW()
            """,
            session_id
        )
        return dict(row) if row else None


async def login_user(request: Request, email: str, password: str) -> str:
    """Validate credentials and create a new session. Returns session_id."""
    from passlib.hash import bcrypt

    db_pool = request.app.state.db_pool
    async with db_pool.acquire() as conn:
        user = await conn.fetchrow("SELECT * FROM users WHERE email = $1", email)
        if not user:
            raise HTTPException(status_code=401, detail="Invalid credentials")
        if not bcrypt.verify(password, user["password_hash"]):
            raise HTTPException(status_code=401, detail="Invalid credentials")

        # Create session
        session_id = uuid.uuid4().hex
        expires_at = datetime.now(timezone.utc) + timedelta(days=SESSION_TTL_DAYS)
        await conn.execute(
            """
            INSERT INTO sessions(session_id, user_id, expires_at, ip, user_agent)
            VALUES ($1, $2, $3, $4, $5)
            """,
            session_id,
            user["id"],
            expires_at,
            request.client.host if request.client else None,
            request.headers.get("user-agent")
        )
        await conn.execute("UPDATE users SET last_login_at = NOW() WHERE id = $1", user["id"])
        return session_id


async def logout_user(request: Request) -> None:
    session_id = request.cookies.get(SESSION_COOKIE)
    if not session_id:
        return
    db_pool = request.app.state.db_pool
    async with db_pool.acquire() as conn:
        await conn.execute("DELETE FROM sessions WHERE session_id = $1", session_id)


async def register_user(request: Request, email: str, password: str, role: str = "user") -> int:
    """Create a new user; returns user id. Idempotent on email."""
    from passlib.hash import bcrypt
    pwd_hash = bcrypt.hash(password)
    db_pool = request.app.state.db_pool
    async with db_pool.acquire() as conn:
        existing = await conn.fetchrow("SELECT id FROM users WHERE email = $1", email)
        if existing:
            return existing["id"]
        user_id = await conn.fetchval(
            """
            INSERT INTO users(email, password_hash, role)
            VALUES ($1, $2, $3)
            RETURNING id
            """,
            email, pwd_hash, role
        )
        return user_id
