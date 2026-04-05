# djinn-netrunner Agent Rules

## Project overview
djinn-netrunner is a multi-agent AI CLI / agentic coding platform.
Stack: FastAPI, Python 3.12+, PostgreSQL, Redis, Docker/Compose, HTMX/Jinja2, JWT+RBAC, self-host-ready.

## Module ownership
- `api/` — FastAPI routes and OpenAPI schemas
- `agents/` — Agent definitions, skills, MCP wiring
- `db/` — SQLAlchemy models + Alembic migrations
- `auth/` — JWT issuance, RBAC middleware
- `frontend/` — HTMX templates + Jinja2 layouts
- `infra/` — Docker Compose, Nginx, Redis configs

## Agent role assignments
- **build** → feature implementation, routes, migrations
- **plan** → RFC writing, schema design, API contracts
- **review** → security audit, RBAC/JWT checks, PR feedback
- **infra** → Docker, Postgres, Redis, deployment
- **netrunner** → research, library eval, CVE lookup

## Coding standards
- All endpoints require JWT auth unless marked `@public`
- RBAC: roles defined in `auth/roles.py`; never hardcode role names in routes
- Migrations: always use Alembic; never ALTER TABLE manually in prod
- All secrets via `{env:VAR}` or `.env` (never committed)
- Tests in `tests/`; run `pytest` before any PR

## Security
- Validate all inputs with Pydantic v2 models
- Rate-limit all public endpoints via Redis
- Audit log all state-changing operations
