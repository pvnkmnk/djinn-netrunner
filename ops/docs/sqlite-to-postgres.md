# SQLite to PostgreSQL Migration

## When to migrate

- Running multiple worker instances (`MaxConcurrentJobs > 1` with separate processes)
- Need advisory lock support for safe concurrent job claims
- Want `LISTEN/NOTIFY` for instant worker wakeup instead of polling
- Production deployment with multiple users

## Prerequisites

- PostgreSQL 16+ running (use the `docker-compose.yml` postgres service or external instance)
- `pgloader` installed on the migration host: `apt install pgloader` or `brew install pgloader`
- NetRunner services stopped during migration

## Migration Steps

### 1. Stop services

```bash
docker compose stop ops-web ops-worker
```

### 2. Export SQLite data

```bash
# Ensure SQLite database is not being written to
cp netrunner.db netrunner_migration.db
```

### 3. Create target database

If using the Docker Compose postgres service, the database already exists. For external Postgres:

```bash
psql -h <host> -U <user> -c "CREATE DATABASE musicops;"
```

### 4. Run pgloader

Create a migration config file:

```bash
cat > migrate.load <<'EOF'
LOAD DATABASE
  FROM sqlite:///path/to/netrunner_migration.db
  INTO postgresql://musicops:${POSTGRES_PASSWORD}@localhost:5432/musicops

WITH include drop, create tables, create indexes, reset sequences

SET work_mem to '16MB', maintenance_work_mem to '512 MB'

CAST type datetime to timestamptz using zero-dates-to-null,
     type text to varchar drop typemod;
EOF

pgloader migrate.load
```

### 5. Run GORM migrations

Start the server briefly to run AutoMigrate and reconcile any schema differences:

```bash
# Update .env to point to Postgres
# DATABASE_URL=postgresql://musicops:<password>@localhost:5432/musicops?sslmode=disable

cd backend
go run ./cmd/server &
sleep 5
kill %1
```

### 6. Update environment

```bash
# Edit .env
DATABASE_URL=postgresql://musicops:${POSTGRES_PASSWORD}@postgres:5432/musicops?sslmode=disable
```

### 7. Restart services

```bash
docker compose up -d
```

## Validation

```bash
# Check database connectivity
curl http://localhost/api/health

# Verify record counts match
sqlite3 netrunner_migration.db "SELECT COUNT(*) FROM jobs;"
docker compose exec postgres psql -U musicops musicops -c "SELECT COUNT(*) FROM jobs;"

# Verify watchlists migrated
cd backend && go run ./cmd/cli watchlist list

# Verify libraries migrated
cd backend && go run ./cmd/cli library list

# Run a test sync to verify full pipeline
cd backend && go run ./cmd/cli watchlist sync <watchlist-id>
```

## Rollback

If migration fails, revert `DATABASE_URL` in `.env` to the SQLite path and restart:

```bash
# DATABASE_URL=netrunner.db
docker compose up -d
```

The original SQLite database is unmodified (we used a copy for migration).
