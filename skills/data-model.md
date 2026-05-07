***
skill: data-model
version: 1
repo: netrunner
language: go
tags: [database, gorm, migrations, schema]
***

# Skill: Data Model Operations

## Purpose
Safely evolve NetRunner's persistence layer across GORM models and SQL migration files.

## Prerequisites
- Understand target entity relationships in `backend/internal/database/models.go`.
- Test environment with SQLite and/or PostgreSQL access.

## Core Concepts
- Runtime migration is centralized in `database.Migrate(db)`.
- PostgreSQL path includes enum-to-text compatibility conversion logic.
- SQL bootstrap/migrations live under `ops/db/init/`.

## Step-by-Step Procedures
1. Update or add model fields in `backend/internal/database/models.go`.
2. If data transformation is needed, add SQL migration in `ops/db/init/migrations/`.
3. Ensure `database.Migrate(db)` remains idempotent for both fresh and existing DBs.
4. Validate migration path:
```bash
cd backend
go test ./internal/database -v
go test ./cmd/worker -v
```
5. Validate feature path that consumes changed model(s).

## Code Patterns
UUID initialization pattern:
```go
func (m *Library) BeforeCreate(tx *gorm.DB) error {
    if m.ID == uuid.Nil {
        m.ID = uuid.New()
    }
    return nil
}
```

## Validation
- `AutoMigrate` succeeds with no fatal errors.
- Existing records remain queryable after migration.
- New fields read/write correctly through handlers/services.

## Edge Cases & Error Handling
- For PostgreSQL enum changes, review existing `migrate.go` cast/rename logic before adding new enum types.
- Keep migrations idempotent (`IF EXISTS`, `IF NOT EXISTS`) where possible.
- Avoid raw SQL in services when model-driven GORM queries are sufficient.

## References
- `backend/internal/database/models.go`
- `backend/internal/database/migrate.go`
- `backend/internal/database/connection.go`
- `ops/db/init/01-schema.sql`
- `ops/db/init/migrations/`