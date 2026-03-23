# AGENTS.md — Djinn NETRUNNER

AGENTS.md is the canonical “README for agents” used by agentic IDE workflows (Cursor, Claude Code, etc.).
If instructions conflict, follow .cursor/rules/*.mdc first, then the docs in docs/.

## Project identity
NETRUNNER is a console-first operations UI for a music acquisition pipeline.
Logs are the primary progress visualization (do not replace with progress bars/spinners).

## Non-negotiable constraints
- **Language**: Go 1.25+
- **Architecture**: Single-binary focus with embedded SQLite (CGO-free via `modernc.org/sqlite`).
- **Frontend**: HTMX + server-rendered Fiber templates + Vanilla CSS (No SPA frameworks).
- **Concurrency**: Native goroutines for job workers; round-robin orchestration.
- **Database**: SQLite in WAL mode is the system-of-record.
- **Privacy**: All P2P/API traffic must support SOCKS5/HTTP proxying.

## Key invariants (backend)
- **Persistence**: Use GORM for all database interactions to maintain SQLite/Postgres compatibility.
- **Exclusivity**: Per-scope exclusivity uses file-based or database-level locks (see `internal/database/locks.go`).
- **Jobs**: Job items are created before execution; retries resume and never re-derive state.
- **Real-time**: Events are broadcast via WebSockets from the Fiber API.

## Where to look
- `docs/ARCHITECTURE.md` — locking model, state machines, invariants.
- `docs/UIIMPLEMENTATION.md` — console socket + attach modes + minimal JS contract.
- `docs/RUNBOOK.md` — operational procedures and failure modes.
- `backend/internal/services/` — Core logic for slskd, Gonic, and Watchlists.
- `backend/cmd/agent/` — MCP server implementation.

## How agents should work (practical)
1. **Scout**: Check `internal/database/models.go` before changing schema.
2. **Preserve**: Make the smallest change that preserves architectural invariants.
3. **Document**: Update `docs/` if logic or behavior changes.
4. **Agentic Surface**: Ensure any new core feature is exposed via both the CLI and the MCP server.

## “Do not do” list
- Do not introduce React/Vue/SPA routing.
- Do not split CSS into multiple files.
- Do not add external heavy queues (Redis/RabbitMQ).
- Do not bypass the `WatchlistService` for source management.

## Development workflow

### Prerequisites
- Go 1.25+
- Docker & Docker Compose
- Make (optional, for convenience commands)

### Common commands

#### Building
```bash
# Build server and worker binaries
go build -o netrunner-server ./backend/cmd/server/main.go
go build -o netrunner-worker ./backend/cmd/worker/main.go

# Or use Docker Compose (recommended for development)
docker compose build
docker compose up -d
```

#### Running tests
```bash
# Run all tests
go test ./backend/... -v

# Run tests with coverage
go test ./backend/... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run a specific test package
go test ./backend/internal/services/watchlist_service.go ./backend/internal/services/watchlist_service_test.go -v

# Run a single test function
go test ./backend/... -run TestWatchlistService_CreateWatchlist -v

# Run tests with race detector
go test ./backend/... -race
```

#### Linting and formatting
```bash
# Format code (run before committing)
go fmt ./backend/...

# Import ordering (run before committing)
goimports -local github.com/pvnkmnk/netrunner -w ./backend/

# Vet for suspicious constructs
go vet ./backend/...

# Static analysis (if golangci-lint is installed)
golangci-lint run ./backend/
```

#### Database operations
```bash
# Run migrations
go run ./backend/cmd/server/main.go migrate

# Reset database (development only)
docker compose down -v
docker compose up -d
go run ./backend/cmd/server/main.go migrate
```

#### Docker Compose shortcuts
```bash
# Start all services
docker compose up -d

# Stop all services
docker compose down

# View logs
docker compose logs -f

# View logs for specific service
docker compose logs -f netrunner

# Rebuild and restart
docker compose up -d --build

# Enter container shell
docker compose exec netrunner sh
```

## Code style guidelines

### Imports
- Group imports: standard library, third-party, project internal
- Within each group, sort alphabetically
- Use blank lines between groups
- Never use relative imports in internal packages
- Example:
  ```go
  import (
      "encoding/json"
      "time"

      "github.com/gofiber/fiber/v2"
      "gorm.io/gorm"

      "github.com/pvnkmnk/netrunner/backend/internal/database"
      "github.com/pvnkmnk/netrunner/backend/internal/services"
  )
  ```

### Formatting
- Use `go fmt` as the canonical formatter
- Line length: aim for ≤100 characters, but prioritize readability
- Blank lines: use to separate logical sections within functions
- No trailing whitespace
- Use tabs for indentation (Go standard)

### Types and variables
- Use mixedCaps for exported names, camelCase for unexported
- Prefix interface names with `-er` when single-method (e.g., `Handler`, `Validator`)
- Struct fields: exported when needed externally, otherwise unexported
- Error variables: prefix with `Err` (e.g., `ErrNotFound`)
- Constants: MixedCaps or ALL_CAPS for enum-like values
- Pointer receivers: use when modifying receiver or for large structs (> few fields)

### Naming conventions
- Package names: lowercase, single word, no underscores
- Variable names: descriptive but concise; prefer brevity for small scopes
- Function names: MixedCaps, clear verb-noun pairing when applicable
- Error messages: lowercase, no period unless multiple sentences
- HTTP handlers: suffix with `Handler` or `Page` or `Partial` as appropriate
- Service methods: verb-first (Create, Get, Update, Delete, List)

### Error handling
- Always check errors unless intentionally ignored (with comment)
- Wrap errors with context when propagating upward using `%w` or `fmt.Errorf("...: %w", err)`
- Sentinel errors: declare as `var ErrSomething = errors.New("something")`
- HTTP handlers: return appropriate status codes (4xx for client errors, 5xx for server)
- Log errors at appropriate level (debug/info/warn/error) based on severity
- Never ignore errors from disk/network operations without justification

### Comments
- File comment: package purpose and key invariants
- Function comment: what it does, preconditions, postconditions, return values
- Comment complex logic blocks, not obvious code
- TODO comments: include ticket/reference if applicable
- Use `//` for comments, not `/* */` except for large blocked comments

### HTTP handlers (Fiber)
- Extract request binding/validation to top of handler
- Use `c.Locals()` for request-scoped data (user, db, etc.)
- Return early for error conditions
- Set response headers before writing body when needed
- Use proper content types (application/json, text/html, etc.)
- For HTMX endpoints: check `Htmx-Request` header when behavior differs
- Auth handlers: validate session, return 401/redirect appropriately

### Concurrency
- Prefer channels over mutexes for goroutine communication
- Context cancellation: always respect `ctx.Done()`
- Use `sync.WaitGroup` for waiting on goroutine groups
- Never leak goroutines; ensure clean shutdown paths
- For workers: use round-robin dispatch as per project invariants

### Testing
- Table-driven tests for functions with multiple input/output cases
- Mock external dependencies (services, databases) when testing units
- Test both happy path and error conditions
- Name test functions: `TestWhatItDoes_ExpectedBehavior_Scenario`
- Avoid testing private functions directly; test through public interface
- Use `require` and `assert` from `testify` for clearer assertions
- Benchmark only when performance is critical; validate with real-world data

## Project-specific conventions (from .cursor/rules)

### Backend/Worker
- Claims must use `FOR UPDATE SKIP LOCKED`
- Job scopes must be protected by advisory locks (session-level)
- Connection discipline:
  - notifyconn: autocommit LISTEN wakeups
  - lockconn: long-lived advisory locks for running jobs
  - maintenance: short-lived connection for reapers (connect → reap → close)
- Determinism: Never regenerate jobitems plans on retry
- Fairness: Keep round-robin dispatch
- Logging: Logs are the UI; keep lines meaningful and operator-usable

### UI/Console
- Framework constraints: Keep HTMX + server-rendered partials; no SPA frameworks
- No progress bars by default; logs are the progress surface
- Console behavior: Preserve attach modes (STARTED vs ATTACHED / quiet attach)
- Preserve WebSocket + NOTIFY-based streaming
- Styling: Single CSS file. Terminal palette, sharp edges. Minimal animation.

### SQL Migrations
- Safety: Migrations should be safe/idempotent where feasible
- Contracts: Changing jobtypes/states requires worker updates + docs updates
- Reaper functions: Keep SQL reaper functions simple; correctness relies on running them from a short-lived maintenance connection so locks drop on session end

## Verification before completion
- Run `go fmt ./backend/...` and `goimports -local github.com/pvnkmnk/netrunner -w ./backend/...`
- Run `go vet ./backend/...`
- Run relevant tests: `go test ./backend/... -run <testname>` or full suite
- Check for linting errors with available tools
- Verify changes align with architectural invariants in docs/
- Ensure new features are accessible via both CLI and MCP server