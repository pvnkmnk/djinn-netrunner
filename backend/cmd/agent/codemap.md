# backend/cmd/agent/

## Responsibility
Provides an MCP (Model Context Protocol) server that exposes NetRunner's core functionality as tools to AI agents. Serves over stdio, allowing agentic IDEs and LLMs to interact with the music acquisition pipeline.

## Design
- **Entry Point**: `main.go` - Creates an MCP server and registers ~20 tools
- **Tool Registration Pattern**: Uses `mcp-go` library's `server.NewMCPServer()` with `AddTool()` for each command
- **Tool Handler Pattern**: Each tool is a closure that receives `mcp.CallToolRequest` and returns `*mcp.CallToolResult`
- **Service Layer**: Delegates to `internal/agent` package functions for actual business logic
- **Dependency Injection**: Services created in `main()` and passed to handlers (WatchlistService, GonicClient)

## Flow
1. `main()` loads config and connects to database
2. Initializes services (WatchlistService, GonicClient, SpotifyAuthHandler)
3. Creates MCP server with name "NetRunner Agent Interface" v1.0.0
4. Registers 20 tools in sequence:
   - System: `probe_system`, `bootstrap`
   - Config: `read_config`, `update_config`, `register_webhook`
   - Watchlists: `list_watchlists`, `add_watchlist`, `sync_watchlist`
   - Jobs: `list_jobs`, `get_job_logs`, `cancel_job`, `retry_job`, `enqueue_acquisition`
   - Library: `search_library`, `list_libraries`, `add_library`, `scan_library`
   - Stats: `get_stats`, `list_quality_profiles`, `list_monitored_artists`
5. Each tool handler parses request params, calls `agent.*` functions, formats results
6. Calls `server.ServeStdio(s)` to listen on stdio for MCP requests

## Integration
- **Depends On**: `internal/config`, `internal/database`, `internal/api`, `internal/services`, `internal/agent`
- **External**: `mcp-go` library (MCP protocol), Spotify/Gonic/slskd APIs
- **What Uses It**: Agentic IDEs (Cursor, Claude Code), MCP clients
