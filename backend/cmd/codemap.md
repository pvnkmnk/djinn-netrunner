# backend/cmd/

## Responsibility
backend/cmd/ contains all executable entry points for the NetRunner system. Each subdirectory represents a distinct binary that can be built and run independently, sharing common internal libraries while providing different interfaces to the system's core functionality.

## Design

| Subdirectory | Binary Name | Purpose | Entry Point |
|--------------|-------------|---------|-------------|
| `agent/` | `netrunner-agent` | MCP server over stdio for AI agent integration | `backend/cmd/agent/main.go` |
| `cli/` | `netrunner-cli` | Cobra CLI for terminal/script access to all operations | `backend/cmd/cli/main.go` |
| `server/` | `netrunner-server` | HTTP server (Fiber + HTMX + WebSocket) on :8080 | `backend/cmd/server/main.go` |
| `worker/` | `netrunner-worker` | Background job processor (round-robin, 5 concurrent jobs) | `backend/cmd/worker/main.go` |
| `test_sqlite/` | `netrunner-test-sqlite` | SQLite test utility for validation | `backend/cmd/test_sqlite/main.go` |
| `migrate_sources/` | *(none)* | Empty directory reserved for future source migration tools | *(no main.go)* |

All executables follow the same initialization pattern:
1. Load configuration via `backend/internal/config`
2. Establish database connection via `backend/internal/database`
3. Initialize required services
4. Execute main functionality

## Flow
The build system uses standard Go tooling:
- Local development: `go run ./cmd/<subdir>` or `go build ./cmd/<subdir>`
- Docker multi-stage builds: Compiled in build stage, copied to runtime stage
- All binaries share the same version (from git tags) and build metadata

Integration testing validates that:
- All binaries compile successfully
- Configuration loading works for each entry point
- Database connections initialize correctly
- Core services can be instantiated

## Integration
Subcommands share these internal packages:
- `backend/internal/config` - Environment-based configuration loading
- `backend/internal/database` - GORM models, connection, migrations
- `backend/internal/services` - Core business logic (watchlists, acquisition, metadata)
- `backend/internal/agent` - Transport-agnostic functions used by MCP and CLI
- `backend/internal/api` - HTTP handlers and WebSocket management (server only)

The agent and CLI share the most code since both use the agent facade for MCP-compatible operations. The server shares service initialization but adds HTTP-specific layers. The worker shares job processing logic but implements its own orchestration loop. Test utilities share only database and config packages for validation purposes.