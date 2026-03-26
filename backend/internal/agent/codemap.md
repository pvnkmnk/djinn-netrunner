# backend/internal/agent/

## Responsibility
Agent-facing handler functions shared between MCP server and CLI. Transport-agnostic business logic for agent-initiated operations.

## Design
- Pure functions taking `*gorm.DB`, `*config.Config`, service instances as parameters
- No HTTP/Fiber/MCP dependencies — designed for both MCP and CLI consumption
- Key functions: `ProbeSystem()`, `SearchArtists()`, `GetWatchlists()`, `CreateWatchlist()`, `DeleteWatchlist()`, `GetLibraryStats()`, `ListJobs()`, `RetryJob()`, `MonitorArtist()`, `GetArtistStatus()`
- Returns typed structs, formatted by the caller

## Flow
1. Caller (MCP handler or CLI command) invokes function with DB + config
2. Function queries database via GORM or calls external services
3. Returns result struct or error
4. Caller formats response (MCP result or CLI output)

## Integration
- **Consumed by**: `cmd/agent` (MCP server), `cmd/cli` (Cobra CLI)
- **Consumes**: `internal/database`, `internal/services`, `internal/config`
- **Invariant**: Must remain transport-agnostic
