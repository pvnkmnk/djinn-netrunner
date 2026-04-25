# backend/cmd/test_sqlite/

## Responsibility
Standalone utility for testing SQLite connectivity and the LockManager's advisory locking functionality. Used during development to verify the database layer works correctly before running full integration tests.

## Design
- **Entry Point**: `main.go` - Minimal single-file utility
- **Pattern**: Creates test database file, connects, migrates schema, tests lock acquisition/release
- **No Framework**: Direct database operations
- **Cleanup**: Removes test database file on exit via `defer`

## Flow
1. Create test database config (`test_standalone.db`)
2. Connect to SQLite via `database.Connect(cfg)`
3. Run migrations via `database.Migrate(db)`
4. Create LockManager instance
5. Acquire a scope lock (key: "artist", "test-123")
6. Release the lock
7. Log success/failure at each step

## Integration
- **Depends On**: `internal/config`, `internal/database`
- **External**: SQLite database file
- **What Uses It**: Developers testing database connectivity; CI/CD verification
