# backend/cmd/cli/

## Responsibility
Provides a command-line interface (CLI) for NetRunner operations. Exposes the same core functionality as the MCP server but through a traditional CLI with subcommands, allowing operators to interact with the system from terminals or scripts.

## Design
- **Entry Point**: `main.go` - Uses Cobra framework for CLI structure
- **Command Pattern**: `rootCmd` with `PersistentPreRun` that loads config/db for all subcommands
- **Output Modes**: Supports `--json` flag for structured output vs human-readable text
- **Service Layer**: Delegates to `internal/agent` package functions for business logic
- **Structured Commands**:
  - `status` - System health check
  - `config` - List configuration (subcommand: `list`)
  - `watchlist` - Manage watchlists (subcommands: `list`, `add`, `sync`, `import`)
  - `library` - Manage libraries (subcommands: `list`, `add`, `scan`, `rm`)
  - `profile` - Manage quality profiles (subcommands: `list`, `add`, `rm`, `set-default`)
  - `stats` - View statistics (subcommands: `summary`, `jobs`, `library`)

## Flow
1. `main()` sets up rootCmd with `--json` flag
2. Adds subcommands via `AddCommand()`
3. Each subcommand has `PersistentPreRun` that loads config and database
4. Handler functions call `agent.*` functions (e.g., `agent.ProbeSystem`, `agent.ListWatchlists`)
5. Results printed in either JSON or formatted text based on `--json` flag
6. Errors handled via `handleError()` which outputs JSON or to stderr

## Integration
- **Depends On**: `internal/config`, `internal/database`, `internal/api`, `internal/services`, `internal/agent`, `github.com/spf13/cobra`
- **External**: Database (SQLite/Postgres)
- **What Uses It**: Terminal users, shell scripts, automation pipelines
