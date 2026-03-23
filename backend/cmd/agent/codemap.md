# backend/cmd/agent/

## Responsibility
MCP (Model Context Protocol) server entry point. Exposes NETRUNNER's core operations as 20+ tools over stdio for AI agent integration via the `mcp-go` library.

## Design
- Single `main.go` (~460 lines) registering tools on a `server.MCPServer`
- Tool registration: `s.AddTool(mcp.NewTool(name, opts...), handlerFunc)`
- Each handler delegates to `internal/agent` functions or directly to services
- Tools: probe_system, list_watchlists, create_watchlist, delete_watchlist, get_library_stats, list_jobs, retry_job, search_artists, monitor_artist, get_artist_status, and more

## Flow
1. `main()` → `config.Load()` → `database.Connect()` → initialize services
2. Create MCP server (`server.NewMCPServer("NetRunner Agent Interface", "1.0.0")`)
3. Register ~20 tools with handler closures capturing db/cfg/services
4. `server.ServeStdio(s)` — blocks, communicating over stdin/stdout

## Integration
- **Consumes**: `internal/agent`, `internal/services`, `internal/database`, `internal/config`
- **Consumed by**: MCP-compatible AI clients (Claude, OpenCode)
- **Transport**: stdio
